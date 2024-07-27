package coordinator

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/riscv-builders/service/models"
)

func (c *Coor) serveRunner(ctx context.Context) {
	cleanup := time.NewTicker(time.Hour)
	passive := time.NewTicker(10 * time.Second)
	slog.Debug("serve runner")
	for {
		select {
		case <-passive.C:
			rl := []models.Runner{}
			err := c.db.NewSelect().Model(&rl).
				Column("runner.*").
				Relation("Builder").
				Relation("Job").
				Where("runner.status = ?", models.RunnerScheduled).Limit(5).Scan(ctx, &rl)
			switch err {
			case sql.ErrNoRows:
				continue
			case nil:
				for _, r := range rl {
					go c.run(ctx, &r)
				}
			default:
				slog.Error("passive", "err", err)
				continue
			}

		case ru := <-c.runner:
			go c.run(ctx, ru)
		case <-cleanup.C:
			//c.doRunnerHouseKeeping(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (c *Coor) run(ctx context.Context, r *models.Runner) {
	defer c.cleanUpRunner(r)
	ctx, cancel := context.WithCancel(ctx)
	c.runnerCm.Store(r.ID, cancel)
	err := c.doRunner(ctx, r)
	if err == nil {
		// if it's normal, it will update status itself
		return
	}
	r.Status = models.RunnerFailed
	slog.Error("runner failed", "err", err)
	sctx, scancel := context.WithTimeout(ctx, time.Minute)
	defer scancel()
	_, err = c.db.NewUpdate().Model(r).WherePK().Column("status").Exec(sctx)
	if err != nil {
		slog.Error("runner status failed", "err", err)
	}
}

func (c *Coor) cleanUpRunner(r *models.Runner) {
	cancel, loaded := c.runnerCm.LoadAndDelete(r.ID)
	if loaded {
		if f, ok := cancel.(func()); ok {
			f()
		}
	}
	c.releaseRunner(r)
}

func (c *Coor) releaseRunner(r *models.Runner) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	r.Builder.Status = models.BuilderIdle
	c.db.NewUpdate().Model(r.Builder).
		Where("id = ? AND status = ?", r.BuilderID, models.BuilderWorking).
		Column("status").Exec(ctx)

	r.Job.Status = models.WorkflowJobCompleted
	c.db.NewUpdate().Model(r.Job).
		Where("id = ? AND status = ?", r.JobID, models.WorkflowJobInProgress).
		Column("status").Exec(ctx)
}

func (c *Coor) doRunner(ctx context.Context, r *models.Runner) (err error) {
	if r.ExpiredAt.Before(time.Now()) {
		slog.Warn("runner has expired", "id", r.ID)
		return
	}
	ctx, cancel := context.WithDeadline(ctx, r.ExpiredAt)
	defer cancel()
	r.Status = models.RunnerInProgress
	c.db.NewUpdate().Model(r).WherePK().Column("status").Exec(ctx)

	cmd := []string{"github-act-runner", "configure",
		"--url", r.URL,
		"--token", r.RegToken,
		"--name", fmt.Sprintf("riscv-builder-%s", r.Builder.Name),
		"--no-default-labels", "--system-labels", "riscv64,riscv,linux",
	}

	labels := []string{"riscv-builders"}
	if len(r.Builder.Labels) > 0 {
		labels = append(labels, r.Builder.Labels...)
	}
	cmd = append(cmd, "--labels", strings.Join(labels, ","))

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
		go func() {
			<-ctx.Done()
			cli.Close()
		}()

		session, err := cli.NewSession()
		if err != nil {
			slog.Debug("runner ssh session", "err", err)
			return err
		}
		defer session.Close()

		p, err := session.Output(strings.Join(cmd, " "))
		slog.Debug("runner session configure", "runner_id", r.ID, "msg", string(p))
		if err != nil {
			if !strings.Contains(string(p), "runner already configured.") {
				return err
			}
		}

		session, err = cli.NewSession()
		if err != nil {
			slog.Debug("runner ssh session", "err", err)
			return err
		}
		defer session.Close()

		slog.Debug("runner start running", "builder_name", r.Builder.Name, "runner_id", r.ID)
		p, err = session.Output("github-act-runner run --once")
		slog.Debug("runner session run", "runner_id", r.ID, "msg", string(p))
		if err != nil {
			return err
		}
		r.Status = models.RunnerCompleted
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		c.db.NewUpdate().Model(r).WherePK().Column("status").Exec(ctx)
	default:
		return fmt.Errorf("unsupported runner %s for job:%d", r.Builder.Type, r.JobID)
	}
	return nil
}
