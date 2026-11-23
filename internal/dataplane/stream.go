package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"
)

// chatStreamChunk 是 SSE 输出的 OpenAI 兼容 chunk。
type chatStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int                  `json:"created"`
	Model   string               `json:"model"`
	Choices []chatStreamChoice   `json:"choices"`
	Usage   *chatCompletionUsage `json:"usage,omitempty"`
}

type chatStreamChoice struct {
	Index        int                                    `json:"index"`
	Delta        *schemas.ChatStreamResponseChoiceDelta `json:"delta"`
	FinishReason *string                                `json:"finish_reason"`
}

// handleStream 调用上游并把流式 chunk 转成 SSE 输出;结束时定稿用量。
// 客户端断开记 interrupted;上游报错记 failed 并在流内输出错误事件。
func (d Deps) handleStream(bctx *schemas.BifrostContext, streamCtx context.Context, r *http.Request, w http.ResponseWriter,
	requestID uuid.UUID, keyCtx apiKeyContext, chain routeChain, requestedModel string,
	req *schemas.BifrostChatRequest, start time.Time, firstTokenSeen *atomic.Bool) {
	d.handleStreamObserved(bctx, streamCtx, r, w, requestID, keyCtx, chain, requestedModel, req, start, firstTokenSeen, nil)
}

func (d Deps) handleStreamObserved(bctx *schemas.BifrostContext, streamCtx context.Context, r *http.Request, w http.ResponseWriter,
	requestID uuid.UUID, keyCtx apiKeyContext, chain routeChain, requestedModel string,
	req *schemas.BifrostChatRequest, start time.Time, firstTokenSeen *atomic.Bool, observer *completionObserver) {
	stream, bifrostErr := d.Engine.Client().ChatCompletionStreamRequest(bctx, req)
	if bifrostErr != nil {
		// 看门狗在建立流阶段取消(非客户端断开):按首 token 超时定稿,不记上游错误。
		if streamCtx.Err() != nil && r.Context().Err() == nil && firstTokenSeen != nil && !firstTokenSeen.Load() {
			d.recordFirstTokenTimeout(r.Context(), requestID, chain, start, routingEngineLogs(bctx))
			if observer != nil {
				observer.onComplete("failed", DebugResult{TotalMS: int(time.Since(start).Milliseconds())}, context.DeadlineExceeded)
			}
			apiError(w, http.StatusGatewayTimeout, "timeout",
				"Upstream did not return the first token within first_token_timeout_ms.",
				apiErrorOptions{Reason: "first_token_timeout"})
			return
		}
		d.recordFailure(r.Context(), requestID, chain, bctx, bifrostErr, start, nil, usageSnapshot{})
		if observer != nil {
			observer.onComplete("failed", DebugResult{TotalMS: int(time.Since(start).Milliseconds())}, errors.New(errorMessageOf(bifrostErr)))
		}
		writeBifrostError(w, bifrostErr)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		if observer != nil {
			observer.onComplete("failed", DebugResult{TotalMS: int(time.Since(start).Milliseconds())}, errors.New("The current connection does not support streaming responses."))
		}
		apiError(w, http.StatusInternalServerError, "internal_error", "The current connection does not support streaming responses.",
			apiErrorOptions{Reason: "streaming_unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	var snapshot usageSnapshot
	var firstTokenMS *int
	var servedProvider, servedModel, chunkID string
	var chunkCreated int
	var streamErr *schemas.BifrostError
	interrupted := false
	var content strings.Builder
	finishObserver := func(status string, err error) {
		if observer == nil {
			return
		}
		cost := 0.0
		currency := currencyOf(chain[0])
		if served, lookupErr := chain.findCandidate(servedProvider, servedModel); lookupErr == nil {
			cost = served.costUSD(snapshot.InputTokens, snapshot.OutputTokens)
			currency = currencyOf(served)
		}
		observer.onComplete(status, DebugResult{
			Response: content.String(), Provider: servedProvider, UpstreamModel: servedModel,
			InputTokens: snapshot.InputTokens, OutputTokens: snapshot.OutputTokens,
			CachedTokens: snapshot.CachedTokens, ReasoningTokens: snapshot.ReasoningTokens,
			Cost: cost, Currency: currency, FirstTokenMS: firstTokenMS, TotalMS: int(time.Since(start).Milliseconds()),
		}, err)
	}

	writeEvent := func(payload []byte) bool {
		if _, err := w.Write(append(append([]byte("data: "), payload...), '\n', '\n')); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

streamLoop:
	for {
		select {
		case <-r.Context().Done():
			interrupted = true
			break streamLoop
		case chunk, open := <-stream:
			if !open {
				break streamLoop
			}
			if chunk.BifrostError != nil {
				streamErr = chunk.BifrostError
				break streamLoop
			}
			resp := chunk.BifrostChatResponse
			if resp == nil {
				continue
			}
			if chunkID == "" {
				chunkID = resp.ID
				chunkCreated = resp.Created
				servedProvider = string(resp.ExtraFields.RoutingInfo.Provider)
				servedModel = resp.ExtraFields.RoutingInfo.Model
			}
			if u := usageFromBifrost(resp.Usage); u.hasTokens() {
				// 用量在最终 chunk 上以累计值出现,这里直接覆盖为最新值。
				snapshot = usageSnapshot(u)
			}
			outChunk := chatStreamChunk{
				ID:      resp.ID,
				Object:  "chat.completion.chunk",
				Created: resp.Created,
				Model:   requestedModel,
				Choices: []chatStreamChoice{},
			}
			for i, choice := range resp.Choices {
				if observer != nil && choice.Delta != nil && choice.Delta.Content != nil {
					content.WriteString(*choice.Delta.Content)
				}
				if choice.Delta != nil && choice.Delta.Content != nil && *choice.Delta.Content != "" && firstTokenMS == nil {
					elapsed := int(time.Since(start).Milliseconds())
					firstTokenMS = &elapsed
					firstTokenSeen.Store(true)
				}
				outChunk.Choices = append(outChunk.Choices, chatStreamChoice{Index: i, Delta: choice.Delta, FinishReason: choice.FinishReason})
			}
			payload, err := json.Marshal(outChunk)
			if err != nil {
				d.Logger.Warn("marshal stream chunk failed", "request_id", requestID, "error", err)
				break streamLoop
			}
			if !writeEvent(payload) {
				// 写失败说明客户端连接已不可用。
				interrupted = true
				break streamLoop
			}
		}
	}

	if interrupted {
		d.recordInterrupted(r.Context(), requestID, chain, start, firstTokenMS, snapshot, routingEngineLogs(bctx))
		finishObserver("interrupted", nil)
		return
	}
	if streamCtx.Err() != nil && firstTokenMS == nil {
		// 看门狗取消且没有收到任何内容:按首 token 超时定稿并在流内输出错误事件。
		d.recordFirstTokenTimeout(r.Context(), requestID, chain, start, routingEngineLogs(bctx))
		finishObserver("failed", context.DeadlineExceeded)
		errorPayload := apiErrorEvent(http.StatusGatewayTimeout, "timeout", "Upstream did not return the first token within first_token_timeout_ms.", requestID.String(),
			apiErrorOptions{Reason: "first_token_timeout"})
		_ = writeEvent(errorPayload)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
		return
	}
	if streamErr != nil {
		// 流已开始,状态码无法再改;按 OpenAI 惯例在流内输出错误事件后结束。
		errorPayload := apiErrorEvent(http.StatusBadGateway, "upstream_error", "Upstream request failed.", requestID.String(),
			apiErrorOptions{Type: "upstream_error", Reason: "upstream_request_failed", Detail: errorMessageOf(streamErr), Params: map[string]any{"upstream_type": errorTypeOf(streamErr)}})
		_ = writeEvent(errorPayload)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
		d.recordFailure(r.Context(), requestID, chain, bctx, streamErr, start, firstTokenMS, snapshot)
		finishObserver("failed", errors.New(errorMessageOf(streamErr)))
		return
	}

	final := chatStreamChunk{
		ID:      chunkID,
		Object:  "chat.completion.chunk",
		Created: chunkCreated,
		Model:   requestedModel,
		Choices: []chatStreamChoice{},
		Usage: &chatCompletionUsage{
			PromptTokens:     int(snapshot.InputTokens),
			CompletionTokens: int(snapshot.OutputTokens),
			TotalTokens:      int(snapshot.InputTokens + snapshot.OutputTokens),
		},
	}
	if payload, err := json.Marshal(final); err == nil {
		_ = writeEvent(payload)
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()

	d.recordStreamSuccess(r.Context(), requestID, keyCtx, chain, servedProvider, servedModel, start, firstTokenMS, snapshot, routingEngineLogs(bctx))
	finishObserver("success", nil)
}

// recordFirstTokenTimeout 定稿一次首 token 超时的流式请求。
func (d Deps) recordFirstTokenTimeout(ctx context.Context, requestID uuid.UUID, chain routeChain,
	start time.Time, logs []schemas.RoutingEngineLogEntry) {
	total := int(time.Since(start).Milliseconds())
	primary := chain[0]
	store := usageStore{pool: d.Pool}
	if err := store.finalize(context.WithoutCancel(ctx), requestID, finalUsage{
		State:        "failed",
		StatusCode:   intPtr(http.StatusGatewayTimeout),
		Provider:     stringPtr(primary.Provider),
		TotalMS:      total,
		ErrorType:    stringPtr("first_token_timeout"),
		ErrorMessage: stringPtr("Upstream did not return the first token within first_token_timeout_ms."),
		AttemptTrail: attemptTrailJSON(chain, candidate{}, "failed", "first_token_timeout", logs),
	}); err != nil {
		d.Logger.Warn("finalize first token timeout usage failed", "request_id", requestID, "error", err)
	}
}

// recordStreamSuccess 定稿一次成功的流式请求。
func (d Deps) recordStreamSuccess(ctx context.Context, requestID uuid.UUID, keyCtx apiKeyContext, chain routeChain,
	servedProvider, servedModel string, start time.Time, firstTokenMS *int, snapshot usageSnapshot,
	logs []schemas.RoutingEngineLogEntry) {
	total := int(time.Since(start).Milliseconds())
	served, lookupErr := chain.findCandidate(servedProvider, servedModel)
	if lookupErr != nil {
		// 上游返回的 provider/model 与候选不匹配时,不猜候选,候选与成本记空。
		d.Logger.Warn("served candidate not found in chain", "request_id", requestID,
			"provider", servedProvider, "model", servedModel, "error", lookupErr)
	}
	cost := 0.0
	if lookupErr == nil {
		cost = served.costUSD(snapshot.InputTokens, snapshot.OutputTokens)
	}
	store := usageStore{pool: d.Pool}
	if err := store.finalize(context.WithoutCancel(ctx), requestID, finalUsage{
		State:           "success",
		StatusCode:      intPtr(http.StatusOK),
		CandidateID:     uuidPtr(served.ID, lookupErr == nil),
		Provider:        stringPtr(servedProvider),
		UpstreamModel:   stringPtr(servedModel),
		InputTokens:     snapshot.InputTokens,
		OutputTokens:    snapshot.OutputTokens,
		CachedTokens:    snapshot.CachedTokens,
		ReasoningTokens: snapshot.ReasoningTokens,
		Cost:            cost,
		Currency:        currencyOf(served),
		FirstTokenMS:    firstTokenMS,
		TotalMS:         total,
		UsageSource:     stringPtrNil("computed", snapshot.hasTokens()),
		AttemptTrail:    attemptTrailJSON(chain, served, "success", "", logs),
	}); err != nil {
		d.Logger.Warn("finalize stream usage failed", "request_id", requestID, "error", err)
	}
	d.settleBudget(context.WithoutCancel(ctx), keyCtx, requestID, cost, currencyOf(served))
}

// recordInterrupted 定稿一次客户端断开的流式请求。
func (d Deps) recordInterrupted(ctx context.Context, requestID uuid.UUID, chain routeChain,
	start time.Time, firstTokenMS *int, snapshot usageSnapshot, logs []schemas.RoutingEngineLogEntry) {
	total := int(time.Since(start).Milliseconds())
	store := usageStore{pool: d.Pool}
	if err := store.finalize(context.WithoutCancel(ctx), requestID, finalUsage{
		State:           "interrupted",
		FirstTokenMS:    firstTokenMS,
		TotalMS:         total,
		InputTokens:     snapshot.InputTokens,
		OutputTokens:    snapshot.OutputTokens,
		CachedTokens:    snapshot.CachedTokens,
		ReasoningTokens: snapshot.ReasoningTokens,
		UsageSource:     stringPtrNil("computed", snapshot.hasTokens()),
		AttemptTrail:    attemptTrailJSON(chain, candidate{}, "interrupted", "client disconnected", logs),
	}); err != nil {
		d.Logger.Warn("finalize interrupted usage failed", "request_id", requestID, "error", err)
	}
}

func errorTypeOf(err *schemas.BifrostError) string {
	if err.Type != nil {
		return *err.Type
	}
	return "upstream_error"
}

func errorMessageOf(err *schemas.BifrostError) string {
	if err.Error != nil && err.Error.Message != "" {
		return err.Error.Message
	}
	return "upstream request failed"
}
