package manager

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

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
	var wg sync.WaitGroup
	wg.Add(len(jl))
	for _, j := range jl {
		c.newTask(ctx, wg, j)
	}
	wg.Wait()
	return err
}

func (c *Coor) newTask(ctx context.Context, wg sync.WaitGroup, job *models.GithubWorkflowJob) {
	defer wg.Done()
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
