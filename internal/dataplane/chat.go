package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/maximhq/bifrost/core/schemas"
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
func parseChatRequest(w http.ResponseWriter, r *http.Request) (chatRequestMeta, bool) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxChatBodyBytes))
	if err := decoder.Decode(&raw); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", "请求体不是合法 JSON: "+err.Error())
		return chatRequestMeta{}, false
	}
	meta := chatRequestMeta{}
	if v, ok := raw["model"]; ok {
		if err := json.Unmarshal(v, &meta.Model); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request", "model 字段必须是字符串")
			return chatRequestMeta{}, false
		}
	}
	if v, ok := raw["stream"]; ok {
		if err := json.Unmarshal(v, &meta.Stream); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request", "stream 字段必须是布尔值")
			return chatRequestMeta{}, false
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
			apiError(w, http.StatusBadRequest, "invalid_request", "请求参数无法序列化")
			return chatRequestMeta{}, false
		}
		var params schemas.ChatParameters
		if err := json.Unmarshal(paramsBytes, &params); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request", "请求参数不合法: "+err.Error())
			return chatRequestMeta{}, false
		}
		meta.Params = &params
	}
	if meta.Model == "" {
		apiError(w, http.StatusBadRequest, "invalid_request", "缺少 model 字段")
		return chatRequestMeta{}, false
	}
	if len(meta.Messages) == 0 {
		apiError(w, http.StatusBadRequest, "invalid_request", "缺少 messages 字段")
		return chatRequestMeta{}, false
	}
	return meta, true
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

// loadCandidateHealth 统计候选近 7 天的调用表现,用于 latency/reliability 评分。
// 没有任何历史时返回空表,所有候选评分退化为等值(等价于旧的加权随机)。
func (d Deps) loadCandidateHealth(ctx context.Context, orgID uuid.UUID, chain []candidate) *policyScores {
	ids := make([]uuid.UUID, 0, len(chain))
	for _, c := range chain {
		if parsed := parseUUID(c.ID); parsed != uuid.Nil {
			ids = append(ids, parsed)
		}
	}
	if len(ids) == 0 {
		return &policyScores{Health: map[uuid.UUID]candidateHealth{}}
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT model_candidate_id,
		       avg(total_ms) FILTER (WHERE total_ms IS NOT NULL),
		       count(*) FILTER (WHERE state = 'success'),
		       count(*)
		FROM usage_records
		WHERE organization_id = $1 AND model_candidate_id = ANY($2)
		  AND state <> 'pending' AND created_at >= now() - interval '7 days'
		GROUP BY model_candidate_id
	`, orgID, ids)
	if err != nil {
		return &policyScores{Health: map[uuid.UUID]candidateHealth{}}
	}
	defer rows.Close()
	health := map[uuid.UUID]candidateHealth{}
	for rows.Next() {
		var id uuid.UUID
		var h candidateHealth
		var avgMS *float64
		if err := rows.Scan(&id, &avgMS, &h.Successes, &h.Total); err != nil {
			continue
		}
		if avgMS != nil {
			h.AvgMS = *avgMS
		}
		health[id] = h
	}
	return &policyScores{Health: health}
}

// loadService 按 org + name 解析 active 模型服务、active 候选与绑定的路由策略。
// 策略被禁用或不存在时回退到内置默认值。
func (d Deps) loadService(ctx context.Context, orgID uuid.UUID, name string) (resolvedService, bool, error) {
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
	svc.PolicyID = policyID
	if policyID != nil {
		var weightsRaw, constraintsRaw []byte
		err = d.Pool.QueryRow(ctx, `
			SELECT strategy, fallback_limit, provider_timeout_ms, weights, first_token_timeout_ms, constraints
			FROM route_policies
			WHERE id = $1 AND status = 'active'
		`, *policyID).Scan(&svc.Policy.Strategy, &svc.Policy.FallbackLimit, &svc.Policy.ProviderTimeoutMS,
			&weightsRaw, &svc.Policy.FirstTokenTimeoutMS, &constraintsRaw)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return svc, false, err
		}
		if errors.Is(err, pgx.ErrNoRows) {
			svc.Policy = defaultPolicy
		} else {
			svc.Policy.Weights = parsePolicyWeights(weightsRaw)
			svc.Policy.Constraints = parsePolicyConstraints(constraintsRaw)
		}
	} else {
		svc.Policy = defaultPolicy
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT c.id, a.provider, a.id, c.upstream_model, c.priority, c.weight,
		       c.input_cost_per_million, c.output_cost_per_million, c.quality_score
		FROM model_candidates c
		JOIN provider_accounts a ON a.id = c.provider_account_id
		WHERE c.model_service_id = $1 AND c.status = 'active' AND a.status = 'active'
		ORDER BY c.priority ASC
	`, svc.ID)
	if err != nil {
		return svc, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.ID, &c.Provider, &c.AccountID, &c.UpstreamModel,
			&c.Priority, &c.Weight, &c.InputCostPerM, &c.OutputCostPerM, &c.Quality); err != nil {
			return svc, false, err
		}
		svc.Candidates = append(svc.Candidates, c)
	}
	if err := rows.Err(); err != nil {
		return svc, false, err
	}
	// 硬约束在评分前过滤:被约束排除的候选不参与路由,也不进 fallback 链。
	svc.Candidates = svc.Policy.Constraints.filter(svc.Candidates)
	return svc, len(svc.Candidates) > 0, nil
}

// ChatCompletions 处理 POST /v1/chat/completions。
func (d Deps) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "缺少 API Key")
		return
	}
	meta, ok := parseChatRequest(w, r)
	if !ok {
		return
	}
	start := time.Now()

	svc, found, err := d.loadService(r.Context(), keyCtx.OrganizationID, meta.Model)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "模型服务解析失败")
		return
	}
	if !found {
		apiError(w, http.StatusNotFound, "model_not_found", "模型服务不存在或未启用: "+meta.Model)
		return
	}
	if !d.checkBudgets(w, r, keyCtx) {
		return
	}
	scores := d.loadCandidateHealth(r.Context(), keyCtx.OrganizationID, svc.Candidates)
	chain := selectChain(svc.Candidates, svc.Policy, scores)
	primary := chain[0]
	requestID := uuid.New()

	provider, err := bifrostx.MapProvider(primary.Provider)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "候选 provider 无法识别: "+primary.Provider)
		return
	}

	if err := d.recordPending(r.Context(), requestID, keyCtx, svc, chain); err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "路由决策记录失败")
		return
	}

	var input []schemas.ChatMessage
	if err := json.Unmarshal(meta.Messages, &input); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", "messages 字段不合法: "+err.Error())
		return
	}

	timeout := time.Duration(svc.Policy.ProviderTimeoutMS) * time.Millisecond
	// 上游调用从可取消的子 ctx 派生:首 token 看门狗用它中断卡死的流。
	requestCtx, cancelRequest := context.WithCancel(r.Context())
	defer cancelRequest()
	bctx := schemas.NewBifrostContext(requestCtx, time.Now().Add(timeout))
	req := &schemas.BifrostChatRequest{
		Provider:  provider,
		Model:     primary.UpstreamModel,
		Input:     input,
		Params:    meta.Params,
		Fallbacks: fallbackSpecs(chain),
	}

	if meta.Stream {
		// first_token_timeout_ms 仅对流式生效:超时未出首字则取消上游,按失败定稿。
		var firstTokenSeen atomic.Bool
		if svc.Policy.FirstTokenTimeoutMS > 0 {
			d.startFirstTokenWatchdog(requestCtx, cancelRequest, &firstTokenSeen,
				time.Duration(svc.Policy.FirstTokenTimeoutMS)*time.Millisecond, requestID)
		}
		d.handleStream(bctx, requestCtx, r, w, requestID, keyCtx, chain, meta.Model, req, start, &firstTokenSeen)
		return
	}
	d.handleNonStream(bctx, r, w, requestID, chain, meta.Model, req, start)
}

// startFirstTokenWatchdog 在首 token 未按时出现时取消上游调用。
// 看门狗只做取消,结果路径由 handleStream 统一按 first_token_timeout 定稿。
func (d Deps) startFirstTokenWatchdog(requestCtx context.Context, cancel context.CancelFunc,
	firstTokenSeen *atomic.Bool, timeout time.Duration, requestID uuid.UUID) {
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-requestCtx.Done():
			return
		case <-timer.C:
			if !firstTokenSeen.Load() {
				d.Logger.Warn("first token timeout", "request_id", requestID, "timeout_ms", timeout.Milliseconds())
				cancel()
			}
		}
	}()
}

// fallbackSpecs 把主候选之后的链转成 Bifrost 的 fallback 列表,并用 KeyID 钉住对应账号。
func fallbackSpecs(chain routeChain) []schemas.Fallback {
	if len(chain) <= 1 {
		return nil
	}
	fallbacks := make([]schemas.Fallback, 0, len(chain)-1)
	for _, c := range chain[1:] {
		provider, err := bifrostx.MapProvider(c.Provider)
		if err != nil {
			continue
		}
		fallbacks = append(fallbacks, schemas.Fallback{Provider: provider, Model: c.UpstreamModel, KeyID: c.AccountID})
	}
	if len(fallbacks) == 0 {
		return nil
	}
	return fallbacks
}

// recordPending 写入 pending 用量与 allowed 路由决策。
func (d Deps) recordPending(ctx context.Context, requestID uuid.UUID, keyCtx apiKeyContext, svc resolvedService, chain routeChain) error {
	candidatesJSON, err := json.Marshal(chain)
	if err != nil {
		return fmt.Errorf("marshal candidates: %w", err)
	}
	primary := chain[0]
	fallbackIDs := make([]uuid.UUID, 0, len(chain)-1)
	for _, c := range chain[1:] {
		fallbackIDs = append(fallbackIDs, uuid.MustParse(c.ID))
	}
	selectedID := uuid.MustParse(primary.ID)
	store := usageStore{pool: d.Pool}
	return store.insertPending(ctx, pendingUsage{
		RequestID:      requestID,
		OrganizationID: keyCtx.OrganizationID,
		ProjectID:      keyCtx.ProjectID,
		ApplicationID:  keyCtx.ApplicationID,
		APIKeyID:       keyCtx.KeyID,
		ModelServiceID: svc.ID,
		RoutePolicyID:  svc.PolicyID,
		Reason: fmt.Sprintf("selected candidate %s (%s/%s) by priority and weight",
			primary.ID, primary.Provider, primary.UpstreamModel),
		Candidates:  candidatesJSON,
		SelectedID:  &selectedID,
		FallbackIDs: fallbackIDs,
	})
}

// handleNonStream 调用上游并返回完整响应。
func (d Deps) handleNonStream(bctx *schemas.BifrostContext, r *http.Request, w http.ResponseWriter,
	requestID uuid.UUID, chain routeChain, requestedModel string,
	req *schemas.BifrostChatRequest, start time.Time) {
	resp, bifrostErr := d.Engine.Client().ChatCompletionRequest(bctx, req)
	if bifrostErr != nil {
		d.recordFailure(r.Context(), requestID, chain, bctx, bifrostErr, start, nil, usageSnapshot{})
		writeBifrostError(w, bifrostErr)
		return
	}
	total := int(time.Since(start).Milliseconds())
	usage := usageFromBifrost(resp.Usage)
	servedProvider := string(resp.ExtraFields.RoutingInfo.Provider)
	servedModel := resp.ExtraFields.RoutingInfo.Model
	served, lookupErr := chain.findCandidate(servedProvider, servedModel)

	candidateID := uuidPtr(served.ID, lookupErr == nil)
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

	store := usageStore{pool: d.Pool}
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
		TotalMS:         total,
		UsageSource:     usageSource,
		AttemptTrail:    attemptTrailJSON(chain, served, "success", "", routingEngineLogs(bctx)),
	})
	if finalizeErr != nil {
		d.Logger.Warn("finalize usage failed", "request_id", requestID, "error", finalizeErr)
	}
	d.settleBudget(context.WithoutCancel(r.Context()), keyFromContextOrEmpty(r), requestID, cost)
}

// recordFailure 在上游失败时定稿用量并记录失败明细。finalize 使用不受请求取消影响的 ctx。
// bctx 用于读取 Bifrost 路由引擎日志,把每次失败尝试写入 attempt_trail。
func (d Deps) recordFailure(ctx context.Context, requestID uuid.UUID, chain routeChain,
	bctx *schemas.BifrostContext, bifrostErr *schemas.BifrostError, start time.Time, firstTokenMS *int, partial usageSnapshot) {
	total := int(time.Since(start).Milliseconds())
	statusCode := http.StatusBadGateway
	if bifrostErr.StatusCode != nil && *bifrostErr.StatusCode >= 400 && *bifrostErr.StatusCode < 600 {
		statusCode = *bifrostErr.StatusCode
	}
	errType := "upstream_error"
	if bifrostErr.Type != nil {
		errType = *bifrostErr.Type
	}
	message := ""
	if bifrostErr.Error != nil {
		message = bifrostErr.Error.Message
	}
	usageSource := stringPtrNil("computed", partial.hasTokens())

	// 失败归因:错误响应携带实际尝试的 provider/model 时,定位链中对应候选,
	// 让失败用量与汇总报表能归到具体上游,而不是留空。
	attemptedProvider := string(bifrostErr.ExtraFields.RoutingInfo.Provider)
	attempted, lookupErr := chain.findCandidate(attemptedProvider, bifrostErr.ExtraFields.RoutingInfo.Model)
	var attemptedModel *string
	if bifrostErr.ExtraFields.RoutingInfo.Model != "" {
		attemptedModel = stringPtr(bifrostErr.ExtraFields.RoutingInfo.Model)
	}

	store := usageStore{pool: d.Pool}
	if err := store.finalize(context.WithoutCancel(ctx), requestID, finalUsage{
		State:           "failed",
		StatusCode:      intPtr(statusCode),
		CandidateID:     uuidPtr(attempted.ID, lookupErr == nil),
		Provider:        stringPtr(attemptedProvider),
		UpstreamModel:   attemptedModel,
		InputTokens:     partial.InputTokens,
		OutputTokens:    partial.OutputTokens,
		CachedTokens:    partial.CachedTokens,
		ReasoningTokens: partial.ReasoningTokens,
		FirstTokenMS:    firstTokenMS,
		TotalMS:         total,
		UsageSource:     usageSource,
		ErrorType:       stringPtr(errType),
		ErrorMessage:    stringPtr(message),
		AttemptTrail:    attemptTrailJSON(chain, attempted, "failed", message, routingEngineLogs(bctx)),
	}); err != nil {
		d.Logger.Warn("finalize failed usage error", "request_id", requestID, "error", err)
	}
}

// writeBifrostError 把 Bifrost 错误转成 OpenAI 风格错误 JSON。
func writeBifrostError(w http.ResponseWriter, bifrostErr *schemas.BifrostError) {
	status := http.StatusBadGateway
	if bifrostErr.StatusCode != nil && *bifrostErr.StatusCode >= 400 && *bifrostErr.StatusCode < 600 {
		status = *bifrostErr.StatusCode
	}
	errType := "upstream_error"
	if bifrostErr.Type != nil {
		errType = *bifrostErr.Type
	}
	message := "upstream request failed"
	if bifrostErr.Error != nil && bifrostErr.Error.Message != "" {
		message = bifrostErr.Error.Message
	}
	apiError(w, status, errType, message)
}
