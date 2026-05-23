package main

import (
	"fmt"
	"os"

	"github.com/alangrainger/immich-public-proxy/internal/config"
	"github.com/alangrainger/immich-public-proxy/internal/immich"
	"github.com/alangrainger/immich-public-proxy/internal/server"
	"github.com/alangrainger/immich-public-proxy/internal/session"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	sessions, err := session.New()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	app, err := server.New(cfg, immich.New(), sessions)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	if err := server.Run(app); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
