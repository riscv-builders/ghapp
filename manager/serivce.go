package manager

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
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

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		slog.Info("recv signal", "name", <-sigs)
		cancel()
	}()

	go c.serveJob(ctx)
	c.serveAssigned(ctx)
	return ctx.Err()
}

func (c *Coor) serveJob(ctx context.Context) {

	d := time.Second
	for {
		start := time.Now()
		c.findAvailableJob(ctx)
		c.doPendingTask(ctx)
		if time.Since(start) < 10*time.Second {
			d = 10*time.Second - time.Since(start)
		} else {
			d = time.Second
		}
		select {
		case <-time.NewTimer(d).C:
		case <-ctx.Done():
			return
		}
	}
}

func (c *Coor) serveAssigned(ctx context.Context) {
	d := time.Second
	for {
		start := time.Now()
		tasks, err := c.findTasks(ctx, models.TaskBuilderAssigned, 10)
		if err != nil {
			slog.Error("serve assigned failed", "err", err)
			time.Sleep(time.Second)
			continue
		}
		for _, t := range tasks {
			sctx, _ := context.WithDeadline(ctx, t.DeadLine)
			go c.doAssigned(sctx, t.ID)
		}
		if time.Since(start) < 10*time.Second {
			d = 10*time.Second - time.Since(start)
		} else {
			d = time.Second
		}

		select {
		case <-ctx.Done():
			return
		case <-time.NewTimer(d).C:
		}
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
	ctx, cancel := context.WithTimeout(ctx, 7*time.Hour)
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.UpdatedAt = time.Now()
				c.db.NewUpdate().Model(t).WherePK().Column("updated_at").Exec(ctx)
			}
		}
	}()

	err = c.prepareBuilder(ctx, t)
	if err != nil {
		slog.Error("prepare task failed", "id", taskID, "err", err)
	}
	err = c.startBuilder(ctx, t)
	if err != nil {
		slog.Error("start task failed", "id", taskID, "err", err)
	}
	c.releaseBuilder(t)
}

func (c *Coor) searchAndDestroyHangedTask(ctx context.Context) {
}
