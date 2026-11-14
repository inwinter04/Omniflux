package dataplane

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

// Deps 是数据面处理器的依赖集合。
type Deps struct {
	Logger       *slog.Logger
	Pool         *pgxpool.Pool
	Engine       *bifrostx.Engine
	APIKeyPepper string

	rateLimiter *rateLimiter
}

// SetRateLimit 配置基础限流;limit<=0 或 Redis 不可用时禁用。
func (d *Deps) SetRateLimit(client *redis.Client, limit int) {
	d.rateLimiter = newRateLimiter(client, limit)
}

// apiError 输出 OpenAI 风格的错误 JSON。
func apiError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"type":    errType,
			"message": message,
			"code":    status,
		},
	})
}

// RateLimit 返回数据面限流中间件。
func (d Deps) RateLimit(next http.Handler) http.Handler {
	return d.rateLimiter.middleware(next)
}
