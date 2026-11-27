package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/inwinter04/omniflux/internal/bifrostx"
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

func chunkHasMeaningfulDelta(resp *schemas.BifrostChatResponse) bool {
	if resp == nil {
		return false
	}
	for _, ch := range resp.Choices {
		if ch.Delta != nil {
			if (ch.Delta.Content != nil && *ch.Delta.Content != "") ||
				(ch.Delta.Reasoning != nil && *ch.Delta.Reasoning != "") ||
				len(ch.Delta.ToolCalls) > 0 {
				return true
			}
		}
		if ch.FinishReason != nil {
			return true
		}
	}
	return false
}

// awaitFirstStreamChunk 在向客户端提交 HTTP 200 SSE 之前等待首条有效内容，
// 确保 TTFT 超时或建流报错时能够无痕切换到备用候选而不污染输出流。
func awaitFirstStreamChunk(ctx context.Context, stream chan *schemas.BifrostStreamChunk) (
	firstResp *schemas.BifrostChatResponse, snapshot usageSnapshot, streamErr *schemas.BifrostError, closed bool) {
	for {
		select {
		case <-ctx.Done():
			return nil, snapshot, nil, false
		case chunk, open := <-stream:
			if !open {
				return nil, snapshot, nil, true
			}
			if chunk.BifrostError != nil {
				return nil, snapshot, chunk.BifrostError, false
			}
			resp := chunk.BifrostChatResponse
			if resp == nil {
				continue
			}
			if u := usageFromBifrost(resp.Usage); u.hasTokens() {
				snapshot = usageSnapshot(u)
			}
			if chunkHasMeaningfulDelta(resp) {
				return resp, snapshot, nil, false
			}
		}
	}
}

// handleStreamChain 执行带 TTFT 故障转移、错误码矩阵与短期熔断的流式路由链：
// 一旦发出首条有效内容即提交 SSE 响应，此后禁止透明 fallback 避免回答拼接。
func (d Deps) handleStreamChain(r *http.Request, w http.ResponseWriter, requestID uuid.UUID, keyCtx *apiKeyContext,
	svc resolvedService, decision RouteDecision, requestedModel string, input []schemas.ChatMessage,
	params *schemas.ChatParameters, start time.Time, observer *completionObserver) {
	rp := svc.Policy.RetryPolicy
	cb := svc.Policy.CircuitBreaker
	perTimeout := time.Duration(svc.Policy.ProviderTimeoutMS) * time.Millisecond
	if perTimeout <= 0 {
		perTimeout = 60 * time.Second
	}
	ttftDur := time.Duration(svc.Policy.FirstTokenTimeoutMS) * time.Millisecond
	if ttftDur <= 0 {
		ttftDur = 45 * time.Second
	}

	idleTimeout := defaultStreamIdleTimeout
	if perTimeout > idleTimeout {
		idleTimeout = perTimeout
	}

	attempts := make([]AttemptRecord, 0, len(decision.Chain))
	skipAccounts := make(map[string]bool)
	var (
		winningCand   candidate
		winningStream chan *schemas.BifrostStreamChunk
		firstResp     *schemas.BifrostChatResponse
		initSnapshot  usageSnapshot
		cancelWinner  context.CancelFunc
		lastErr       *schemas.BifrostError
		lastTTFT      bool
		clientAborted bool
	)
	recordTTFT := func(cand candidate, attemptMS, retryIdx int) bool {
		lastTTFT = true
		lastErr = &schemas.BifrostError{
			StatusCode: intPtr(http.StatusGatewayTimeout),
			Type:       stringPtr("first_token_timeout"),
			Error:      &schemas.ErrorField{Code: stringPtr("ttft_timeout"), Message: "Upstream did not return the first token within first_token_timeout_ms."},
		}
		action := ClassifyAttemptOutcome(http.StatusGatewayTimeout, "ttft_timeout", "first_token_timeout", rp)
		d.Breaker().RecordFailure(r.Context(), cand.ID, http.StatusGatewayTimeout, "ttft_timeout", "first_token_timeout", rp, cb)
		attempts = append(attempts, AttemptRecord{
			CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
			StatusCode: http.StatusGatewayTimeout, ErrorClass: "ttft_timeout", ErrorType: "first_token_timeout",
			ErrorMessage: "first token timeout", LatencyMS: attemptMS, RetryCount: retryIdx, Outcome: "ttft_timeout",
		})
		return action.ShouldFallback
	}

candidateLoop:
	for idx, cand := range decision.Chain {
		if r.Context().Err() != nil {
			clientAborted = true
			break
		}
		if skipAccounts[cand.AccountID] {
			attempts = append(attempts, AttemptRecord{
				CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
				Outcome: "skipped_same_account",
			})
			continue
		}
		provider, err := bifrostx.EngineProviderKey(cand.AccountID, cand.Provider, cand.APIProtocol)
		if err != nil {
			attempts = append(attempts, AttemptRecord{
				CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
				ErrorClass: "unsupported_provider", ErrorMessage: err.Error(), Outcome: "failed",
			})
			continue
		}

		isFinalCandidate := idx == len(decision.Chain)-1
		preCommitTimeout := ttftDur
		if isFinalCandidate {
			preCommitTimeout = minFinalCandidatePreCommitTimeout
			if perTimeout > preCommitTimeout {
				preCommitTimeout = perTimeout
			}
		}

		maxRetries := rp.MaxRetriesPerCandidate
		for retryIdx := 0; retryIdx <= maxRetries; retryIdx++ {
			attemptStart := time.Now()
			ctrl := newStreamAttemptControl(r.Context(), preCommitTimeout)
			bctx := schemas.NewBifrostContext(ctrl.ctx, schemas.NoDeadline)
			bctx.SetValue(schemas.BifrostContextKeyAPIKeyID, cand.AccountID)
			bctx.SetValue(schemas.BifrostContextKeyStreamIdleTimeout, idleTimeout)
			if !isFinalCandidate {
				bctx.SetValue(schemas.BifrostContextKeyStreamFirstTokenTimeout, ttftDur)
			}
			candParams, passthrough := applyProviderStreamingHintsForChat(params, cand)
			if passthrough {
				bctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
			}
			req := &schemas.BifrostChatRequest{
				Provider: provider,
				Model:    cand.UpstreamModel,
				Input:    input,
				Params:   candParams,
			}

			stream, bifrostErr := d.Engine.Client().ChatCompletionStreamRequest(bctx, req)
			if bifrostErr != nil {
				wasTTFT := ctrl.disarmTimedOut()
				ctrl.cancel()
				attemptMS := int(time.Since(attemptStart).Milliseconds())
				if r.Context().Err() != nil {
					clientAborted = true
					break candidateLoop
				}
				if wasTTFT {
					if !recordTTFT(cand, attemptMS, retryIdx) {
						break candidateLoop
					}
					break
				}
				statusCode, errCode, errType, errMsg := extractBifrostErrorInfo(bifrostErr)
				if d.OnTransportError != nil && statusCode == 0 {
					d.OnTransportError(logicalProviderName(cand.Provider))
				}
				action := ClassifyAttemptOutcome(statusCode, errCode, errType, rp)
				d.Breaker().RecordFailure(r.Context(), cand.ID, statusCode, errCode, errType, rp, cb)
				lastErr = bifrostErr
				lastTTFT = false

				if action.ShouldRetry && retryIdx < maxRetries && r.Context().Err() == nil {
					backoff := 250 * time.Millisecond
					if statusCode == http.StatusTooManyRequests {
						backoff = 600 * time.Millisecond
					}
					select {
					case <-time.After(backoff):
						continue
					case <-r.Context().Done():
						clientAborted = true
						break candidateLoop
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
					break candidateLoop
				}
				break
			}

			resp, snap, preErr, closedEarly := awaitFirstStreamChunk(ctrl.ctx, stream)
			wasTTFT := ctrl.disarmTimedOut()
			attemptMS := int(time.Since(attemptStart).Milliseconds())

			if r.Context().Err() != nil {
				ctrl.cancel()
				clientAborted = true
				break candidateLoop
			}
			if wasTTFT {
				ctrl.cancel()
				if !recordTTFT(cand, attemptMS, retryIdx) {
					break candidateLoop
				}
				break
			}
			if preErr != nil {
				ctrl.cancel()
				lastTTFT = false
				lastErr = preErr
				statusCode, errCode, errType, errMsg := extractBifrostErrorInfo(preErr)
				if d.OnTransportError != nil && statusCode == 0 {
					d.OnTransportError(logicalProviderName(cand.Provider))
				}
				action := ClassifyAttemptOutcome(statusCode, errCode, errType, rp)
				d.Breaker().RecordFailure(r.Context(), cand.ID, statusCode, errCode, errType, rp, cb)
				attempts = append(attempts, AttemptRecord{
					CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
					StatusCode: statusCode, ErrorClass: action.Class, ErrorType: errType, ErrorMessage: errMsg,
					LatencyMS: attemptMS, RetryCount: retryIdx, Outcome: "failed",
				})
				if action.SkipSameAccount {
					skipAccounts[cand.AccountID] = true
				}
				if !action.ShouldFallback {
					break candidateLoop
				}
				break
			}
			if closedEarly || resp == nil || !ctrl.commit() {
				ctrl.disarmTimedOut()
				ctrl.cancel()
				lastTTFT = false
				lastErr = &schemas.BifrostError{
					StatusCode: intPtr(http.StatusBadGateway),
					Type:       stringPtr("upstream_error"),
					Error:      &schemas.ErrorField{Message: "Upstream stream closed before producing content."},
				}
				d.Breaker().RecordFailure(r.Context(), cand.ID, http.StatusBadGateway, "", "upstream_error", rp, cb)
				attempts = append(attempts, AttemptRecord{
					CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
					StatusCode: http.StatusBadGateway, ErrorClass: "server_error", ErrorType: "upstream_error",
					ErrorMessage: "empty stream", LatencyMS: attemptMS, RetryCount: retryIdx, Outcome: "failed",
				})
				if !rp.FallbackOnServerError {
					break candidateLoop
				}
				break
			}

			// 首个有效内容已经就绪：锁定当前候选并退出尝试循环，开始 SSE 流式提交。
			winningCand = cand
			winningStream = stream
			firstResp = resp
			initSnapshot = snap
			cancelWinner = ctrl.cancel
			break candidateLoop
		}
	}

	if clientAborted {
		d.recordStreamInterrupted(r.Context(), requestID, winningCand, attempts, start, nil, usageSnapshot{})
		if observer != nil {
			observer.onComplete("interrupted", DebugResult{TotalMS: int(time.Since(start).Milliseconds())}, nil)
		}
		return
	}
	if firstResp == nil || winningStream == nil {
		if lastErr == nil {
			lastErr = &schemas.BifrostError{
				StatusCode: intPtr(http.StatusBadGateway),
				Type:       stringPtr("upstream_error"),
				Error:      &schemas.ErrorField{Message: "All upstream model candidates failed."},
			}
		}
		if lastTTFT {
			d.recordChainFailureWithProtocol(r.Context(), keyCtx, requestID, decision.Chain, attempts, lastErr, start, nil, usageSnapshot{}, stringOrDefault(decision.Protocol, ClientProtocolOpenAIChat))
			if observer != nil {
				observer.onComplete("failed", DebugResult{TotalMS: int(time.Since(start).Milliseconds())}, context.DeadlineExceeded)
			}
			apiError(w, http.StatusGatewayTimeout, "timeout",
				"Upstream did not return the first token within first_token_timeout_ms.",
				apiErrorOptions{Reason: "first_token_timeout"})
			return
		}
		d.recordChainFailureWithProtocol(r.Context(), keyCtx, requestID, decision.Chain, attempts, lastErr, start, nil, usageSnapshot{}, stringOrDefault(decision.Protocol, ClientProtocolOpenAIChat))
		if observer != nil {
			observer.onComplete("failed", DebugResult{TotalMS: int(time.Since(start).Milliseconds())}, errors.New(errorMessageOf(lastErr)))
		}
		writeBifrostError(w, lastErr)
		return
	}
	defer cancelWinner()

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
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// Clear connection write deadline once SSE headers are committed so long responses are not cut at http.Server.WriteTimeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	flusher.Flush()

	writeEvent := func(payload []byte) bool {
		if _, err := w.Write(append(append([]byte("data: "), payload...), '\n', '\n')); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	elapsedFirst := int(time.Since(start).Milliseconds())
	firstTokenMS := &elapsedFirst
	snapshot := initSnapshot
	chunkID := firstResp.ID
	chunkCreated := firstResp.Created
	servedProvider := logicalProviderName(winningCand.Provider)
	if servedProvider == "" {
		servedProvider = logicalProviderName(string(firstResp.ExtraFields.RoutingInfo.Provider))
	}
	servedModel := firstResp.ExtraFields.RoutingInfo.Model
	if servedModel == "" {
		servedModel = winningCand.UpstreamModel
	}

	var content strings.Builder
	emitRespChunk := func(resp *schemas.BifrostChatResponse) bool {
		outChunk := chatStreamChunk{
			ID:      resp.ID,
			Object:  "chat.completion.chunk",
			Created: resp.Created,
			Model:   requestedModel,
			Choices: make([]chatStreamChoice, 0, len(resp.Choices)),
		}
		for i, choice := range resp.Choices {
			if observer != nil && choice.Delta != nil && choice.Delta.Content != nil {
				content.WriteString(*choice.Delta.Content)
			}
			outChunk.Choices = append(outChunk.Choices, chatStreamChoice{Index: i, Delta: choice.Delta, FinishReason: choice.FinishReason})
		}
		payload, err := json.Marshal(outChunk)
		if err != nil {
			d.Logger.Warn("marshal stream chunk failed", "request_id", requestID, "error", err)
			return false
		}
		return writeEvent(payload)
	}

	interrupted := !emitRespChunk(firstResp)
	var streamErr *schemas.BifrostError

	if !interrupted {
		keepalive := time.NewTicker(sseKeepaliveInterval)
		defer keepalive.Stop()
	streamLoop:
		for {
			select {
			case <-r.Context().Done():
				interrupted = true
				break streamLoop
			case <-keepalive.C:
				if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
					interrupted = true
					break streamLoop
				}
				flusher.Flush()
			case chunk, open := <-winningStream:
				if !open {
					break streamLoop
				}
				keepalive.Reset(sseKeepaliveInterval)
				if chunk.BifrostError != nil {
					streamErr = chunk.BifrostError
					break streamLoop
				}
				resp := chunk.BifrostChatResponse
				if resp == nil {
					continue
				}
				if u := usageFromBifrost(resp.Usage); u.hasTokens() {
					snapshot = usageSnapshot(u)
				}
				if !emitRespChunk(resp) {
					interrupted = true
					break streamLoop
				}
			}
		}
	}

	if interrupted {
		attempts = append(attempts, AttemptRecord{
			CandidateID: winningCand.ID, Provider: winningCand.Provider, UpstreamModel: winningCand.UpstreamModel,
			LatencyMS: int(time.Since(start).Milliseconds()), Outcome: "interrupted",
		})
		d.recordStreamInterrupted(r.Context(), requestID, winningCand, attempts, start, firstTokenMS, snapshot)
		if observer != nil {
			observer.onComplete("interrupted", DebugResult{
				Response: content.String(), Provider: servedProvider, UpstreamModel: servedModel,
				InputTokens: snapshot.InputTokens, OutputTokens: snapshot.OutputTokens,
				CachedTokens: snapshot.CachedTokens, ReasoningTokens: snapshot.ReasoningTokens,
				FirstTokenMS: firstTokenMS, TotalMS: int(time.Since(start).Milliseconds()),
			}, nil)
		}
		return
	}

	if streamErr != nil {
		// 首内容发出后发生中断错误：仅按 OpenAI SSE 错误事件终止流，绝不跨候选重放。
		statusCode, errCode, errType, errMsg := extractBifrostErrorInfo(streamErr)
		d.Breaker().RecordFailure(r.Context(), winningCand.ID, statusCode, errCode, errType, rp, cb)
		attempts = append(attempts, AttemptRecord{
			CandidateID: winningCand.ID, Provider: winningCand.Provider, UpstreamModel: winningCand.UpstreamModel,
			StatusCode: statusCode, ErrorType: errType, ErrorMessage: errMsg,
			LatencyMS: int(time.Since(start).Milliseconds()), Outcome: "failed",
		})
		errorPayload := apiErrorEvent(http.StatusBadGateway, "upstream_error", "Upstream request failed.", requestID.String(),
			apiErrorOptions{Type: "upstream_error", Reason: "upstream_request_failed", Detail: errMsg, Params: map[string]any{"upstream_type": errType}})
		_ = writeEvent(errorPayload)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
		d.recordChainFailureWithProtocol(r.Context(), keyCtx, requestID, decision.Chain, attempts, streamErr, start, firstTokenMS, snapshot, stringOrDefault(decision.Protocol, ClientProtocolOpenAIChat))
		if observer != nil {
			cost := 0.0
			if winningCand.ID != "" {
				cost = winningCand.costForUsage(snapshot)
			}
			observer.onComplete("failed", DebugResult{
				Response: content.String(), Provider: servedProvider, UpstreamModel: servedModel,
				InputTokens: snapshot.InputTokens, OutputTokens: snapshot.OutputTokens,
				CachedTokens: snapshot.CachedTokens, ReasoningTokens: snapshot.ReasoningTokens,
				Cost: cost, Currency: currencyOf(winningCand), FirstTokenMS: firstTokenMS,
				TotalMS: int(time.Since(start).Milliseconds()),
			}, errors.New(errMsg))
		}
		return
	}

	d.Breaker().RecordSuccess(r.Context(), winningCand.ID)
	if svc.Policy.SessionAffinity.Enabled && decision.SessionKey != "" && winningCand.ID != "" {
		d.Affinity().Set(context.WithoutCancel(r.Context()), svc.ID.String(), decision.SessionKey, winningCand.ID,
			time.Duration(svc.Policy.SessionAffinity.TTLSeconds)*time.Second)
	}
	attempts = append(attempts, AttemptRecord{
		CandidateID: winningCand.ID, Provider: winningCand.Provider, UpstreamModel: winningCand.UpstreamModel,
		StatusCode: http.StatusOK, LatencyMS: int(time.Since(start).Milliseconds()), Outcome: "success",
	})

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

	total := int(time.Since(start).Milliseconds())
	cost := winningCand.costForUsage(snapshot)
	store := usageStore{pool: d.Pool, fxRates: d.fxRatesSnapshot()}
	if err := store.finalize(context.WithoutCancel(r.Context()), requestID, finalUsage{
		State:               "success",
		StatusCode:          intPtr(http.StatusOK),
		CandidateID:         uuidPtr(winningCand.ID, winningCand.ID != ""),
		Provider:            stringPtr(servedProvider),
		UpstreamModel:       stringPtr(servedModel),
		InputTokens:         snapshot.InputTokens,
		OutputTokens:        snapshot.OutputTokens,
		CachedTokens:        snapshot.CachedTokens,
		CacheCreationTokens: snapshot.CacheCreationTokens,
		ReasoningTokens:     snapshot.ReasoningTokens,
		Protocol:            stringOrDefault(decision.Protocol, ClientProtocolOpenAIChat),
		Cost:                cost,
		Currency:            currencyOf(winningCand),
		FirstTokenMS:        firstTokenMS,
		TotalMS:             total,
		UsageSource:         stringPtrNil("computed", snapshot.hasTokens()),
		AttemptTrail:        attemptTrailFromRecords(attempts),
	}); err != nil {
		d.Logger.Warn("finalize stream usage failed", "request_id", requestID, "error", err)
	}
	if keyCtx != nil {
		d.settleBudget(context.WithoutCancel(r.Context()), *keyCtx, requestID, cost, currencyOf(winningCand))
	}
	if observer != nil {
		observer.onComplete("success", DebugResult{
			Response: content.String(), Provider: servedProvider, UpstreamModel: servedModel,
			InputTokens: snapshot.InputTokens, OutputTokens: snapshot.OutputTokens,
			CachedTokens: snapshot.CachedTokens, ReasoningTokens: snapshot.ReasoningTokens,
			Cost: cost, Currency: currencyOf(winningCand), FirstTokenMS: firstTokenMS, TotalMS: total,
		}, nil)
	}
}

func (d Deps) recordStreamInterrupted(ctx context.Context, requestID uuid.UUID, winningCand candidate,
	attempts []AttemptRecord, start time.Time, firstTokenMS *int, snapshot usageSnapshot) {
	total := int(time.Since(start).Milliseconds())
	store := usageStore{pool: d.Pool, fxRates: d.fxRatesSnapshot()}
	if err := store.finalize(context.WithoutCancel(ctx), requestID, finalUsage{
		State:               "interrupted",
		CandidateID:         uuidPtr(winningCand.ID, winningCand.ID != ""),
		Provider:            stringPtrNil(logicalProviderName(winningCand.Provider), winningCand.Provider != ""),
		UpstreamModel:       stringPtrNil(winningCand.UpstreamModel, winningCand.UpstreamModel != ""),
		FirstTokenMS:        firstTokenMS,
		TotalMS:             total,
		InputTokens:         snapshot.InputTokens,
		OutputTokens:        snapshot.OutputTokens,
		CachedTokens:        snapshot.CachedTokens,
		CacheCreationTokens: snapshot.CacheCreationTokens,
		ReasoningTokens:     snapshot.ReasoningTokens,
		Currency:            currencyOf(winningCand),
		UsageSource:         stringPtrNil("computed", snapshot.hasTokens()),
		AttemptTrail:        attemptTrailFromRecords(attempts),
	}); err != nil {
		d.Logger.Warn("finalize interrupted usage failed", "request_id", requestID, "error", err)
	}
}

func errorTypeOf(err *schemas.BifrostError) string {
	if err != nil && err.Type != nil {
		return *err.Type
	}
	return "upstream_error"
}

func errorMessageOf(err *schemas.BifrostError) string {
	if err != nil && err.Error != nil && err.Error.Message != "" {
		return err.Error.Message
	}
	return "upstream request failed"
}
