package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// strategyWeights 把策略类型映射为评分权重预设;调度读取 weights,strategy 是语义入口。
func strategyWeights(strategy string) string {
	switch strategy {
	case "cost_first":
		return `{"cost":0.7,"quality":0.1,"latency":0.1,"reliability":0.1}`
	case "quality_first":
		return `{"cost":0.1,"quality":0.7,"latency":0.1,"reliability":0.1}`
	default:
		return `{"cost":0.4,"quality":0.3,"latency":0.15,"reliability":0.15}`
	}
}

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
		// strategy 是权重预设入口:未显式给 weights 时按策略类型写入对应预设。
		weights = json.RawMessage(strategyWeights(body.Strategy))
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

// DeleteRoutePolicy 删除路由策略;被模型服务引用或存在策略决策记录时拒绝。
func (d ServerDeps) DeleteRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	policyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "路由策略 ID 不合法", nil)
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM route_policies WHERE id = $1 AND organization_id = $2)
	`, policyID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DELETE_FAILED", "删除路由策略失败", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "路由策略不存在或不属于当前组织", nil)
		return
	}
	var serviceCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM model_services WHERE route_policy_id = $1
	`, policyID).Scan(&serviceCount); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DELETE_FAILED", "删除路由策略失败", nil)
		return
	}
	if serviceCount > 0 {
		adminError(w, http.StatusConflict, "POLICY_IN_USE",
			"路由策略已被模型服务引用,请先调整模型服务后再删除", nil)
		return
	}
	var decisionCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM policy_decisions WHERE route_policy_id = $1
	`, policyID).Scan(&decisionCount); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DELETE_FAILED", "删除路由策略失败", nil)
		return
	}
	if decisionCount > 0 {
		adminError(w, http.StatusConflict, "POLICY_HAS_DECISIONS",
			"路由策略存在决策记录,为保留审计数据无法删除", nil)
		return
	}
	if _, err := d.Pool.Exec(r.Context(), `DELETE FROM route_policies WHERE id = $1`, policyID); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DELETE_FAILED", "删除路由策略失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id)
		VALUES ($1,$2,'user','route_policy.deleted','route_policy',$3)
	`, principal.OrganizationID, principal.UserID, policyID.String())
	adminOK(w, nil)
}

type updateRoutePolicyRequest struct {
	Name                string `json:"name"`
	Strategy            string `json:"strategy"`
	FallbackLimit       *int   `json:"fallback_limit"`
	ProviderTimeoutMS   *int   `json:"provider_timeout_ms"`
	FirstTokenTimeoutMS *int   `json:"first_token_timeout_ms"`
}

// UpdateRoutePolicy 更新路由策略名称与调度参数;weights/constraints 仍按服务端默认生效。
func (d ServerDeps) UpdateRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	var body updateRoutePolicyRequest
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
	if body.FallbackLimit != nil && *body.FallbackLimit < 0 {
		adminError(w, http.StatusBadRequest, "POLICY_INVALID", "fallback_limit 不能为负数", nil)
		return
	}
	if body.ProviderTimeoutMS != nil && *body.ProviderTimeoutMS <= 0 {
		adminError(w, http.StatusBadRequest, "POLICY_INVALID", "provider_timeout_ms 必须大于 0", nil)
		return
	}
	if body.FirstTokenTimeoutMS != nil && *body.FirstTokenTimeoutMS <= 0 {
		adminError(w, http.StatusBadRequest, "POLICY_INVALID", "first_token_timeout_ms 必须大于 0", nil)
		return
	}
	policyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "路由策略 ID 不合法", nil)
		return
	}
	var oldName string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT name FROM route_policies WHERE id = $1 AND organization_id = $2
	`, policyID, principal.OrganizationID).Scan(&oldName)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "路由策略不存在或不属于当前组织", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_UPDATE_FAILED", "更新路由策略失败", nil)
		return
	}
	tag, err := d.Pool.Exec(r.Context(), `
		UPDATE route_policies SET
			name = $3, strategy = $4,
			weights = $5::jsonb,
			fallback_limit = COALESCE($6, fallback_limit),
			provider_timeout_ms = COALESCE($7, provider_timeout_ms),
			first_token_timeout_ms = COALESCE($8, first_token_timeout_ms),
			version = version + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2
	`, policyID, principal.OrganizationID, body.Name, body.Strategy,
		json.RawMessage(strategyWeights(body.Strategy)),
		body.FallbackLimit, body.ProviderTimeoutMS, body.FirstTokenTimeoutMS)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "route_policies_organization_id_name_key" {
			adminError(w, http.StatusConflict, "POLICY_NAME_EXISTS", "同名路由策略已存在", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "POLICY_UPDATE_FAILED", "更新路由策略失败", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "路由策略不存在或不属于当前组织", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
		VALUES ($1,$2,'user','route_policy.updated','route_policy',$3,$4,$5)
	`, principal.OrganizationID, principal.UserID, policyID.String(),
		auditJSON(map[string]any{"name": oldName}),
		auditJSON(map[string]any{"name": body.Name, "strategy": body.Strategy,
			"fallback_limit": body.FallbackLimit, "provider_timeout_ms": body.ProviderTimeoutMS,
			"first_token_timeout_ms": body.FirstTokenTimeoutMS}))
	adminOK(w, nil)
}

