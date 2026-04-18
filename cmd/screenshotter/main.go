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
	"strings"
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
	configPath := flag.String("config", "", "path to TOML config file (default: $SCREENSHOTTER_CONFIG or config.toml)")
	flag.Parse()

	if *configPath == "" {
		if env := os.Getenv("SCREENSHOTTER_CONFIG"); env != "" {
			*configPath = env
		} else {
			*configPath = "config.toml"
		}
	}

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

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	tmpls, err := tmpl.Parse()
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	database, err := db.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		return err
	}
	defer database.Close()

	stor, err := storage.New(cfg.Server.StoragePath)
	if err != nil {
		return err
	}

	h := handlers.New(cfg, database, stor, tmpls, tmpl.FontTTF)

	var oidcHandler *auth.OIDCHandler
	if cfg.OIDC.Enabled {
		initCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		oidcHandler, err = auth.NewOIDCHandler(initCtx, cfg, database)
		cancel()
		if err != nil {
			return err
		}
	}

	httpHandler := server.New(cfg, h, oidcHandler, logger)

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      httpHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	logger.Info("starting server",
		"addr", cfg.ListenAddr,
		"canonical_address", cfg.CanonicalAddress,
		"oidc_enabled", cfg.OIDC.Enabled,
		"tailscale_enabled", cfg.Tailscale.Enabled,
		"proxy_auth_enabled", cfg.ProxyAuth.Enabled,
		"anonymous_enabled", cfg.Anonymous.Enabled,
	)

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
		logger.Info("shutting down")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
	}
	return nil
}

// newLogger returns a structured logger configured at the given level.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
