package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const attemptTrailFallbackSQL = `(
	CASE
		WHEN jsonb_typeof(pd.attempt_trail) = 'array' THEN jsonb_array_length(pd.attempt_trail)
		WHEN jsonb_typeof(pd.attempt_trail -> 'attempts') = 'array' THEN jsonb_array_length(pd.attempt_trail -> 'attempts')
		ELSE 0
	END > 1
	OR (pd.selected_candidate_id IS NOT NULL AND u.model_candidate_id IS NOT NULL AND u.model_candidate_id <> pd.selected_candidate_id)
)`

type boundModelServiceSummary struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	CandidateCount int    `json:"candidate_count"`
}

type routePolicyResponse struct {
	ID                  string                     `json:"id"`
	Name                string                     `json:"name"`
	Version             int                        `json:"version"`
	Strategy            string                     `json:"strategy"`
	CompareCurrency     string                     `json:"compare_currency"`
	Constraints         json.RawMessage            `json:"constraints"`
	Weights             json.RawMessage            `json:"weights"`
	Rules               json.RawMessage            `json:"rules"`
	RetryPolicy         json.RawMessage            `json:"retry_policy"`
	CircuitBreaker      json.RawMessage            `json:"circuit_breaker"`
	SessionAffinity     json.RawMessage            `json:"session_affinity"`
	BaselineCandidateID *string                    `json:"baseline_candidate_id"`
	FallbackLimit       int                        `json:"fallback_limit"`
	ProviderTimeoutMS   int                        `json:"provider_timeout_ms"`
	FirstTokenTimeoutMS int                        `json:"first_token_timeout_ms"`
	Status              string                     `json:"status"`
	BoundServices       []boundModelServiceSummary `json:"bound_services"`
	Requests24h         int64                      `json:"requests_24h"`
	SuccessRate24h      *float64                   `json:"success_rate_24h"`
	FallbackRate24h     *float64                   `json:"fallback_rate_24h"`
	AvgTTFTMS24h        *float64                   `json:"avg_ttft_ms_24h"`
	AvgLatencyMS24h     *float64                   `json:"avg_latency_ms_24h"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
}

type createRoutePolicyRequest struct {
	Name                string          `json:"name"`
	Strategy            string          `json:"strategy"`
	CompareCurrency     string          `json:"compare_currency"`
	Constraints         json.RawMessage `json:"constraints"`
	Weights             json.RawMessage `json:"weights"`
	Rules               json.RawMessage `json:"rules"`
	RetryPolicy         json.RawMessage `json:"retry_policy"`
	CircuitBreaker      json.RawMessage `json:"circuit_breaker"`
	SessionAffinity     json.RawMessage `json:"session_affinity"`
	BaselineCandidateID *string         `json:"baseline_candidate_id"`
	FallbackLimit       *int            `json:"fallback_limit"`
	ProviderTimeoutMS   *int            `json:"provider_timeout_ms"`
	FirstTokenTimeoutMS *int            `json:"first_token_timeout_ms"`
}

type updateRoutePolicyRequest = createRoutePolicyRequest

type cloneRoutePolicyRequest struct {
	Name string `json:"name"`
}

func isPresetWeightStrategy(strategy string) bool {
	switch strings.TrimSpace(strategy) {
	case dataplane.StrategyBalanced,
		dataplane.StrategyCostFirst,
		dataplane.StrategyQualityFirst,
		dataplane.StrategyLatencyFirst,
		dataplane.StrategyReliabilityFirst:
		return true
	default:
		return false
	}
}

func (d ServerDeps) validateBaselineCandidate(ctx context.Context, orgID uuid.UUID, raw *string) (*uuid.UUID, error) {
	if raw == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return nil, errors.New("invalid baseline candidate id")
	}
	var exists bool
	if err := d.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM model_candidates c
			JOIN model_services s ON s.id = c.model_service_id
			WHERE c.id = $1 AND s.organization_id = $2 AND c.retired_at IS NULL
		)
	`, parsed, orgID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("baseline candidate not found")
	}
	return &parsed, nil
}

func validateRoutePolicyJSONFields(w http.ResponseWriter, weights, constraints, rules, retryPolicy, circuitBreaker, sessionAffinity json.RawMessage) bool {
	if len(weights) > 0 && string(weights) != "null" {
		if _, err := dataplane.ValidateWeightsJSON(weights); err != nil {
			adminError(w, http.StatusBadRequest, "POLICY_WEIGHTS_INVALID", "invalid-route-policy-weights", nil, err.Error())
			return false
		}
	}
	if len(constraints) > 0 && string(constraints) != "null" {
		if err := dataplane.ValidateConstraintsJSON(constraints); err != nil {
			adminError(w, http.StatusBadRequest, "POLICY_CONSTRAINTS_INVALID", "policy-constraints-invalid", nil, err.Error())
			return false
		}
	}
	if len(rules) > 0 && string(rules) != "null" {
		if err := dataplane.ValidateRulesJSON(rules); err != nil {
			adminError(w, http.StatusBadRequest, "POLICY_RULES_INVALID", "invalid-route-policy-rules", nil, err.Error())
			return false
		}
	}
	if len(retryPolicy) > 0 && string(retryPolicy) != "null" {
		if err := dataplane.ValidateRetryPolicyJSON(retryPolicy); err != nil {
			adminError(w, http.StatusBadRequest, "POLICY_RETRY_INVALID", "invalid-route-policy-retry-policy", nil, err.Error())
			return false
		}
	}
	if len(circuitBreaker) > 0 && string(circuitBreaker) != "null" {
		if err := dataplane.ValidateCircuitBreakerJSON(circuitBreaker); err != nil {
			adminError(w, http.StatusBadRequest, "POLICY_BREAKER_INVALID", "invalid-route-policy-circuit-breaker", nil, err.Error())
			return false
		}
	}
	if len(sessionAffinity) > 0 && string(sessionAffinity) != "null" {
		if err := dataplane.ValidateSessionAffinityJSON(sessionAffinity); err != nil {
			adminError(w, http.StatusBadRequest, "POLICY_SESSION_AFFINITY_INVALID", "invalid-route-policy-session-affinity", nil, err.Error())
			return false
		}
	}
	return true
}

func validateRoutePolicyLimits(w http.ResponseWriter, fallbackLimit, providerTimeoutMS, firstTokenTimeoutMS *int) bool {
	if fallbackLimit != nil {
		if *fallbackLimit < 0 {
			adminError(w, http.StatusBadRequest, "POLICY_INVALID", "fallback-limit-cannot-be-negative", nil)
			return false
		}
		if *fallbackLimit > 20 {
			adminError(w, http.StatusBadRequest, "POLICY_INVALID", "invalid-route-policy-fallback-limit", nil)
			return false
		}
	}
	if providerTimeoutMS != nil && *providerTimeoutMS < 1000 {
		adminError(w, http.StatusBadRequest, "POLICY_INVALID", "provider-timeout-ms-must-be-greater", nil)
		return false
	}
	if firstTokenTimeoutMS != nil && *firstTokenTimeoutMS < 0 {
		adminError(w, http.StatusBadRequest, "POLICY_INVALID", "first-token-timeout-ms-must-be", nil)
		return false
	}
	return true
}

// RoutePolicies 返回当前组织的路由策略列表，附带绑定模型服务与 24h 运行指标。
func (d ServerDeps) RoutePolicies(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermRoutePolicyRead) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, name, version, strategy, compare_currency, constraints, weights,
		       rules, retry_policy, circuit_breaker, session_affinity, baseline_candidate_id,
		       fallback_limit, provider_timeout_ms, first_token_timeout_ms,
		       status, created_at, updated_at
		FROM route_policies
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_LIST_FAILED", "failed-to-query-route-policies", nil, err.Error())
		return
	}
	defer rows.Close()

	list := []routePolicyResponse{}
	indexByID := map[uuid.UUID]int{}
	for rows.Next() {
		var item routePolicyResponse
		var id uuid.UUID
		var baselineID *uuid.UUID
		if err := rows.Scan(&id, &item.Name, &item.Version, &item.Strategy, &item.CompareCurrency,
			&item.Constraints, &item.Weights, &item.Rules, &item.RetryPolicy, &item.CircuitBreaker, &item.SessionAffinity, &baselineID,
			&item.FallbackLimit, &item.ProviderTimeoutMS, &item.FirstTokenTimeoutMS,
			&item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "POLICY_LIST_FAILED", "failed-to-query-route-policies", nil, err.Error())
			return
		}
		item.ID = id.String()
		if baselineID != nil {
			s := baselineID.String()
			item.BaselineCandidateID = &s
		}
		item.BoundServices = []boundModelServiceSummary{}
		list = append(list, item)
		indexByID[id] = len(list) - 1
	}
	if err := rows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_LIST_FAILED", "failed-to-query-route-policies", nil, err.Error())
		return
	}
	if len(list) == 0 {
		adminOK(w, list)
		return
	}

	svcRows, err := d.Pool.Query(r.Context(), `
		SELECT ms.id, ms.name, ms.status, ms.route_policy_id,
		       count(mc.id) FILTER (WHERE mc.retired_at IS NULL AND mc.status = 'active')::int AS candidate_count
		FROM model_services ms
		LEFT JOIN model_candidates mc ON mc.model_service_id = ms.id
		WHERE ms.organization_id = $1 AND ms.route_policy_id IS NOT NULL
		GROUP BY ms.id, ms.name, ms.status, ms.route_policy_id
		ORDER BY ms.name ASC
	`, principal.OrganizationID)
	if err == nil {
		defer svcRows.Close()
		for svcRows.Next() {
			var svcID, policyID uuid.UUID
			var summary boundModelServiceSummary
			if scanErr := svcRows.Scan(&svcID, &summary.Name, &summary.Status, &policyID, &summary.CandidateCount); scanErr == nil {
				summary.ID = svcID.String()
				if idx, exists := indexByID[policyID]; exists {
					list[idx].BoundServices = append(list[idx].BoundServices, summary)
				}
			}
		}
	}

	statRows, err := d.Pool.Query(r.Context(), fmt.Sprintf(`
		SELECT u.route_policy_id,
		       count(*)::bigint AS total_reqs,
		       avg(CASE WHEN u.state = 'success' THEN 1.0 ELSE 0.0 END)::float8 AS success_rate,
		       avg(CASE WHEN u.state = 'success' AND %s THEN 1.0 ELSE 0.0 END)::float8 AS fallback_rate,
		       avg(u.first_token_ms) FILTER (WHERE u.state = 'success' AND u.first_token_ms IS NOT NULL)::float8 AS avg_ttft_ms,
		       avg(u.total_ms) FILTER (WHERE u.state = 'success')::float8 AS avg_latency_ms
		FROM usage_records u
		LEFT JOIN policy_decisions pd ON pd.request_id = u.request_id
		WHERE u.organization_id = $1
		  AND u.route_policy_id IS NOT NULL
		  AND u.state <> 'pending'
		  AND u.created_at >= now() - interval '24 hours'
		GROUP BY u.route_policy_id
	`, attemptTrailFallbackSQL), principal.OrganizationID)
	if err == nil {
		defer statRows.Close()
		for statRows.Next() {
			var policyID uuid.UUID
			var total int64
			var succRate, fbRate, avgTTFT, avgLat *float64
			if scanErr := statRows.Scan(&policyID, &total, &succRate, &fbRate, &avgTTFT, &avgLat); scanErr == nil {
				if idx, exists := indexByID[policyID]; exists {
					list[idx].Requests24h = total
					list[idx].SuccessRate24h = succRate
					list[idx].FallbackRate24h = fbRate
					list[idx].AvgTTFTMS24h = avgTTFT
					list[idx].AvgLatencyMS24h = avgLat
				}
			}
		}
	}

	adminOK(w, list)
}

// CreateRoutePolicy 创建路由策略（支持 9 种调度模式、权重滑块、条件规则、韧性与熔断配置）。
func (d ServerDeps) CreateRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermRoutePolicyManage) != nil {
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
		body.Strategy = dataplane.StrategyBalanced
	}
	if !dataplane.IsAllowedStrategy(body.Strategy) {
		adminError(w, http.StatusBadRequest, "POLICY_STRATEGY_UNSUPPORTED", "unsupported-policy-type", nil)
		return
	}
	compareCurrency, err := dataplane.NormalizeCurrencyCode(body.CompareCurrency)
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_COMPARE_CURRENCY_INVALID", "invalid-route-policy-compare-currency", nil, err.Error())
		return
	}
	if !validateRoutePolicyLimits(w, body.FallbackLimit, body.ProviderTimeoutMS, body.FirstTokenTimeoutMS) {
		return
	}
	if !validateRoutePolicyJSONFields(w, body.Weights, body.Constraints, body.Rules, body.RetryPolicy, body.CircuitBreaker, body.SessionAffinity) {
		return
	}
	baselineUUID, err := d.validateBaselineCandidate(r.Context(), principal.OrganizationID, body.BaselineCandidateID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_BASELINE_INVALID", "invalid-route-policy-baseline-candidate", nil, err.Error())
		return
	}

	fallbackLimit := 3
	if body.FallbackLimit != nil {
		fallbackLimit = *body.FallbackLimit
	}
	providerTimeout := 60000
	if body.ProviderTimeoutMS != nil {
		providerTimeout = *body.ProviderTimeoutMS
	}
	firstTokenTimeout := 15000
	if body.FirstTokenTimeoutMS != nil {
		firstTokenTimeout = *body.FirstTokenTimeoutMS
	}

	constraints := jsonOrDefault(body.Constraints, json.RawMessage("{}"))
	weights := body.Weights
	if len(weights) == 0 || string(weights) == "null" {
		weights = dataplane.MarshalPresetWeights(body.Strategy)
	}
	rules := jsonOrDefault(body.Rules, json.RawMessage("[]"))
	retryPolicy := body.RetryPolicy
	if len(retryPolicy) == 0 || string(retryPolicy) == "null" {
		b, _ := json.Marshal(dataplane.DefaultRetryPolicy())
		retryPolicy = b
	}
	circuitBreaker := body.CircuitBreaker
	if len(circuitBreaker) == 0 || string(circuitBreaker) == "null" {
		b, _ := json.Marshal(dataplane.DefaultCircuitBreakerPolicy())
		circuitBreaker = b
	}
	sessionAffinity := body.SessionAffinity
	if len(sessionAffinity) == 0 || string(sessionAffinity) == "null" {
		b, _ := json.Marshal(dataplane.DefaultSessionAffinityPolicy())
		sessionAffinity = b
	}

	policyID := uuid.New()
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO route_policies(
				id, organization_id, name, strategy, compare_currency,
				constraints, weights, rules, retry_policy, circuit_breaker, session_affinity,
				baseline_candidate_id, fallback_limit, provider_timeout_ms, first_token_timeout_ms
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		`, policyID, principal.OrganizationID, body.Name, body.Strategy, compareCurrency,
			constraints, weights, rules, retryPolicy, circuitBreaker, sessionAffinity,
			nullablePolicyID(baselineUUID), fallbackLimit, providerTimeout, firstTokenTimeout); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"route_policy.created", "route_policy", policyID.String(), nil, map[string]any{
				"name": body.Name, "strategy": body.Strategy, "compare_currency": compareCurrency,
				"weights": json.RawMessage(weights), "constraints": json.RawMessage(constraints),
				"rules": json.RawMessage(rules), "retry_policy": json.RawMessage(retryPolicy),
				"circuit_breaker": json.RawMessage(circuitBreaker), "session_affinity": json.RawMessage(sessionAffinity),
			})
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "route_policies_organization_id_name_key" {
			adminError(w, http.StatusConflict, "POLICY_NAME_EXISTS", "a-route-policy-with-the-same", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "POLICY_CREATE_FAILED", "failed-to-create-the-route-policy", nil, err.Error())
		return
	}
	adminOK(w, map[string]string{"id": policyID.String()})
}

// DeleteRoutePolicy 删除路由策略;被模型服务引用或存在策略决策记录时拒绝。
func (d ServerDeps) DeleteRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermRoutePolicyManage) != nil {
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
		adminError(w, http.StatusConflict, "POLICY_IN_USE", "policy-in-use", nil)
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
		adminError(w, http.StatusConflict, "POLICY_HAS_DECISIONS", "policy-has-decisions", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
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

// UpdateRoutePolicy 更新策略，未传入的 weights/rules/constraints/retry/circuit 字段保留原值并自增版本号。
func (d ServerDeps) UpdateRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermRoutePolicyManage) != nil {
		return
	}
	policyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "invalid-route-policy-id", nil)
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
	if !validateRoutePolicyLimits(w, body.FallbackLimit, body.ProviderTimeoutMS, body.FirstTokenTimeoutMS) {
		return
	}
	if !validateRoutePolicyJSONFields(w, body.Weights, body.Constraints, body.Rules, body.RetryPolicy, body.CircuitBreaker, body.SessionAffinity) {
		return
	}

	var oldName, oldStrategy, oldCurrency string
	var oldConstraints, oldWeights, oldRules, oldRetry, oldBreaker, oldAffinity []byte
	var oldBaselineID *uuid.UUID
	var oldFallbackLimit, oldProviderTimeout, oldFirstTokenTimeout int
	err = d.Pool.QueryRow(r.Context(), `
		SELECT name, strategy, compare_currency, constraints, weights, rules, retry_policy, circuit_breaker, session_affinity,
		       baseline_candidate_id, fallback_limit, provider_timeout_ms, first_token_timeout_ms
		FROM route_policies WHERE id = $1 AND organization_id = $2
	`, policyID, principal.OrganizationID).Scan(&oldName, &oldStrategy, &oldCurrency,
		&oldConstraints, &oldWeights, &oldRules, &oldRetry, &oldBreaker, &oldAffinity,
		&oldBaselineID, &oldFallbackLimit, &oldProviderTimeout, &oldFirstTokenTimeout)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_UPDATE_FAILED", "failed-to-update-the-route-policy", nil, err.Error())
		return
	}

	newStrategy := strings.TrimSpace(body.Strategy)
	if newStrategy == "" {
		newStrategy = oldStrategy
	}
	if !dataplane.IsAllowedStrategy(newStrategy) {
		adminError(w, http.StatusBadRequest, "POLICY_STRATEGY_UNSUPPORTED", "unsupported-policy-type", nil)
		return
	}

	newCurrency := oldCurrency
	if strings.TrimSpace(body.CompareCurrency) != "" {
		newCurrency, err = dataplane.NormalizeCurrencyCode(body.CompareCurrency)
		if err != nil {
			adminError(w, http.StatusBadRequest, "POLICY_COMPARE_CURRENCY_INVALID", "invalid-route-policy-compare-currency", nil, err.Error())
			return
		}
	}

	newWeights := oldWeights
	if len(body.Weights) > 0 && string(body.Weights) != "null" {
		newWeights = body.Weights
	} else if newStrategy != oldStrategy && isPresetWeightStrategy(newStrategy) {
		newWeights = dataplane.MarshalPresetWeights(newStrategy)
	}

	newConstraints := oldConstraints
	if len(body.Constraints) > 0 && string(body.Constraints) != "null" {
		newConstraints = body.Constraints
	}
	newRules := oldRules
	if len(body.Rules) > 0 && string(body.Rules) != "null" {
		newRules = body.Rules
	}
	newRetry := oldRetry
	if len(body.RetryPolicy) > 0 && string(body.RetryPolicy) != "null" {
		newRetry = body.RetryPolicy
	}
	newBreaker := oldBreaker
	if len(body.CircuitBreaker) > 0 && string(body.CircuitBreaker) != "null" {
		newBreaker = body.CircuitBreaker
	}
	newAffinity := oldAffinity
	if len(body.SessionAffinity) > 0 && string(body.SessionAffinity) != "null" {
		newAffinity = body.SessionAffinity
	}

	newBaseline := oldBaselineID
	if body.BaselineCandidateID != nil {
		if strings.TrimSpace(*body.BaselineCandidateID) == "" {
			newBaseline = nil
		} else {
			newBaseline, err = d.validateBaselineCandidate(r.Context(), principal.OrganizationID, body.BaselineCandidateID)
			if err != nil {
				adminError(w, http.StatusBadRequest, "POLICY_BASELINE_INVALID", "invalid-route-policy-baseline-candidate", nil, err.Error())
				return
			}
		}
	}

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

	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `
			UPDATE route_policies SET
				name = $3, strategy = $4, compare_currency = $5,
				weights = $6::jsonb, constraints = $7::jsonb,
				rules = $8::jsonb, retry_policy = $9::jsonb, circuit_breaker = $10::jsonb,
				session_affinity = $11::jsonb,
				baseline_candidate_id = $12,
				fallback_limit = $13, provider_timeout_ms = $14, first_token_timeout_ms = $15,
				version = version + 1, updated_at = now()
			WHERE id = $1 AND organization_id = $2
		`, policyID, principal.OrganizationID, body.Name, newStrategy, newCurrency,
			json.RawMessage(newWeights), json.RawMessage(newConstraints),
			json.RawMessage(newRules), json.RawMessage(newRetry), json.RawMessage(newBreaker),
			json.RawMessage(newAffinity),
			nullablePolicyID(newBaseline), newFallbackLimit, newProviderTimeout, newFirstTokenTimeout)
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
			auditJSON(map[string]any{
				"name": oldName, "strategy": oldStrategy, "compare_currency": oldCurrency,
				"weights": rawJSONOrEmpty(oldWeights), "constraints": rawJSONOrEmpty(oldConstraints),
				"rules": rawJSONOrEmpty(oldRules), "retry_policy": rawJSONOrEmpty(oldRetry),
				"circuit_breaker": rawJSONOrEmpty(oldBreaker), "session_affinity": rawJSONOrEmpty(oldAffinity),
				"baseline_candidate_id": uuidPtrString(oldBaselineID),
				"fallback_limit": oldFallbackLimit, "provider_timeout_ms": oldProviderTimeout, "first_token_timeout_ms": oldFirstTokenTimeout,
			}),
			auditJSON(map[string]any{
				"name": body.Name, "strategy": newStrategy, "compare_currency": newCurrency,
				"weights": rawJSONOrEmpty(newWeights), "constraints": rawJSONOrEmpty(newConstraints),
				"rules": rawJSONOrEmpty(newRules), "retry_policy": rawJSONOrEmpty(newRetry),
				"circuit_breaker": rawJSONOrEmpty(newBreaker), "session_affinity": rawJSONOrEmpty(newAffinity),
				"baseline_candidate_id": uuidPtrString(newBaseline),
				"fallback_limit": newFallbackLimit, "provider_timeout_ms": newProviderTimeout, "first_token_timeout_ms": newFirstTokenTimeout,
			}))
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
		adminError(w, http.StatusInternalServerError, "POLICY_UPDATE_FAILED", "failed-to-update-the-route-policy", nil, err.Error())
		return
	}
	adminOK(w, nil)
}

// CloneRoutePolicy 复制既有路由策略的全部策略、权重、规则与韧性配置为新策略。
func (d ServerDeps) CloneRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermRoutePolicyManage) != nil {
		return
	}
	sourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "invalid-route-policy-id", nil)
		return
	}
	var body cloneRoutePolicyRequest
	if r.ContentLength != 0 {
		if !d.decodeJSON(w, r, &body) {
			return
		}
	}
	customName := strings.TrimSpace(body.Name)

	var srcName, strategy, compareCurrency string
	var constraints, weights, rules, retryPolicy, circuitBreaker, sessionAffinity []byte
	var baselineID *uuid.UUID
	var fallbackLimit, providerTimeout, firstTokenTimeout int
	err = d.Pool.QueryRow(r.Context(), `
		SELECT name, strategy, compare_currency, constraints, weights, rules, retry_policy, circuit_breaker, session_affinity,
		       baseline_candidate_id, fallback_limit, provider_timeout_ms, first_token_timeout_ms
		FROM route_policies WHERE id = $1 AND organization_id = $2
	`, sourceID, principal.OrganizationID).Scan(&srcName, &strategy, &compareCurrency,
		&constraints, &weights, &rules, &retryPolicy, &circuitBreaker, &sessionAffinity,
		&baselineID, &fallbackLimit, &providerTimeout, &firstTokenTimeout)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_CLONE_FAILED", "failed-to-clone-route-policy", nil, err.Error())
		return
	}

	targetName := customName
	if targetName == "" {
		base := fmt.Sprintf("%s (Copy)", srcName)
		targetName = base
		for suffix := 2; suffix <= 20; suffix++ {
			var taken bool
			if checkErr := d.Pool.QueryRow(r.Context(), `
				SELECT EXISTS (SELECT 1 FROM route_policies WHERE organization_id = $1 AND name = $2)
			`, principal.OrganizationID, targetName).Scan(&taken); checkErr == nil && !taken {
				break
			}
			targetName = fmt.Sprintf("%s (Copy %d)", srcName, suffix)
		}
	}

	clonedID := uuid.New()
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO route_policies(
				id, organization_id, name, strategy, compare_currency,
				constraints, weights, rules, retry_policy, circuit_breaker, session_affinity,
				baseline_candidate_id, fallback_limit, provider_timeout_ms, first_token_timeout_ms
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		`, clonedID, principal.OrganizationID, targetName, strategy, compareCurrency,
			constraints, weights, rules, retryPolicy, circuitBreaker, sessionAffinity,
			nullablePolicyID(baselineID), fallbackLimit, providerTimeout, firstTokenTimeout); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"route_policy.cloned", "route_policy", clonedID.String(),
			map[string]any{"source_policy_id": sourceID.String(), "source_name": srcName},
			map[string]any{"name": targetName, "strategy": strategy})
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "route_policies_organization_id_name_key" {
			adminError(w, http.StatusConflict, "POLICY_NAME_EXISTS", "a-route-policy-with-the-same", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "POLICY_CLONE_FAILED", "failed-to-clone-route-policy", nil, err.Error())
		return
	}
	adminOK(w, map[string]string{"id": clonedID.String(), "name": targetName})
}

func jsonOrDefault(raw, fallback json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return fallback
	}
	return raw
}

func rawJSONOrEmpty(raw []byte) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	return json.RawMessage(raw)
}
