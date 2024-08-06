package manager

import (
	"context"

	"github.com/riscv-builders/ghapp/models"
)

func (c *Coor) preparePodmanBuilder(ctx context.Context, bdr *models.Builder) error {
	//url := bdr.Meta["url"]
	//// ident := bdr.Token
	//conn, err := bindings.NewConnection(ctx, url)
	//if err != nil {
	//	return err
	//}
	////l, err := containers.List(conn, nil)
	return nil
}

func (c *Coor) doPodmanBuilder(ctx context.Context, r *models.Task, configCmd []string) error {
	return nil
}
