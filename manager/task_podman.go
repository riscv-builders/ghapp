package manager

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/bindings/volumes"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/riscv-builders/ghapp/models"
)

const (
	defaultImage = "ghcr.io/riscv-builders/action-runner:latest"
	actionCache  = "act-cache-%d"
)

var containerCreated = errors.New("podman container created")

func (c *Coor) getPodmanConnection(ctx context.Context, bdr *models.Builder) (conn context.Context, err error) {
	uri := bdr.Meta["uri"]
	ident := bdr.Token
	return bindings.NewConnectionWithIdentity(ctx, uri, ident, false)
}

func (c *Coor) preparePodmanBuilder(ctx context.Context, bdr *models.Builder, t *models.Task) error {
	conn, err := c.getPodmanConnection(ctx, bdr)
	if err != nil {
		return err
	}
	if _, ok := bdr.Meta["act-cache"]; ok {
		size := bdr.Meta["act-cache-size"]
		if size == "" {
			size = "1G"
		}
		vco := types.VolumeCreateOptions{
			IgnoreIfExists: true,
			Name:           fmt.Sprintf(actionCache, t.Job.InstallationID),
			Options: map[string]string{
				"size":   size,
				"device": "tmpfs",
				"type":   "tmpfs",
			},
		}
		_, err := volumes.Create(conn, vco, nil)
		if err != nil {
			return err
		}
	}

	var policy = "newer"
	opts := &images.PullOptions{
		Policy: &policy,
	}
	_, err = images.Pull(conn, defaultImage, opts)

	return err
}

func (c *Coor) isBuilderReady(ctx context.Context, t *models.Task) {
	r := &models.Task{}
	err := c.db.NewSelect().Model(r).
		Relation("Builder").
		Where("task.id = ?", t.ID).Limit(1).Scan(ctx, r)
	if err != nil {
		return
	}
	conn, err := c.getPodmanConnection(ctx, r.Builder)
	if err != nil {
		return
	}

	summary, err := images.List(conn, &images.ListOptions{
		Filters: map[string][]string{"reference": []string{defaultImage}},
	})

	if err != nil {
		return
	}

	if len(summary) == 0 {
		c.moveToBack(t, 30*time.Second)
	}
	return
}

func (c *Coor) doPodmanBuilder(ctx context.Context, r *models.Task, cmd []string) error {
	conn, err := c.getPodmanConnection(ctx, r.Builder)
	if err != nil {
		return err
	}

	spec := specgen.NewSpecGenerator(defaultImage, false)
	spec.Annotations = map[string]string{
		"repo":    r.Job.RepoName,
		"owner":   r.Job.Owner,
		"task_id": strconv.FormatInt(r.ID, 10),
	}
	spec.Name = fmt.Sprintf("rvb-task-%d", r.ID)
	spec.Timeout = uint(r.DeadLine.Sub(time.Now()).Seconds())
	spec.Command = cmd
	spec.Remove = func(b bool) *bool { return &b }(true)
	spec.Mounts = append(spec.Mounts, specs.Mount{
		Destination: "/root/.cache",
		Type:        "tmpfs",
		Source:      fmt.Sprintf(actionCache, r.Job.InstallationID),
	})

	createResponse, err := containers.CreateWithSpec(conn, spec, nil)
	if err != nil {
		return err
	}
	err = containers.Start(conn, createResponse.ID, nil)
	if err != nil {
		return err
	}

	_, err = containers.Wait(conn, createResponse.ID, nil)
	return err
}
