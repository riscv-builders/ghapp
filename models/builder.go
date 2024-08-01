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

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

type BuilderType string

const (
	BuilderSSH    BuilderType = "ssh"
	BuilderPodman             = "podman"
)

type BuilderStatus string

const (
	BuilderIdle        BuilderStatus = "idle"
	BuilderLocked                    = "locked"
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
