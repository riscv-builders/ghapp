package coordinator

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"os"
	"sync"
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

	queue    chan *models.GithubWorkflowJob
	runner   chan *models.Runner
	runnerCm sync.Map //key: runner id , val: cancel function
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
		cfg:      cfg,
		queue:    make(chan *models.GithubWorkflowJob),
		runner:   make(chan *models.Runner),
		runnerCm: sync.Map{},
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
	go c.serveRunner(ctx)
	passiveDuration := time.Minute
	ticker := time.NewTicker(passiveDuration)
	slog.Info("Coor started")
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(ctx, passiveDuration)
			err := c.passiveWork(ctx)
			if err != nil {
				slog.Warn("passive", "error", err)
			}
			cancel()
		case j := <-c.queue:
			err := c.handleQueue(ctx, j)
			if err != nil {
				slog.Warn("queue", "error", err)
			}
		}
	}
}

func (c *Coor) handleQueue(ctx context.Context, j *models.GithubWorkflowJob) (err error) {
	builders, err := c.findBuilder(ctx)
	if err != nil {
		return err
	}

	if len(builders) == 0 {
		return nil
	}
	c.bind(ctx, j, builders[0])
	return nil
}

func (c *Coor) passiveWork(ctx context.Context) error {
	builders, err := c.findBuilder(ctx)
	if err != nil {
		return err
	}

	if len(builders) == 0 {
		return nil
	}

	jobs, err := c.findJob(ctx)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	l := min(len(builders), len(jobs))
	wg := sync.WaitGroup{}
	wg.Add(l)
	for i := 0; i < l; i++ {
		go c.bindWG(ctx, wg, jobs[i], builders[i])
	}
	wg.Wait()
	return nil
}

func (c *Coor) findJob(ctx context.Context) ([]*models.GithubWorkflowJob, error) {
	const maxJobs = 5
	jl := make([]*models.GithubWorkflowJob, 0, maxJobs)
	err := c.db.NewSelect().Model((*models.GithubWorkflowJob)(nil)).
		Where("status = ?", models.WorkflowJobQueued).
		Limit(maxJobs).Scan(ctx, &jl)

	return jl, err
}

func (c *Coor) findBuilder(ctx context.Context) ([]*models.Builder, error) {
	const maxBuilder = 5
	bl := make([]*models.Builder, 0, maxBuilder)
	err := c.db.NewSelect().Model((*models.Builder)(nil)).
		Where("status = ?", models.BuilderIdle).
		Limit(maxBuilder).Scan(ctx, &bl)
	return bl, err
}

func (c *Coor) bindWG(ctx context.Context, wg sync.WaitGroup, job *models.GithubWorkflowJob, bdr *models.Builder) {
	defer wg.Done()
	c.bind(ctx, job, bdr)

}

func (c *Coor) bind(ctx context.Context, job *models.GithubWorkflowJob, bdr *models.Builder) {
	slog.Info("run job", "job", job.ID, "owner", job.Owner, "repo", job.RepoName, "builder", bdr.Name)
	err := c.prepareBuilder(ctx, bdr)
	if err != nil {
		slog.Error("prepare failed", "err", err)
		return
	}
	err = c.runJob(ctx, job, bdr)
	if err != nil {
		slog.Error("run job failed", "err", err)
		return
	}
}
