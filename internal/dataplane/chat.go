package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

// chatRequestMeta 是 OpenAI 兼容的请求体骨架;其余字段原样透传给上游参数。
type chatRequestMeta struct {
	Model    string
	Messages json.RawMessage
	Stream   bool
	Params   *schemas.ChatParameters
}

const maxChatBodyBytes = 32 * 1024 * 1024

// parseChatRequest 解析请求体:model/messages/stream 单独识别,其余键合并为上游参数。
func parseChatRequest(w http.ResponseWriter, r *http.Request) (chatRequestMeta, []schemas.ChatMessage, bool) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxChatBodyBytes))
	if err := decoder.Decode(&raw); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", "Request body is not valid JSON.", apiErrorOptions{Reason: "invalid_json", Detail: err.Error()})
		return chatRequestMeta{}, nil, false
	}
	meta := chatRequestMeta{}
	if v, ok := raw["model"]; ok {
		if err := json.Unmarshal(v, &meta.Model); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request", "The model field must be a string.", apiErrorOptions{Reason: "invalid_model", Param: "model"})
			return chatRequestMeta{}, nil, false
		}
	}
	if v, ok := raw["stream"]; ok {
		if err := json.Unmarshal(v, &meta.Stream); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request", "The stream field must be a boolean.", apiErrorOptions{Reason: "invalid_stream", Param: "stream"})
			return chatRequestMeta{}, nil, false
		}
	}
	if v, ok := raw["messages"]; ok {
		meta.Messages = v
	}
	delete(raw, "model")
	delete(raw, "stream")
	delete(raw, "messages")
	if len(raw) > 0 {
		paramsBytes, err := json.Marshal(raw)
		if err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request", "Request parameters cannot be serialized.", apiErrorOptions{Reason: "invalid_parameters"})
			return chatRequestMeta{}, nil, false
		}
		var params schemas.ChatParameters
		if err := json.Unmarshal(paramsBytes, &params); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request", "Request parameters are invalid.", apiErrorOptions{Reason: "invalid_parameters", Detail: err.Error()})
			return chatRequestMeta{}, nil, false
		}
		meta.Params = &params
	}
	if meta.Model == "" {
		apiError(w, http.StatusBadRequest, "invalid_request", "The model parameter is required.", apiErrorOptions{Reason: "missing_model", Param: "model"})
		return chatRequestMeta{}, nil, false
	}
	if len(meta.Messages) == 0 {
		apiError(w, http.StatusBadRequest, "invalid_request", "The messages parameter is required.", apiErrorOptions{Reason: "missing_messages", Param: "messages"})
		return chatRequestMeta{}, nil, false
	}
	var input []schemas.ChatMessage
	if err := json.Unmarshal(meta.Messages, &input); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", "The messages parameter is invalid.", apiErrorOptions{Reason: "invalid_messages", Param: "messages", Detail: err.Error()})
		return chatRequestMeta{}, nil, false
	}
	return meta, input, true
}

// resolvedService 是数据库中解析出的模型服务与候选集合。
type resolvedService struct {
	ID         uuid.UUID
	Name       string
	PolicyID   *uuid.UUID
	Policy     routePolicy
	Candidates []candidate
}

// parsePolicyWeights 解析 route_policies.weights JSON;缺省键按 0 处理,空对象返回空权重。
func parsePolicyWeights(raw []byte) policyWeights {
	var parsed struct {
		Cost        *float64 `json:"cost"`
		Quality     *float64 `json:"quality"`
		Latency     *float64 `json:"latency"`
		Reliability *float64 `json:"reliability"`
	}
	if len(raw) == 0 {
		return policyWeights{}
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return policyWeights{}
	}
	w := policyWeights{}
	if parsed.Cost != nil {
		w.Cost = *parsed.Cost
	}
	if parsed.Quality != nil {
		w.Quality = *parsed.Quality
	}
	if parsed.Latency != nil {
		w.Latency = *parsed.Latency
	}
	if parsed.Reliability != nil {
		w.Reliability = *parsed.Reliability
	}
	return w
}

// loadCandidateHealth 优先读取热统计缓存，结合账号主动探测与候选短期熔断状态，用于低开销路由评估。
func (d Deps) loadCandidateHealth(ctx context.Context, orgID uuid.UUID, chain []candidate) *policyScores {
	return d.loadCandidateHealthWithBreaker(ctx, orgID, chain, DefaultCircuitBreakerPolicy())
}

func (d Deps) loadCandidateHealthWithBreaker(ctx context.Context, orgID uuid.UUID, chain []candidate, cbCfg CircuitBreakerPolicy) *policyScores {
	scores := &policyScores{
		Health:        map[uuid.UUID]candidateHealth{},
		AccountHealth: map[uuid.UUID]string{},
		Circuits:      map[uuid.UUID]CircuitState{},
	}
	ids := make([]uuid.UUID, 0, len(chain))
	accountIDs := make([]uuid.UUID, 0, len(chain))
	seenAccounts := make(map[uuid.UUID]bool, len(chain))
	for _, c := range chain {
		if parsed := parseUUID(c.ID); parsed != uuid.Nil {
			ids = append(ids, parsed)
		}
		if accID := parseUUID(c.AccountID); accID != uuid.Nil && !seenAccounts[accID] {
			seenAccounts[accID] = true
			accountIDs = append(accountIDs, accID)
		}
	}
	if len(ids) > 0 {
		if cached, ok := d.Breaker().CachedStats(orgID, ids); ok {
			scores.Health = cached
		} else {
			rows, err := d.Pool.Query(ctx, `
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
			`, orgID, ids)
			if err == nil {
				for rows.Next() {
					var id uuid.UUID
					var h candidateHealth
					var avgMS, avgTTFT *float64
					if err := rows.Scan(&id, &avgMS, &avgTTFT, &h.TTFTCount, &h.Successes, &h.Total); err != nil {
						continue
					}
					if avgMS != nil {
						h.AvgMS = *avgMS
					}
					if avgTTFT != nil {
						h.AvgTTFTMS = *avgTTFT
					}
					scores.Health[id] = h
				}
				rows.Close()
				d.Breaker().StoreStats(orgID, ids, scores.Health)
			}
		}
		scores.Circuits = d.Breaker().States(ctx, ids, cbCfg)
	}
	if len(accountIDs) > 0 {
		stateRows, err := d.Pool.Query(ctx, `
			SELECT provider_account_id, status
			FROM provider_account_health_states
			WHERE provider_account_id = ANY($1)
		`, accountIDs)
		if err == nil {
			for stateRows.Next() {
				var accID uuid.UUID
				var status string
				if err := stateRows.Scan(&accID, &status); err == nil {
					scores.AccountHealth[accID] = status
				}
			}
			stateRows.Close()
		}
	}
	return scores
}

func (d Deps) loadPolicyByID(ctx context.Context, orgID, policyID uuid.UUID) (routePolicy, bool, error) {
	var p routePolicy
	var pid uuid.UUID
	var weightsRaw, constraintsRaw, rulesRaw, retryRaw, cbRaw []byte
	err := d.Pool.QueryRow(ctx, `
		SELECT id, version, strategy, compare_currency, fallback_limit, provider_timeout_ms, first_token_timeout_ms,
		       weights, constraints, rules, retry_policy, circuit_breaker
		FROM route_policies
		WHERE id = $1 AND organization_id = $2 AND status = 'active'
	`, policyID, orgID).Scan(&pid, &p.Version, &p.Strategy, &p.CompareCurrency, &p.FallbackLimit,
		&p.ProviderTimeoutMS, &p.FirstTokenTimeoutMS, &weightsRaw, &constraintsRaw, &rulesRaw, &retryRaw, &cbRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultPolicy, false, nil
	}
	if err != nil {
		return routePolicy{}, false, err
	}
	p.ID = pid.String()
	p.Weights = parsePolicyWeights(weightsRaw)
	p.Constraints = parsePolicyConstraints(constraintsRaw)
	p.Rules = ParseRules(rulesRaw)
	p.RetryPolicy = ParseRetryPolicy(retryRaw)
	p.CircuitBreaker = ParseCircuitBreakerPolicy(cbRaw)
	if p.ProviderTimeoutMS <= 0 {
		p.ProviderTimeoutMS = defaultPolicy.ProviderTimeoutMS
	}
	if p.FirstTokenTimeoutMS <= 0 {
		p.FirstTokenTimeoutMS = defaultPolicy.FirstTokenTimeoutMS
	}
	if p.CompareCurrency == "" {
		p.CompareCurrency = "USD"
	}
	return p, true, nil
}

// loadService 按 org + name 解析 active 模型服务、active 候选与绑定的路由策略。
func (d Deps) loadService(ctx context.Context, orgID uuid.UUID, name string) (resolvedService, bool, error) {
	return d.loadServiceWithPolicyOverride(ctx, orgID, name, nil)
}

func (d Deps) loadServiceWithPolicyOverride(ctx context.Context, orgID uuid.UUID, name string, policyOverride *uuid.UUID) (resolvedService, bool, error) {
	svc := resolvedService{}
	var policyID *uuid.UUID
	err := d.Pool.QueryRow(ctx, `
		SELECT id, name, route_policy_id FROM model_services
		WHERE organization_id = $1 AND name = $2 AND status = 'active'
	`, orgID, name).Scan(&svc.ID, &svc.Name, &policyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return svc, false, nil
	}
	if err != nil {
		return svc, false, err
	}
	if policyOverride != nil {
		policyID = policyOverride
	}
	svc.PolicyID = policyID
	if policyID != nil {
		loadedPolicy, foundPolicy, err := d.loadPolicyByID(ctx, orgID, *policyID)
		if err != nil {
			return svc, false, err
		}
		if foundPolicy {
			svc.Policy = loadedPolicy
		} else {
			svc.Policy = defaultPolicy
		}
	} else {
		svc.Policy = defaultPolicy
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT c.id, a.provider, a.id, a.name, c.upstream_model, c.priority, c.weight,
		       c.input_cost_per_million, c.output_cost_per_million, c.cost_currency, c.quality_score,
		       c.context_window, c.capabilities, c.tags
		FROM model_candidates c
		JOIN provider_accounts a ON a.id = c.provider_account_id
		WHERE c.model_service_id = $1 AND c.status = 'active' AND a.status = 'active' AND c.retired_at IS NULL
		ORDER BY c.priority ASC
	`, svc.ID)
	if err != nil {
		return svc, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.ID, &c.Provider, &c.AccountID, &c.AccountName, &c.UpstreamModel,
			&c.Priority, &c.Weight, &c.InputCostPerM, &c.OutputCostPerM, &c.CostCurrency, &c.Quality,
			&c.ContextWindow, &c.Capabilities, &c.Tags); err != nil {
			return svc, false, err
		}
		svc.Candidates = append(svc.Candidates, c)
	}
	if err := rows.Err(); err != nil {
		return svc, false, err
	}
	return svc, len(svc.Candidates) > 0, nil
}

// ChatCompletions 处理 POST /v1/chat/completions。
func (d Deps) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	meta, input, ok := parseChatRequest(w, r)
	if !ok {
		return
	}
	start := time.Now()

	svc, found, err := d.loadService(r.Context(), keyCtx.OrganizationID, meta.Model)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to resolve model service.")
		return
	}
	if !found {
		apiError(w, http.StatusNotFound, "model_not_found", "Model service does not exist or is not enabled.", apiErrorOptions{Reason: "model_not_found", Param: "model", Params: map[string]any{"model": meta.Model}})
		return
	}
	allowed, err := d.hasModelServiceAccess(r.Context(), keyCtx.OrganizationID, keyCtx.ProjectID, svc.ID, "invoke")
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to check model service access.")
		return
	}
	if !allowed {
		d.auditDeniedModelAccess(r, keyCtx, svc.ID, "chat")
		apiError(w, http.StatusForbidden, "model_access_denied", "The current project is not allowed to invoke this model service.")
		return
	}
	if !d.checkBudgets(w, r, keyCtx) {
		return
	}

	reqCtx := ExtractChatRouteContext(meta, keyCtx, r.Header)
	scores := d.loadCandidateHealthWithBreaker(r.Context(), keyCtx.OrganizationID, svc.Candidates, svc.Policy.CircuitBreaker)
	var rr uint64
	if svc.Policy.Strategy == StrategyRoundRobin {
		rr = d.Breaker().NextRoundRobin(r.Context(), svc.ID.String())
	}
	decision := EvaluateRoute(svc.Candidates, svc.Policy, reqCtx, scores, d.fxRatesSnapshot(), rr)
	if len(decision.Chain) == 0 {
		apiError(w, http.StatusNotFound, "no_eligible_candidates", "No upstream model candidate matches the route policy constraints or request requirements.",
			apiErrorOptions{Reason: "no_eligible_candidates", Param: "model", Params: map[string]any{"model": meta.Model}})
		return
	}

	requestID := uuid.New()
	if err := d.recordPendingDecision(r.Context(), requestID, &keyCtx, nil, "api", svc, decision); err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to record routing decision.")
		return
	}

	if meta.Stream {
		d.handleStreamChain(r, w, requestID, &keyCtx, svc, decision, meta.Model, input, meta.Params, start, nil)
		return
	}
	d.handleNonStreamChain(r, w, requestID, &keyCtx, svc, decision, meta.Model, input, meta.Params, start, nil)
}

// recordPending 保留原有签名供 embeddings 等路径复用。
func (d Deps) recordPending(ctx context.Context, requestID uuid.UUID, keyCtx apiKeyContext, svc resolvedService, chain routeChain) error {
	decision := RouteDecision{
		Chain:             chain,
		PolicyID:          uuidStrPtr(svc.PolicyID),
		PolicyVersion:     svc.Policy.Version,
		Strategy:          svc.Policy.Strategy,
		EffectiveStrategy: svc.Policy.Strategy,
		CompareCurrency:   svc.Policy.CompareCurrency,
		Weights:           svc.Policy.Weights,
	}
	return d.recordPendingDecision(ctx, requestID, &keyCtx, nil, "api", svc, decision)
}

func (d Deps) recordPendingDecision(ctx context.Context, requestID uuid.UUID, keyCtx *apiKeyContext, actorUserID *uuid.UUID,
	source string, svc resolvedService, decision RouteDecision) error {
	chain := decision.Chain
	candidatesJSON, err := json.Marshal(chain)
	if err != nil {
		return fmt.Errorf("marshal candidates: %w", err)
	}
	detailJSON, err := json.Marshal(decision)
	if err != nil {
		detailJSON = []byte("{}")
	}
	primary := chain[0]
	fallbackIDs := make([]uuid.UUID, 0, len(chain)-1)
	for _, c := range chain[1:] {
		if parsed, err := uuid.Parse(c.ID); err == nil {
			fallbackIDs = append(fallbackIDs, parsed)
		}
	}
	selectedID := uuid.MustParse(primary.ID)
	reason := fmt.Sprintf("selected %s (%s/%s) via %s", primary.ID, primary.Provider, primary.UpstreamModel, decision.EffectiveStrategy)
	if decision.MatchedRuleName != "" {
		reason += fmt.Sprintf(" rule=%s", decision.MatchedRuleName)
	}

	var orgID uuid.UUID
	var projPtr, appPtr, keyPtr *uuid.UUID
	if keyCtx != nil {
		orgID = keyCtx.OrganizationID
		p := keyCtx.ProjectID
		a := keyCtx.ApplicationID
		k := keyCtx.KeyID
		projPtr, appPtr, keyPtr = &p, &a, &k
	} else {
		// Playground 调用通过 model_services 查组织归属。
		if err := d.Pool.QueryRow(ctx, `SELECT organization_id FROM model_services WHERE id = $1`, svc.ID).Scan(&orgID); err != nil {
			return err
		}
	}
	var pv *int
	if decision.PolicyVersion > 0 {
		v := decision.PolicyVersion
		pv = &v
	}

	store := usageStore{pool: d.Pool, fxRates: d.fxRatesSnapshot()}
	return store.insertPending(ctx, pendingUsage{
		RequestID:       requestID,
		OrganizationID:  orgID,
		ProjectID:       projPtr,
		ApplicationID:   appPtr,
		APIKeyID:        keyPtr,
		ActorUserID:     actorUserID,
		Source:          source,
		ModelServiceID:  svc.ID,
		RoutePolicyID:   svc.PolicyID,
		PolicyVersion:   pv,
		Strategy:        decision.EffectiveStrategy,
		MatchedRuleID:   decision.MatchedRuleID,
		MatchedRuleName: decision.MatchedRuleName,
		DecisionDetail:  detailJSON,
		Reason:          reason,
		Candidates:      candidatesJSON,
		SelectedID:      &selectedID,
		FallbackIDs:     fallbackIDs,
	})
}

type completionObserver struct {
	onComplete func(status string, result DebugResult, err error)
}

func (d Deps) executeNonStreamChain(ctx context.Context, svc resolvedService, chain routeChain,
	input []schemas.ChatMessage, params *schemas.ChatParameters) (candidate, *schemas.BifrostChatResponse, []AttemptRecord, *schemas.BifrostError) {
	rp := svc.Policy.RetryPolicy
	cb := svc.Policy.CircuitBreaker
	perTimeout := time.Duration(svc.Policy.ProviderTimeoutMS) * time.Millisecond
	if perTimeout <= 0 {
		perTimeout = 60 * time.Second
	}

	attempts := make([]AttemptRecord, 0, len(chain))
	skipAccounts := make(map[string]bool)
	var lastErr *schemas.BifrostError

	for _, cand := range chain {
		if ctx.Err() != nil {
			break
		}
		if skipAccounts[cand.AccountID] {
			attempts = append(attempts, AttemptRecord{
				CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
				Outcome: "skipped_same_account",
			})
			continue
		}
		provider, err := bifrostx.MapProvider(cand.Provider)
		if err != nil {
			attempts = append(attempts, AttemptRecord{
				CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
				ErrorClass: "unsupported_provider", ErrorMessage: err.Error(), Outcome: "failed",
			})
			continue
		}

		maxRetries := rp.MaxRetriesPerCandidate
		for retryIdx := 0; retryIdx <= maxRetries; retryIdx++ {
			attemptStart := time.Now()
			attemptCtx, cancelAttempt := context.WithTimeout(ctx, perTimeout)
			bctx := schemas.NewBifrostContext(attemptCtx, time.Now().Add(perTimeout))
			bctx.SetValue(schemas.BifrostContextKeyAPIKeyID, cand.AccountID)
			req := &schemas.BifrostChatRequest{
				Provider: provider,
				Model:    cand.UpstreamModel,
				Input:    input,
				Params:   params,
			}
			resp, bifrostErr := d.Engine.Client().ChatCompletionRequest(bctx, req)
			cancelAttempt()
			attemptMS := int(time.Since(attemptStart).Milliseconds())

			if bifrostErr == nil {
				d.Breaker().RecordSuccess(ctx, cand.ID)
				attempts = append(attempts, AttemptRecord{
					CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
					StatusCode: http.StatusOK, LatencyMS: attemptMS, RetryCount: retryIdx, Outcome: "success",
				})
				return cand, resp, attempts, nil
			}

			lastErr = bifrostErr
			statusCode, errCode, errType, errMsg := extractBifrostErrorInfo(bifrostErr)
			if d.OnTransportError != nil && statusCode == 0 {
				d.OnTransportError(string(provider))
			}
			action := ClassifyAttemptOutcome(statusCode, errCode, errType, rp)
			d.Breaker().RecordFailure(ctx, cand.ID, statusCode, errCode, errType, rp, cb)

			if action.ShouldRetry && retryIdx < maxRetries && ctx.Err() == nil {
				backoff := 250 * time.Millisecond
				if statusCode == http.StatusTooManyRequests {
					backoff = 600 * time.Millisecond
				}
				select {
				case <-time.After(backoff):
					continue
				case <-ctx.Done():
				}
			}

			attempts = append(attempts, AttemptRecord{
				CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
				StatusCode: statusCode, ErrorClass: action.Class, ErrorType: errType, ErrorMessage: errMsg,
				LatencyMS: attemptMS, RetryCount: retryIdx, Outcome: "failed",
			})
			if action.SkipSameAccount {
				skipAccounts[cand.AccountID] = true
			}
			if !action.ShouldFallback {
				return candidate{}, nil, attempts, lastErr
			}
			break
		}
	}
	if lastErr == nil {
		msg := "All upstream model candidates failed."
		lastErr = &schemas.BifrostError{
			StatusCode: intPtr(http.StatusBadGateway),
			Type:       stringPtr("upstream_error"),
			Error:      &schemas.ErrorField{Message: msg},
		}
	}
	return candidate{}, nil, attempts, lastErr
}

func (d Deps) handleNonStreamChain(r *http.Request, w http.ResponseWriter, requestID uuid.UUID, keyCtx *apiKeyContext,
	svc resolvedService, decision RouteDecision, requestedModel string, input []schemas.ChatMessage,
	params *schemas.ChatParameters, start time.Time, observer *completionObserver) {
	served, resp, attempts, bifrostErr := d.executeNonStreamChain(r.Context(), svc, decision.Chain, input, params)
	if bifrostErr != nil {
		d.recordChainFailure(r.Context(), keyCtx, requestID, decision.Chain, attempts, bifrostErr, start, nil, usageSnapshot{})
		if observer != nil {
			observer.onComplete("failed", DebugResult{TotalMS: int(time.Since(start).Milliseconds())}, errors.New(errorMessageOf(bifrostErr)))
		}
		writeBifrostError(w, bifrostErr)
		return
	}

	total := int(time.Since(start).Milliseconds())
	usage := usageFromBifrost(resp.Usage)
	servedProvider := string(resp.ExtraFields.RoutingInfo.Provider)
	servedModel := resp.ExtraFields.RoutingInfo.Model
	if servedProvider == "" {
		if mapped, err := bifrostx.MapProvider(served.Provider); err == nil {
			servedProvider = string(mapped)
		} else {
			servedProvider = served.Provider
		}
	}
	if servedModel == "" {
		servedModel = served.UpstreamModel
	}

	candidateID := uuidPtr(served.ID, served.ID != "")
	inputTokens, outputTokens, cachedTokens, reasoningTokens := usage.tokens()
	cost := 0.0
	usageSource := stringPtrNil("computed", usage.hasTokens())
	if usage.hasTokens() {
		cost = served.costUSD(inputTokens, outputTokens)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(chatCompletionResponseFrom(servedModel, requestedModel, resp, usage)); err != nil {
		d.Logger.Warn("write chat completion response failed", "request_id", requestID, "error", err)
	}

	store := usageStore{pool: d.Pool, fxRates: d.fxRatesSnapshot()}
	finalizeErr := store.finalize(context.WithoutCancel(r.Context()), requestID, finalUsage{
		State:           "success",
		StatusCode:      intPtr(http.StatusOK),
		CandidateID:     candidateID,
		Provider:        stringPtr(servedProvider),
		UpstreamModel:   stringPtr(servedModel),
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		CachedTokens:    cachedTokens,
		ReasoningTokens: reasoningTokens,
		Cost:            cost,
		Currency:        currencyOf(served),
		TotalMS:         total,
		UsageSource:     usageSource,
		AttemptTrail:    attemptTrailFromRecords(attempts),
	})
	if finalizeErr != nil {
		d.Logger.Warn("finalize usage failed", "request_id", requestID, "error", finalizeErr)
	}
	if keyCtx != nil {
		d.settleBudget(context.WithoutCancel(r.Context()), *keyCtx, requestID, cost, currencyOf(served))
	}
	if observer != nil {
		observer.onComplete("success", DebugResult{
			Response: debugResponseContent(resp), Provider: servedProvider, UpstreamModel: servedModel,
			InputTokens: inputTokens, OutputTokens: outputTokens, CachedTokens: cachedTokens,
			ReasoningTokens: reasoningTokens, Cost: cost, Currency: currencyOf(served), TotalMS: total,
		}, nil)
	}
}

func extractBifrostErrorInfo(err *schemas.BifrostError) (statusCode int, errCode, errType, message string) {
	if err == nil {
		return http.StatusBadGateway, "", "upstream_error", "upstream request failed"
	}
	if err.StatusCode != nil {
		statusCode = *err.StatusCode
	}
	if err.Type != nil {
		errType = *err.Type
	}
	if errType == "" {
		errType = "upstream_error"
	}
	if err.Error != nil {
		if err.Error.Code != nil {
			errCode = *err.Error.Code
		}
		if err.Error.Type != nil && *err.Error.Type != "" {
			errType = *err.Error.Type
		}
		message = err.Error.Message
	}
	if message == "" {
		message = "upstream request failed"
	}
	return statusCode, errCode, errType, message
}

func (d Deps) recordChainFailure(ctx context.Context, keyCtx *apiKeyContext, requestID uuid.UUID, chain routeChain,
	attempts []AttemptRecord, bifrostErr *schemas.BifrostError, start time.Time, firstTokenMS *int, partial usageSnapshot) {
	total := int(time.Since(start).Milliseconds())
	statusCode := http.StatusBadGateway
	if bifrostErr != nil && bifrostErr.StatusCode != nil && *bifrostErr.StatusCode >= 400 && *bifrostErr.StatusCode < 600 {
		statusCode = *bifrostErr.StatusCode
	}
	_, _, errType, message := extractBifrostErrorInfo(bifrostErr)
	usageSource := stringPtrNil("computed", partial.hasTokens())

	var lastCandidate candidate
	if len(attempts) > 0 {
		lastID := attempts[len(attempts)-1].CandidateID
		for _, c := range chain {
			if c.ID == lastID {
				lastCandidate = c
				break
			}
		}
	} else if len(chain) > 0 {
		lastCandidate = chain[0]
	}

	var attemptedProvider *string
	if mapped, err := bifrostx.MapProvider(lastCandidate.Provider); err == nil {
		attemptedProvider = stringPtr(string(mapped))
	} else if lastCandidate.Provider != "" {
		attemptedProvider = stringPtr(lastCandidate.Provider)
	}
	var attemptedModel *string
	if lastCandidate.UpstreamModel != "" {
		attemptedModel = stringPtr(lastCandidate.UpstreamModel)
	}

	cost := 0.0
	currency := currencyOf(lastCandidate)
	if partial.hasTokens() && lastCandidate.ID != "" {
		cost = lastCandidate.costUSD(partial.InputTokens, partial.OutputTokens)
	}
	store := usageStore{pool: d.Pool, fxRates: d.fxRatesSnapshot()}
	if err := store.finalize(context.WithoutCancel(ctx), requestID, finalUsage{
		State:           "failed",
		StatusCode:      intPtr(statusCode),
		CandidateID:     uuidPtr(lastCandidate.ID, lastCandidate.ID != ""),
		Provider:        attemptedProvider,
		UpstreamModel:   attemptedModel,
		InputTokens:     partial.InputTokens,
		OutputTokens:    partial.OutputTokens,
		CachedTokens:    partial.CachedTokens,
		ReasoningTokens: partial.ReasoningTokens,
		Cost:            cost,
		Currency:        currency,
		FirstTokenMS:    firstTokenMS,
		TotalMS:         total,
		UsageSource:     usageSource,
		ErrorType:       stringPtr(errType),
		ErrorMessage:    stringPtr(message),
		AttemptTrail:    attemptTrailFromRecords(attempts),
	}); err != nil {
		d.Logger.Warn("finalize failed usage error", "request_id", requestID, "error", err)
	}
	if keyCtx != nil && cost > 0 {
		d.settleBudget(context.WithoutCancel(ctx), *keyCtx, requestID, cost, currency)
	}
}

// writeBifrostError 把 Bifrost 错误转成 OpenAI 风格错误 JSON。
func writeBifrostError(w http.ResponseWriter, bifrostErr *schemas.BifrostError) {
	status := http.StatusBadGateway
	if bifrostErr != nil && bifrostErr.StatusCode != nil && *bifrostErr.StatusCode >= 400 && *bifrostErr.StatusCode < 600 {
		status = *bifrostErr.StatusCode
	}
	_, _, errType, message := extractBifrostErrorInfo(bifrostErr)
	apiError(w, status, "upstream_error", "Upstream request failed.",
		apiErrorOptions{Type: "upstream_error", Reason: "upstream_request_failed", Detail: message, Params: map[string]any{"upstream_type": errType}})
}

// PlaygroundResult 是管理台路由在线体验返回的完整实时执行报告。
type PlaygroundResult struct {
	RequestID         string          `json:"request_id"`
	ModelServiceID    string          `json:"model_service_id"`
	ModelServiceName  string          `json:"model_service_name"`
	Reply             string          `json:"reply"`
	ServedProvider    string          `json:"served_provider"`
	ServedModel       string          `json:"served_model"`
	InputTokens       int64           `json:"input_tokens"`
	OutputTokens      int64           `json:"output_tokens"`
	CachedTokens      int64           `json:"cached_tokens"`
	ReasoningTokens   int64           `json:"reasoning_tokens"`
	Cost              float64         `json:"cost"`
	Currency          string          `json:"currency"`
	TotalMS           int             `json:"total_ms"`
	Attempts          []AttemptRecord `json:"attempts"`
	Decision          RouteDecision   `json:"decision"`
}

// ExecutePlayground 从管理台按指定路由策略发起真实测试调用，写入 route_playground 用量并记录完整决策轨迹。
func (d Deps) ExecutePlayground(ctx context.Context, orgID, userID, serviceID uuid.UUID, policyID *uuid.UUID,
	prompt string, streamPref bool, requiredCaps []string, metadata map[string]string) (PlaygroundResult, error) {
	var serviceName string
	if err := d.Pool.QueryRow(ctx, `
		SELECT name FROM model_services WHERE id = $1 AND organization_id = $2 AND status = 'active'
	`, serviceID, orgID).Scan(&serviceName); err != nil {
		return PlaygroundResult{}, fmt.Errorf("model service not found or inactive")
	}
	svc, found, err := d.loadServiceWithPolicyOverride(ctx, orgID, serviceName, policyID)
	if err != nil || !found {
		return PlaygroundResult{}, fmt.Errorf("model service has no active candidates")
	}
	messages := []schemas.ChatMessage{AgentTextMessage(schemas.ChatMessageRoleUser, prompt)}
	msgBytes, _ := json.Marshal(messages)
	estIn, _, _ := EstimateTokensFromMessagesJSON(msgBytes)
	reqCtx := RouteContext{
		EstimatedInputTokens:  estIn,
		EstimatedOutputTokens: 500,
		Stream:                streamPref,
		RequiredCapabilities:  requiredCaps,
		Source:                "route_playground",
		KeywordText:           prompt,
		Metadata:              metadata,
	}
	scores := d.loadCandidateHealthWithBreaker(ctx, orgID, svc.Candidates, svc.Policy.CircuitBreaker)
	var rr uint64
	if svc.Policy.Strategy == StrategyRoundRobin {
		rr = d.Breaker().NextRoundRobin(ctx, svc.ID.String())
	}
	decision := EvaluateRoute(svc.Candidates, svc.Policy, reqCtx, scores, d.fxRatesSnapshot(), rr)
	if len(decision.Chain) == 0 {
		return PlaygroundResult{}, fmt.Errorf("no eligible candidates matched route policy constraints")
	}

	requestID := uuid.New()
	if err := d.recordPendingDecision(ctx, requestID, nil, &userID, "route_playground", svc, decision); err != nil {
		return PlaygroundResult{}, err
	}

	start := time.Now()
	served, resp, attempts, bifrostErr := d.executeNonStreamChain(ctx, svc, decision.Chain, messages, nil)
	if bifrostErr != nil {
		d.recordChainFailure(ctx, nil, requestID, decision.Chain, attempts, bifrostErr, start, nil, usageSnapshot{})
		_, _, _, msg := extractBifrostErrorInfo(bifrostErr)
		return PlaygroundResult{
			RequestID:        requestID.String(),
			ModelServiceID:   svc.ID.String(),
			ModelServiceName: svc.Name,
			Attempts:         attempts,
			Decision:         decision,
		}, errors.New(msg)
	}

	total := int(time.Since(start).Milliseconds())
	usage := usageFromBifrost(resp.Usage)
	servedProvider := string(resp.ExtraFields.RoutingInfo.Provider)
	if servedProvider == "" {
		servedProvider = served.Provider
	}
	servedModel := resp.ExtraFields.RoutingInfo.Model
	if servedModel == "" {
		servedModel = served.UpstreamModel
	}
	inTok, outTok, cachedTok, reasonTok := usage.tokens()
	cost := 0.0
	if usage.hasTokens() {
		cost = served.costUSD(inTok, outTok)
	}
	store := usageStore{pool: d.Pool, fxRates: d.fxRatesSnapshot()}
	if err := store.finalize(context.WithoutCancel(ctx), requestID, finalUsage{
		State:           "success",
		StatusCode:      intPtr(http.StatusOK),
		CandidateID:     uuidPtr(served.ID, served.ID != ""),
		Provider:        stringPtr(servedProvider),
		UpstreamModel:   stringPtr(servedModel),
		InputTokens:     inTok,
		OutputTokens:    outTok,
		CachedTokens:    cachedTok,
		ReasoningTokens: reasonTok,
		Cost:            cost,
		Currency:        currencyOf(served),
		TotalMS:         total,
		UsageSource:     stringPtrNil("computed", usage.hasTokens()),
		AttemptTrail:    attemptTrailFromRecords(attempts),
	}); err != nil {
		return PlaygroundResult{}, fmt.Errorf("finalize playground usage: %w", err)
	}

	return PlaygroundResult{
		RequestID:        requestID.String(),
		ModelServiceID:   svc.ID.String(),
		ModelServiceName: svc.Name,
		Reply:            debugResponseContent(resp),
		ServedProvider:   servedProvider,
		ServedModel:      servedModel,
		InputTokens:      inTok,
		OutputTokens:     outTok,
		CachedTokens:     cachedTok,
		ReasoningTokens:  reasonTok,
		Cost:             cost,
		Currency:         currencyOf(served),
		TotalMS:          total,
		Attempts:         attempts,
		Decision:         decision,
	}, nil
}

func uuidStrPtr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func stringOrDefault(v string, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
