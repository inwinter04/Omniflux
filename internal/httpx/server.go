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
	"github.com/inwinter04/omniflux/internal/fx"
	"github.com/inwinter04/omniflux/internal/governance"
	"github.com/inwinter04/omniflux/internal/infra"
	"github.com/inwinter04/omniflux/internal/mail"
)

type ServerDeps struct {
	Logger          *slog.Logger
	Pool            *pgxpool.Pool
	Redis           *redis.Client
	Engine          *bifrostx.Engine
	FX              *fx.Service
	Plane           *dataplane.Deps
	Auth            *auth.Store
	Env             string
	MasterKey       []byte
	APIKeyPepper    string
	Mailer          mail.Sender
	Governance      *governance.Manager
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
	r.Get("/admin", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/admin/", http.StatusMovedPermanently)
	})
	r.Handle("/admin/*", console.Handler())

	// 数据面:OpenAI 兼容接口,由 API Key 认证。
	var onTransportError func(string)
	if deps.Governance != nil {
		onTransportError = deps.Governance.ReportTransportError
	}
	plane := dataplane.Deps{
		Logger:                deps.Logger,
		Pool:                  deps.Pool,
		Engine:                deps.Engine,
		FX:                    deps.FX,
		APIKeyPepper:          deps.APIKeyPepper,
		OnTransportError:      onTransportError,
		QueryUpstreamBalances: deps.queryAuthorizedUpstreamBalances,
	}
	plane.SetRateLimit(deps.Redis, deps.RateLimitPerMin)
	deps.Plane = &plane
	r.Route("/v1", func(r chi.Router) {
		r.Use(plane.RequireAPIKey)
		r.Use(plane.RateLimit)
		r.Get("/models", plane.ListModels)
		r.Get("/quota", plane.Quota)
		r.Get("/prompt-templates", plane.ListPromptTemplates)
		r.Get("/prompt-templates/{name}", plane.GetPromptTemplate)
		r.Post("/prompt-templates/{name}/render", plane.RenderPromptTemplate)
		r.Get("/agents", plane.ListAgents)
		r.Get("/agents/{name}", plane.GetAgent)
		r.Post("/agents/{name}/chat/completions", plane.ChatAgent)
		r.Post("/chat/completions", plane.ChatCompletions)
		r.Post("/responses", plane.Responses)
		r.Post("/responses/compact", plane.ResponsesCompact)
		r.Post("/messages", plane.AnthropicMessages)
		r.Post("/messages/count_tokens", plane.AnthropicCountTokens)
		r.Post("/embeddings", plane.Embeddings)
	})
	r.Route("/v1beta", func(r chi.Router) {
		r.Use(plane.RequireAPIKey)
		r.Use(plane.RateLimit)
		r.Get("/models", plane.GeminiListModels)
		r.Post("/models/*", plane.GeminiModelAction)
	})

	// 管理 API:服务端会话认证,组织隔离。
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/login", deps.Login)
		r.Get("/registration-options", deps.RegistrationOptions)
		r.Post("/register", deps.Register)
		r.Post("/resend-verification", deps.ResendEmailVerification)
		r.Post("/verify-email", deps.VerifyEmail)
		r.Post("/forgot-password", deps.ForgotPassword)
		r.Post("/reset-password", deps.ResetPassword)
		r.Post("/accept-invitation", deps.AcceptInvitation)
		r.Group(func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler { return RequireSession(deps, next) })
			r.Get("/me", deps.Me)
			r.Post("/logout", deps.Logout)
		})
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler { return RequireSession(deps, next) })
			r.Get("/dashboard", deps.Dashboard)
			r.Get("/system/settings", deps.SystemSettings)
			r.Patch("/system/settings", deps.UpdateSystemSettings)
			r.Get("/system/gateway-info", deps.GatewayInfo)
			r.Get("/system/email/settings", deps.EmailSettings)
			r.Patch("/system/email/settings", deps.UpdateEmailSettings)
			r.Post("/system/email/test", deps.TestEmailSettings)
			r.Get("/system/fx-rates", deps.FXRates)
			r.Patch("/system/fx-rates/settings", deps.UpdateFXSettings)
			r.Put("/system/fx-rates/{currency}", deps.UpdateFXRate)
			r.Post("/system/fx-rates/sync", deps.SyncFXRates)
			r.Get("/organizations", deps.Organizations)
			r.Post("/organizations", deps.CreateOrganization)
			r.Patch("/organizations/{id}", deps.UpdateOrganization)
			r.Get("/departments", deps.Departments)
			r.Post("/departments", deps.CreateDepartment)
			r.Patch("/departments/{id}", deps.UpdateDepartment)
			r.Delete("/departments/{id}", deps.DeleteDepartment)
			r.Get("/budgets", deps.Budgets)
			r.Post("/budgets", deps.CreateBudget)
			r.Patch("/budgets/{id}", deps.UpdateBudget)
			r.Delete("/budgets/{id}", deps.DeleteBudget)
			r.Get("/projects", deps.Projects)
			r.Patch("/projects/{id}", deps.UpdateProject)
			r.Patch("/projects/{id}/status", deps.UpdateProjectStatus)
			r.Post("/projects", deps.CreateProject)
			r.Delete("/projects/{id}", deps.DeleteProject)
			r.Get("/applications", deps.Applications)
			r.Patch("/applications/{id}", deps.UpdateApplication)
			r.Patch("/applications/{id}/status", deps.UpdateApplicationStatus)
			r.Post("/applications", deps.CreateApplication)
			r.Delete("/applications/{id}", deps.DeleteApplication)
			r.Get("/provider-catalog", deps.ProviderCatalog)
			r.Get("/proxies", deps.PlatformProxies)
			r.Post("/proxies", deps.CreatePlatformProxy)
			r.Get("/proxies/settings", deps.ProxyGovernanceSettings)
			r.Patch("/proxies/settings", deps.UpdateProxyGovernanceSettings)
			r.Get("/proxies/failover-events", deps.ProxyFailoverEvents)
			r.Put("/proxies/providers/{providerKey}/order", deps.UpdateProviderProxyOrder)
			r.Post("/proxies/providers/{providerKey}/switch", deps.SwitchProviderProxy)
			r.Post("/proxies/providers/{providerKey}/probe", deps.ProbeProviderProxies)
			r.Patch("/proxies/{id}", deps.UpdatePlatformProxy)
			r.Delete("/proxies/{id}", deps.DeletePlatformProxy)
			r.Post("/proxies/{id}/test", deps.TestPlatformProxy)
			r.Get("/provider-accounts", deps.ProviderAccounts)
			r.Get("/provider-accounts/availability", deps.ProviderAccountsAvailability)
			r.Post("/provider-accounts/test-all", deps.TestAllProviderAccounts)
			r.Post("/provider-accounts", deps.CreateProviderAccount)
			r.Patch("/provider-accounts/{id}", deps.UpdateProviderAccount)
			r.Post("/provider-accounts/{id}/test", deps.TestProviderAccount)
			r.Get("/provider-accounts/{id}/health-history", deps.ProviderAccountHealthHistory)
			r.Get("/provider-accounts/{id}/models", deps.ProviderModels)
			r.Patch("/provider-accounts/{id}/status", deps.UpdateProviderAccountStatus)
			r.Get("/provider-accounts/{id}/balance", deps.ProviderAccountBalance)
			r.Delete("/provider-accounts/{id}", deps.DeleteProviderAccount)
			r.Get("/provider-health/settings", deps.ProviderHealthSettings)
			r.Patch("/provider-health/settings", deps.UpdateProviderHealthSettings)
			r.Get("/route-policies", deps.RoutePolicies)
			r.Post("/route-policies", deps.CreateRoutePolicy)
			r.Post("/route-policies/simulate", deps.SimulateRoutePolicy)
			r.Patch("/route-policies/{id}", deps.UpdateRoutePolicy)
			r.Patch("/route-policies/{id}/status", deps.UpdateRoutePolicyStatus)
			r.Post("/route-policies/{id}/clone", deps.CloneRoutePolicy)
			r.Post("/route-policies/{id}/playground", deps.PlaygroundRoutePolicy)
			r.Get("/route-policies/{id}/overview", deps.RoutePolicyOverview)
			r.Get("/route-policies/{id}/metrics", deps.RoutePolicyMetrics)
			r.Get("/route-policies/{id}/decisions", deps.RoutePolicyDecisions)
			r.Delete("/route-policies/{id}", deps.DeleteRoutePolicy)
			r.Get("/model-services", deps.ModelServices)
			r.Post("/model-services", deps.CreateModelService)
			r.Patch("/model-services/{id}", deps.UpdateModelService)
			r.Patch("/model-services/{id}/status", deps.UpdateModelServiceStatus)
			r.Get("/model-candidates/{id}", deps.ModelCandidateLookup)
			r.Patch("/model-candidates/{id}/status", deps.UpdateModelCandidateStatus)
			r.Get("/model-services/{id}/grants", deps.ModelServiceGrants)
			r.Post("/model-services/{id}/grants", deps.UpsertModelServiceGrant)
			r.Delete("/model-services/{id}/grants/{grantID}", deps.RevokeModelServiceGrant)
			r.Delete("/model-services/{id}", deps.DeleteModelService)
			r.Get("/api-keys", deps.APIKeys)
			r.Get("/usage/summary", deps.UsageSummary)
			r.Get("/usage/trend", deps.UsageTrend)
			r.Get("/usage/records", deps.UsageRecords)
			r.Get("/usage/records/export", deps.UsageRecordsExport)
			r.Get("/audit-events", deps.AuditEvents)
			r.Get("/audit-events/{id}", deps.AuditEventDetail)
			r.Get("/assets", deps.Assets)
			r.Post("/assets", deps.CreateAsset)
			r.Get("/assets/{id}", deps.AssetDetail)
			r.Patch("/assets/{id}", deps.UpdateAsset)
			r.Post("/assets/{id}/publish", deps.PublishAsset)
			r.Post("/assets/{id}/rollback", deps.RollbackAsset)
			r.Patch("/assets/{id}/status", deps.UpdateAssetStatus)
			r.Get("/assets/{id}/versions/diff", deps.AssetVersionDiff)
			r.Get("/assets/{id}/versions/{versionID}", deps.AssetVersion)
			r.Post("/assets/{id}/render", deps.RenderAssetPreview)
			r.Post("/assets/{id}/run", deps.RunPromptAsset)
			r.Get("/assets/{id}/runs", deps.PromptRunHistory)
			r.Get("/assets/{id}/agent/sessions", deps.AgentSessions)
			r.Post("/assets/{id}/agent/chat", deps.RunAgent)
			r.Get("/assets/{id}/agent/runs", deps.AgentRunHistory)
			r.Get("/assets/{id}/agent/sessions/{sessionID}", deps.AgentSessionDetail)
			r.Delete("/assets/{id}/agent/sessions/{sessionID}", deps.DeleteAgentSession)
			r.Get("/assets/{id}/evaluation-sets", deps.PromptEvaluationSets)
			r.Post("/assets/{id}/evaluation-sets", deps.CreatePromptEvaluationSet)
			r.Patch("/assets/{id}/evaluation-sets/{setID}", deps.UpdatePromptEvaluationSet)
			r.Delete("/assets/{id}/evaluation-sets/{setID}", deps.DeletePromptEvaluationSet)
			r.Get("/evaluation-sets", deps.AllPromptEvaluationSets)
			r.Post("/assets/{id}/evaluate", deps.EvaluatePromptAsset)
			r.Get("/assets/{id}/grants", deps.AssetGrants)
			r.Post("/assets/{id}/grants", deps.UpsertAssetGrant)
			r.Delete("/assets/{id}/grants/{grantID}", deps.RevokeAssetGrant)
			r.Get("/users", deps.Users)
			r.Post("/users", deps.CreateUser)
			r.Patch("/users/{id}", deps.UpdateUser)
			r.Get("/users/{id}/permissions", deps.UserPermissions)
			r.Put("/users/{id}/permissions", deps.UpdateUserPermissions)
			r.Get("/users/invite-options", deps.InviteOptions)
			r.Post("/users/invite", deps.InviteUser)
			r.Post("/users/{id}/invitation/resend", deps.ResendUserInvitation)
			r.Delete("/users/{id}/invitation", deps.RevokeUserInvitation)
			r.Get("/permissions/catalog", deps.PermissionsCatalog)
			r.Get("/permission-groups", deps.PermissionGroups)
			r.Post("/permission-groups", deps.CreatePermissionGroup)
			r.Patch("/permission-groups/{id}", deps.UpdatePermissionGroup)
			r.Delete("/permission-groups/{id}", deps.DeletePermissionGroup)
			r.Post("/api-keys", deps.CreateAPIKey)
			r.Patch("/api-keys/{id}", deps.UpdateAPIKeyStatus)
			r.Delete("/api-keys/{id}", deps.DeleteAPIKey)
		})
	})
	return r
}
