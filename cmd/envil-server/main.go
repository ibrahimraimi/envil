package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ibrahimraimi/envil/internal/auth"
	"github.com/ibrahimraimi/envil/internal/config"
	"github.com/ibrahimraimi/envil/internal/server"
	"github.com/ibrahimraimi/envil/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.LoadServerConfig()

	appStore, cleanup, err := initStore(cfg.DatabaseURL, logger)
	if err != nil {
		logger.Error("failed to initialize store", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer cleanup()

	nonces := auth.NewMemoryNonceTracker()
	serverConfig := server.Config{
		ListenAddr:     cfg.ListenAddr,
		RateLimitRPS:   cfg.RateLimitRPS,
		RateLimitBurst: cfg.RateLimitBurst,
		MaxBlobBytes:   cfg.MaxBlobBytes,
	}

	srv := server.NewServer(serverConfig, appStore, nonces, logger)

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server startup error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	logger.Info("envil server running", slog.String("addr", cfg.ListenAddr))
	<-shutdownSignal

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("error during graceful shutdown", slog.String("error", err.Error()))
	}
	logger.Info("envil server stopped")
}

func initStore(databaseURL string, logger *slog.Logger) (store.Store, func(), error) {
	if databaseURL == "" {
		logger.Info("no DATABASE_URL provided, using in-memory store")
		return store.NewMemoryStore(), func() {}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		pool.Close()
	}

	return store.NewPostgresStore(pool), cleanup, nil
}
