package app

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"

	"github.com/inwinter04/omniflux/internal/migrations"
)

func Migrate(ctx context.Context, logger *slog.Logger, db *sql.DB) error {
	root := migrations.FS()
	source, err := fs.Sub(root, ".")
	if err != nil {
		return fmt.Errorf("create migration fs: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, source)
	if err != nil {
		return fmt.Errorf("create goose provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	for _, result := range results {
		logger.Info("migration applied", "path", result.Source.Path, "empty", result.Empty)
	}
	return nil
}
