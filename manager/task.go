package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/riscv-builders/ghapp/models"
	"github.com/uptrace/bun"
)

func (c *Coor) findTasks(ctx context.Context, status models.TaskStatus, limit int) (tasks []*models.Task, err error) {
	tasks = make([]*models.Task, 0, 10)
	err = c.db.NewSelect().Model(&tasks).
		Where("status = ?", status).
		Where("queued_at < ?", time.Now()).
		Order("queued_at ASC").
		Limit(10).Scan(ctx, &tasks)
	return
}

func (c *Coor) doScheduledTasks(ctx context.Context) (err error) {

	tasks, err := c.findTasks(ctx, models.TaskScheduled, 10)
	if err != nil || len(tasks) == 0 {
		return
	}

	eg := []error{}
	for _, t := range tasks {
		eg = append(eg, c.findAvailableBuilder(ctx, t))
	}
	return errors.Join(eg...)
}

func (c *Coor) findAvailableBuilder(ctx context.Context, r *models.Task) (err error) {
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

func (c *Coor) doFoundBuilder(ctx context.Context) (err error) {
	tasks, err := c.findTasks(ctx, models.TaskFoundBuilder, 10)
	if err != nil || len(tasks) == 0 {
		return
	}
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for _, t := range tasks {
		go c.prepareBuilder(ctx, wg, t)
	}
	wg.Wait()
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

func (c *Coor) prepareBuilder(ctx context.Context, wg sync.WaitGroup, r *models.Task) {
	defer wg.Done()

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

func (c *Coor) doBuilderReady(ctx context.Context) (err error) {
	tasks, err := c.findTasks(ctx, models.TaskBuilderReady, 10)
	if err != nil || len(tasks) == 0 {
		return
	}
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for _, t := range tasks {
		go c.startBuilder(ctx, wg, t)
	}
	wg.Wait()
	return nil
}

func (c *Coor) startBuilder(pctx context.Context, wg sync.WaitGroup, r *models.Task) {
	defer wg.Done()

	r = &models.Task{}
	err := c.db.NewSelect().Model(r).
		Relation("Job").
		Relation("Builder").
		Where("task.id = ?", r.ID).Scan(pctx)
	if err != nil {
		c.resetBuilderID(pctx, r)
		return
	}

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

	err = c.doTask(ctx, r)
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

func (c *Coor) doInProgress(ctx context.Context) (err error) {

	tasks, err := c.findTasks(ctx, models.TaskInProgress, 10)
	if err != nil || len(tasks) == 0 {
		return
	}

	for _, t := range tasks {
		go c.serveInProgress(ctx, t)
	}
	return nil
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
	case models.WorkflowJobQueued, models.WorkflowJobScheduled, models.WorkflowJobInProgress,
		models.WorkflowPending, models.WorkflowRequested, models.WorkflowWaiting:
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
