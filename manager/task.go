package manager

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
	status   models.TaskStatus
	action   func(ctx context.Context, r *models.Task)
	d        time.Duration
	parallam bool
}

func (c *Coor) taskStage(ctx context.Context, s stage) (count int, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.d)
	defer cancel()
	slog.Debug("task stage", "name", s.status)
	now := time.Now()
	count, err = c.db.NewSelect().Model((*models.Task)(nil)).
		Where("status = ?", s.status).
		Where("queued_at < ?", now).
		Count(ctx)

	slog.Debug("task stage", "name", s.status, "count", count, "err", err)
	if err != nil || count == 0 {
		return count, err
	}

	const maxConcurrentTask = 5

	rl := []*models.Task{}
	err = c.db.NewSelect().Model(&rl).
		Where("status = ?", s.status).
		Where("queued_at < ?", now).
		Order("queued_at ASC").
		Limit(min(maxConcurrentTask, count)).Scan(ctx, &rl)

	if err != nil {
		return count, err
	}

	if s.parallam {
		var wg sync.WaitGroup
		wg.Add(len(rl))
		for _, r := range rl {
			go func(r *models.Task) {
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

func (c *Coor) findAvailableBuilder(ctx context.Context, r *models.Task) {
	bdr, err := c.findBuilder(ctx, nil)
	if err != nil {
		slog.Error("find available builder error", "err", err)
		return
	}
	if bdr == nil || bdr.ID == 0 {
		slog.Debug("no available builder")
		// re queue
		r.QueuedAt = time.Now().Add(3 * time.Minute)
		c.db.NewUpdate().Model(r).WherePK().Column("queued_at", "updated_at").Exec(ctx)
		return
	}

	err = c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		r.BuilderID = bdr.ID
		r.Labels = append([]string{"riscv-builders"}, bdr.Labels...)
		r.Name = fmt.Sprintf("riscv-builder-%s", bdr.Name)
		r.Status = models.TaskFoundBuilder
		_, err := tx.NewUpdate().Model(r).WherePK().
			Column("builder_id", "labels", "name", "status", "updated_at").Exec(ctx)
		if err != nil {
			return err
		}
		bdr.Status = models.BuilderLocked
		bdr.FailedCount = 0
		r, err := tx.NewUpdate().Model(bdr).
			Column("status", "failed_count", "updated_at").
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

func (c *Coor) resetBuilderID(ctx context.Context, r *models.Task) {
	r.BuilderID = 0
	r.Status = models.TaskScheduled
	_, err := c.db.NewUpdate().Model(r).WherePK().
		Column("builder_id", "status", "updated_at").Exec(ctx)
	if err != nil {
		slog.Warn("reset builder error", "err", err)
	}
	return
}

func (c *Coor) prepareBuilder(ctx context.Context, r *models.Task) {

	if r.Status != models.TaskFoundBuilder {
		slog.Warn("prepare invalid builder", "task_id", r.ID)
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
	case models.BuilderPodman:
		err = c.preparePodmanBuilder(ctx, bdr)
	default:
		slog.Error("unsupported builder", "type", bdr.Type)
	}
	if err != nil {
		slog.Error("prepare builder failed", "err", err)
		c.tryQuarantineBuilder(ctx, r.BuilderID)
		c.resetBuilderID(ctx, r)
		return
	}

	r.Status = models.TaskBuilderReady
	_, err = c.db.NewUpdate().Model(r).WherePK().
		Column("status", "updated_at").Exec(ctx)
	return
}

func (c *Coor) updateTaskStatus(ctx context.Context, t *models.Task, status models.TaskStatus) {
	t.Status = status
	c.db.NewUpdate().Model(t).WherePK().
		Column("status", "updated_at", "queued_at").Exec(ctx)
	return
}

func (c *Coor) serveInProgress(ctx context.Context, t *models.Task) {

	// TODO check completed task
	// TODO check deadline task
	// TODO release all available builders

	if t.JobID == 0 {
		slog.Warn("in_progress task no job id, change to failed", "task_id", t.ID)
		c.updateTaskStatus(ctx, t, models.TaskFailed)
		return
	}

	if time.Now().After(t.DeadLine) {
		//XXX: This might double exec
		slog.Warn("task timeouted", "task_id", t.ID)
		c.releaseBuilder(ctx, t)
		c.updateTaskStatus(ctx, t, models.TaskTimeout)
		return
	}

	j := &models.GithubWorkflowJob{ID: t.JobID}
	_, err := c.db.NewSelect().Model(j).Where("id = ?", t.JobID).Limit(1).Exec(ctx, j)
	if err != nil {
		slog.Error("in progress find failed", "err", err, "task_id", t.ID)
		return
	}

	switch j.Status {
	case models.WorkflowJobQueued, models.WorkflowJobScheduled, models.WorkflowJobInProgress:
		t.QueuedAt = time.Now().Add(time.Minute)
		c.db.NewUpdate().Model(t).WherePK().
			Column("queued_at").Exec(ctx)
	case models.WorkflowJobCompleted:
		if t.BuilderID != 0 {
			c.releaseBuilder(ctx, t)
		}
		c.updateTaskStatus(ctx, t, models.TaskCompleted)
	default:
	}
	return
}

func (c *Coor) serveTask(pctx context.Context) (err error) {
	slog.Debug("serve task")
	const maxJob = 5
	stages := []stage{
		{models.TaskScheduled, c.findAvailableBuilder, 10 * time.Second, false},
		{models.TaskFoundBuilder, c.prepareBuilder, 10 * time.Second, false},
		{models.TaskBuilderReady, c.runForest, 10 * time.Second, true},
		{models.TaskInProgress, c.serveInProgress, 10 * time.Second, true},
	}
	for {
		for _, s := range stages {
			count, err := c.taskStage(pctx, s)
			if err != nil {
				slog.Warn("serve task failed", "stage status", s.status, "err", err)
			}
			if count < maxJob {
				time.Sleep(time.Second)
			}
		}
	}
	return nil
}

func (c *Coor) runForest(pctx context.Context, r *models.Task) {

	nr := &models.Task{}
	err := c.db.NewSelect().Model(nr).
		Relation("Job").
		Relation("Builder").
		Where("task.id = ?", r.ID).Scan(pctx)
	if err != nil {
		c.resetBuilderID(pctx, r)
		return
	}

	// run in goroutine
	go func(r *models.Task) {
		// default of github job executime limit is 6h
		// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idtimeout-minutes
		// we can change by config JOB_EXEC_TIME_LIMIT
		ctx, cancel := context.WithTimeout(context.Background(), c.cfg.JobExecTimeLimit)
		defer cancel()
		// make sure we don't get call again
		r.Status = models.TaskInProgress
		r.QueuedAt = time.Now().Add(time.Minute)
		_, err = c.db.NewUpdate().Model(r).WherePK().
			Column("status", "updated_at", "queued_at").Exec(ctx)
		if err != nil {
			return
		}

		defer c.releaseBuilder(ctx, r)

		err := c.doTask(ctx, r)
		switch err {
		case containerCreated:
			return
		case nil:
			r.Status = models.TaskCompleted
		case context.DeadlineExceeded:
			r.Status = models.TaskTimeout
		default:
			r.Status = models.TaskFailed
			slog.Error("task failed", "err", err)
		}

		sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		c.db.NewUpdate().Model(r).WherePK().Column("status", "updated_at").Exec(sctx)
	}(nr)
}

func (c *Coor) releaseBuilder(ctx context.Context, r *models.Task) {
	if r.BuilderID == 0 {
		return
	}

	// try our best to release builder
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*models.Builder)(nil)).
			Where("id = ? AND status = ?", r.BuilderID, models.BuilderLocked).
			Set("status = ?", models.BuilderIdle).
			Set("updated_at = ?", time.Now()).
			Exec(ctx)
		if err != nil {
			return err
		}
		r.BuilderID = 0
		_, err = tx.NewUpdate().Model(r).
			Set("updated_at = ?", time.Now()).
			Set("builder_id = ?", 0).WherePK().Exec(ctx)
		return err
	})
	return
}

func (c *Coor) doTask(ctx context.Context, r *models.Task) (err error) {

	if r.Builder == nil || r.BuilderID == 0 {
		return fmt.Errorf("invalid builder for task", "builder", r.BuilderID, "task", r.ID)
	}

	token, _, err := c.getActionRegistrationToken(ctx,
		r.Job.InstallationID,
		r.Job.Owner, r.Job.RepoName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(c.cfg.JobExecTimeLimit))
	defer cancel()

	switch r.Builder.Type {
	case models.BuilderSSH:
		configCmd := []string{"github-act-runner", "configure",
			"--url", r.URL,
			"--token", token,
			"--name", r.Name,
			"--no-default-labels",
			"--system-labels", strings.Join(r.SystemLabels, ","),
			"--labels", strings.Join(r.Labels, ","),
		}

		if r.Ephemeral {
			configCmd = append(configCmd, "--ephemeral")
		}
		return c.doSSHBuilder(ctx, r, configCmd)
	case models.BuilderPodman:
		cmd := []string{"run-once",
			"--url", r.URL,
			"--token", token,
			"--name", r.Name,
			"--no-default-labels",
			"--system-labels", strings.Join(r.SystemLabels, ","),
			"--labels", strings.Join(r.Labels, ","),
			"--ephemeral",
		}
		return c.doPodmanBuilder(ctx, r, cmd)
	default:
		return fmt.Errorf("unsupported runner %#v for job:%d", r.Builder, r.JobID)
	}
	return nil
}
