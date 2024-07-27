package db

import (
	"database/sql"
	"log/slog"

	_ "github.com/go-sql-driver/mysql"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/extra/bundebug"
)

func New(DBURL, DBType string) (db *bun.DB, err error) {
	switch DBType {
	default:
		sqlitedb, err := sql.Open(sqliteshim.ShimName, "file:sqlite.db?cache=shared")
		if err != nil {
			slog.Error(err.Error())
			return nil, err
		}
		sqlitedb.SetMaxOpenConns(3)
		db = bun.NewDB(sqlitedb, sqlitedialect.New())
	case "mysql":
		sqldb, err := sql.Open("mysql", DBURL)
		if err != nil {
			slog.Error(err.Error())
			return nil, err
		}
		db = bun.NewDB(sqldb, mysqldialect.New())
	}
	db.AddQueryHook(bundebug.NewQueryHook(
		bundebug.WithVerbose(true),
		bundebug.FromEnv("BUNDEBUG"),
	))
	return db, nil
}
