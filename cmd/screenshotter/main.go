package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/config"
	"github.com/mkende/screenshotter/server/internal/db"
	"github.com/mkende/screenshotter/server/internal/handlers"
	"github.com/mkende/screenshotter/server/internal/server"
	"github.com/mkende/screenshotter/server/internal/storage"
	"github.com/mkende/screenshotter/server/internal/tmpl"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to TOML config file")
	flag.Parse()

	if err := run(*configPath); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	tmpls, err := tmpl.Parse()
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	database, err := db.Open(cfg.Database.Backend, cfg.Database.DSN)
	if err != nil {
		return err
	}
	defer database.Close()

	stor, err := storage.New(cfg.Server.StoragePath)
	if err != nil {
		return err
	}

	// OIDC provider discovery requires a network call; give it 30 s.
	initCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	authSvc, err := auth.New(initCtx, cfg)
	cancel()
	if err != nil {
		return err
	}

	h := handlers.New(cfg, database, stor, authSvc, tmpls)
	httpHandler := server.New(cfg, h, authSvc)

	srv := &http.Server{
		Addr:         cfg.Server.Listen,
		Handler:      httpHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	slog.Info("starting server", "addr", cfg.Server.Listen, "domain", cfg.Server.Domain)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-quit:
		slog.Info("shutting down")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
	}
	return nil
}
