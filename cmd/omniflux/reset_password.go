package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/infra"
	"github.com/inwinter04/omniflux/internal/logging"
)

// runPasswordReset 执行 reset-password 子命令,直接更新指定邮箱用户的密码哈希。
// 密码重置只依赖数据库连接,不需要 MASTER_KEY 与 API_KEY_PEPPER。
func runPasswordReset(args []string) error {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	email := fs.String("email", "", "target user email")
	password := fs.String("password", "", "new password, 8-128 characters including letters and numbers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	normalizedEmail := strings.ToLower(strings.TrimSpace(*email))
	if normalizedEmail == "" || !strings.Contains(normalizedEmail, "@") {
		return errors.New("--email is required")
	}
	if err := auth.ValidatePassword(*password); err != nil {
		return errors.New("--password must be 8-128 characters and include both letters and numbers")
	}
	databaseURL := strings.TrimSpace(os.Getenv("OMNIFLUX_DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("OMNIFLUX_DATABASE_URL is required")
	}
	env := strings.TrimSpace(os.Getenv("OMNIFLUX_ENV"))
	if env == "" {
		env = "development"
	}
	logger := logging.New(env)
	pool, err := infra.NewPGXPool(context.Background(), databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	hash, err := auth.HashPassword(*password)
	if err != nil {
		return err
	}
	tag, err := pool.Exec(context.Background(), `UPDATE users SET password_hash = $1 WHERE email = $2`, hash, normalizedEmail)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found: %s", normalizedEmail)
	}
	logger.Info("password reset", "email", normalizedEmail)
	return nil
}
