package models

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type TaskStatus string

const (
	TaskScheduled    TaskStatus = "scheduled"
	TaskFoundBuilder            = "found_builder"
	TaskBuilderReady            = "builder_ready"
	TaskInProgress              = "in_progress"
	TaskCompleted               = "completed"
	TaskTimeout                 = "timeout"
	TaskFailed                  = "failed"
)

type Task struct {
	ID        int64              `bun:",pk,autoincrement" json:"id"`
	Builder   *Builder           `bun:"rel:belongs-to,join:builder_id=id" json:"-"`
	BuilderID int64              `bun:"builder_id" json:"builder_id"`
	Job       *GithubWorkflowJob `bun:"rel:belongs-to,join:job_id=id" json:"-"`
	JobID     int64              `bun:"job_id,unique" json:"job_id"`

	Name         string
	Labels       []string
	SystemLabels []string
	URL          string `bun:",type:text"`
	Ephemeral    bool
	Status       TaskStatus

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
	QueuedAt  time.Time `bun:queued_at,nullzero,notnull,default:"current_timestamp"`
	DeadLine  time.Time `bun:"deadline,nullzero,notnull" json:"deadline"`
}

var _ bun.AfterCreateTableHook = (*Task)(nil)

func (*Task) AfterCreateTable(ctx context.Context, query *bun.CreateTableQuery) error {
	_, err := query.DB().NewCreateIndex().
		Model((*Task)(nil)).
		Index("tasks_status_idx").
		Column("status").Exec(ctx)
	if err != nil {
		return err
	}

	_, err = query.DB().NewCreateIndex().
		Model((*Task)(nil)).
		Index("tasks_queued_at_idx").
		Column("queued_at").Exec(ctx)
	return err
}

var _ bun.BeforeAppendModelHook = (*Task)(nil)

func (g *Task) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	switch query.(type) {
	case *bun.InsertQuery:
		g.CreatedAt = time.Now()
		g.UpdatedAt = time.Now()
	case *bun.UpdateQuery:
		g.UpdatedAt = time.Now()
	}
	return nil
}
