package main

import (
	"log"

	"github.com/JeremyLoy/config"
	"github.com/riscv-builders/service/coordinator"
)

func main() {
	cfg := &coordinator.Config{}
	config.From("coordinator.env").FromEnv().To(cfg)
	ctrl, err := coordinator.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(ctrl.Serve())
}
