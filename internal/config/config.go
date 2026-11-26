package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/inwinter04/omniflux/internal/auth"
)

type Config struct {
	Env               string
	HTTPAddr          string
	DatabaseURL       string
	RedisAddr         string
	BootstrapEmail    string
	BootstrapPassword string
	MasterKey         []byte
	APIKeyPepper      string
	OTLTEndpoint      string
	RateLimitPerMin   int
}

func Load() (Config, error) {
	c := Config{
		Env:               getenv("OMNIFLUX_ENV", "development"),
		HTTPAddr:          getenv("OMNIFLUX_HTTP_ADDR", "127.0.0.1:8080"),
		DatabaseURL:       os.Getenv("OMNIFLUX_DATABASE_URL"),
		RedisAddr:         getenv("OMNIFLUX_REDIS_ADDR", "localhost:6379"),
		BootstrapEmail:    strings.ToLower(strings.TrimSpace(os.Getenv("OMNIFLUX_BOOTSTRAP_EMAIL"))),
		BootstrapPassword: os.Getenv("OMNIFLUX_BOOTSTRAP_PASSWORD"),
		APIKeyPepper:      os.Getenv("OMNIFLUX_API_KEY_PEPPER"),
		OTLTEndpoint:      os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		RateLimitPerMin:   getenvInt("OMNIFLUX_RATE_LIMIT_PER_MIN", 60),
	}
	masterKey, err := base64.StdEncoding.DecodeString(os.Getenv("OMNIFLUX_MASTER_KEY"))
	if err != nil {
		return c, fmt.Errorf("decode OMNIFLUX_MASTER_KEY: %w", err)
	}
	if len(masterKey) != 32 {
		return c, errors.New("OMNIFLUX_MASTER_KEY must decode to exactly 32 bytes")
	}
	c.MasterKey = masterKey
	if c.DatabaseURL == "" {
		return c, errors.New("OMNIFLUX_DATABASE_URL is required")
	}
	if len(c.APIKeyPepper) < 32 {
		return c, errors.New("OMNIFLUX_API_KEY_PEPPER must be at least 32 characters")
	}
	if c.BootstrapEmail == "" || !strings.Contains(c.BootstrapEmail, "@") {
		return c, errors.New("OMNIFLUX_BOOTSTRAP_EMAIL is invalid")
	}
	if err := auth.ValidatePassword(c.BootstrapPassword); err != nil {
		return c, errors.New("OMNIFLUX_BOOTSTRAP_PASSWORD must be 8-128 characters and include both letters and numbers")
	}
	return c, nil
}

func getenv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func getenvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
