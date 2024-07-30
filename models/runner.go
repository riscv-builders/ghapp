package models

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type RunnerStatus string

const (
	RunnerScheduled    RunnerStatus = "scheduled"
	RunnerFoundBuilder              = "found_builder"
	RunnerBuilderReady              = "builder_ready"
	RunnerInProgress                = "in_progress"
	RunnerCompleted                 = "completed"
	RunnerTimeout                   = "timeout"
	RunnerFailed                    = "failed"
)

type Runner struct {
	ID        int64              `bun:",pk,autoincrement,default:1" json:"id"`
	Builder   *Builder           `bun:"rel:belongs-to,join:builder_id=id" json:"-"`
	BuilderID int64              `bun:"builder_id" json:"builder_id"`
	Job       *GithubWorkflowJob `bun:"rel:belongs-to,join:job_id=id" json:"-"`
	JobID     int64              `bun:"job_id" json:"job_id"`

	Name         string
	Labels       []string `bun:",type:text"`
	SystemLabels []string `bun:",type:text"`
	URL          string   `bun:",type:text"`
	Ephemeral    bool
	Status       RunnerStatus

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
	QueuedAt  time.Time `bun:queued_at,nullzero,notnull,default:"current_timestamp"`
	DeadLine  time.Time `bun:"deadline,nullzero,notnull" json:"deadline"`
}

var _ bun.AfterCreateTableHook = (*Runner)(nil)

func (*Runner) AfterCreateTable(ctx context.Context, query *bun.CreateTableQuery) error {
	_, err := query.DB().NewCreateIndex().
		Model((*Runner)(nil)).
		Index("runner_status_idx").
		Column("status").Exec(ctx)
	if err != nil {
		return err
	}

	_, err = query.DB().NewCreateIndex().
		Model((*Runner)(nil)).
		Index("runner_queued_at_idx").
		Column("queued_at").Exec(ctx)
	return err
}

var _ bun.BeforeAppendModelHook = (*Runner)(nil)

func (g *Runner) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	switch query.(type) {
	case *bun.InsertQuery:
		g.CreatedAt = time.Now()
		g.UpdatedAt = time.Now()
	case *bun.UpdateQuery:
		g.UpdatedAt = time.Now()
	}
	return nil
}
