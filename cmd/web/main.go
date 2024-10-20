package main

import (
	"log/slog"
	"os"

	"github.com/JeremyLoy/config"
	"github.com/riscv-builders/ghapp/web"
)

func main() {

	var lv slog.Level
	lvs := os.Getenv("RVB_LOG")
	if lvs == "" {
		lvs = "DEBUG"
	}
	lv.UnmarshalText([]byte(lvs))
	slog.SetLogLoggerLevel(slog.LevelDebug)

	cfg := &web.Config{}
	config.From("ghapp.env").FromEnv().To(cfg)
	ctrl, err := web.New(cfg)
	if err != nil {
		slog.Error("github-service", "err", err.Error())
		os.Exit(1)
	}
	slog.Info("serve", "log", ctrl.Serve())
}
