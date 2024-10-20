package manager

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/riscv-builders/ghapp/models"
	"github.com/uptrace/bun"
	"golang.org/x/crypto/ssh"
)

func (c *Coor) findBuilder(ctx context.Context, labels []string) (*models.Builder, error) {
	bdr := &models.Builder{}
	query := c.db.NewSelect().Model(bdr).
		Where("status = ? AND task_id IS NULL", models.BuilderIdle)

	if len(labels) > 0 {
		query.Where("jsonb_exists_all(labels::jsonb, array[?])", bun.In(labels))
	}

	err := query.Limit(1).Scan(ctx, bdr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return bdr, err
}

func (c *Coor) runSSHBuilder(ctx context.Context, job *models.GithubWorkflowJob, bdr *models.Builder) error {

	task := &models.Task{
		BuilderID:    bdr.ID,
		Builder:      bdr,
		Job:          job,
		JobID:        job.ID,
		Name:         fmt.Sprintf("riscv-builder-%s", bdr.Name),
		SystemLabels: []string{"riscv64", "riscv", "linux"},
		URL:          fmt.Sprintf("https://github.com/%s/%s", job.Owner, job.RepoName),
		Ephemeral:    true,
		Status:       models.TaskPending,
	}
	_, err := c.db.NewInsert().Model(task).Exec(ctx)
	return err
}

func (c *Coor) getSSHClient(ctx context.Context, bdr *models.Builder) (*ssh.Client, error) {
	priv := bdr.Token // private key for this builder
	addr := bdr.Meta["addr"]
	user := bdr.Meta["user"]
	// check act task available
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

func (c *Coor) tryQuarantineBuilder(bdrID int64) {

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

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
	bdr.TaskID = 0
	bdr.Status = models.BuilderIdle
	if bdr.FailedCount > 2 {
		bdr.Status = models.BuilderQuarantined
	}
	_, err = c.db.NewUpdate().Model(bdr).WherePK().
		Column("status", "failed_count", "updated_at", "task_id").Exec(ctx)
	if err != nil {
		slog.Warn("TryQuarantineBuilder failed", "err", err)
	}
}
