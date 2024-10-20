package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/google/go-github/v62/github"
	"github.com/riscv-builders/ghapp/models"
	"github.com/uptrace/bun"
)

func (c *Coor) findTasks(ctx context.Context, status models.TaskStatus, limit int) (tasks []*models.Task, err error) {
	if limit < 1 {
		limit = 1
	}
	tasks = make([]*models.Task, 0, limit)
	err = c.db.NewSelect().Model(&tasks).
		Where("status = ?", status).
		Where("queued_at < ?", time.Now()).
		Order("queued_at ASC").
		Limit(limit).Scan(ctx, &tasks)
	return
}

func (c *Coor) moveToBack(t *models.Task, d time.Duration) {
	// re queue
	if d == 0 {
		d = time.Minute
	}
	t.QueuedAt = time.Now().Add(d)
	t.UpdatedAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	result, err := c.db.NewUpdate().Model(t).WherePK().
		Column("queued_at", "updated_at").Exec(ctx)
	if err != nil {
		slog.Error("move to back failed", "err", err)
		return
	}

	af, err := result.RowsAffected()
	if err != nil || af == 0 {
		slog.Error("move to back failed", "err", err, "affect_row", af)
	}
	return
}

func (c *Coor) doPendingTask(ctx context.Context) (err error) {

	tasks, err := c.findTasks(ctx, models.TaskPending, 10)
	slog.Debug("doPendingTasks", "status", models.TaskPending, "tasks", len(tasks), "err", err)
	if err != nil || len(tasks) == 0 {
		return
	}

	eg := []error{}
	for _, t := range tasks {
		eg = append(eg, c.findAvailableBuilder(ctx, t))
	}
	return errors.Join(eg...)
}

func trimLabels(l []string) []string {
	j := len(l) - 1
	for i := range l {
		if l[i] == "riscv-builders" {
			l[i] = l[j]
			j--
		}
	}
	return l[:j+1]
}

func (c *Coor) findAvailableBuilder(ctx context.Context, r *models.Task) (err error) {
	l := make([]string, len(r.Labels))
	copy(l, r.Labels)
	bdr, err := c.findBuilder(ctx, trimLabels(l))
	if err != nil {
		slog.Error("find available builder error", "err", err)
		return
	}

	if bdr == nil || bdr.ID == 0 {
		slog.Debug("no available builder")
		c.moveToBack(r, time.Minute+time.Second*time.Duration(rand.Int63n(10)))
		return
	}

	return c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		r.BuilderID = bdr.ID
		r.Name = fmt.Sprintf("riscv-builder-%s", bdr.Name)
		r.Status = models.TaskBuilderAssigned
		_, err := tx.NewUpdate().Model(r).WherePK().
			Column("builder_id", "name", "status", "updated_at").Exec(ctx)
		if err != nil {
			return err
		}
		bdr.Status = models.BuilderLocked
		bdr.FailedCount = 0
		bdr.TaskID = r.ID
		r, err := tx.NewUpdate().Model(bdr).
			Column("status", "failed_count", "updated_at", "task_id").
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
}

func (c *Coor) waitForAsync(ctx context.Context, tasks []*models.Task, f func(context.Context, *models.Task)) {

	ch := make(chan struct{})
	defer close(ch)

	for _, t := range tasks {
		go func(t *models.Task) {
			f(ctx, t)
			ch <- struct{}{}
		}(t)
	}

	for range tasks {
		select {
		case <-ch:
		case <-ctx.Done():
			break
		}
	}
}

func (c *Coor) prepareBuilder(ctx context.Context, r *models.Task) (err error) {

	if r.BuilderID == 0 {
		return fmt.Errorf("prepare invalid, without builder id")
	}
	bdr := r.Builder

	err = c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		r.Status = models.TaskBuilderPreparing
		r.QueuedAt = time.Now().Add(time.Second * 30)
		_, err := tx.NewUpdate().Model(r).WherePK().
			Column("status", "updated_at", "queued_at").Exec(ctx)
		if err != nil {
			return err
		}
		bdr.Status = models.BuilderPreparing
		_, err = tx.NewUpdate().Model(bdr).WherePK().
			Column("status", "updated_at").Exec(ctx)
		return err
	})

	if err != nil {
		return
	}

	switch bdr.Type {
	case models.BuilderSSH:
		err = c.prepareSSHBuilder(ctx, bdr)
	case models.BuilderPodman:
		err = c.preparePodmanBuilder(ctx, bdr, r)
	default:
		err = fmt.Errorf("unsupported builder :%s", bdr.Type)
	}

	if err != nil {
		c.tryQuarantineBuilder(r.BuilderID)
	}
	return
}

func (c *Coor) updateTaskStatus(ctx context.Context, t *models.Task, status models.TaskStatus) {
	t.Status = status
	c.db.NewUpdate().Model(t).WherePK().
		Column("status", "updated_at", "queued_at").Exec(ctx)
	return
}

func (c *Coor) startBuilder(ctx context.Context, t *models.Task) (err error) {

	err = c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		t.Status = models.TaskInProgress
		t.QueuedAt = time.Now().Add(time.Second * 10)
		_, err := c.db.NewUpdate().Model(t).WherePK().
			Column("status", "updated_at", "queued_at").Exec(ctx)
		bdr := t.Builder
		bdr.Status = models.BuilderWorking
		_, err = c.db.NewUpdate().Model(bdr).WherePK().
			Column("status", "updated_at").Exec(ctx)
		return err
	})

	if err != nil {
		return
	}

	// default of github job executime limit is 6h
	// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#jobsjob_idtimeout-minutes
	// make sure we don't get call again
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	err = c.doTask(ctx, t)
	switch err.(type) {
	case nil:
		t.Status = models.TaskCompleted
	case *github.RateLimitError:
		d := 10 * time.Minute
		rerr, ok := err.(*github.RateLimitError)
		if ok {
			d = rerr.Rate.Reset.Sub(time.Now())
		}
		// re pending
		t.Status = models.TaskPending
		t.QueuedAt = time.Now().Add(d)
	default:
		t.Status = models.TaskFailed
		slog.Error("task failed", "err", err)
	}
	c.db.NewUpdate().Model(t).WherePK().Column("status", "queued_at", "updated_at").Exec(ctx)
	return err
}

func (c *Coor) releaseBuilder(r *models.Task) {
	if r.BuilderID == 0 {
		return
	}

	// try our best to release builder
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*models.Builder)(nil)).
			Where("id = ? AND status IN (?)", r.BuilderID,
				bun.In([]models.BuilderStatus{
					models.BuilderLocked,
					models.BuilderPreparing,
					models.BuilderWorking})).
			Set("status = ?", models.BuilderIdle).
			Set("updated_at = ?", time.Now()).
			Set("task_id = NULL").
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
		c.releaseBuilder(t)
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
		c.moveToBack(t, time.Minute)
	case models.WorkflowJobCompleted:
		if t.BuilderID != 0 {
			c.releaseBuilder(t)
		}
		c.updateTaskStatus(ctx, t, models.TaskCompleted)
	default:
		slog.Error("in progress invalid job status", "status", j.Status, "task_id", t.ID)
	}
	return
}
