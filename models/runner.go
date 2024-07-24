package models

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type Builder struct {
	ID     int64 `bun:",pk,autoincrement,default:1" json:"id"`
	Name   string
	Cred   string `bun:",type:text" json:"cred"`
	Meta   string `bun:",type:text"`
	Labels []string
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
	BuilderIdle    BuilderStatus = "idle"
	BuilderWorking               = "working"
	BuilderDied                  = "died"
)

type Runner struct {
	ID        int64   `bun:",pk,autoincrement,default:1" json:"id"`
	Builder   Builder `bun:"rel:belongs-to,join:builder_id=id" json:"-"`
	BuilderID int64   `bun:"connect_id" json:"builder_id"`
	RunID     int64   `bun:"run_id"`
	Token     string  `bun:",unique" json:"token"`
	Labels    map[string]string

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
	ExpiredAt time.Time `json:"expired_at"`
}

func (r *Runner) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	r.UpdatedAt = time.Now()
	return nil
}
