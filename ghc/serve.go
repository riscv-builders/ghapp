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

	if c.cfg.ListenAddr == "" {
		c.cfg.ListenAddr = ":6946"
	}
	slog.Info("api", "listen", c.cfg.ListenAddr)

	mux := http.NewServeMux()
	mux.HandleFunc("/gh/webhook", c.GithubWebhook)

	c.srv = &http.Server{
		WriteTimeout:      c.ghtimeout,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       1 * time.Second,
		Handler:           mux,
		Addr:              c.cfg.ListenAddr,
	}

	return err
}

func (c *GithubService) Serve() error {
	return c.srv.ListenAndServe()
}
