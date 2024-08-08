package db

import (
	"database/sql"
	"log/slog"

	_ "github.com/go-sql-driver/mysql"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/extra/bundebug"
)

func New(dbType, dbURL string) (db *bun.DB, err error) {
	slog.Info("db connect", "type", dbType, "url", dbURL)
	switch dbType {
	default:
		sqlitedb, err := sql.Open(sqliteshim.ShimName, "file:sqlite.db?cache=shared")
		if err != nil {
			slog.Error(err.Error())
			return nil, err
		}
		sqlitedb.SetMaxOpenConns(3)
		db = bun.NewDB(sqlitedb, sqlitedialect.New())
	case "postgres":
		sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbURL)))
		db = bun.NewDB(sqldb, pgdialect.New())
	}
	db.AddQueryHook(bundebug.NewQueryHook(
		bundebug.WithVerbose(false),
		bundebug.FromEnv("BUNDEBUG"),
	))
	return db, nil
}
