package ghc

import (
	"context"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/riscv-builders/service/models"
)

func (c *GithubService) Migrate() (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err = c.db.NewCreateTable().Model((*models.GithubWorkflowJobEvent)(nil)).Exec(ctx)
	return
}
