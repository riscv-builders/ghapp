package coordinator

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/riscv-builders/service/db"
	"github.com/riscv-builders/service/models"
	"github.com/uptrace/bun"
)

type Coor struct {
	cfg *Config
	db  *bun.DB

	privateKey  *rsa.PrivateKey
	jwtExpireAt time.Time
	jwt         string
}

func loadPrivateFile(p string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}

	pemBlock, _ := pem.Decode(data)
	return x509.ParsePKCS1PrivateKey(pemBlock.Bytes)
}

func New(cfg *Config) (*Coor, error) {
	s := &Coor{
		cfg: cfg,
	}

	var err error
	s.db, err = db.New(cfg.DBURL, cfg.DBType)
	if err != nil {
		return nil, err
	}
	s.privateKey, err = loadPrivateFile(cfg.PrivateFile)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (c *Coor) Serve(ctx context.Context) error {
	go c.serveAvailableJob(ctx)
	slog.Info("Coor job started")
	return c.serveRunner(ctx)
}

func (c *Coor) serveAvailableJob(ctx context.Context) {
	const maxJob = 10
	for {
		start := time.Now()
		count, err := c.findAvailableJob(ctx)
		if err != nil {
			slog.Warn("find queued job failed", "err", err)
			continue
		}
		since := time.Since(start)
		if count < maxJob || since < 10*time.Second {
			time.Sleep(10*time.Second - since)
		}
	}
}

func (c *Coor) findAvailableJob(ctx context.Context) (int, error) {
	passiveDuration := 30 * time.Second
	ctx, cancel := context.WithTimeout(ctx, passiveDuration)
	defer cancel()

	count, err := c.db.NewSelect().
		Model((*models.GithubWorkflowJob)(nil)).
		Where("status = ?", models.WorkflowJobQueued).Count(ctx)

	if err != nil || count == 0 {
		return count, err
	}

	jl := []*models.GithubWorkflowJob{}
	err = c.db.NewSelect().Model(&jl).
		Where("status = ?", models.WorkflowJobQueued).Limit(10).
		Order("id ASC").Scan(ctx, &jl)

	if err != nil {
		return count, err
	}
	for _, j := range jl {
		c.newRunner(ctx, j)
	}
	return count, err
}

func (c *Coor) newRunner(ctx context.Context, job *models.GithubWorkflowJob) {
	if job.Status != models.WorkflowJobQueued {
		slog.Warn("job not queued", "id", job.ID, "status", job.Status)
		return
	}

	token, expireAt, err := c.getActionRegistrationToken(ctx, job.InstallationID, job.Owner, job.RepoName)
	if err != nil {
		slog.Warn("job get token failed", "id", job.ID, "name", job.Name)
		return
	}

	c.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) (err error) {
		job.Status = models.WorkflowJobScheduled
		_, err = tx.NewUpdate().Model(job).WherePK().Column("status").Exec(ctx)
		if err != nil {
			return
		}

		runner := &models.Runner{
			Job:      job,
			JobID:    job.ID,
			RegToken: token,
			// Name:         fmt.Sprintf("riscv-builder-%s", bdr.Name),
			// Labels:       append([]string{"riscv-builers"}, bdr.Labels...),
			SystemLabels:   []string{"riscv64", "riscv", "linux"},
			URL:            fmt.Sprintf("https://github.com/%s/%s", job.Owner, job.RepoName),
			Ephemeral:      true,
			Status:         models.RunnerScheduled,
			TokenExpiredAt: expireAt,
			QueuedAt:       time.Now(),
			DeadLine:       time.Now().Add(35 * 24 * time.Hour),
		}
		_, err = tx.NewInsert().Model(runner).Ignore().Exec(ctx)
		return
	})
	return
}

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
