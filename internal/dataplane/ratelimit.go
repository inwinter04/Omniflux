package dataplane

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimiter 用 Redis 固定窗口对每个 API Key 做基础限流;limit<=0 表示不限流。
type rateLimiter struct {
	redis *redis.Client
	limit int
}

func newRateLimiter(client *redis.Client, limit int) *rateLimiter {
	if client == nil || limit <= 0 {
		return nil
	}
	return &rateLimiter{redis: client, limit: limit}
}

// middleware 返回限流中间件;Redis 不可用时放行,避免基础设施故障放大为全量拒绝。
func (l *rateLimiter) middleware(next http.Handler) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keyCtx, ok := keyFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		window := time.Now().Unix() / 60
		key := fmt.Sprintf("omniflux:rl:%s:%d", keyCtx.KeyID, window)
		count, err := l.redis.Incr(ctx, key).Result()
		if err != nil {
			// 限流器故障不阻断请求,如实记录日志由调用方排查。
			next.ServeHTTP(w, r)
			return
		}
		if count == 1 {
			l.redis.Expire(ctx, key, 2*time.Minute)
		}
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(l.limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(max64(int64(l.limit)-count, 0), 10))
		if count > int64(l.limit) {
			apiError(w, http.StatusTooManyRequests, "rate_limit_exceeded",
				fmt.Sprintf("请求超过限流阈值(%d 次/分钟),请稍后重试", l.limit))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

