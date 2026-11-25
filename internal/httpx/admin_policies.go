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
	"github.com/inwinter04/omniflux/internal/dataplane"
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
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, name, version, strategy, constraints, weights, fallback_limit,
		       provider_timeout_ms, first_token_timeout_ms, status, created_at, updated_at
		FROM route_policies WHERE organization_id = $1 ORDER BY created_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_LIST_FAILED", "failed-to-query-route-policies", nil)
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
			adminError(w, http.StatusInternalServerError, "POLICY_LIST_FAILED", "failed-to-query-route-policies", nil)
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
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	var body createRoutePolicyRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "POLICY_NAME_REQUIRED", "enter-a-policy-name", nil)
		return
	}
	if body.Strategy == "" {
		body.Strategy = "balanced"
	}
	if !allowedStrategies[body.Strategy] {
		adminError(w, http.StatusBadRequest, "POLICY_STRATEGY_UNSUPPORTED", "unsupported-policy-type", nil)
		return
	}
	fallbackLimit := 2
	if body.FallbackLimit != nil {
		if *body.FallbackLimit < 0 {
			adminError(w, http.StatusBadRequest, "POLICY_INVALID", "fallback-limit-cannot-be-negative", nil)
			return
		}
		fallbackLimit = *body.FallbackLimit
	}
	providerTimeout := 60000
	if body.ProviderTimeoutMS != nil {
		if *body.ProviderTimeoutMS <= 0 {
			adminError(w, http.StatusBadRequest, "POLICY_INVALID", "provider-timeout-ms-must-be-greater", nil)
			return
		}
		providerTimeout = *body.ProviderTimeoutMS
	}
	firstTokenTimeout := 15000
	if body.FirstTokenTimeoutMS != nil {
		if *body.FirstTokenTimeoutMS <= 0 {
			adminError(w, http.StatusBadRequest, "POLICY_INVALID", "first-token-timeout-ms-must-be", nil)
			return
		}
		firstTokenTimeout = *body.FirstTokenTimeoutMS
	}
	constraints := body.Constraints
	if len(constraints) == 0 {
		constraints = json.RawMessage("{}")
	}
	if err := dataplane.ValidateConstraintsJSON(constraints); err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_CONSTRAINTS_INVALID", "policy-constraints-invalid", nil)
		return
	}
	weights := body.Weights
	if len(weights) == 0 {
		// strategy 是权重预设入口:未显式给 weights 时按策略类型写入对应预设。
		weights = json.RawMessage(strategyWeights(body.Strategy))
	}
	policyID := uuid.New()
	err := pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO route_policies(id, organization_id, name, strategy, constraints, weights,
			                           fallback_limit, provider_timeout_ms, first_token_timeout_ms)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`, policyID, principal.OrganizationID, body.Name, body.Strategy, constraints, weights,
			fallbackLimit, providerTimeout, firstTokenTimeout); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"route_policy.created", "route_policy", policyID.String(), nil, map[string]any{
				"name": body.Name, "strategy": body.Strategy, "constraints": json.RawMessage(constraints),
			})
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "route_policies_organization_id_name_key" {
			adminError(w, http.StatusConflict, "POLICY_NAME_EXISTS", "a-route-policy-with-the-same", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "POLICY_CREATE_FAILED", "failed-to-create-the-route-policy", nil)
		return
	}
	adminOK(w, map[string]string{"id": policyID.String()})
}

// DeleteRoutePolicy 删除路由策略;被模型服务引用或存在策略决策记录时拒绝。
func (d ServerDeps) DeleteRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	policyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "invalid-route-policy-id", nil)
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM route_policies WHERE id = $1 AND organization_id = $2)
	`, policyID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DELETE_FAILED", "failed-to-delete-the-route-policy", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
		return
	}
	var serviceCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM model_services WHERE route_policy_id = $1
	`, policyID).Scan(&serviceCount); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DELETE_FAILED", "failed-to-delete-the-route-policy", nil)
		return
	}
	if serviceCount > 0 {
		adminError(w, http.StatusConflict, "POLICY_IN_USE",
			"policy-in-use", nil)
		return
	}
	var decisionCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM policy_decisions WHERE route_policy_id = $1
	`, policyID).Scan(&decisionCount); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DELETE_FAILED", "failed-to-delete-the-route-policy", nil)
		return
	}
	if decisionCount > 0 {
		adminError(w, http.StatusConflict, "POLICY_HAS_DECISIONS",
			"policy-has-decisions", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		// 资源删除后已查不到名称,审计里记下它,审计列表才能显示“删的是什么”。
		var name string
		if err := tx.QueryRow(r.Context(), `DELETE FROM route_policies WHERE id = $1 RETURNING name`, policyID).Scan(&name); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"route_policy.deleted", "route_policy", policyID.String(), map[string]any{"name": name}, nil)
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DELETE_FAILED", "failed-to-delete-the-route-policy", nil)
		return
	}
	adminOK(w, nil)
}

type updateRoutePolicyRequest struct {
	Name                string          `json:"name"`
	Strategy            string          `json:"strategy"`
	FallbackLimit       *int            `json:"fallback_limit"`
	ProviderTimeoutMS   *int            `json:"provider_timeout_ms"`
	FirstTokenTimeoutMS *int            `json:"first_token_timeout_ms"`
	Constraints         json.RawMessage `json:"constraints"`
}

// UpdateRoutePolicy 更新策略名称、调度参数与硬约束;constraints 缺省时保留原值。
func (d ServerDeps) UpdateRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	var body updateRoutePolicyRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "POLICY_NAME_REQUIRED", "enter-a-policy-name", nil)
		return
	}
	if body.Strategy == "" {
		body.Strategy = "balanced"
	}
	if !allowedStrategies[body.Strategy] {
		adminError(w, http.StatusBadRequest, "POLICY_STRATEGY_UNSUPPORTED", "unsupported-policy-type", nil)
		return
	}
	if body.FallbackLimit != nil && *body.FallbackLimit < 0 {
		adminError(w, http.StatusBadRequest, "POLICY_INVALID", "fallback-limit-cannot-be-negative", nil)
		return
	}
	if body.ProviderTimeoutMS != nil && *body.ProviderTimeoutMS <= 0 {
		adminError(w, http.StatusBadRequest, "POLICY_INVALID", "provider-timeout-ms-must-be-greater", nil)
		return
	}
	if body.FirstTokenTimeoutMS != nil && *body.FirstTokenTimeoutMS <= 0 {
		adminError(w, http.StatusBadRequest, "POLICY_INVALID", "first-token-timeout-ms-must-be", nil)
		return
	}
	constraints := []byte(body.Constraints)
	if len(constraints) > 0 {
		if err := dataplane.ValidateConstraintsJSON(constraints); err != nil {
			adminError(w, http.StatusBadRequest, "POLICY_CONSTRAINTS_INVALID", "policy-constraints-invalid", nil)
			return
		}
	}
	policyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "invalid-route-policy-id", nil)
		return
	}
	var oldName, oldStrategy string
	var oldConstraints []byte
	var oldFallbackLimit, oldProviderTimeout, oldFirstTokenTimeout int
	err = d.Pool.QueryRow(r.Context(), `
		SELECT name, strategy, constraints, fallback_limit, provider_timeout_ms, first_token_timeout_ms
		FROM route_policies WHERE id = $1 AND organization_id = $2
	`, policyID, principal.OrganizationID).Scan(&oldName, &oldStrategy, &oldConstraints,
		&oldFallbackLimit, &oldProviderTimeout, &oldFirstTokenTimeout)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_UPDATE_FAILED", "failed-to-update-the-route-policy", nil)
		return
	}
	// 未提交的可选字段沿用原值(与 UPDATE 的 COALESCE 一致),审计快照记录生效后的值。
	newFallbackLimit, newProviderTimeout, newFirstTokenTimeout := oldFallbackLimit, oldProviderTimeout, oldFirstTokenTimeout
	if body.FallbackLimit != nil {
		newFallbackLimit = *body.FallbackLimit
	}
	if body.ProviderTimeoutMS != nil {
		newProviderTimeout = *body.ProviderTimeoutMS
	}
	if body.FirstTokenTimeoutMS != nil {
		newFirstTokenTimeout = *body.FirstTokenTimeoutMS
	}
	newConstraints := oldConstraints
	if len(constraints) > 0 {
		newConstraints = constraints
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `
			UPDATE route_policies SET
				name = $3, strategy = $4,
				weights = $5::jsonb,
				fallback_limit = COALESCE($6, fallback_limit),
				provider_timeout_ms = COALESCE($7, provider_timeout_ms),
				first_token_timeout_ms = COALESCE($8, first_token_timeout_ms),
				constraints = COALESCE($9::jsonb, constraints),
				version = version + 1, updated_at = now()
			WHERE id = $1 AND organization_id = $2
		`, policyID, principal.OrganizationID, body.Name, body.Strategy,
			json.RawMessage(strategyWeights(body.Strategy)),
			body.FallbackLimit, body.ProviderTimeoutMS, body.FirstTokenTimeoutMS, constraints)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		_, err = tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
			VALUES ($1,$2,'user','route_policy.updated','route_policy',$3,$4,$5)
		`, principal.OrganizationID, principal.UserID, policyID.String(),
			auditJSON(map[string]any{"name": oldName, "strategy": oldStrategy,
				"fallback_limit": oldFallbackLimit, "provider_timeout_ms": oldProviderTimeout,
				"first_token_timeout_ms": oldFirstTokenTimeout, "constraints": rawJSONOrEmpty(oldConstraints)}),
			auditJSON(map[string]any{"name": body.Name, "strategy": body.Strategy,
				"fallback_limit": newFallbackLimit, "provider_timeout_ms": newProviderTimeout,
				"first_token_timeout_ms": newFirstTokenTimeout, "constraints": rawJSONOrEmpty(newConstraints)}))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
		return
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "route_policies_organization_id_name_key" {
			adminError(w, http.StatusConflict, "POLICY_NAME_EXISTS", "a-route-policy-with-the-same", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "POLICY_UPDATE_FAILED", "failed-to-update-the-route-policy", nil)
		return
	}
	adminOK(w, nil)
}

func rawJSONOrEmpty(raw []byte) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	return json.RawMessage(raw)
}
