package manager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/pkg/errors"
	"github.com/riscv-builders/ghapp/models"
	"github.com/uptrace/bun"
)

func (c *Coor) findAvailableJob(ctx context.Context) error {

	jl := []*models.GithubWorkflowJob{}
	err := c.db.NewSelect().Model(&jl).
		Where("status = ?", models.WorkflowJobQueued).Limit(10).
		Order("id ASC").Scan(ctx, &jl)

	if err != nil || len(jl) == 0 {
		return err
	}
	for _, j := range jl {
		c.newTask(ctx, j)
	}
	return nil
}

func (c *Coor) newTask(ctx context.Context, job *models.GithubWorkflowJob) {
	slog.Debug("new task", "job_id", job.ID)
	if job.Status != models.WorkflowJobQueued {
		slog.Warn("job not queued", "id", job.ID, "status", job.Status)
		return
	}

	err := c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) (err error) {
		job.Status = models.WorkflowJobScheduled
		row, err := tx.NewUpdate().Model(job).WherePK().
			Where("status = ?", models.WorkflowJobQueued).
			Column("status", "updated_at").Exec(ctx)
		if err != nil {
			return
		}

		a, err := row.RowsAffected()
		if err != nil || a != 1 {
			return errors.Wrap(err, "affected row 0")
		}

		task := &models.Task{
			Job:   job,
			JobID: job.ID,
			// Name:         fmt.Sprintf("riscv-builder-%s", bdr.Name),
			// Labels:       append([]string{"riscv-builers"}, bdr.Labels...),
			SystemLabels: []string{"riscv64", "riscv", "linux"},
			URL:          fmt.Sprintf("https://github.com/%s/%s", job.Owner, job.RepoName),
			Ephemeral:    true,
			Status:       models.TaskPending,
			QueuedAt:     time.Now(),
			DeadLine:     time.Now().Add(35 * 24 * time.Hour), // Github has 35 days limitation
		}
		_, err = tx.NewInsert().Model(task).Ignore().Exec(ctx)
		return
	})
	if err != nil {
		slog.Error("new task failed", "err", err)
	}
	return
}
