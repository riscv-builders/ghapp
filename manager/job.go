package manager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riscv-builders/ghapp/models"
	"github.com/uptrace/bun"
)

func (c *Coor) serveAvailableJob(ctx context.Context) {
	const maxJob = 10
	for {
		start := time.Now()
		count, err := c.findAvailableJob(ctx)
		if err != nil {
			slog.Warn("find queued job failed", "err", err)
			continue
		}
		since := time.Since(start)
		if count < maxJob || since < 10*time.Second {
			time.Sleep(10*time.Second - since)
		}
	}
}

func (c *Coor) findAvailableJob(ctx context.Context) (int, error) {
	passiveDuration := 30 * time.Second
	ctx, cancel := context.WithTimeout(ctx, passiveDuration)
	defer cancel()

	count, err := c.db.NewSelect().
		Model((*models.GithubWorkflowJob)(nil)).
		Where("status = ?", models.WorkflowJobQueued).Count(ctx)

	if err != nil || count == 0 {
		return count, err
	}

	jl := []*models.GithubWorkflowJob{}
	err = c.db.NewSelect().Model(&jl).
		Where("status = ?", models.WorkflowJobQueued).Limit(10).
		Order("id ASC").Scan(ctx, &jl)

	if err != nil {
		return count, err
	}
	for _, j := range jl {
		c.newTask(ctx, j)
	}
	return count, err
}

func (c *Coor) newTask(ctx context.Context, job *models.GithubWorkflowJob) {
	if job.Status != models.WorkflowJobQueued {
		slog.Warn("job not queued", "id", job.ID, "status", job.Status)
		return
	}

	c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) (err error) {
		job.Status = models.WorkflowJobScheduled
		_, err = tx.NewUpdate().Model(job).WherePK().Column("status", "updated_at").Exec(ctx)
		if err != nil {
			return
		}

		task := &models.Task{
			Job:   job,
			JobID: job.ID,
			// Name:         fmt.Sprintf("riscv-builder-%s", bdr.Name),
			// Labels:       append([]string{"riscv-builers"}, bdr.Labels...),
			SystemLabels: []string{"riscv64", "riscv", "linux"},
			URL:          fmt.Sprintf("https://github.com/%s/%s", job.Owner, job.RepoName),
			Ephemeral:    true,
			Status:       models.TaskScheduled,
			QueuedAt:     time.Now(),
			DeadLine:     time.Now().Add(35 * 24 * time.Hour), // Github has 35 days limitation
		}
		_, err = tx.NewInsert().Model(task).Ignore().Exec(ctx)
		return
	})
	return
}
