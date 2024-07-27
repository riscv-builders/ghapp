package models

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type Builder struct {
	ID      int64  `bun:",pk,autoincrement,default:1" json:"id"`
	Name    string `bun:",unique"`
	Sponsor string
	Token   string            `bun:",type:text" json:"token"`
	Meta    map[string]string `bun:",type:text"`
	Labels  []string          `bun:",type:text"`

	Type   BuilderType
	Status BuilderStatus

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

type BuilderType string

const (
	BuilderSSH   BuilderType = "ssh"
	BuilderAgent             = "agent"
)

type BuilderStatus string

const (
	BuilderIdle        BuilderStatus = "idle"
	BuilderWorking                   = "working"
	BuilderDied                      = "died"
	BuilderQuarantined               = "quarantined"
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
