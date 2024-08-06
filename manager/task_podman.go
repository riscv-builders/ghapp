package manager

import (
	"context"
	"fmt"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/riscv-builders/ghapp/models"
)

const defaultImage = "ghcr.io/riscv-builders/action-runner:latest"

func (c *Coor) getPodmanConnection(ctx context.Context, bdr *models.Builder) (conn context.Context, err error) {
	uri := bdr.Meta["uri"]
	ident := bdr.Token
	return bindings.NewConnectionWithIdentity(ctx, uri, ident, false)
}

func (c *Coor) preparePodmanBuilder(ctx context.Context, bdr *models.Builder) error {
	conn, err := c.getPodmanConnection(ctx, bdr)
	if err != nil {
		return err
	}
	var policy = "newer"
	opts := &images.PullOptions{
		Policy: &policy,
	}
	_, err = images.Pull(conn, defaultImage, opts)
	return err
}

func (c *Coor) doPodmanBuilder(ctx context.Context, r *models.Task, cmd []string) error {
	conn, err := c.getPodmanConnection(ctx, r.Builder)
	if err != nil {
		return err
	}

	spec := specgen.NewSpecGenerator(defaultImage, false)
	spec.Name = fmt.Sprintf("%s-%s:%d", r.Job.RepoName, r.Job.Owner, r.ID)

	spec.Command = cmd
	createResponse, err := containers.CreateWithSpec(conn, spec, nil)
	if err != nil {
		return err
	}
	err = containers.Start(conn, createResponse.ID, nil)
	return err
}
