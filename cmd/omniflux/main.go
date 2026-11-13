package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/inwinter04/omniflux/internal/app"
	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/bootstrap"
	"github.com/inwinter04/omniflux/internal/config"
	"github.com/inwinter04/omniflux/internal/httpx"
	"github.com/inwinter04/omniflux/internal/infra"
	"github.com/inwinter04/omniflux/internal/logging"
)

func main() {
	if err := run(); err != nil {
		slog.Error("omniflux exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := infra.NewPGXPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	mdb, err := infra.NewMigrationDB(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer mdb.Close()
	if err := app.Migrate(ctx, logger, mdb); err != nil {
		return err
	}
	if err := bootstrap.Seed(ctx, logger, pool, cfg); err != nil {
		return err
	}
	redisClient, err := infra.NewRedis(ctx, cfg.RedisAddr)
	if err != nil {
		return err
	}
	defer redisClient.Close()
	engine, engineErr := bifrostx.NewEngine(ctx, nil)
	if engineErr != nil {
		return engineErr
	}
	defer engine.Close()
	authStore := auth.NewStore(pool)
	handler := httpx.New(httpx.ServerDeps{Logger: logger, Pool: pool, Redis: redisClient, Engine: engine, Auth: authStore, Env: cfg.Env})
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      300 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 * 1024,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("omniflux listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	logger.Info("omniflux shutting down")
	return server.Shutdown(shutdownCtx)
}
