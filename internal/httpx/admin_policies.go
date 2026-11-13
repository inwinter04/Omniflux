package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/inwinter04/omniflux/internal/auth"
)

type routePolicyResponse struct {
	ID                  string          `json:"id"`
	Name                string          `json:"name"`
	Version             int             `json:"version"`
	Strategy            string          `json:"strategy"`
	Constraints         json.RawMessage `json:"constraints"`
	Weights             json.RawMessage `json:"weights"`
	FallbackLimit       int             `json:"fallback_limit"`
	ProviderTimeoutMS   int             `json:"provider_timeout_ms"`
	FirstTokenTimeoutMS int             `json:"first_token_timeout_ms"`
	Status              string          `json:"status"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type createRoutePolicyRequest struct {
	Name                string          `json:"name"`
	Strategy            string          `json:"strategy"`
	Constraints         json.RawMessage `json:"constraints"`
	Weights             json.RawMessage `json:"weights"`
	FallbackLimit       *int            `json:"fallback_limit"`
	ProviderTimeoutMS   *int            `json:"provider_timeout_ms"`
	FirstTokenTimeoutMS *int            `json:"first_token_timeout_ms"`
}

var allowedStrategies = map[string]bool{"cost_first": true, "quality_first": true, "balanced": true}

// RoutePolicies 返回当前组织的路由策略列表。
func (d ServerDeps) RoutePolicies(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, name, version, strategy, constraints, weights, fallback_limit,
		       provider_timeout_ms, first_token_timeout_ms, status, created_at, updated_at
		FROM route_policies WHERE organization_id = $1 ORDER BY created_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_LIST_FAILED", "无法查询路由策略", nil)
		return
	}
	defer rows.Close()
	list := []routePolicyResponse{}
	for rows.Next() {
		var item routePolicyResponse
		var id uuid.UUID
		if err := rows.Scan(&id, &item.Name, &item.Version, &item.Strategy, &item.Constraints, &item.Weights,
			&item.FallbackLimit, &item.ProviderTimeoutMS, &item.FirstTokenTimeoutMS,
			&item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "POLICY_LIST_FAILED", "无法查询路由策略", nil)
			return
		}
		item.ID = id.String()
		list = append(list, item)
	}
	adminOK(w, list)
}

// CreateRoutePolicy 创建路由策略。评分字段当前由默认权重生效,创建时保存原始配置。
func (d ServerDeps) CreateRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	var body createRoutePolicyRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "POLICY_NAME_REQUIRED", "请填写策略名称", nil)
		return
	}
	if body.Strategy == "" {
		body.Strategy = "balanced"
	}
	if !allowedStrategies[body.Strategy] {
		adminError(w, http.StatusBadRequest, "POLICY_STRATEGY_UNSUPPORTED", "不支持的策略类型", nil)
		return
	}
	fallbackLimit := 2
	if body.FallbackLimit != nil {
		if *body.FallbackLimit < 0 {
			adminError(w, http.StatusBadRequest, "POLICY_INVALID", "fallback_limit 不能为负数", nil)
			return
		}
		fallbackLimit = *body.FallbackLimit
	}
	providerTimeout := 60000
	if body.ProviderTimeoutMS != nil {
		if *body.ProviderTimeoutMS <= 0 {
			adminError(w, http.StatusBadRequest, "POLICY_INVALID", "provider_timeout_ms 必须大于 0", nil)
			return
		}
		providerTimeout = *body.ProviderTimeoutMS
	}
	firstTokenTimeout := 15000
	if body.FirstTokenTimeoutMS != nil {
		if *body.FirstTokenTimeoutMS <= 0 {
			adminError(w, http.StatusBadRequest, "POLICY_INVALID", "first_token_timeout_ms 必须大于 0", nil)
			return
		}
		firstTokenTimeout = *body.FirstTokenTimeoutMS
	}
	constraints := body.Constraints
	if len(constraints) == 0 {
		constraints = json.RawMessage("{}")
	}
	weights := body.Weights
	if len(weights) == 0 {
		weights = json.RawMessage(`{"cost":0.4,"quality":0.3,"latency":0.15,"reliability":0.15}`)
	}
	policyID := uuid.New()
	if _, err := d.Pool.Exec(r.Context(), `
		INSERT INTO route_policies(id, organization_id, name, strategy, constraints, weights,
		                           fallback_limit, provider_timeout_ms, first_token_timeout_ms)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, policyID, principal.OrganizationID, body.Name, body.Strategy, constraints, weights,
		fallbackLimit, providerTimeout, firstTokenTimeout); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "route_policies_organization_id_name_key" {
			adminError(w, http.StatusConflict, "POLICY_NAME_EXISTS", "同名路由策略已存在", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "POLICY_CREATE_FAILED", "创建路由策略失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
		VALUES ($1,$2,'user','route_policy.created','route_policy',$3,$4)
	`, principal.OrganizationID, principal.UserID, policyID.String(), auditJSON(map[string]any{"name": body.Name, "strategy": body.Strategy}))
	adminOK(w, map[string]string{"id": policyID.String()})
}
