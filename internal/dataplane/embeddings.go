package dataplane

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/maximhq/bifrost/core/schemas"
)

const maxEmbeddingBodyBytes = 8 * 1024 * 1024

// embeddingRequestMeta 是 OpenAI 兼容 embeddings 请求体骨架。
type embeddingRequestMeta struct {
	Model string
	Input []string
}

// parseEmbeddingRequest 解析 model 与 input;input 支持 string 或 string[]。
// 其余字段(M 维参数)不透传,embedding 阶段仅做纯文本向量化。
func parseEmbeddingRequest(w http.ResponseWriter, r *http.Request) (embeddingRequestMeta, bool) {
	var raw struct {
		Model string          `json:"model"`
		Input json.RawMessage `json:"input"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEmbeddingBodyBytes))
	if err := decoder.Decode(&raw); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", "请求体不是合法 JSON: "+err.Error())
		return embeddingRequestMeta{}, false
	}
	if raw.Model == "" {
		apiError(w, http.StatusBadRequest, "invalid_request", "缺少 model 字段")
		return embeddingRequestMeta{}, false
	}
	meta := embeddingRequestMeta{Model: raw.Model}
	var single string
	if err := json.Unmarshal(raw.Input, &single); err == nil {
		meta.Input = []string{single}
	} else {
		if err := json.Unmarshal(raw.Input, &meta.Input); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request", "input 字段必须是 string 或 string[]")
			return embeddingRequestMeta{}, false
		}
	}
	if len(meta.Input) == 0 {
		apiError(w, http.StatusBadRequest, "invalid_request", "缺少 input 字段")
		return embeddingRequestMeta{}, false
	}
	for _, text := range meta.Input {
		if text == "" {
			apiError(w, http.StatusBadRequest, "invalid_request", "input 中存在空文本")
			return embeddingRequestMeta{}, false
		}
	}
	return meta, true
}

// Embeddings 处理 POST /v1/embeddings:路由到主候选,失败时按 fallback 链重试(非流式)。
func (d Deps) Embeddings(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "缺少 API Key")
		return
	}
	meta, ok := parseEmbeddingRequest(w, r)
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

	items := make([]schemas.EmbeddingInputItem, 0, len(meta.Input))
	for _, text := range meta.Input {
		text := text
		items = append(items, schemas.EmbeddingInputItem{
			Content: schemas.EmbeddingContent{
				{Type: schemas.EmbeddingContentPartTypeText, Text: &text},
			},
		})
	}

	timeout := time.Duration(svc.Policy.ProviderTimeoutMS) * time.Millisecond
	bctx := schemas.NewBifrostContext(r.Context(), time.Now().Add(timeout))
	req := &schemas.BifrostEmbeddingRequest{
		Provider:  provider,
		Model:     primary.UpstreamModel,
		Input:     items,
		Fallbacks: fallbackSpecs(chain),
	}

	resp, bifrostErr := d.Engine.Client().EmbeddingRequest(bctx, req)
	if bifrostErr != nil {
		d.recordFailure(r.Context(), requestID, chain, bctx, bifrostErr, start, nil, usageSnapshot{})
		writeBifrostError(w, bifrostErr)
		return
	}

	servedProvider := string(resp.ExtraFields.RoutingInfo.Provider)
	servedModel := resp.ExtraFields.RoutingInfo.Model
	served, lookupErr := chain.findCandidate(servedProvider, servedModel)

	usage := usageFromBifrost(resp.Usage)
	inputTokens, outputTokens, _, _ := usage.tokens()
	cost := 0.0
	usageSource := stringPtrNil("computed", usage.hasTokens())
	if usage.hasTokens() {
		// embedding 成本按输入 token 计价。
		cost = served.costUSD(inputTokens, 0)
	}

	// 转回 OpenAI 响应结构;EmbeddingStruct 保留 provider 原始精度并自带 marshal。
	data := make([]map[string]any, 0, len(resp.Data))
	for _, item := range resp.Data {
		data = append(data, map[string]any{
			"object":    "embedding",
			"index":     item.Index,
			"embedding": item.Embedding,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"model":  servedModel,
		"data":   data,
		"usage": map[string]any{
			"prompt_tokens": inputTokens,
			"total_tokens":  inputTokens + outputTokens,
		},
	})

	total := int(time.Since(start).Milliseconds())
	candidateID := uuidPtr(served.ID, lookupErr == nil)
	store := usageStore{pool: d.Pool}
	if err := store.finalize(context.WithoutCancel(r.Context()), requestID, finalUsage{
		State:         "success",
		StatusCode:    intPtr(http.StatusOK),
		CandidateID:   candidateID,
		Provider:      stringPtr(servedProvider),
		UpstreamModel: stringPtr(servedModel),
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		Cost:          cost,
		Currency:      currencyOf(served),
		TotalMS:       total,
		UsageSource:   usageSource,
		AttemptTrail:  attemptTrailJSON(chain, served, "success", "", routingEngineLogs(bctx)),
	}); err != nil {
		d.Logger.Warn("finalize usage failed", "request_id", requestID, "error", err)
	}
	d.settleBudget(context.WithoutCancel(r.Context()), keyCtx, requestID, cost, currencyOf(served))
}
