package ghc

import (
	"time"

	"github.com/riscv-builders/service/db"
	"github.com/uptrace/bun"
)

type Config struct {
	ListenAddr string `config:"LISTEN_ADDR"`

	DBURL  string `config:"DB_URL"`
	DBType string `config:"DB_TYPE"`

	// API related
	GHSecretKey    string `config:"GH_WEBHOOK_SECRET_KEY"`
	GHTimeout      string `config:"GH_WEBHOOK_TIMEOUT"`
	GHStarsRequire int    `config:"GH_STARS_REQUIRE"`
}

type GithubService struct {
	cfg *Config
	db  *bun.DB

	ghtimeout time.Duration
}

func New(cfg *Config) (ins *GithubService, err error) {
	ins = &GithubService{cfg: cfg}
	ins.db, err = db.New(cfg.DBURL, cfg.DBType)
	if err != nil {
		return
	}
	err = ins.Migrate()
	if err != nil {
		return
	}

	err = ins.initAPI()
	if err != nil {
		return
	}

	return ins, err
}
