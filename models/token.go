package models

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type TokenType string

const (
	InstallAccessToken      TokenType = "install_access"
	ActionRegistrationToken           = "action_registration"
)

type Token struct {
	ID    int64 `bun:"id,pk,autoincrement"`
	Type  TokenType
	Key   string
	Value string `bun:,text`

	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
	ExpiredAt time.Time `bun:",nullzero,notnull" json:"expired_at"`
}

var _ bun.AfterCreateTableHook = (*Token)(nil)

func (*Token) AfterCreateTable(ctx context.Context, query *bun.CreateTableQuery) error {
	_, err := query.DB().NewCreateIndex().
		Model((*Token)(nil)).
		Index("token_type_key_idx").
		Column("type").
		Column("key").
		Unique().Exec(ctx)
	if err != nil {
		return err
	}
	return nil
}

var _ bun.BeforeAppendModelHook = (*Token)(nil)

func (g *Token) BeforeAppendModel(ctx context.Context, query bun.Query) error {
	switch query.(type) {
	case *bun.InsertQuery:
		g.CreatedAt = time.Now()
		g.UpdatedAt = time.Now()
	case *bun.UpdateQuery:
		g.UpdatedAt = time.Now()
	}
	return nil
}
