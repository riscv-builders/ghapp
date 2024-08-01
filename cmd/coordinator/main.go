package main

import (
	"context"
	"log"
	"log/slog"

	"github.com/JeremyLoy/config"
	"github.com/riscv-builders/ghapp/coordinator"
)

func main() {
	cfg := &coordinator.Config{}
	config.From("ghapp.env").FromEnv().To(cfg)
	ctrl, err := coordinator.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := context.Background()
	log.Fatal(ctrl.Serve(ctx))
}
