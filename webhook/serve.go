package webhook

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

func (c *GithubService) initAPI() (err error) {
	if c.cfg.GHTimeout == "" {
		c.cfg.GHTimeout = "1m"
	}
	c.ghtimeout, err = time.ParseDuration(c.cfg.GHTimeout)
	if err != nil {
		return
	}
	slog.Info("github webhook", "timeout", c.ghtimeout.String())

	if c.cfg.GHSecretKey == "" {
		slog.Warn("github webhook", "secret", "")
	} else {
		slog.Info("event", "webhook", c.cfg.GHSecretKey)
	}

	if c.cfg.ListenAddr == "" {
		c.cfg.ListenAddr = ":6946"
	}
	slog.Info("api", "listen", c.cfg.ListenAddr)

	c.rt = gin.Default()

	c.rt.POST("/github/events", func(gc *gin.Context) {
		c.GithubEvents(gc.Writer, gc.Request)
	})

	return err
}

func (c *GithubService) Serve() error {
	return c.rt.Run(c.cfg.ListenAddr)
}
