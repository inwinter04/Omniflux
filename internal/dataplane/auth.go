package dataplane

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/apikeys"
)

// apiKeyContext 是通过 API Key 认证后附到请求上下文上的调用方信息。
type apiKeyContext struct {
	RateLimitPerMin int
	KeyID           uuid.UUID
	OrganizationID  uuid.UUID
	ProjectID       uuid.UUID
	ApplicationID   uuid.UUID
}

type apiKeyContextKey struct{}

func keyFromContext(ctx context.Context) (apiKeyContext, bool) {
	value, ok := ctx.Value(apiKeyContextKey{}).(apiKeyContext)
	return value, ok
}

// RequireAPIKey 校验 Authorization: Bearer <api key>。
// Key 是 keyed hash 存储,直接按 hash 等值查询;命中后检查 Key 的状态与有效期,
// 以及 Key 所属项目、应用是否停用,通过后更新 last_used_at。
func (d Deps) RequireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			apiError(w, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
			return
		}
		full := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if !apikeys.ValidFormat(full) {
			apiError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid API key format.", apiErrorOptions{Reason: "invalid_api_key_format"})
			return
		}
		hash := apikeys.Hash(d.APIKeyPepper, full)
		var ctxValue apiKeyContext
		var status, projectStatus, applicationStatus string
		var expiresAt *time.Time
		err := d.Pool.QueryRow(r.Context(), `
			SELECT k.id, k.organization_id, k.project_id, k.application_id, COALESCE(k.rate_limit_per_min, 0),
			       k.status, k.expires_at, p.status, a.status
			FROM api_keys k
			JOIN projects p ON p.id = k.project_id
			JOIN applications a ON a.id = k.application_id
			WHERE k.secret_hash = $1 AND k.deleted_at IS NULL
		`, hash).Scan(&ctxValue.KeyID, &ctxValue.OrganizationID, &ctxValue.ProjectID, &ctxValue.ApplicationID,
			&ctxValue.RateLimitPerMin, &status, &expiresAt, &projectStatus, &applicationStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			apiError(w, http.StatusUnauthorized, "invalid_api_key", "API key does not exist or has expired.")
			return
		}
		if err != nil {
			apiError(w, http.StatusInternalServerError, "internal_error", "Authentication service is temporarily unavailable.")
			return
		}
		if status != "active" {
			apiError(w, http.StatusUnauthorized, "invalid_api_key", "API key is disabled.", apiErrorOptions{Reason: "api_key_disabled"})
			return
		}
		if expiresAt != nil && !expiresAt.After(time.Now()) {
			apiError(w, http.StatusUnauthorized, "invalid_api_key", "API key has expired.", apiErrorOptions{Reason: "api_key_expired"})
			return
		}
		// 项目或应用被停用后,名下 Key 一并停止调用,重新启用即恢复;在更新 last_used_at 之前拦截。
		if projectStatus != "active" {
			apiError(w, http.StatusForbidden, "project_disabled", "The project that owns this API key is disabled.", apiErrorOptions{Reason: "project_disabled"})
			return
		}
		if applicationStatus != "active" {
			apiError(w, http.StatusForbidden, "application_disabled", "The application that owns this API key is disabled.", apiErrorOptions{Reason: "application_disabled"})
			return
		}
		if _, err := d.Pool.Exec(r.Context(), `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, ctxValue.KeyID); err != nil {
			// last_used_at 是统计字段,更新失败不阻断请求,记日志保留真实错误。
			d.Logger.Warn("update api key last_used_at failed", "api_key_id", ctxValue.KeyID, "error", err)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyContextKey{}, ctxValue)))
	})
}
