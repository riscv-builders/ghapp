package coordinator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/riscv-builders/ghapp/models"
	"github.com/uptrace/bun"
)

type stage struct {
	status   models.RunnerStatus
	action   func(ctx context.Context, r *models.Runner)
	d        time.Duration
	parallam bool
}

func (c *Coor) runnerStage(ctx context.Context, s stage) (count int, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.d)
	defer cancel()
	slog.Debug("runner stage", "name", s.status)
	now := time.Now()
	count, err = c.db.NewSelect().Model((*models.Runner)(nil)).
		Where("status = ?", s.status).
		Where("queued_at < ?", now).
		Count(ctx)

	slog.Debug("runner stage", "name", s.status, "count", count, "err", err)
	if err != nil || count == 0 {
		return count, err
	}

	const maxRunner = 5

	rl := []*models.Runner{}
	err = c.db.NewSelect().Model(&rl).
		Where("status = ?", s.status).
		Where("queued_at < ?", now).
		Order("queued_at ASC").
		Limit(min(maxRunner, count)).Scan(ctx, &rl)

	if err != nil {
		return count, err
	}

	if s.parallam {
		var wg sync.WaitGroup
		wg.Add(len(rl))
		for _, r := range rl {
			go func(r *models.Runner) {
				defer wg.Done()
				s.action(ctx, r)
			}(r)
		}
		wg.Wait()
	} else {
		for _, r := range rl {
			s.action(ctx, r)
		}
	}

	return count, err
}

func (c *Coor) findAvailableBuilder(ctx context.Context, r *models.Runner) {
	bdr, err := c.findBuilder(ctx, nil)
	if err != nil {
		slog.Error("find available builder error", "err", err)
		return
	}
	if bdr == nil || bdr.ID == 0 {
		slog.Debug("no available builder")
		// re queue
		r.QueuedAt = time.Now().Add(3 * time.Minute)
		c.db.NewUpdate().Model(r).WherePK().Column("queued_at").Exec(ctx)
		return
	}

	err = c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		r.BuilderID = bdr.ID
		r.Labels = append([]string{"riscv-builders"}, bdr.Labels...)
		r.Name = fmt.Sprintf("riscv-builder-%s", bdr.Name)
		r.Status = models.RunnerFoundBuilder
		_, err := tx.NewUpdate().Model(r).WherePK().
			Column("builder_id", "labels", "name", "status").Exec(ctx)
		if err != nil {
			return err
		}
		bdr.Status = models.BuilderLocked
		bdr.FailedCount = 0
		r, err := tx.NewUpdate().Model(bdr).
			Column("status", "failed_count").
			WherePK().Where("status = ?", models.BuilderIdle).Exec(ctx)
		if err != nil {
			return err
		}
		affected, err := r.RowsAffected()
		if err != nil || affected == 0 {
			return fmt.Errorf("err=%v affected = 0 ", err)
		}
		return nil
	})
	if err != nil {
		slog.Error("find builder failed", "err", err)
	}
	return
}

func (c *Coor) resetBuilderID(ctx context.Context, r *models.Runner) {
	r.BuilderID = 0
	r.Status = models.RunnerScheduled
	_, err := c.db.NewUpdate().Model(r).WherePK().Column("builder_id", "status").Exec(ctx)
	if err != nil {
		slog.Warn("reset builder error", "err", err)
	}
	return
}

func (c *Coor) prepareBuilder(ctx context.Context, r *models.Runner) {

	if r.Status != models.RunnerFoundBuilder {
		slog.Warn("prepare invalid builder", "runner_id", r.ID)
		return
	}

	bdr := &models.Builder{}
	_, err := c.db.NewSelect().Model(bdr).
		Where("id = ? AND status = ?", r.BuilderID, models.BuilderLocked).Exec(ctx)
	if err != nil {
		slog.Error("prepare builder: can't find builder", "err", err)
		c.resetBuilderID(ctx, r)
		return
	}

	switch bdr.Type {
	case models.BuilderSSH:
		err = c.prepareSSHBuilder(ctx, bdr)
	default:
		slog.Error("unsupported builder", "type", bdr.Type)
	}
	if err != nil {
		slog.Error("prepare builder failed", "err", err)
		c.resetBuilderID(ctx, r)
		return
	}

	c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		r.Status = models.RunnerBuilderReady
		_, err := tx.NewUpdate().Model(r).WherePK().
			Column("status").Exec(ctx)
		return err
	})
	return
}

func (c *Coor) serveRunner(pctx context.Context) (err error) {
	slog.Debug("serve runner")
	const maxJob = 5
	stages := []stage{
		{models.RunnerScheduled, c.findAvailableBuilder, 10 * time.Second, false},
		{models.RunnerFoundBuilder, c.prepareBuilder, 10 * time.Second, false},
		{models.RunnerBuilderReady, c.runForest, 10 * time.Second, true},
	}
	for {
		for _, s := range stages {
			count, err := c.runnerStage(pctx, s)
			if err != nil {
				slog.Warn("serve runner failed", "stage status", s.status, "err", err)
			}
			if count < maxJob {
				time.Sleep(time.Second)
			}
		}
	}
	return nil
}

func (c *Coor) runForest(pctx context.Context, r *models.Runner) {

	nr := &models.Runner{}
	err := c.db.NewSelect().Model(nr).
		Relation("Job").
		Relation("Builder").
		Where("runner.id = ?", r.ID).Scan(pctx)
	if err != nil {
		c.resetBuilderID(pctx, r)
		return
	}

	// run in goroutine
	go func(r *models.Runner) {
		// default of github job executime limit is 6h
		// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idtimeout-minutes
		// we can change by config JOB_EXEC_TIME_LIMIT
		ctx, cancel := context.WithTimeout(context.Background(), c.cfg.JobExecTimeLimit)
		defer cancel()
		// make sure we don't get call again
		r.Status = models.RunnerInProgress
		_, err = c.db.NewUpdate().Model(r).WherePK().
			Column("status").Exec(ctx)
		if err != nil {
			return
		}

		defer c.releaseBuilder(ctx, r)
		defer c.finalizeJob(ctx, r)

		err := c.doRunner(ctx, r)
		r.Status = models.RunnerFailed
		switch err {
		case nil:
			r.Status = models.RunnerCompleted
		case context.DeadlineExceeded:
			r.Status = models.RunnerTimeout
		default:
			slog.Error("runner failed", "err", err)
		}

		sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		c.db.NewUpdate().Model(r).WherePK().Column("status").Exec(sctx)
	}(nr)
}

func (c *Coor) finalizeJob(ctx context.Context, r *models.Runner) {
	// should not happen, but...
	if r.JobID == 0 {
		return
	}

	var js models.GithubWorkflowJobStatus
	switch r.Status {
	case models.RunnerCompleted:
		js = models.WorkflowJobCompleted
	case models.RunnerTimeout:
		js = models.WorkflowJobTimeout
	case models.RunnerFailed:
		js = models.WorkflowJobFailed
	default:
		return
	}

	c.db.NewUpdate().Model((*models.GithubWorkflowJob)(nil)).
		Where("id = ? AND status = ?", r.JobID, models.WorkflowJobInProgress).
		Set("status = ?", js).
		Set("updated_at = ?", time.Now()).Exec(ctx)
	return
}

func (c *Coor) releaseBuilder(ctx context.Context, r *models.Runner) {
	if r.BuilderID == 0 {
		return
	}

	// try our best to release builder
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*models.Builder)(nil)).
			Where("id = ? AND status = ?", r.BuilderID, models.BuilderLocked).
			Set("status = ?", models.BuilderIdle).Exec(ctx)
		if err != nil {
			return err
		}

		_, err = tx.NewUpdate().Model(r).
			Set("builder_id = ?", 0).WherePK().Exec(ctx)
		return err
	})
	return
}

func (c *Coor) doRunner(ctx context.Context, r *models.Runner) (err error) {

	if r.Builder == nil || r.BuilderID == 0 {
		return fmt.Errorf("invalid builder for runner", "builder", r.BuilderID, "runner", r.ID)
	}

	token, _, err := c.getActionRegistrationToken(ctx,
		r.Job.InstallationID,
		r.Job.Owner, r.Job.RepoName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(c.cfg.JobExecTimeLimit))
	defer cancel()

	cmd := []string{"github-act-runner", "configure",
		"--url", r.URL,
		"--token", token,
		"--name", r.Name,
		"--no-default-labels",
		"--system-labels", strings.Join(r.SystemLabels, ","),
		"--labels", strings.Join(r.Labels, ","),
	}

	if r.Ephemeral {
		cmd = append(cmd, "--ephemeral")
	}

	switch r.Builder.Type {
	case models.BuilderSSH:
		cli, err := c.getSSHClient(ctx, r.Builder)
		if err != nil {
			slog.Debug("runner ssh client", "err", err)
			return err
		}
		defer cli.Close()

		session, err := cli.NewSession()
		if err != nil {
			slog.Debug("runner ssh session", "err", err)
			return err
		}

		p, err := session.Output(strings.Join(cmd, " "))
		slog.Debug("runner session configure", "runner_id", r.ID, "msg", string(p))
		if err != nil {
			if !strings.Contains(string(p), "runner already configured.") {
				return err
			}
		}
		session.Close()

		session, err = cli.NewSession()
		if err != nil {
			slog.Debug("runner ssh session", "err", err)
			return err
		}
		defer session.Close()

		slog.Debug("runner start running", "builder_name", r.Builder.Name, "runner_id", r.ID)
		p, err = session.Output("github-act-runner run --once")
		slog.Debug("runner session run", "runner_id", r.ID, "msg", string(p))
		return err
	default:
		return fmt.Errorf("unsupported runner %#v for job:%d", r.Builder, r.JobID)
	}
	return nil
}
