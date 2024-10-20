package web

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/riscv-builders/ghapp/db"
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
	rt  *gin.Engine

	ghtimeout time.Duration
	stats     Stats
}

func New(cfg *Config) (ins *GithubService, err error) {
	ins = &GithubService{cfg: cfg}
	ins.db, err = db.New(cfg.DBType, cfg.DBURL)
	if err != nil {
		return
	}

	err = ins.initAPI()
	if err != nil {
		return
	}

	return ins, err
}
