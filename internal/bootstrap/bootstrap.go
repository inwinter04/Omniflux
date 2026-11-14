package bootstrap

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/config"
)

func Seed(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, cfg config.Config) error {
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		hash, err := auth.HashPassword(cfg.BootstrapPassword)
		if err != nil {
			return err
		}
		orgID := uuid.New()
		deptID := uuid.New()
		projectID := uuid.New()
		appID := uuid.New()
		userID := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO organizations(id,name,slug) VALUES($1,$2,$3)`, orgID, "Omniflux", "omniflux"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO departments(id,organization_id,name) VALUES($1,$2,$3)`, deptID, orgID, "Platform"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO projects(id,organization_id,department_id,name) VALUES($1,$2,$3,$4)`, projectID, orgID, deptID, "Platform"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO applications(id,organization_id,project_id,name) VALUES($1,$2,$3,$4)`, appID, orgID, projectID, "Console"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO users(id,organization_id,email,password_hash,role) VALUES($1,$2,$3,$4,'platform_admin')`, userID, orgID, cfg.BootstrapEmail, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO audit_events(organization_id,actor_user_id,actor_type,action,resource_type,resource_id) VALUES($1,$2,'system','bootstrap.created','user',$3)`, orgID, userID, userID.String()); err != nil {
			return err
		}
		logger.Info("bootstrap platform admin created", "email", cfg.BootstrapEmail, "organization_id", orgID)
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}
