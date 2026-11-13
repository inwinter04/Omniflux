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
	KeyID          uuid.UUID
	OrganizationID uuid.UUID
	ProjectID      uuid.UUID
	ApplicationID  uuid.UUID
}

type apiKeyContextKey struct{}

func keyFromContext(ctx context.Context) (apiKeyContext, bool) {
	value, ok := ctx.Value(apiKeyContextKey{}).(apiKeyContext)
	return value, ok
}

// RequireAPIKey 校验 Authorization: Bearer <api key>。
// Key 是 keyed hash 存储,直接按 hash 等值查询;命中后检查状态与有效期并更新 last_used_at。
func (d Deps) RequireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			apiError(w, http.StatusUnauthorized, "missing_api_key", "缺少 API Key,请通过 Authorization: Bearer 请求头提供")
			return
		}
		full := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if !apikeys.ValidFormat(full) {
			apiError(w, http.StatusUnauthorized, "invalid_api_key", "API Key 格式不正确")
			return
		}
		hash := apikeys.Hash(d.APIKeyPepper, full)
		var ctxValue apiKeyContext
		var status string
		var expiresAt *time.Time
		err := d.Pool.QueryRow(r.Context(), `
			SELECT id, organization_id, project_id, application_id, status, expires_at
			FROM api_keys WHERE secret_hash = $1
		`, hash).Scan(&ctxValue.KeyID, &ctxValue.OrganizationID, &ctxValue.ProjectID, &ctxValue.ApplicationID, &status, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			apiError(w, http.StatusUnauthorized, "invalid_api_key", "API Key 不存在或已失效")
			return
		}
		if err != nil {
			apiError(w, http.StatusInternalServerError, "internal_error", "认证服务暂时不可用")
			return
		}
		if status != "active" {
			apiError(w, http.StatusUnauthorized, "invalid_api_key", "API Key 已被禁用")
			return
		}
		if expiresAt != nil && !expiresAt.After(time.Now()) {
			apiError(w, http.StatusUnauthorized, "invalid_api_key", "API Key 已过期")
			return
		}
		if _, err := d.Pool.Exec(r.Context(), `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, ctxValue.KeyID); err != nil {
			// last_used_at 是统计字段,更新失败不阻断请求,记日志保留真实错误。
			d.Logger.Warn("update api key last_used_at failed", "api_key_id", ctxValue.KeyID, "error", err)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyContextKey{}, ctxValue)))
	})
}
