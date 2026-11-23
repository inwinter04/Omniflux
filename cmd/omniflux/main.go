package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/inwinter04/omniflux/internal/app"
	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/bootstrap"
	"github.com/inwinter04/omniflux/internal/config"
	"github.com/inwinter04/omniflux/internal/httpx"
	"github.com/inwinter04/omniflux/internal/infra"
	"github.com/inwinter04/omniflux/internal/logging"
	"github.com/inwinter04/omniflux/internal/vault"
)

const usage = `Omniflux service and management commands.

Usage:
  omniflux                     Start the HTTP service
  omniflux reset-password      Reset a user password
    --email <email>            Target user email
    --password <new-password>  New password, at least 16 characters
  omniflux help                Show this help
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "reset-password":
			if err := runPasswordReset(os.Args[2:]); err != nil {
				slog.Error("reset-password failed", "error", err)
				os.Exit(1)
			}
			return
		case "help", "-h", "--help":
			fmt.Print(usage)
			return
		default:
			slog.Error("unknown command", "command", os.Args[1])
			fmt.Print(usage)
			os.Exit(1)
		}
	}
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
	app.StartMaintenance(ctx, logger, pool)
	redisClient, err := infra.NewRedis(ctx, cfg.RedisAddr)
	if err != nil {
		return err
	}
	defer redisClient.Close()
	specs, err := loadProviderSpecs(ctx, pool, cfg.MasterKey)
	if err != nil {
		return err
	}
	engine, engineErr := bifrostx.NewEngine(ctx, specs)
	if engineErr != nil {
		return engineErr
	}
	defer engine.Close()
	logger.Info("provider engine loaded", "accounts", len(specs))
	reloadEngine := func(ctx context.Context) error {
		next, err := loadProviderSpecs(ctx, pool, cfg.MasterKey)
		if err != nil {
			return err
		}
		return engine.Reload(ctx, next)
	}
	authStore := auth.NewStore(pool)
	handler := httpx.New(httpx.ServerDeps{
		Logger: logger, Pool: pool, Redis: redisClient, Engine: engine,
		Auth: authStore, Env: cfg.Env,
		MasterKey: cfg.MasterKey, APIKeyPepper: cfg.APIKeyPepper,
		ReloadEngine:    reloadEngine,
		RateLimitPerMin: cfg.RateLimitPerMin,
	})
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

// loadProviderSpecs 从数据库读取 active Provider 账号并解密凭据,构建上游引擎配置。
func loadProviderSpecs(ctx context.Context, pool *pgxpool.Pool, masterKey []byte) ([]bifrostx.ProviderSpec, error) {
	rows, err := pool.Query(ctx, `
		SELECT a.id, a.provider, a.name, a.base_url, c.ciphertext
		FROM provider_accounts a
		JOIN provider_credentials c ON c.provider_account_id = a.id
		WHERE a.status = 'active'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	specs := []bifrostx.ProviderSpec{}
	for rows.Next() {
		var id uuid.UUID
		var provider, name string
		var baseURL *string
		var ciphertext []byte
		if err := rows.Scan(&id, &provider, &name, &baseURL, &ciphertext); err != nil {
			return nil, err
		}
		plaintext, err := vault.Decrypt(masterKey, ciphertext)
		if err != nil {
			return nil, err
		}
		spec := bifrostx.ProviderSpec{
			Provider:  provider,
			AccountID: id.String(),
			Name:      name,
			APIKey:    string(plaintext),
		}
		if baseURL != nil {
			spec.BaseURL = *baseURL
		}
		specs = append(specs, spec)
	}
	return specs, rows.Err()
}
