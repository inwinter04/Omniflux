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
	"github.com/inwinter04/omniflux/internal/dataplane"
	"github.com/inwinter04/omniflux/internal/infra"
)

type ServerDeps struct {
	Logger          *slog.Logger
	Pool            *pgxpool.Pool
	Redis           *redis.Client
	Engine          *bifrostx.Engine
	Auth            *auth.Store
	Env             string
	MasterKey       []byte
	APIKeyPepper    string
	ReloadEngine    func(context.Context) error
	RateLimitPerMin int
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

	// 数据面:OpenAI 兼容接口,由 API Key 认证。
	plane := dataplane.Deps{
		Logger:       deps.Logger,
		Pool:         deps.Pool,
		Engine:       deps.Engine,
		APIKeyPepper: deps.APIKeyPepper,
	}
	plane.SetRateLimit(deps.Redis, deps.RateLimitPerMin)
	r.Route("/v1", func(r chi.Router) {
		r.Use(plane.RequireAPIKey)
		r.Use(plane.RateLimit)
		r.Get("/models", plane.ListModels)
		r.Post("/chat/completions", plane.ChatCompletions)
		r.Post("/embeddings", plane.Embeddings)
	})

	// 管理 API:服务端会话认证,组织隔离。
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/login", deps.Login)
		r.Group(func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler { return RequireSession(deps, next) })
			r.Get("/me", deps.Me)
			r.Post("/logout", deps.Logout)
		})
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler { return RequireSession(deps, next) })
			r.Get("/departments", deps.Departments)
			r.Get("/budgets", deps.Budgets)
			r.Post("/budgets", deps.CreateBudget)
			r.Patch("/budgets/{id}", deps.UpdateBudget)
			r.Delete("/budgets/{id}", deps.DeleteBudget)
			r.Get("/projects", deps.Projects)
			r.Patch("/projects/{id}", deps.UpdateProject)
			r.Post("/projects", deps.CreateProject)
			r.Delete("/projects/{id}", deps.DeleteProject)
			r.Get("/applications", deps.Applications)
			r.Patch("/applications/{id}", deps.UpdateApplication)
			r.Post("/applications", deps.CreateApplication)
			r.Delete("/applications/{id}", deps.DeleteApplication)
			r.Get("/provider-accounts", deps.ProviderAccounts)
			r.Post("/provider-accounts", deps.CreateProviderAccount)
			r.Patch("/provider-accounts/{id}", deps.UpdateProviderAccount)
			r.Delete("/provider-accounts/{id}", deps.DeleteProviderAccount)
			r.Get("/route-policies", deps.RoutePolicies)
			r.Post("/route-policies", deps.CreateRoutePolicy)
			r.Patch("/route-policies/{id}", deps.UpdateRoutePolicy)
			r.Delete("/route-policies/{id}", deps.DeleteRoutePolicy)
			r.Get("/model-services", deps.ModelServices)
			r.Post("/model-services", deps.CreateModelService)
			r.Patch("/model-services/{id}", deps.UpdateModelService)
			r.Delete("/model-services/{id}", deps.DeleteModelService)
			r.Get("/api-keys", deps.APIKeys)
			r.Get("/usage/summary", deps.UsageSummary)
			r.Get("/usage/records", deps.UsageRecords)
			r.Get("/audit-events", deps.AuditEvents)
			r.Post("/api-keys", deps.CreateAPIKey)
			r.Patch("/api-keys/{id}", deps.UpdateAPIKeyStatus)
			r.Delete("/api-keys/{id}", deps.DeleteAPIKey)
		})
	})
	return r
}

