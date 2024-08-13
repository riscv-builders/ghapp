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
	go c.serveJob(ctx)
	c.serveAssigned(ctx)
	return ctx.Err()
}

func (c *Coor) serveJob(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		start := time.Now()
		c.findAvailableJob(ctx)
		c.doPendingTask(ctx)
		if time.Since(start) < time.Second {
			time.Sleep(time.Second - time.Since(start))
		}
	}
}

func (c *Coor) serveAssigned(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		tasks, err := c.findTasks(ctx, models.TaskBuilderAssigned, 10)
		if err != nil {
			slog.Error("serve assigned failed", "err", err)
			time.Sleep(time.Second)
			continue
		}
		for _, t := range tasks {
			sctx, _ := context.WithTimeout(ctx, 35*24*time.Hour)
			go c.doAssigned(sctx, t.ID)
		}
		time.Sleep(time.Second)
	}
}

func (c *Coor) doAssigned(ctx context.Context, taskID int64) {
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
