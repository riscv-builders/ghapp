package ghc

import (
	"log/slog"
	"net/http"
	"time"
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

	http.HandleFunc("/gh/webhook", c.GithubWebhook)
	return err
}

func (c *GithubService) Serve() error {
	addr := c.cfg.ListenAddr
	if addr == "" {
		addr = ":6946"
	}

	slog.Info("api", "listen", addr)
	return http.ListenAndServe(addr, nil)
}
