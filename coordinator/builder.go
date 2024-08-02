package coordinator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/riscv-builders/ghapp/models"
	"github.com/uptrace/bun"
	"golang.org/x/crypto/ssh"
)

func (c *Coor) findBuilder(ctx context.Context, query *bun.Query) (*models.Builder, error) {
	// TODO match labels in the future
	bdr := &models.Builder{}
	err := c.db.NewSelect().Model(bdr).
		Where("status = ?", models.BuilderIdle).
		Limit(1).Scan(ctx, bdr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return bdr, err
}

func (c *Coor) runJob(ctx context.Context, job *models.GithubWorkflowJob, bdr *models.Builder) error {
	if bdr.Status != models.BuilderLocked {
		return fmt.Errorf("builder:%d not setup", bdr.ID)
	}
	if job.Status != models.WorkflowJobQueued {
		return fmt.Errorf("job:%d not queued", job.ID)
	}

	tx, err := c.db.Begin()
	if err != nil {
		return err
	}

	bdr.Status = models.BuilderWorking
	tx.NewUpdate().Model(bdr).Column("status", "updated_at").WherePK().Exec(ctx)

	job.Status = models.WorkflowJobScheduled
	tx.NewUpdate().Model(job).Column("status", "updated_at").WherePK().Exec(ctx)

	err = tx.Commit()
	if err != nil {
		return err
	}

	switch bdr.Type {
	case models.BuilderSSH:
		return c.runSSHBuilder(ctx, job, bdr)
	}
	return fmt.Errorf("unsupported builder:%s", bdr.Type)
}

func (c *Coor) runSSHBuilder(ctx context.Context, job *models.GithubWorkflowJob, bdr *models.Builder) error {

	runner := &models.Runner{
		BuilderID:    bdr.ID,
		Builder:      bdr,
		Job:          job,
		JobID:        job.ID,
		Name:         fmt.Sprintf("riscv-builder-%s", bdr.Name),
		Labels:       append([]string{"riscv-builers"}, bdr.Labels...),
		SystemLabels: []string{"riscv64", "riscv", "linux"},
		URL:          fmt.Sprintf("https://github.com/%s/%s", job.Owner, job.RepoName),
		Ephemeral:    true,
		Status:       models.RunnerScheduled,
	}
	_, err := c.db.NewInsert().Model(runner).Exec(ctx)
	return err
}

func (c *Coor) getSSHClient(ctx context.Context, bdr *models.Builder) (*ssh.Client, error) {
	priv := bdr.Token // private key for this builder
	addr := bdr.Meta["addr"]
	user := bdr.Meta["user"]
	// check act runner available
	if priv == "" || user == "" || addr == "" {
		return nil, fmt.Errorf("invalid ssh builder:%s user:%s", addr, user)
	}

	signer, err := ssh.ParsePrivateKey([]byte(priv))
	if err != nil {
		return nil, errors.Join(fmt.Errorf("can't parse private key for builder:%s", bdr.Name), err)
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO check this
	}

	return ssh.Dial("tcp", addr, config)
}

func (c *Coor) tryQuarantineBuilder(ctx context.Context, bdrID int64) {
	if bdrID == 0 {
		slog.Warn("TryQuarantineBuilder failed", "err", "builder id = 0")
		return
	}
	bdr := &models.Builder{ID: bdrID}
	err := c.db.NewSelect().Model(bdr).WherePK().Limit(1).Scan(ctx, bdr)
	if err != nil {
		slog.Warn("TryQuarantineBuilder failed", "err", err)
		return
	}
	bdr.FailedCount += 1
	bdr.Status = models.BuilderIdle
	if bdr.FailedCount > 2 {
		bdr.Status = models.BuilderQuarantined
	}
	_, err = c.db.NewUpdate().Model(bdr).WherePK().
		Column("status", "failed_count", "updated_at").Exec(ctx)
	if err != nil {
		slog.Warn("TryQuarantineBuilder failed", "err", err)
	}
}
