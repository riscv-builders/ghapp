package coordinator

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/riscv-builders/ghapp/models"
)

func (c *Coor) prepareSSHBuilder(ctx context.Context, bdr *models.Builder) error {
	client, err := c.getSSHClient(ctx, bdr)
	if err != nil {
		return err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return err
	}

	defer session.Close()
	out, err := session.Output("github-act-runner --version")
	if err != nil {
		bdr.Status = models.BuilderQuarantined
		c.db.NewUpdate().Model(bdr).Column("status", "updated_at").WherePK().Exec(ctx)
		return errors.Join(err, errors.New(string(out)))
	}
	bdr.Meta["runner-version"] = string(out)
	_, err = c.db.NewUpdate().Model(bdr).Column("meta", "updated_at").WherePK().Exec(ctx)
	return err
}

func (c *Coor) doSSHBuilder(ctx context.Context, r *models.Runner, configCmd []string) error {
	cli, err := c.getSSHClient(ctx, r.Builder)
	if err != nil {
		slog.Debug("runner ssh client", "err", err)
		return err
	}
	defer cli.Close()

	session, err := cli.NewSession()
	if err != nil {
		slog.Debug("runner ssh session", "err", err)
		return err
	}

	p, err := session.Output(strings.Join(configCmd, " "))
	slog.Debug("runner session configure", "runner_id", r.ID, "msg", string(p))
	if err != nil {
		if !strings.Contains(string(p), "runner already configured.") {
			return err
		}
	}
	session.Close()

	session, err = cli.NewSession()
	if err != nil {
		slog.Debug("runner ssh session", "err", err)
		return err
	}
	defer session.Close()

	slog.Debug("runner start running", "builder_name", r.Builder.Name, "runner_id", r.ID)
	p, err = session.Output("github-act-runner run --once")
	slog.Debug("runner session run", "runner_id", r.ID, "msg", string(p))
	return err
}
