package models

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type Builder struct {
	ID      int64  `bun:",pk,autoincrement" json:"id"`
	Name    string `bun:",unique"`
	Sponsor string
	Token   string `bun:",type:text" json:"token"`
	Meta    map[string]string
	Labels  []string

	Type        BuilderType
	Status      BuilderStatus
	FailedCount int
	TaskID      int64 `bun:"task_id"`

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

type BuilderType string

const (
	BuilderSSH    BuilderType = "ssh"
	BuilderPodman BuilderType = "podman"
)

type BuilderStatus string

const (
	BuilderIdle        BuilderStatus = "idle"
	BuilderLocked      BuilderStatus = "locked"
	BuilderPreparing   BuilderStatus = "preparing"
	BuilderWorking     BuilderStatus = "working"
	BuilderDied        BuilderStatus = "died"
	BuilderQuarantined BuilderStatus = "quarantined"
)

var _ bun.AfterCreateTableHook = (*Builder)(nil)

func (*Builder) AfterCreateTable(ctx context.Context, query *bun.CreateTableQuery) error {
	_, err := query.DB().NewCreateIndex().
		Model((*Builder)(nil)).
		Index("builder_status_idx").
		Column("status").Exec(ctx)
	if err != nil {
		return err
	}
	return err
}
