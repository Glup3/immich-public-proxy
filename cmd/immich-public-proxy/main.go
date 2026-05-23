package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/alangrainger/immich-public-proxy/internal/app"
	"github.com/alangrainger/immich-public-proxy/internal/immich"
	"github.com/alangrainger/immich-public-proxy/internal/server"
	"github.com/alangrainger/immich-public-proxy/internal/session"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	runtime, err := app.LoadFromEnv()
	if err != nil {
		logger.Error("load runtime configuration", "error", err)
		os.Exit(1)
	}

	sessions, err := session.NewManager([]byte(runtime.SessionSecret), nil, session.DefaultCookieOptions(), logger)
	if err != nil {
		logger.Error("create session manager", "error", err)
		os.Exit(1)
	}

	handler, err := server.New(server.Options{
		Config:        runtime.Config,
		Client:        immich.NewClient(runtime.ImmichURL, &http.Client{}, nil, logger),
		Sessions:      sessions,
		Logger:        logger,
		PublicBaseURL: runtime.PublicBaseURL,
	})
	if err != nil {
		logger.Error("create server", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.Run(ctx, runtime.Address(), handler, logger); err != nil {
		logger.Error("run server", "error", err)
		os.Exit(1)
	}
}
