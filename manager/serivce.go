package manager

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"os"
	"time"

	"github.com/riscv-builders/ghapp/db"
	"github.com/riscv-builders/ghapp/models"
	"github.com/uptrace/bun"
)

type Coor struct {
	cfg *Config
	db  *bun.DB

	privateKey  *rsa.PrivateKey
	jwtExpireAt time.Time
	jwt         string
	cronMap     []Cron
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
	s.db, err = db.New(cfg.DBType, cfg.DBURL)
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
	slog.Info("Coor job started")
	return ctx.Err()
}

func (c *Coor) serveAssigned(ctx context.Context, taskID int64) {
	defer func() {
		if err := recover(); err != nil {
			slog.Error("task failed", "id", taskID)
		}
	}()

	t := &models.Task{}
	err := c.db.NewSelect().Model(t).
		Relation("Job").
		Relation("Builder").
		Where("task.id = ?", taskID).Limit(1).Scan(ctx, t)
	if err != nil {
		slog.Error("task failed", "id", taskID, "err", err)
		return
	}

	steps := []func(context.Context, *models.Task) error{
		c.prepareBuilder,
		c.startBuilder,
	}
	for _, f := range steps {
		err = f(ctx, t)
		if err != nil {
			c.releaseBuilder(t)
			return
		}
	}
}
