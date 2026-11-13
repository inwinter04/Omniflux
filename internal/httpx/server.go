package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/console"
	"github.com/inwinter04/omniflux/internal/infra"
)

type ServerDeps struct {
	Logger *slog.Logger
	Pool   *pgxpool.Pool
	Redis  *redis.Client
	Engine *bifrostx.Engine
	Auth   *auth.Store
	Env    string
}

func New(deps ServerDeps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			start := time.Now()
			sw := middleware.NewWrapResponseWriter(w, req.ProtoMajor)
			next.ServeHTTP(sw, req)
			deps.Logger.Info("http request", "method", req.Method, "path", req.URL.Path, "status", sw.Status(), "duration_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(req.Context()))
		})
	})
	r.Get("/livez", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), time.Second)
		defer cancel()
		if err := deps.Pool.Ping(ctx); err != nil {
			JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "database unavailable"})
			return
		}
		if err := infra.CheckRedis(ctx, deps.Redis); err != nil {
			JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "redis unavailable"})
			return
		}
		JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	r.Handle("/metrics", promhttp.Handler())
	r.Get("/admin", func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, nil, "/admin/", http.StatusMovedPermanently)
	})
	r.Handle("/admin/*", console.Handler())
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/login", deps.Login)
		r.Group(func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler { return RequireSession(deps, next) })
			r.Get("/me", deps.Me)
			r.Post("/logout", deps.Logout)
		})
	})
	return r
}

func writeJSON(w http.ResponseWriter, status int, body any) { JSON(w, status, body) }
