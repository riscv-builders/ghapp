package main

import (
	"context"
	"log"
	"log/slog"

	"github.com/JeremyLoy/config"
	"github.com/riscv-builders/ghapp/manager"
)

func main() {
	cfg := &manager.Config{}
	config.From("ghapp.env").FromEnv().To(cfg)
	ctrl, err := manager.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := context.Background()
	log.Fatal(ctrl.Serve(ctx))
}
