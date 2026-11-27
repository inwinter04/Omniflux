package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/dataplane"
	"github.com/inwinter04/omniflux/internal/fx"
)

type simulateRoutePolicyRequest struct {
	ModelServiceID             string            `json:"model_service_id"`
	PolicyID                   *string           `json:"policy_id"`
	Strategy                   string            `json:"strategy"`
	CompareCurrency            string            `json:"compare_currency"`
	Weights                    json.RawMessage   `json:"weights"`
	Constraints                json.RawMessage   `json:"constraints"`
	Rules                      json.RawMessage   `json:"rules"`
	RetryPolicy                json.RawMessage   `json:"retry_policy"`
	CircuitBreaker             json.RawMessage   `json:"circuit_breaker"`
	SessionAffinity            json.RawMessage   `json:"session_affinity"`
	BaselineCandidateID        *string           `json:"baseline_candidate_id"`
	FallbackLimit              *int              `json:"fallback_limit"`
	ProviderTimeoutMS          *int              `json:"provider_timeout_ms"`
	FirstTokenTimeoutMS        *int              `json:"first_token_timeout_ms"`
	EstimatedInputTokens       int               `json:"estimated_input_tokens"`
	EstimatedOutputTokens      int               `json:"estimated_output_tokens"`
	Stream                     bool              `json:"stream"`
	RequiredCapabilities       []string          `json:"required_capabilities"`
	ProjectID                  string            `json:"project_id"`
	ApplicationID              string            `json:"application_id"`
	APIKeyID                   string            `json:"api_key_id"`
	KeywordText                string            `json:"keyword_text"`
	Metadata                   map[string]string `json:"metadata"`
	RandomSeed                 int64             `json:"random_seed"`
	RRCounter                  uint64            `json:"rr_counter"`
	SimulateFailedCandidateIDs []string          `json:"simulate_failed_candidate_ids"`
}

type playgroundRoutePolicyRequest struct {
	ModelServiceID       string            `json:"model_service_id"`
	Prompt               string            `json:"prompt"`
	Stream               bool              `json:"stream"`
	RequiredCapabilities []string          `json:"required_capabilities"`
	Metadata             map[string]string `json:"metadata"`
}

type simulatedFailoverStep struct {
	Attempt       int    `json:"attempt"`
	CandidateID   string `json:"candidate_id"`
	Provider      string `json:"provider"`
	UpstreamModel string `json:"upstream_model"`
	AccountName   string `json:"account_name"`
	Outcome       string `json:"outcome"`
}

type simulatedSavings struct {
	CompareCurrency       string   `json:"compare_currency"`
	BaselineCandidateID   *string  `json:"baseline_candidate_id"`
	BaselineUpstreamModel *string  `json:"baseline_upstream_model"`
	BaselineCost          *float64 `json:"baseline_cost"`
	SelectedCost          *float64 `json:"selected_cost"`
	SavedCost             *float64 `json:"saved_cost"`
	SavingsRate           *float64 `json:"savings_rate"`
}

func (d ServerDeps) fxRatesSnapshot() map[string]float64 {
	if d.FX != nil {
		return d.FX.Snapshot()
	}
	return (&fx.Service{}).Snapshot()
}

// SimulateRoutePolicy 执行零消耗的路由器 Dry-Run 仿真，支持未保存草稿参数与故障模拟。
func (d ServerDeps) SimulateRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermRoutePolicyRead) != nil {
		return
	}
	var body simulateRoutePolicyRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	serviceID, err := uuid.Parse(strings.TrimSpace(body.ModelServiceID))
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_ID_INVALID", "invalid-model-service-id", nil)
		return
	}
	var svcExists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM model_services WHERE id = $1 AND organization_id = $2)
	`, serviceID, principal.OrganizationID).Scan(&svcExists); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_SIMULATE_FAILED", "failed-to-simulate-route-policy", nil, err.Error())
		return
	}
	if !svcExists {
		adminError(w, http.StatusNotFound, "SERVICE_NOT_FOUND", "model-service-does-not-exist-or", nil)
		return
	}

	// 1) 初始化策略配置（默认 -> 已存策略 -> 表单草稿覆盖）。
	simPol := dataplane.SimPolicy{
		Version:             1,
		Strategy:            dataplane.StrategyBalanced,
		CompareCurrency:     "USD",
		FallbackLimit:       3,
		ProviderTimeoutMS:   60000,
		FirstTokenTimeoutMS: 15000,
		RetryPolicy:         dataplane.DefaultRetryPolicy(),
		CircuitBreaker:      dataplane.DefaultCircuitBreakerPolicy(),
		SessionAffinity:     dataplane.DefaultSessionAffinityPolicy(),
	}
	simPol.ApplyWeightsJSON(dataplane.MarshalPresetWeights(dataplane.StrategyBalanced))

	var baselineID *uuid.UUID
	if body.PolicyID != nil && strings.TrimSpace(*body.PolicyID) != "" {
		pid, err := uuid.Parse(strings.TrimSpace(*body.PolicyID))
		if err != nil {
			adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "invalid-route-policy-id", nil)
			return
		}
		var dbWeights, dbConstraints, dbRules, dbRetry, dbCB, dbAffinity []byte
		err = d.Pool.QueryRow(r.Context(), `
			SELECT version, strategy, compare_currency, weights, constraints, rules, retry_policy, circuit_breaker, session_affinity,
			       baseline_candidate_id, fallback_limit, provider_timeout_ms, first_token_timeout_ms
			FROM route_policies WHERE id = $1 AND organization_id = $2
		`, pid, principal.OrganizationID).Scan(&simPol.Version, &simPol.Strategy, &simPol.CompareCurrency,
			&dbWeights, &dbConstraints, &dbRules, &dbRetry, &dbCB, &dbAffinity,
			&baselineID, &simPol.FallbackLimit, &simPol.ProviderTimeoutMS, &simPol.FirstTokenTimeoutMS)
		if errors.Is(err, pgx.ErrNoRows) {
			adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
			return
		}
		if err != nil {
			adminError(w, http.StatusInternalServerError, "POLICY_SIMULATE_FAILED", "failed-to-simulate-route-policy", nil, err.Error())
			return
		}
		simPol.ID = pid.String()
		simPol.ApplyWeightsJSON(dbWeights)
		simPol.ApplyConstraintsJSON(dbConstraints)
		simPol.Rules = dataplane.ParseRules(dbRules)
		simPol.RetryPolicy = dataplane.ParseRetryPolicy(dbRetry)
		simPol.CircuitBreaker = dataplane.ParseCircuitBreakerPolicy(dbCB)
		simPol.SessionAffinity = dataplane.ParseSessionAffinityPolicy(dbAffinity)
	}

	if !validateRoutePolicyLimits(w, body.FallbackLimit, body.ProviderTimeoutMS, body.FirstTokenTimeoutMS) {
		return
	}
	if !validateRoutePolicyJSONFields(w, body.Weights, body.Constraints, body.Rules, body.RetryPolicy, body.CircuitBreaker, body.SessionAffinity) {
		return
	}
	if strings.TrimSpace(body.Strategy) != "" {
		if !dataplane.IsAllowedStrategy(body.Strategy) {
			adminError(w, http.StatusBadRequest, "POLICY_STRATEGY_UNSUPPORTED", "unsupported-policy-type", nil)
			return
		}
		prev := simPol.Strategy
		simPol.Strategy = strings.TrimSpace(body.Strategy)
		if simPol.Strategy != prev && len(body.Weights) == 0 && isPresetWeightStrategy(simPol.Strategy) {
			simPol.ApplyWeightsJSON(dataplane.MarshalPresetWeights(simPol.Strategy))
		}
	}
	if strings.TrimSpace(body.CompareCurrency) != "" {
		cur, err := dataplane.NormalizeCurrencyCode(body.CompareCurrency)
		if err != nil {
			adminError(w, http.StatusBadRequest, "POLICY_COMPARE_CURRENCY_INVALID", "invalid-route-policy-compare-currency", nil, err.Error())
			return
		}
		simPol.CompareCurrency = cur
	}
	if len(body.Weights) > 0 && string(body.Weights) != "null" {
		simPol.ApplyWeightsJSON(body.Weights)
	}
	if len(body.Constraints) > 0 && string(body.Constraints) != "null" {
		simPol.ApplyConstraintsJSON(body.Constraints)
	}
	if len(body.Rules) > 0 && string(body.Rules) != "null" {
		simPol.Rules = dataplane.ParseRules(body.Rules)
	}
	if len(body.RetryPolicy) > 0 && string(body.RetryPolicy) != "null" {
		simPol.RetryPolicy = dataplane.ParseRetryPolicy(body.RetryPolicy)
	}
	if len(body.CircuitBreaker) > 0 && string(body.CircuitBreaker) != "null" {
		simPol.CircuitBreaker = dataplane.ParseCircuitBreakerPolicy(body.CircuitBreaker)
	}
	if len(body.SessionAffinity) > 0 && string(body.SessionAffinity) != "null" {
		simPol.SessionAffinity = dataplane.ParseSessionAffinityPolicy(body.SessionAffinity)
	}
	if body.FallbackLimit != nil {
		simPol.FallbackLimit = *body.FallbackLimit
	}
	if body.ProviderTimeoutMS != nil {
		simPol.ProviderTimeoutMS = *body.ProviderTimeoutMS
	}
	if body.FirstTokenTimeoutMS != nil {
		simPol.FirstTokenTimeoutMS = *body.FirstTokenTimeoutMS
	}
	if body.BaselineCandidateID != nil {
		if strings.TrimSpace(*body.BaselineCandidateID) == "" {
			baselineID = nil
		} else {
			parsedBase, err := d.validateBaselineCandidate(r.Context(), principal.OrganizationID, body.BaselineCandidateID)
			if err != nil {
				adminError(w, http.StatusBadRequest, "POLICY_BASELINE_INVALID", "invalid-route-policy-baseline-candidate", nil, err.Error())
				return
			}
			baselineID = parsedBase
		}
	}

	reqCaps, err := dataplane.ValidateCandidateCapabilities(body.RequiredCapabilities)
	if err != nil {
		adminError(w, http.StatusBadRequest, "CANDIDATE_CAPABILITIES_INVALID", "invalid-candidate-capabilities", nil, err.Error())
		return
	}

	// 2) 加载候选、7d 性能指标、账号探测状态与实时熔断状态。
	candRows, err := d.Pool.Query(r.Context(), `
		SELECT c.id, a.provider, a.api_protocol, a.id, a.name, c.upstream_model, c.priority, c.weight,
		       c.input_cost_per_million::float8, c.output_cost_per_million::float8,
		       c.cache_read_cost_per_million::float8, c.cache_write_cost_per_million::float8,
		       c.cost_currency, c.quality_score::float8, c.context_window, c.capabilities, c.tags
		FROM model_candidates c
		JOIN provider_accounts a ON a.id = c.provider_account_id
		WHERE c.model_service_id = $1 AND c.retired_at IS NULL AND c.status = 'active' AND a.status = 'active'
		ORDER BY c.priority ASC, c.weight DESC, c.created_at ASC
	`, serviceID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_SIMULATE_FAILED", "failed-to-simulate-route-policy", nil, err.Error())
		return
	}
	defer candRows.Close()

	simCands := []dataplane.SimCandidate{}
	candUUIDs := []uuid.UUID{}
	accountUUIDs := []uuid.UUID{}
	candMap := map[string]dataplane.SimCandidate{}
	for candRows.Next() {
		var cid, aid uuid.UUID
		var c dataplane.SimCandidate
		if err := candRows.Scan(&cid, &c.Provider, &c.APIProtocol, &aid, &c.AccountName, &c.UpstreamModel, &c.Priority, &c.Weight,
			&c.InputCostPerM, &c.OutputCostPerM, &c.CacheReadCostPerM, &c.CacheWriteCostPerM,
			&c.CostCurrency, &c.Quality, &c.ContextWindow, &c.Capabilities, &c.Tags); err != nil {
			adminError(w, http.StatusInternalServerError, "POLICY_SIMULATE_FAILED", "failed-to-simulate-route-policy", nil, err.Error())
			return
		}
		c.ID = cid.String()
		c.AccountID = aid.String()
		simCands = append(simCands, c)
		candUUIDs = append(candUUIDs, cid)
		accountUUIDs = append(accountUUIDs, aid)
		candMap[c.ID] = c
	}

	healthByCand := make(map[string]dataplane.SimHealth, len(simCands))
	if len(candUUIDs) > 0 {
		hRows, hErr := d.Pool.Query(r.Context(), `
			SELECT model_candidate_id,
			       avg(total_ms) FILTER (WHERE total_ms IS NOT NULL),
			       avg(first_token_ms) FILTER (WHERE first_token_ms IS NOT NULL),
			       count(first_token_ms),
			       count(*) FILTER (WHERE state = 'success'),
			       count(*)
			FROM usage_records
			WHERE organization_id = $1 AND model_candidate_id = ANY($2)
			  AND state <> 'pending' AND created_at >= now() - interval '7 days'
			GROUP BY model_candidate_id
		`, principal.OrganizationID, candUUIDs)
		if hErr == nil {
			defer hRows.Close()
			for hRows.Next() {
				var cid uuid.UUID
				var h dataplane.SimHealth
				var avgMS, avgTTFT *float64
				if scanErr := hRows.Scan(&cid, &avgMS, &avgTTFT, &h.TTFTCount, &h.Successes, &h.Total); scanErr == nil {
					if avgMS != nil {
						h.AvgMS = *avgMS
					}
					if avgTTFT != nil {
						h.AvgTTFTMS = *avgTTFT
					}
					healthByCand[cid.String()] = h
				}
			}
		}

		if len(accountUUIDs) > 0 {
			accHealth := map[string]string{}
			aRows, aErr := d.Pool.Query(r.Context(), `
				SELECT provider_account_id, status FROM provider_account_health_states WHERE provider_account_id = ANY($1)
			`, accountUUIDs)
			if aErr == nil {
				defer aRows.Close()
				for aRows.Next() {
					var aid uuid.UUID
					var status string
					if scanErr := aRows.Scan(&aid, &status); scanErr == nil {
						accHealth[aid.String()] = status
					}
				}
			}
			for _, c := range simCands {
				if st := accHealth[c.AccountID]; st != "" {
					h := healthByCand[c.ID]
					h.AccountHealth = st
					healthByCand[c.ID] = h
				}
			}
		}

		if d.Plane != nil {
			cbStates := d.Plane.Breaker().States(r.Context(), candUUIDs, simPol.CircuitBreaker)
			for cid, st := range cbStates {
				h := healthByCand[cid.String()]
				h.CircuitOpen = st.Open
				healthByCand[cid.String()] = h
			}
		}
	}

	estIn := body.EstimatedInputTokens
	if estIn <= 0 {
		estIn = 2000
	}
	estOut := body.EstimatedOutputTokens
	if estOut <= 0 {
		estOut = 500
	}
	reqCtx := dataplane.RouteContext{
		EstimatedInputTokens:  estIn,
		EstimatedOutputTokens: estOut,
		Stream:                body.Stream,
		RequiredCapabilities:  reqCaps,
		ProjectID:             strings.TrimSpace(body.ProjectID),
		ApplicationID:         strings.TrimSpace(body.ApplicationID),
		APIKeyID:              strings.TrimSpace(body.APIKeyID),
		Source:                "simulate",
		KeywordText:           body.KeywordText,
		Metadata:              body.Metadata,
		RandomSeed:            body.RandomSeed,
	}

	fxRates := d.fxRatesSnapshot()
	decision := dataplane.EvaluateSimulation(simCands, simPol, reqCtx, healthByCand, fxRates, body.RRCounter)

	// 3) 若指定模拟故障候选，推演 failover 轨迹。
	failSet := map[string]bool{}
	for _, id := range body.SimulateFailedCandidateIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			failSet[trimmed] = true
		}
	}
	failoverTrace := []simulatedFailoverStep{}
	if len(failSet) > 0 && len(decision.ChainIDs) > 0 {
		for idx, cid := range decision.ChainIDs {
			c := candMap[cid]
			outcome := "served"
			if failSet[cid] {
				outcome = "simulated_failure"
			}
			failoverTrace = append(failoverTrace, simulatedFailoverStep{
				Attempt:       idx + 1,
				CandidateID:   cid,
				Provider:      c.Provider,
				UpstreamModel: c.UpstreamModel,
				AccountName:   c.AccountName,
				Outcome:       outcome,
			})
			if outcome == "served" {
				break
			}
		}
	}

	// 4) 计算相对基线候选的成本节省预估。
	savings := simulatedSavings{CompareCurrency: decision.CompareCurrency}
	var selectedCost *float64
	for _, ev := range decision.Evaluations {
		if ev.Selected && ev.Rank == 1 && ev.CostComparable {
			val := ev.EstimatedCost
			selectedCost = &val
			break
		}
	}
	var baseIDStr, baseModelStr *string
	var baseCost *float64
	if baselineID != nil {
		for _, ev := range decision.Evaluations {
			if ev.CandidateID == baselineID.String() && ev.CostComparable {
				idCopy, modelCopy := ev.CandidateID, ev.UpstreamModel
				costCopy := ev.EstimatedCost
				baseIDStr, baseModelStr, baseCost = &idCopy, &modelCopy, &costCopy
				break
			}
		}
	}
	if baseCost == nil {
		for _, ev := range decision.Evaluations {
			if !ev.CostComparable {
				continue
			}
			if baseCost == nil || ev.EstimatedCost > *baseCost {
				idCopy, modelCopy := ev.CandidateID, ev.UpstreamModel
				costCopy := ev.EstimatedCost
				baseIDStr, baseModelStr, baseCost = &idCopy, &modelCopy, &costCopy
			}
		}
	}
	savings.BaselineCandidateID = baseIDStr
	savings.BaselineUpstreamModel = baseModelStr
	savings.BaselineCost = baseCost
	savings.SelectedCost = selectedCost
	if baseCost != nil && selectedCost != nil {
		saved := *baseCost - *selectedCost
		savings.SavedCost = &saved
		if *baseCost > 0 {
			rate := saved / *baseCost
			savings.SavingsRate = &rate
		}
	}

	adminOK(w, map[string]any{
		"decision":            decision,
		"fx_rates":            fxRates,
		"estimated_savings":   savings,
		"simulated_failover":  failoverTrace,
	})
}

// PlaygroundRoutePolicy 从管理台发起真实路由试跑调用，记录 source=route_playground 统计与完整决策轨迹。
func (d ServerDeps) PlaygroundRoutePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAnyPermission(w, principal, auth.PermRoutePolicyManage, auth.PermModelServiceManage) != nil {
		return
	}
	policyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "invalid-route-policy-id", nil)
		return
	}
	var policyStatus string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT status FROM route_policies WHERE id = $1 AND organization_id = $2
	`, policyID, principal.OrganizationID).Scan(&policyStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_PLAYGROUND_FAILED", "route-policy-playground-failed", nil, err.Error())
		return
	}
	if policyStatus != "active" {
		adminError(w, http.StatusConflict, "POLICY_DISABLED", "route-policy-is-disabled", nil)
		return
	}

	var body playgroundRoutePolicyRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	serviceID, err := uuid.Parse(strings.TrimSpace(body.ModelServiceID))
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_ID_INVALID", "invalid-model-service-id", nil)
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" || utf8.RuneCountInString(prompt) > 16000 {
		adminError(w, http.StatusBadRequest, "PLAYGROUND_PROMPT_INVALID", "route-policy-playground-prompt-invalid", nil)
		return
	}
	caps, err := dataplane.ValidateCandidateCapabilities(body.RequiredCapabilities)
	if err != nil {
		adminError(w, http.StatusBadRequest, "CANDIDATE_CAPABILITIES_INVALID", "invalid-candidate-capabilities", nil, err.Error())
		return
	}
	if d.Plane == nil {
		adminError(w, http.StatusServiceUnavailable, "POLICY_PLAYGROUND_FAILED", "route-policy-playground-failed", nil)
		return
	}
	result, err := d.Plane.ExecutePlayground(r.Context(), principal.OrganizationID, principal.UserID, serviceID, &policyID, prompt, body.Stream, caps, body.Metadata)
	if err != nil {
		var params map[string]any
		if result.RequestID != "" {
			params = map[string]any{"request_id": result.RequestID}
		}
		adminError(w, http.StatusBadGateway, "POLICY_PLAYGROUND_FAILED", "route-policy-playground-failed", params, err.Error())
		return
	}
	adminOK(w, result)
}

type analyticsFilter struct {
	PolicyID        uuid.UUID
	Days            int
	ModelServiceID  *uuid.UUID
	Source          string
	BaselineID      *uuid.UUID
	UpstreamModels  []string
	State           string
	FallbackOnly    bool
	CompareCurrency string
	PolicyBaseline  *uuid.UUID
}

func (d ServerDeps) parseAnalyticsFilter(r *http.Request, defaultDays int) (uuid.UUID, auth.Principal, analyticsFilter, bool) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return uuid.Nil, principal, analyticsFilter{}, false
	}
	policyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, principal, analyticsFilter{}, false
	}
	q := r.URL.Query()
	days := defaultDays
	if raw := strings.TrimSpace(q.Get("days")); raw != "" {
		if parsed, convErr := strconv.Atoi(raw); convErr == nil {
			if parsed < 1 {
				parsed = 1
			}
			if parsed > 90 {
				parsed = 90
			}
			days = parsed
		}
	}
	var svcPtr *uuid.UUID
	if rawSvc := strings.TrimSpace(q.Get("model_service_id")); rawSvc != "" {
		if parsed, parseErr := uuid.Parse(rawSvc); parseErr == nil {
			svcPtr = &parsed
		}
	}
	var basePtr *uuid.UUID
	if rawBase := strings.TrimSpace(q.Get("baseline_candidate_id")); rawBase != "" {
		if parsed, parseErr := uuid.Parse(rawBase); parseErr == nil {
			basePtr = &parsed
		}
	}
	source := strings.TrimSpace(q.Get("source"))
	switch source {
	case "", "all":
		source = ""
	case "api", "agent_api", "route_playground":
	default:
		source = ""
	}
	models := []string{}
	if rawModels := strings.TrimSpace(q.Get("upstream_models")); rawModels != "" {
		for _, m := range strings.Split(rawModels, ",") {
			trimmed := strings.TrimSpace(m)
			if trimmed != "" {
				models = append(models, trimmed)
			}
		}
	}
	state := strings.TrimSpace(q.Get("state"))
	switch state {
	case "success", "failed", "interrupted":
	default:
		state = ""
	}
	fbOnly := q.Get("fallback_only") == "true" || q.Get("fallback_only") == "1"

	return policyID, principal, analyticsFilter{
		PolicyID:       policyID,
		Days:           days,
		ModelServiceID: svcPtr,
		Source:         source,
		BaselineID:     basePtr,
		UpstreamModels: models,
		State:          state,
		FallbackOnly:   fbOnly,
	}, true
}

func (d ServerDeps) loadPolicyAnalyticsMeta(ctx context.Context, orgID, policyID uuid.UUID) (compareCurrency string, baselineID *uuid.UUID, found bool, err error) {
	err = d.Pool.QueryRow(ctx, `
		SELECT compare_currency, baseline_candidate_id
		FROM route_policies WHERE id = $1 AND organization_id = $2
	`, policyID, orgID).Scan(&compareCurrency, &baselineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	return compareCurrency, baselineID, true, nil
}

// RoutePolicyOverview 返回策略总览（基线省钱对比、模型命中分布、各模型性能明细）。
func (d ServerDeps) RoutePolicyOverview(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermRoutePolicyRead) != nil {
		return
	}
	policyID, _, flt, parsed := d.parseAnalyticsFilter(r, 7)
	if !parsed {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "invalid-route-policy-id", nil)
		return
	}
	compareCurrency, policyBaseline, found, err := d.loadPolicyAnalyticsMeta(r.Context(), principal.OrganizationID, policyID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}
	if !found {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
		return
	}

	fxRates := d.fxRatesSnapshot()

	// 1) 加载绑定模型服务与关联候选（供筛选及基线计算使用）。
	boundServices := []boundModelServiceSummary{}
	bsRows, bsErr := d.Pool.Query(r.Context(), `
		SELECT ms.id, ms.name, ms.status,
		       count(mc.id) FILTER (WHERE mc.retired_at IS NULL AND mc.status = 'active')::int
		FROM model_services ms
		LEFT JOIN model_candidates mc ON mc.model_service_id = ms.id
		WHERE ms.organization_id = $1 AND ms.route_policy_id = $2
		GROUP BY ms.id, ms.name, ms.status
		ORDER BY ms.name ASC
	`, principal.OrganizationID, policyID)
	if bsErr == nil {
		defer bsRows.Close()
		for bsRows.Next() {
			var id uuid.UUID
			var item boundModelServiceSummary
			if scanErr := bsRows.Scan(&id, &item.Name, &item.Status, &item.CandidateCount); scanErr == nil {
				item.ID = id.String()
				boundServices = append(boundServices, item)
			}
		}
	}

	type candPrice struct {
		ID             string
		ServiceID      string
		ServiceName    string
		Provider       string
		AccountName    string
		UpstreamModel  string
		InputCostPerM  float64
		OutputCostPerM float64
		CostCurrency   string
		BoundToPolicy  bool
		BlendedNorm    float64
	}
	candByID := map[string]candPrice{}
	candOptions := []map[string]any{}
	cpRows, cpErr := d.Pool.Query(r.Context(), `
		SELECT c.id, s.id, s.name, a.provider, a.name, c.upstream_model,
		       c.input_cost_per_million::float8, c.output_cost_per_million::float8, c.cost_currency,
		       (s.route_policy_id = $2) AS bound_to_policy
		FROM model_candidates c
		JOIN model_services s ON s.id = c.model_service_id
		JOIN provider_accounts a ON a.id = c.provider_account_id
		WHERE s.organization_id = $1 AND c.retired_at IS NULL
		ORDER BY (s.route_policy_id = $2) DESC, (c.input_cost_per_million + c.output_cost_per_million) DESC
	`, principal.OrganizationID, policyID)
	if cpErr == nil {
		defer cpRows.Close()
		for cpRows.Next() {
			var cid, sid uuid.UUID
			var cp candPrice
			if scanErr := cpRows.Scan(&cid, &sid, &cp.ServiceName, &cp.Provider, &cp.AccountName, &cp.UpstreamModel,
				&cp.InputCostPerM, &cp.OutputCostPerM, &cp.CostCurrency, &cp.BoundToPolicy); scanErr == nil {
				cp.ID = cid.String()
				cp.ServiceID = sid.String()
				if conv, comp := fx.ConvertWithMap((cp.InputCostPerM+cp.OutputCostPerM)/2, cp.CostCurrency, compareCurrency, fxRates); comp {
					cp.BlendedNorm = conv
				}
				candByID[cp.ID] = cp
				if cp.BoundToPolicy || (flt.BaselineID != nil && cp.ID == flt.BaselineID.String()) || (policyBaseline != nil && cp.ID == policyBaseline.String()) {
					candOptions = append(candOptions, map[string]any{
						"id":              cp.ID,
						"model_service":   cp.ServiceName,
						"provider":        cp.Provider,
						"account_name":    cp.AccountName,
						"upstream_model":  cp.UpstreamModel,
						"cost_currency":   cp.CostCurrency,
						"input_cost_per_m": cp.InputCostPerM,
						"output_cost_per_m": cp.OutputCostPerM,
					})
				}
			}
		}
	}

	var effBaseline *candPrice
	if flt.BaselineID != nil {
		if cp, exists := candByID[flt.BaselineID.String()]; exists {
			effBaseline = &cp
		}
	}
	if effBaseline == nil && policyBaseline != nil {
		if cp, exists := candByID[policyBaseline.String()]; exists {
			effBaseline = &cp
		}
	}
	if effBaseline == nil {
		for _, cp := range candByID {
			if !cp.BoundToPolicy {
				continue
			}
			if effBaseline == nil || cp.BlendedNorm > effBaseline.BlendedNorm {
				copyVal := cp
				effBaseline = &copyVal
			}
		}
	}

	// 2) 查询命中明细并按 (candidate_id, provider, upstream_model, cost_currency) 聚合后做跨币种归一化。
	whereParts := []string{
		"u.organization_id = $1",
		"u.route_policy_id = $2",
		"u.state <> 'pending'",
		"u.created_at >= now() - make_interval(days => $3)",
	}
	args := []any{principal.OrganizationID, policyID, flt.Days}
	if flt.ModelServiceID != nil {
		args = append(args, *flt.ModelServiceID)
		whereParts = append(whereParts, fmt.Sprintf("u.model_service_id = $%d", len(args)))
	}
	if flt.Source != "" {
		args = append(args, flt.Source)
		whereParts = append(whereParts, fmt.Sprintf("u.source = $%d", len(args)))
	}
	whereSQL := strings.Join(whereParts, " AND ")

	aggRows, err := d.Pool.Query(r.Context(), fmt.Sprintf(`
		SELECT u.model_candidate_id::text,
		       COALESCE(u.provider, ''),
		       COALESCE(u.upstream_model, ''),
		       COALESCE(NULLIF(u.cost_currency, ''), 'USD'),
		       count(*)::bigint AS total_reqs,
		       count(*) FILTER (WHERE u.state = 'success')::bigint AS succ_reqs,
		       count(*) FILTER (WHERE u.state = 'success' AND %s)::bigint AS fb_reqs,
		       COALESCE(sum(u.input_tokens), 0)::bigint,
		       COALESCE(sum(u.output_tokens), 0)::bigint,
		       COALESCE(sum(u.cost), 0)::float8,
		       COALESCE(sum(u.first_token_ms) FILTER (WHERE u.state = 'success' AND u.first_token_ms IS NOT NULL), 0)::float8,
		       count(u.first_token_ms) FILTER (WHERE u.state = 'success' AND u.first_token_ms IS NOT NULL)::bigint,
		       COALESCE(sum(u.total_ms) FILTER (WHERE u.state = 'success'), 0)::float8,
		       COALESCE(sum(CASE WHEN u.state = 'success' AND u.output_tokens > 0 THEN GREATEST(u.total_ms - COALESCE(u.first_token_ms, 0), 1) ELSE 0 END), 0)::float8,
		       COALESCE(sum(u.output_tokens) FILTER (WHERE u.state = 'success' AND u.output_tokens > 0), 0)::bigint
		FROM usage_records u
		LEFT JOIN policy_decisions pd ON pd.request_id = u.request_id
		WHERE %s
		GROUP BY 1, 2, 3, 4
	`, attemptTrailFallbackSQL, whereSQL), args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}
	defer aggRows.Close()

	type modelBucket struct {
		CandidateID      *string  `json:"candidate_id"`
		Provider         string   `json:"provider"`
		UpstreamModel    string   `json:"upstream_model"`
		Requests         int64    `json:"requests"`
		SuccessRequests  int64    `json:"success_requests"`
		FallbackRequests int64    `json:"fallback_requests"`
		HitShare         float64  `json:"hit_share"`
		SuccessRate      float64  `json:"success_rate"`
		AvgTTFTMS        *float64 `json:"avg_ttft_ms"`
		AvgLatencyMS     *float64 `json:"avg_latency_ms"`
		OTPS             *float64 `json:"otps"`
		InputTokens      int64    `json:"input_tokens"`
		OutputTokens     int64    `json:"output_tokens"`
		ActualCost       float64  `json:"actual_cost"`
		AvgCostPerReq    float64  `json:"avg_cost_per_req"`

		ttftSum, latSum, genDurMS float64
		ttftCnt                   int64
		otpsTokens                int64
	}

	buckets := map[string]*modelBucket{}
	var totalReqs, totalSucc, totalFB, totalInTok, totalOutTok, totalTTFTCnt, totalOTPSTok int64
	var totalActualCost, totalTTFTSum, totalLatSum, totalGenMS float64

	for aggRows.Next() {
		var candID *string
		var provider, model, cur string
		var reqs, succ, fb, inTok, outTok, ttftCnt, otpsTok int64
		var rawCost, ttftSum, latSum, genMS float64
		if scanErr := aggRows.Scan(&candID, &provider, &model, &cur, &reqs, &succ, &fb,
			&inTok, &outTok, &rawCost, &ttftSum, &ttftCnt, &latSum, &genMS, &otpsTok); scanErr != nil {
			adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, scanErr.Error())
			return
		}
		normCost, comp := fx.ConvertWithMap(rawCost, cur, compareCurrency, fxRates)
		if !comp {
			normCost = rawCost
		}
		key := provider + "|" + model
		if candID != nil {
			key = *candID + "|" + key
		}
		b, exists := buckets[key]
		if !exists {
			b = &modelBucket{CandidateID: candID, Provider: provider, UpstreamModel: model}
			buckets[key] = b
		}
		b.Requests += reqs
		b.SuccessRequests += succ
		b.FallbackRequests += fb
		b.InputTokens += inTok
		b.OutputTokens += outTok
		b.ActualCost += normCost
		b.ttftSum += ttftSum
		b.ttftCnt += ttftCnt
		b.latSum += latSum
		b.genDurMS += genMS
		b.otpsTokens += otpsTok

		totalReqs += reqs
		totalSucc += succ
		totalFB += fb
		totalInTok += inTok
		totalOutTok += outTok
		totalActualCost += normCost
		totalTTFTSum += ttftSum
		totalTTFTCnt += ttftCnt
		totalLatSum += latSum
		totalGenMS += genMS
		totalOTPSTok += otpsTok
	}
	if err := aggRows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}

	byModel := make([]modelBucket, 0, len(buckets))
	for _, b := range buckets {
		if totalReqs > 0 {
			b.HitShare = float64(b.Requests) / float64(totalReqs)
		}
		if b.Requests > 0 {
			b.SuccessRate = float64(b.SuccessRequests) / float64(b.Requests)
			b.AvgCostPerReq = b.ActualCost / float64(b.Requests)
		}
		if b.ttftCnt > 0 {
			v := b.ttftSum / float64(b.ttftCnt)
			b.AvgTTFTMS = &v
		}
		if b.SuccessRequests > 0 {
			v := b.latSum / float64(b.SuccessRequests)
			b.AvgLatencyMS = &v
		}
		if b.genDurMS > 0 && b.otpsTokens > 0 {
			v := float64(b.otpsTokens) * 1000.0 / b.genDurMS
			b.OTPS = &v
		}
		byModel = append(byModel, *b)
	}
	sort.Slice(byModel, func(i, j int) bool { return byModel[i].Requests > byModel[j].Requests })

	var succRate, fbRate, avgTTFT, avgLat, otps *float64
	if totalReqs > 0 {
		sr := float64(totalSucc) / float64(totalReqs)
		fr := float64(totalFB) / float64(totalReqs)
		succRate, fbRate = &sr, &fr
	}
	if totalTTFTCnt > 0 {
		v := totalTTFTSum / float64(totalTTFTCnt)
		avgTTFT = &v
	}
	if totalSucc > 0 {
		v := totalLatSum / float64(totalSucc)
		avgLat = &v
	}
	if totalGenMS > 0 && totalOTPSTok > 0 {
		v := float64(totalOTPSTok) * 1000.0 / totalGenMS
		otps = &v
	}

	baselineSummary := map[string]any{
		"compare_currency": compareCurrency,
		"actual_cost":      totalActualCost,
	}
	if effBaseline != nil {
		rawHypo := (float64(totalInTok)*effBaseline.InputCostPerM + float64(totalOutTok)*effBaseline.OutputCostPerM) / 1_000_000.0
		normHypo, comp := fx.ConvertWithMap(rawHypo, effBaseline.CostCurrency, compareCurrency, fxRates)
		if comp {
			saved := normHypo - totalActualCost
			var savingsRate *float64
			if normHypo > 0 {
				sr := saved / normHypo
				savingsRate = &sr
			}
			baselineSummary["candidate_id"] = effBaseline.ID
			baselineSummary["upstream_model"] = effBaseline.UpstreamModel
			baselineSummary["provider"] = effBaseline.Provider
			baselineSummary["baseline_cost"] = normHypo
			baselineSummary["saved_cost"] = saved
			baselineSummary["savings_rate"] = savingsRate
		}
	}

	adminOK(w, map[string]any{
		"policy_id":        policyID.String(),
		"compare_currency": compareCurrency,
		"days":             flt.Days,
		"summary": map[string]any{
			"requests":          totalReqs,
			"success_requests":  totalSucc,
			"fallback_requests": totalFB,
			"success_rate":      succRate,
			"fallback_rate":     fbRate,
			"avg_ttft_ms":       avgTTFT,
			"avg_latency_ms":    avgLat,
			"otps":              otps,
			"input_tokens":      totalInTok,
			"output_tokens":     totalOutTok,
			"actual_cost":       totalActualCost,
		},
		"baseline":          baselineSummary,
		"by_model":          byModel,
		"bound_services":    boundServices,
		"candidate_options": candOptions,
	})
}

// RoutePolicyMetrics 返回实时监控多维指标（TTFT P50/P95、OTPS、RPM/TPM、时序曲线、错误分布）。
func (d ServerDeps) RoutePolicyMetrics(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermRoutePolicyRead) != nil {
		return
	}
	policyID, _, flt, parsed := d.parseAnalyticsFilter(r, 7)
	if !parsed {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "invalid-route-policy-id", nil)
		return
	}
	compareCurrency, _, found, err := d.loadPolicyAnalyticsMeta(r.Context(), principal.OrganizationID, policyID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}
	if !found {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
		return
	}
	fxRates := d.fxRatesSnapshot()

	// 可选模型列表（不受 upstream_models 过滤影响）
	availModels := []string{}
	amRows, amErr := d.Pool.Query(r.Context(), `
		SELECT DISTINCT COALESCE(upstream_model, '')
		FROM usage_records
		WHERE organization_id = $1 AND route_policy_id = $2 AND state <> 'pending'
		  AND created_at >= now() - make_interval(days => $3) AND upstream_model IS NOT NULL
		ORDER BY 1
	`, principal.OrganizationID, policyID, flt.Days)
	if amErr == nil {
		defer amRows.Close()
		for amRows.Next() {
			var m string
			if scanErr := amRows.Scan(&m); scanErr == nil && m != "" {
				availModels = append(availModels, m)
			}
		}
	}

	whereParts := []string{
		"u.organization_id = $1",
		"u.route_policy_id = $2",
		"u.state <> 'pending'",
		"u.created_at >= now() - make_interval(days => $3)",
	}
	args := []any{principal.OrganizationID, policyID, flt.Days}
	if flt.ModelServiceID != nil {
		args = append(args, *flt.ModelServiceID)
		whereParts = append(whereParts, fmt.Sprintf("u.model_service_id = $%d", len(args)))
	}
	if flt.Source != "" {
		args = append(args, flt.Source)
		whereParts = append(whereParts, fmt.Sprintf("u.source = $%d", len(args)))
	}
	if len(flt.UpstreamModels) > 0 {
		args = append(args, flt.UpstreamModels)
		whereParts = append(whereParts, fmt.Sprintf("u.upstream_model = ANY($%d)", len(args)))
	}
	whereSQL := strings.Join(whereParts, " AND ")

	var totalReqs, succReqs, fbReqs, totalTokens, genTokens int64
	var ttftP50, ttftP95, avgLat, genMS *float64
	err = d.Pool.QueryRow(r.Context(), fmt.Sprintf(`
		SELECT count(*)::bigint,
		       count(*) FILTER (WHERE u.state = 'success')::bigint,
		       count(*) FILTER (WHERE u.state = 'success' AND %s)::bigint,
		       COALESCE(sum(u.input_tokens + u.output_tokens), 0)::bigint,
		       percentile_cont(0.50) WITHIN GROUP (ORDER BY u.first_token_ms) FILTER (WHERE u.state = 'success' AND u.first_token_ms IS NOT NULL)::float8,
		       percentile_cont(0.95) WITHIN GROUP (ORDER BY u.first_token_ms) FILTER (WHERE u.state = 'success' AND u.first_token_ms IS NOT NULL)::float8,
		       avg(u.total_ms) FILTER (WHERE u.state = 'success')::float8,
		       COALESCE(sum(u.output_tokens) FILTER (WHERE u.state = 'success' AND u.output_tokens > 0), 0)::bigint,
		       sum(CASE WHEN u.state = 'success' AND u.output_tokens > 0 THEN GREATEST(u.total_ms - COALESCE(u.first_token_ms, 0), 1) ELSE NULL END)::float8
		FROM usage_records u
		LEFT JOIN policy_decisions pd ON pd.request_id = u.request_id
		WHERE %s
	`, attemptTrailFallbackSQL, whereSQL), args...).Scan(&totalReqs, &succReqs, &fbReqs, &totalTokens, &ttftP50, &ttftP95, &avgLat, &genTokens, &genMS)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}

	var totalCost float64
	costRows, cErr := d.Pool.Query(r.Context(), fmt.Sprintf(`
		SELECT COALESCE(NULLIF(u.cost_currency, ''), 'USD'), COALESCE(sum(u.cost), 0)::float8
		FROM usage_records u
		WHERE %s
		GROUP BY 1
	`, whereSQL), args...)
	if cErr == nil {
		defer costRows.Close()
		for costRows.Next() {
			var cur string
			var amt float64
			if scanErr := costRows.Scan(&cur, &amt); scanErr == nil {
				if conv, comp := fx.ConvertWithMap(amt, cur, compareCurrency, fxRates); comp {
					totalCost += conv
				} else {
					totalCost += amt
				}
			}
		}
	}

	var succRate, fbRate, otps *float64
	if totalReqs > 0 {
		sr := float64(succReqs) / float64(totalReqs)
		fr := float64(fbReqs) / float64(totalReqs)
		succRate, fbRate = &sr, &fr
	}
	if genMS != nil && *genMS > 0 && genTokens > 0 {
		v := float64(genTokens) * 1000.0 / *genMS
		otps = &v
	}
	minutes := float64(flt.Days) * 1440.0
	rpm := float64(totalReqs) / minutes
	tpm := float64(totalTokens) / minutes

	granularity := "day"
	timeFmt := "YYYY-MM-DD"
	if flt.Days <= 2 {
		granularity = "hour"
		timeFmt = "YYYY-MM-DD HH24:00"
	}

	type seriesPoint struct {
		Bucket          string   `json:"bucket"`
		Requests        int64    `json:"requests"`
		SuccessRequests int64    `json:"success_requests"`
		FailedRequests  int64    `json:"failed_requests"`
		FallbackReq     int64    `json:"fallback_requests"`
		AvgTTFTMS       *float64 `json:"avg_ttft_ms"`
		AvgLatencyMS    *float64 `json:"avg_latency_ms"`
		TotalTokens     int64    `json:"total_tokens"`
		Cost            float64  `json:"cost"`

		ttftSum float64
		ttftCnt int64
		latSum  float64
	}
	pointMap := map[string]*seriesPoint{}
	bucketOrder := []string{}
	sRows, sErr := d.Pool.Query(r.Context(), fmt.Sprintf(`
		SELECT to_char(date_trunc('%s', u.created_at AT TIME ZONE 'UTC'), '%s') AS bkt,
		       COALESCE(NULLIF(u.cost_currency, ''), 'USD'),
		       count(*)::bigint,
		       count(*) FILTER (WHERE u.state = 'success')::bigint,
		       count(*) FILTER (WHERE u.state <> 'success')::bigint,
		       count(*) FILTER (WHERE u.state = 'success' AND %s)::bigint,
		       COALESCE(sum(u.input_tokens + u.output_tokens), 0)::bigint,
		       COALESCE(sum(u.cost), 0)::float8,
		       COALESCE(sum(u.first_token_ms) FILTER (WHERE u.state = 'success' AND u.first_token_ms IS NOT NULL), 0)::float8,
		       count(u.first_token_ms) FILTER (WHERE u.state = 'success' AND u.first_token_ms IS NOT NULL)::bigint,
		       COALESCE(sum(u.total_ms) FILTER (WHERE u.state = 'success'), 0)::float8
		FROM usage_records u
		LEFT JOIN policy_decisions pd ON pd.request_id = u.request_id
		WHERE %s
		GROUP BY 1, 2
		ORDER BY 1 ASC
	`, granularity, timeFmt, attemptTrailFallbackSQL, whereSQL), args...)
	if sErr != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, sErr.Error())
		return
	}
	defer sRows.Close()
	for sRows.Next() {
		var bkt, cur string
		var reqs, succ, fail, fb, toks, tCnt int64
		var rawCost, tSum, lSum float64
		if scanErr := sRows.Scan(&bkt, &cur, &reqs, &succ, &fail, &fb, &toks, &rawCost, &tSum, &tCnt, &lSum); scanErr != nil {
			adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, scanErr.Error())
			return
		}
		pt, exists := pointMap[bkt]
		if !exists {
			pt = &seriesPoint{Bucket: bkt}
			pointMap[bkt] = pt
			bucketOrder = append(bucketOrder, bkt)
		}
		normCost, comp := fx.ConvertWithMap(rawCost, cur, compareCurrency, fxRates)
		if !comp {
			normCost = rawCost
		}
		pt.Requests += reqs
		pt.SuccessRequests += succ
		pt.FailedRequests += fail
		pt.FallbackReq += fb
		pt.TotalTokens += toks
		pt.Cost += normCost
		pt.ttftSum += tSum
		pt.ttftCnt += tCnt
		pt.latSum += lSum
	}
	if err := sRows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}
	series := make([]seriesPoint, 0, len(bucketOrder))
	for _, bkt := range bucketOrder {
		pt := pointMap[bkt]
		if pt.ttftCnt > 0 {
			v := math.Round((pt.ttftSum/float64(pt.ttftCnt))*10) / 10
			pt.AvgTTFTMS = &v
		}
		if pt.SuccessRequests > 0 {
			v := math.Round((pt.latSum/float64(pt.SuccessRequests))*10) / 10
			pt.AvgLatencyMS = &v
		}
		series = append(series, *pt)
	}

	errDist := []map[string]any{}
	eRows, eErr := d.Pool.Query(r.Context(), fmt.Sprintf(`
		SELECT COALESCE(NULLIF(u.error_type, ''), 'unknown') AS err_type, count(*)::bigint
		FROM usage_records u
		WHERE %s AND u.state <> 'success'
		GROUP BY 1
		ORDER BY 2 DESC
		LIMIT 10
	`, whereSQL), args...)
	if eErr != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, eErr.Error())
		return
	}
	defer eRows.Close()
	for eRows.Next() {
		var errType string
		var cnt int64
		if scanErr := eRows.Scan(&errType, &cnt); scanErr != nil {
			adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, scanErr.Error())
			return
		}
		errDist = append(errDist, map[string]any{"error_type": errType, "count": cnt})
	}
	if err := eRows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_ANALYTICS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}

	adminOK(w, map[string]any{
		"kpis": map[string]any{
			"requests":         totalReqs,
			"rpm":              rpm,
			"tpm":              tpm,
			"success_rate":     succRate,
			"fallback_rate":    fbRate,
			"ttft_p50_ms":      ttftP50,
			"ttft_p95_ms":      ttftP95,
			"avg_latency_ms":   avgLat,
			"otps":             otps,
			"total_cost":       totalCost,
			"compare_currency": compareCurrency,
		},
		"granularity":        granularity,
		"series":             series,
		"error_distribution": errDist,
		"available_models":   availModels,
	})
}

// RoutePolicyDecisions 分页返回指定策略的真实路由决策快照与 fallback attempt trail。
func (d ServerDeps) RoutePolicyDecisions(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermRoutePolicyRead) != nil {
		return
	}
	policyID, _, flt, parsed := d.parseAnalyticsFilter(r, 30)
	if !parsed {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "invalid-route-policy-id", nil)
		return
	}
	_, _, found, err := d.loadPolicyAnalyticsMeta(r.Context(), principal.OrganizationID, policyID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DECISIONS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}
	if !found {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
		return
	}

	q := r.URL.Query()
	page := 1
	if p, convErr := strconv.Atoi(strings.TrimSpace(q.Get("page"))); convErr == nil && p > 0 {
		page = p
	}
	pageSize := 20
	if ps, convErr := strconv.Atoi(strings.TrimSpace(q.Get("page_size"))); convErr == nil && ps > 0 {
		if ps > 100 {
			ps = 100
		}
		pageSize = ps
	}

	whereParts := []string{
		"pd.organization_id = $1",
		"pd.route_policy_id = $2",
	}
	args := []any{principal.OrganizationID, policyID}
	if flt.ModelServiceID != nil {
		args = append(args, *flt.ModelServiceID)
		whereParts = append(whereParts, fmt.Sprintf("u.model_service_id = $%d", len(args)))
	}
	if flt.Source != "" {
		args = append(args, flt.Source)
		whereParts = append(whereParts, fmt.Sprintf("u.source = $%d", len(args)))
	}
	if flt.State != "" {
		args = append(args, flt.State)
		whereParts = append(whereParts, fmt.Sprintf("u.state = $%d", len(args)))
	}
	if flt.FallbackOnly {
		whereParts = append(whereParts, attemptTrailFallbackSQL)
	}
	whereSQL := strings.Join(whereParts, " AND ")

	var total int64
	if err := d.Pool.QueryRow(r.Context(), fmt.Sprintf(`
		SELECT count(*)
		FROM policy_decisions pd
		LEFT JOIN usage_records u ON u.request_id = pd.request_id
		WHERE %s
	`, whereSQL), args...).Scan(&total); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DECISIONS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}

	offset := (page - 1) * pageSize
	argsWithPage := append(append([]any{}, args...), pageSize, offset)
	rows, err := d.Pool.Query(r.Context(), fmt.Sprintf(`
		SELECT pd.id, pd.request_id, pd.policy_version, COALESCE(pd.strategy, ''),
		       pd.matched_rule_id, pd.matched_rule_name, pd.decision, pd.reason,
		       pd.selected_candidate_id, COALESCE(pd.decision_detail, '{}'::jsonb),
		       COALESCE(pd.attempt_trail, '{"attempts":[]}'::jsonb), pd.created_at,
		       u.model_service_id, ms.name, COALESCE(u.source, 'api'),
		       COALESCE(u.state, 'pending'), u.provider, u.upstream_model,
		       u.first_token_ms, u.total_ms, u.cost::float8, u.cost_currency
		FROM policy_decisions pd
		LEFT JOIN usage_records u ON u.request_id = pd.request_id
		LEFT JOIN model_services ms ON ms.id = u.model_service_id
		WHERE %s
		ORDER BY pd.created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(argsWithPage)-1, len(argsWithPage)), argsWithPage...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DECISIONS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}
	defer rows.Close()

	type decisionRow struct {
		ID              string          `json:"id"`
		RequestID       string          `json:"request_id"`
		PolicyVersion   *int            `json:"policy_version"`
		Strategy        string          `json:"strategy"`
		MatchedRuleID   *string         `json:"matched_rule_id"`
		MatchedRuleName *string         `json:"matched_rule_name"`
		Decision        string          `json:"decision"`
		Reason          string          `json:"reason"`
		SelectedCandID  *string         `json:"selected_candidate_id"`
		ModelServiceID  *string         `json:"model_service_id"`
		ModelService    *string         `json:"model_service_name"`
		Source          string          `json:"source"`
		State           string          `json:"state"`
		ServedProvider  *string         `json:"served_provider"`
		ServedModel     *string         `json:"served_model"`
		FirstTokenMS    *int            `json:"first_token_ms"`
		TotalMS         *int            `json:"total_ms"`
		Cost            *float64        `json:"cost"`
		CostCurrency    *string         `json:"cost_currency"`
		DecisionDetail  json.RawMessage `json:"decision_detail"`
		AttemptTrail    json.RawMessage `json:"attempt_trail"`
		CreatedAt       time.Time       `json:"created_at"`
	}
	items := []decisionRow{}
	for rows.Next() {
		var id, reqID uuid.UUID
		var selID, svcID *uuid.UUID
		var item decisionRow
		if scanErr := rows.Scan(&id, &reqID, &item.PolicyVersion, &item.Strategy,
			&item.MatchedRuleID, &item.MatchedRuleName, &item.Decision, &item.Reason,
			&selID, &item.DecisionDetail, &item.AttemptTrail, &item.CreatedAt,
			&svcID, &item.ModelService, &item.Source, &item.State,
			&item.ServedProvider, &item.ServedModel, &item.FirstTokenMS,
			&item.TotalMS, &item.Cost, &item.CostCurrency); scanErr != nil {
			adminError(w, http.StatusInternalServerError, "POLICY_DECISIONS_FAILED", "failed-to-query-route-policy-analytics", nil, scanErr.Error())
			return
		}
		item.ID = id.String()
		item.RequestID = reqID.String()
		if selID != nil {
			s := selID.String()
			item.SelectedCandID = &s
		}
		if svcID != nil {
			s := svcID.String()
			item.ModelServiceID = &s
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_DECISIONS_FAILED", "failed-to-query-route-policy-analytics", nil, err.Error())
		return
	}
	adminOK(w, map[string]any{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
