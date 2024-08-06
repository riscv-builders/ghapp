package migrations

import (
	"context"
	"fmt"

	"github.com/riscv-builders/ghapp/models"
	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) (err error) {
		fmt.Println(" [up migration] ")
		_, err = db.NewCreateTable().Model((*models.GithubWorkflowJob)(nil)).Exec(ctx)
		if err != nil {
			return err
		}
		_, err = db.NewCreateTable().Model((*models.Builder)(nil)).Exec(ctx)
		if err != nil {
			return err
		}
		_, err = db.NewCreateTable().Model((*models.Task)(nil)).Exec(ctx)
		if err != nil {
			return err
		}
		_, err = db.NewCreateTable().Model((*models.Token)(nil)).Exec(ctx)
		return err
	}, func(ctx context.Context, db *bun.DB) error {
		fmt.Println(" [down migration] ")
		return nil
	})
}
