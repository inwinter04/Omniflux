package dataplane

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimiter 用 Redis 固定窗口对每个 API Key 做基础限流。
// limit 是全局默认阈值;Key 自带 rate_limit_per_min 时按 Key 覆盖,两者都缺省则不限流。
type rateLimiter struct {
	redis *redis.Client
	limit int
}

func newRateLimiter(client *redis.Client, limit int) *rateLimiter {
	// Redis 不可用时不限流;全局默认为 0 也保留限流器,Key 级限流仍可生效。
	if client == nil {
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
		effective := l.limit
		if keyCtx.RateLimitPerMin > 0 {
			effective = keyCtx.RateLimitPerMin
		}
		if effective <= 0 {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		// Key 自带阈值优先,否则回落全局默认;两者都为 0 表示该 Key 不限流。
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
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(effective))
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(max64(int64(effective)-count, 0), 10))
		if count > int64(effective) {
			apiError(w, http.StatusTooManyRequests, "rate_limit_exceeded",
				fmt.Sprintf("请求超过限流阈值(%d 次/分钟),请稍后重试", effective))
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
