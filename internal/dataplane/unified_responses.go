package dataplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/tidwall/sjson"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

func readJSONBody(w http.ResponseWriter, r *http.Request, dst any) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxChatBodyBytes))
	if err != nil {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "Request body cannot be read or exceeds size limit.",
			apiErrorOptions{Reason: "invalid_json", Detail: err.Error()})
		return nil, false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "Request body is not valid JSON.",
			apiErrorOptions{Reason: "invalid_json", Detail: err.Error()})
		return nil, false
	}
	return raw, true
}

func isPDFContentBlock(file *schemas.ResponsesInputMessageContentBlockFile) bool {
	if file == nil {
		return false
	}
	if file.FileType != nil && strings.Contains(strings.ToLower(*file.FileType), "pdf") {
		return true
	}
	if file.Filename != nil && strings.HasSuffix(strings.ToLower(strings.TrimSpace(*file.Filename)), ".pdf") {
		return true
	}
	if file.FileURL != nil && strings.Contains(strings.ToLower(*file.FileURL), ".pdf") {
		return true
	}
	if file.FileData != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(*file.FileData)), "data:application/pdf") {
		return true
	}
	return false
}

// ExtractResponsesRouteContext inspects a canonical BifrostResponsesRequest to extract
// required model capabilities, token estimates, metadata, and rule matching context.
func ExtractResponsesRouteContext(req *schemas.BifrostResponsesRequest, stream bool, protocol string, keyCtx apiKeyContext, headers http.Header, sessionKey string) RouteContext {
	var (
		hasVision      bool
		hasPDF         bool
		hasCaching     bool
		runes          int
		sample         strings.Builder
		latestUserText string
		toolTurnCount  int
		messageCount   int
	)

	appendSample := func(txt string) {
		if txt == "" || sample.Len() >= 4000 {
			return
		}
		if sample.Len() > 0 {
			sample.WriteByte(' ')
		}
		sample.WriteString(txt)
	}

	if req != nil {
		messageCount = len(req.Input)
		for _, msg := range req.Input {
			runes += 8
			if msg.Type != nil {
				switch *msg.Type {
				case schemas.ResponsesMessageTypeFunctionCall,
					schemas.ResponsesMessageTypeFunctionCallOutput,
					schemas.ResponsesMessageTypeCustomToolCall,
					schemas.ResponsesMessageTypeCustomToolCallOutput:
					toolTurnCount++
				}
			}
			isUser := msg.Role != nil && *msg.Role == schemas.ResponsesInputMessageRoleUser
			if msg.Content == nil {
				continue
			}
			if msg.Content.ContentStr != nil {
				runes += utf8.RuneCountInString(*msg.Content.ContentStr)
				if isUser {
					if *msg.Content.ContentStr != "" {
						latestUserText = *msg.Content.ContentStr
					}
					appendSample(*msg.Content.ContentStr)
				}
			}
			var turnText strings.Builder
			for _, block := range msg.Content.ContentBlocks {
				if block.CacheControl != nil || block.PromptCacheBreakpoint != nil {
					hasCaching = true
				}
				switch block.Type {
				case schemas.ResponsesInputMessageContentBlockTypeImage:
					hasVision = true
					runes += 512
				case schemas.ResponsesInputMessageContentBlockTypeFile:
					if isPDFContentBlock(block.ResponsesInputMessageContentBlockFile) {
						hasPDF = true
					}
					runes += 512
				case schemas.ResponsesInputMessageContentBlockTypeAudio:
					runes += 512
				default:
					if block.Text != nil {
						runes += utf8.RuneCountInString(*block.Text)
						if isUser {
							if turnText.Len() > 0 {
								turnText.WriteByte(' ')
							}
							turnText.WriteString(*block.Text)
							appendSample(*block.Text)
						}
					}
				}
			}
			if isUser && turnText.Len() > 0 {
				latestUserText = turnText.String()
			}
		}
	}

	estOut := 500
	var hasTools, hasReasoning, hasJSONMode bool
	toolCount := 0
	reasoningEffort := ""
	metaMap := map[string]string{}

	if req != nil && req.Params != nil {
		p := req.Params
		if p.Instructions != nil {
			runes += utf8.RuneCountInString(*p.Instructions)
		}
		toolCount = len(p.Tools)
		if toolCount > 0 || p.ToolChoice != nil {
			hasTools = true
		}
		if p.Reasoning != nil {
			hasReasoning = true
			if p.Reasoning.Effort != nil {
				reasoningEffort = *p.Reasoning.Effort
			}
		}
		if p.Text != nil && p.Text.Format != nil {
			t := strings.ToLower(strings.TrimSpace(p.Text.Format.Type))
			if t == "json_schema" || t == "json_object" {
				hasJSONMode = true
			}
		}
		if p.PromptCacheKey != nil || p.PromptCacheOptions != nil || p.PromptCacheRetention != nil {
			hasCaching = true
		}
		if p.MaxOutputTokens != nil && *p.MaxOutputTokens > 0 {
			estOut = *p.MaxOutputTokens
		}
		if p.Metadata != nil {
			for k, v := range *p.Metadata {
				key := strings.TrimSpace(k)
				if key != "" {
					metaMap[key] = fmt.Sprint(v)
				}
			}
		}
	}

	for name, vals := range headers {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-omniflux-meta-") && len(vals) > 0 {
			metaKey := strings.TrimPrefix(lower, "x-omniflux-meta-")
			if metaKey != "" {
				metaMap[metaKey] = vals[0]
			}
		}
	}

	caps := make([]string, 0, 6)
	if hasVision {
		caps = append(caps, "vision")
	}
	if hasPDF {
		caps = append(caps, "pdf_input")
	}
	if hasTools {
		caps = append(caps, "tools")
	}
	if hasReasoning {
		caps = append(caps, "reasoning")
	}
	if hasJSONMode {
		caps = append(caps, "json_mode")
	}
	if hasCaching {
		caps = append(caps, "prompt_caching")
	}

	estIn := int(math.Ceil(float64(runes) / 3.0))
	if req != nil && estIn < 1 && len(req.Input) > 0 {
		estIn = 1
	}

	return RouteContext{
		EstimatedInputTokens:  estIn,
		EstimatedOutputTokens: estOut,
		Stream:                stream,
		RequiredCapabilities:  caps,
		ProjectID:             keyCtx.ProjectID.String(),
		ApplicationID:         keyCtx.ApplicationID.String(),
		APIKeyID:              keyCtx.KeyID.String(),
		Source:                "api",
		Protocol:              protocol,
		SessionKey:            sessionKey,
		KeywordText:           sample.String(),
		LatestUserText:        latestUserText,
		ToolCount:             toolCount,
		ToolTurnCount:         toolTurnCount,
		MessageCount:          messageCount,
		ReasoningEffort:       reasoningEffort,
		Metadata:              metaMap,
	}
}

func (d Deps) resolveServiceAndRoute(w http.ResponseWriter, r *http.Request, keyCtx apiKeyContext,
	requestedModel string, reqCtx RouteContext, auditOp string) (resolvedService, RouteDecision, bool) {
	svc, found, allowed, err := d.resolveAuthorizedTarget(r.Context(), keyCtx, requestedModel, "invoke")
	if err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to resolve model service.")
		return resolvedService{}, RouteDecision{}, false
	}
	if !found {
		writeProtocolError(w, r, http.StatusNotFound, "model_not_found", "Model service does not exist or is not enabled.",
			apiErrorOptions{Reason: "model_not_found", Param: "model", Params: map[string]any{"model": requestedModel}})
		return resolvedService{}, RouteDecision{}, false
	}
	if !allowed {
		if svc.ID != uuid.Nil {
			d.auditDeniedModelAccess(r, keyCtx, svc.ID, auditOp)
		}
		writeProtocolError(w, r, http.StatusForbidden, "model_access_denied", "The current project is not allowed to invoke this model service.")
		return resolvedService{}, RouteDecision{}, false
	}
	if !d.checkBudgets(w, r, keyCtx) {
		return resolvedService{}, RouteDecision{}, false
	}

	if svc.Policy.SessionAffinity.Enabled && reqCtx.SessionKey != "" {
		reqCtx.PreferredCandidateID = d.Affinity().Get(r.Context(), svc.ID.String(), reqCtx.SessionKey)
	}
	scores := d.loadCandidateHealthWithBreaker(r.Context(), keyCtx.OrganizationID, svc.Candidates, svc.Policy.CircuitBreaker)
	var rr uint64
	if svc.Policy.Strategy == StrategyRoundRobin {
		rr = d.Breaker().NextRoundRobin(r.Context(), svc.ID.String())
	}
	decision := EvaluateRoute(svc.Candidates, svc.Policy, reqCtx, scores, d.fxRatesSnapshot(), rr)
	decision.SessionKey = reqCtx.SessionKey
	decision.Protocol = reqCtx.Protocol
	if len(decision.Chain) == 0 {
		writeProtocolError(w, r, http.StatusNotFound, "no_eligible_candidates",
			"No upstream model candidate matches the route policy constraints or request requirements.",
			apiErrorOptions{Reason: "no_eligible_candidates", Param: "model", Params: map[string]any{"model": requestedModel}})
		return resolvedService{}, RouteDecision{}, false
	}
	return svc, decision, true
}

func cloneResponsesRequestForCandidate(req *schemas.BifrostResponsesRequest, provider schemas.ModelProvider, model string) *schemas.BifrostResponsesRequest {
	copied := *req
	copied.Provider = provider
	copied.Model = model
	copied.Fallbacks = nil
	copied.RawRequestBody = nil
	return &copied
}

func isZhipuChatProvider(provider string) bool {
	switch provider {
	case "zhipu-glm", "zhipu-glm-en", "zhipu-glm-coding", "zhipu-glm-en-coding":
		return true
	default:
		return false
	}
}

func injectToolStreamExtraParam(existing map[string]any) (map[string]any, bool) {
	if existing != nil {
		if _, exists := existing["tool_stream"]; exists {
			return existing, true
		}
	}
	next := make(map[string]any, len(existing)+1)
	for k, v := range existing {
		next[k] = v
	}
	next["tool_stream"] = true
	return next, true
}

func applyProviderStreamingHintsForResponses(req *schemas.BifrostResponsesRequest, cand candidate) bool {
	if req == nil || req.Params == nil || len(req.Params.Tools) == 0 {
		return false
	}
	if bifrostx.EffectiveProtocol(cand.Provider, cand.APIProtocol) != bifrostx.ProtocolOpenAIChat {
		return false
	}
	if !isZhipuChatProvider(logicalProviderName(cand.Provider)) {
		return false
	}
	paramsCopy := *req.Params
	extra, passthrough := injectToolStreamExtraParam(paramsCopy.ExtraParams)
	paramsCopy.ExtraParams = extra
	req.Params = &paramsCopy
	return passthrough
}

func applyProviderStreamingHintsForChat(params *schemas.ChatParameters, cand candidate) (*schemas.ChatParameters, bool) {
	if params == nil || len(params.Tools) == 0 {
		return params, false
	}
	if bifrostx.EffectiveProtocol(cand.Provider, cand.APIProtocol) != bifrostx.ProtocolOpenAIChat {
		return params, false
	}
	if !isZhipuChatProvider(logicalProviderName(cand.Provider)) {
		return params, false
	}
	paramsCopy := *params
	extra, passthrough := injectToolStreamExtraParam(paramsCopy.ExtraParams)
	paramsCopy.ExtraParams = extra
	return &paramsCopy, passthrough
}

func shouldBridgeResponsesUnsupported(cand candidate, bifrostErr *schemas.BifrostError) bool {
	if bifrostx.EffectiveProtocol(cand.Provider, cand.APIProtocol) != bifrostx.ProtocolOpenAIResponses {
		return false
	}
	status, _, _, _ := extractBifrostErrorInfo(bifrostErr)
	return status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented
}

type attemptContextPreparer func(bctx *schemas.BifrostContext, cand candidate)

func (d Deps) executeResponsesNonStreamChain(ctx context.Context, svc resolvedService, chain routeChain,
	req *schemas.BifrostResponsesRequest, prepareCtx attemptContextPreparer) (candidate, *schemas.BifrostResponsesResponse, []AttemptRecord, *schemas.BifrostError) {
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
		provider, err := bifrostx.EngineProviderKey(cand.AccountID, cand.Provider, cand.APIProtocol)
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
			if prepareCtx != nil {
				prepareCtx(bctx, cand)
			}

			candReq := cloneResponsesRequestForCandidate(req, provider, cand.UpstreamModel)
			resp, bifrostErr := d.Engine.Client().ResponsesRequest(bctx, candReq)
			if bifrostErr != nil && shouldBridgeResponsesUnsupported(cand, bifrostErr) && attemptCtx.Err() == nil {
				bridgeChatReq := candReq.ToChatRequest()
				bridgeResp, bridgeErr := d.Engine.Client().ChatCompletionRequest(bctx, bridgeChatReq)
				if bridgeErr == nil && bridgeResp != nil {
					resp = bridgeResp.ToBifrostResponsesResponse()
					bifrostErr = nil
				} else if bridgeErr != nil {
					bifrostErr = bridgeErr
				}
			}
			cancelAttempt()
			attemptMS := int(time.Since(attemptStart).Milliseconds())

			if bifrostErr == nil && resp != nil {
				d.Breaker().RecordSuccess(ctx, cand.ID)
				attempts = append(attempts, AttemptRecord{
					CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
					StatusCode: http.StatusOK, LatencyMS: attemptMS, RetryCount: retryIdx, Outcome: "success",
				})
				return cand, resp, attempts, nil
			}
			if bifrostErr == nil {
				bifrostErr = &schemas.BifrostError{
					StatusCode: intPtr(http.StatusBadGateway),
					Type:       stringPtr("upstream_error"),
					Error:      &schemas.ErrorField{Message: "Upstream returned an empty response."},
				}
			}

			lastErr = bifrostErr
			statusCode, errCode, errType, errMsg := extractBifrostErrorInfo(bifrostErr)
			if d.OnTransportError != nil && statusCode == 0 {
				d.OnTransportError(logicalProviderName(cand.Provider))
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
		lastErr = &schemas.BifrostError{
			StatusCode: intPtr(http.StatusBadGateway),
			Type:       stringPtr("upstream_error"),
			Error:      &schemas.ErrorField{Message: "All upstream model candidates failed."},
		}
	}
	return candidate{}, nil, attempts, lastErr
}

func mergeUsageSnapshot(dst *usageSnapshot, src usageSnapshot) {
	if src.InputTokens > dst.InputTokens {
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens > dst.OutputTokens {
		dst.OutputTokens = src.OutputTokens
	}
	if src.CachedTokens > dst.CachedTokens {
		dst.CachedTokens = src.CachedTokens
	}
	if src.CacheCreationTokens > dst.CacheCreationTokens {
		dst.CacheCreationTokens = src.CacheCreationTokens
	}
	if src.ReasoningTokens > dst.ReasoningTokens {
		dst.ReasoningTokens = src.ReasoningTokens
	}
}

var sseKeepaliveInterval = 15 * time.Second

const (
	defaultStreamIdleTimeout          = 120 * time.Second
	minFinalCandidatePreCommitTimeout = 180 * time.Second
)

type streamAttemptControl struct {
	ctx    context.Context
	cancel context.CancelFunc
	state  atomic.Int32 // 0=pending, 1=timed_out, 2=committed
	timer  *time.Timer
}

func newStreamAttemptControl(parent context.Context, preCommitTimeout time.Duration) *streamAttemptControl {
	ctx, cancel := context.WithCancel(parent)
	ctrl := &streamAttemptControl{
		ctx:    ctx,
		cancel: cancel,
	}
	if preCommitTimeout > 0 {
		ctrl.timer = time.AfterFunc(preCommitTimeout, func() {
			if ctrl.state.CompareAndSwap(0, 1) {
				cancel()
			}
		})
	}
	return ctrl
}

func (c *streamAttemptControl) disarmTimedOut() bool {
	if c == nil {
		return false
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	return c.state.Load() == 1
}

func (c *streamAttemptControl) commit() bool {
	if c == nil {
		return true
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	if c.state.CompareAndSwap(0, 2) {
		return true
	}
	return c.state.Load() == 2
}

func isResponsesTerminalEvent(ev *schemas.BifrostResponsesStreamResponse) bool {
	if ev == nil {
		return false
	}
	switch ev.Type {
	case schemas.ResponsesStreamResponseTypeCompleted,
		schemas.ResponsesStreamResponseTypeIncomplete,
		schemas.ResponsesStreamResponseTypeFailed:
		return true
	default:
		return false
	}
}

func responsesEventShouldCommit(ev *schemas.BifrostResponsesStreamResponse) bool {
	if ev == nil {
		return false
	}
	switch ev.Type {
	case schemas.ResponsesStreamResponseTypePing,
		schemas.ResponsesStreamResponseTypeCreated,
		schemas.ResponsesStreamResponseTypeInProgress,
		schemas.ResponsesStreamResponseTypeQueued,
		schemas.ResponsesStreamResponseTypeContentPartAdded:
		return false
	case schemas.ResponsesStreamResponseTypeOutputItemAdded:
		if ev.Item == nil || ev.Item.Type == nil {
			return false
		}
		switch *ev.Item.Type {
		case schemas.ResponsesMessageTypeReasoning,
			schemas.ResponsesMessageTypeFunctionCall,
			schemas.ResponsesMessageTypeCustomToolCall,
			schemas.ResponsesMessageTypeMessage:
			return true
		default:
			return false
		}
	default:
		return true
	}
}

func awaitFirstResponsesChunks(ctx context.Context, stream chan *schemas.BifrostStreamChunk, allowImmediateCommit bool) (
	buffered []*schemas.BifrostResponsesStreamResponse, snapshot usageSnapshot, streamErr *schemas.BifrostError, closed bool) {
	for {
		select {
		case <-ctx.Done():
			return buffered, snapshot, nil, false
		case chunk, open := <-stream:
			if !open {
				if len(buffered) > 0 {
					return buffered, snapshot, nil, false
				}
				return nil, snapshot, nil, true
			}
			if chunk.BifrostError != nil {
				return nil, snapshot, chunk.BifrostError, false
			}
			ev := chunk.BifrostResponsesStreamResponse
			if ev == nil {
				continue
			}
			if ev.Response != nil && ev.Response.Usage != nil {
				mergeUsageSnapshot(&snapshot, usageFromResponses(ev.Response.Usage))
			}
			if ev.Type == schemas.ResponsesStreamResponseTypeError {
				msg := ""
				if ev.Message != nil {
					msg = *ev.Message
				}
				if msg == "" && ev.Response != nil && ev.Response.Error != nil {
					msg = ev.Response.Error.Message
				}
				if msg == "" {
					msg = "upstream stream failed"
				}
				return nil, snapshot, &schemas.BifrostError{
					StatusCode: intPtr(http.StatusBadGateway),
					Type:       stringPtr("upstream_error"),
					Error:      &schemas.ErrorField{Message: msg},
				}, false
			}
			buffered = append(buffered, ev)
			if allowImmediateCommit {
				if ev.Type != schemas.ResponsesStreamResponseTypePing {
					return buffered, snapshot, nil, false
				}
				continue
			}
			if responsesEventShouldCommit(ev) {
				return buffered, snapshot, nil, false
			}
		}
	}
}

type responsesStreamWinner struct {
	Candidate candidate
	Stream    chan *schemas.BifrostStreamChunk
	Buffered  []*schemas.BifrostResponsesStreamResponse
	Snapshot  usageSnapshot
	Ctx       context.Context
	Cancel    context.CancelFunc
	Attempts  []AttemptRecord
}

func (d Deps) openResponsesStreamWinner(r *http.Request, svc resolvedService, decision RouteDecision,
	req *schemas.BifrostResponsesRequest, prepareCtx attemptContextPreparer) (
	winner *responsesStreamWinner, attempts []AttemptRecord, lastErr *schemas.BifrostError, lastTTFT bool, clientAborted bool) {
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

	attempts = make([]AttemptRecord, 0, len(decision.Chain))
	skipAccounts := make(map[string]bool)
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
			// Stream connections use cancellation + idle timeouts rather than a short whole-request deadline
			// so long reasoning or generation turns never terminate mid-response at ProviderTimeoutMS.
			ctrl := newStreamAttemptControl(r.Context(), preCommitTimeout)
			bctx := schemas.NewBifrostContext(ctrl.ctx, schemas.NoDeadline)
			bctx.SetValue(schemas.BifrostContextKeyAPIKeyID, cand.AccountID)
			bctx.SetValue(schemas.BifrostContextKeyStreamIdleTimeout, idleTimeout)
			if !isFinalCandidate {
				bctx.SetValue(schemas.BifrostContextKeyStreamFirstTokenTimeout, ttftDur)
			}
			if prepareCtx != nil {
				prepareCtx(bctx, cand)
			}

			candReq := cloneResponsesRequestForCandidate(req, provider, cand.UpstreamModel)
			if applyProviderStreamingHintsForResponses(candReq, cand) {
				bctx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
			}
			stream, bifrostErr := d.Engine.Client().ResponsesStreamRequest(bctx, candReq)
			if bifrostErr != nil && shouldBridgeResponsesUnsupported(cand, bifrostErr) && r.Context().Err() == nil {
				bctx.SetValue(schemas.BifrostContextKeyIsResponsesToChatCompletionFallback, true)
				stream, bifrostErr = d.Engine.Client().ChatCompletionStreamRequest(bctx, candReq.ToChatRequest())
			}

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

			buffered, snap, preErr, closedEarly := awaitFirstResponsesChunks(ctrl.ctx, stream, isFinalCandidate)
			wasTTFT := ctrl.disarmTimedOut()
			// If Responses API returned 404/405/501 inside the async stream reader, run a single Chat bridge attempt on this candidate.
			if !wasTTFT && preErr != nil && shouldBridgeResponsesUnsupported(cand, preErr) && r.Context().Err() == nil {
				ctrl.cancel()
				ctrl = newStreamAttemptControl(r.Context(), preCommitTimeout)
				bridgeCtx := schemas.NewBifrostContext(ctrl.ctx, schemas.NoDeadline)
				bridgeCtx.SetValue(schemas.BifrostContextKeyAPIKeyID, cand.AccountID)
				bridgeCtx.SetValue(schemas.BifrostContextKeyStreamIdleTimeout, idleTimeout)
				bridgeCtx.SetValue(schemas.BifrostContextKeyIsResponsesToChatCompletionFallback, true)
				if !isFinalCandidate {
					bridgeCtx.SetValue(schemas.BifrostContextKeyStreamFirstTokenTimeout, ttftDur)
				}
				if prepareCtx != nil {
					prepareCtx(bridgeCtx, cand)
				}
				if candReq.Params != nil && candReq.Params.ExtraParams != nil {
					if _, hasToolStream := candReq.Params.ExtraParams["tool_stream"]; hasToolStream {
						bridgeCtx.SetValue(schemas.BifrostContextKeyPassthroughExtraParams, true)
					}
				}
				bridgeStream, bridgeErr := d.Engine.Client().ChatCompletionStreamRequest(bridgeCtx, candReq.ToChatRequest())
				if bridgeErr == nil {
					stream = bridgeStream
					buffered, snap, preErr, closedEarly = awaitFirstResponsesChunks(ctrl.ctx, stream, isFinalCandidate)
					wasTTFT = ctrl.disarmTimedOut()
				} else {
					preErr = bridgeErr
					wasTTFT = ctrl.disarmTimedOut()
				}
			}
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
			if closedEarly || len(buffered) == 0 || !ctrl.commit() {
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

			return &responsesStreamWinner{
				Candidate: cand,
				Stream:    stream,
				Buffered:  buffered,
				Snapshot:  snap,
				Ctx:       ctrl.ctx,
				Cancel:    ctrl.cancel,
				Attempts:  attempts,
			}, attempts, nil, false, false
		}
	}

	if lastErr == nil {
		lastErr = &schemas.BifrostError{
			StatusCode: intPtr(http.StatusBadGateway),
			Type:       stringPtr("upstream_error"),
			Error:      &schemas.ErrorField{Message: "All upstream model candidates failed."},
		}
	}
	return nil, attempts, lastErr, lastTTFT, clientAborted
}

func (d Deps) finalizeResponsesUsage(ctx context.Context, requestID uuid.UUID, keyCtx apiKeyContext,
	svc resolvedService, decision RouteDecision, served candidate, attempts []AttemptRecord,
	snapshot usageSnapshot, protocol string, start time.Time, firstTokenMS *int) {
	if svc.Policy.SessionAffinity.Enabled && decision.SessionKey != "" && served.ID != "" {
		d.Affinity().Set(context.WithoutCancel(ctx), svc.ID.String(), decision.SessionKey, served.ID,
			time.Duration(svc.Policy.SessionAffinity.TTLSeconds)*time.Second)
	}
	total := int(time.Since(start).Milliseconds())
	cost := served.costForUsage(snapshot)
	servedProvider := logicalProviderName(served.Provider)
	store := usageStore{pool: d.Pool, fxRates: d.fxRatesSnapshot()}
	if err := store.finalize(context.WithoutCancel(ctx), requestID, finalUsage{
		State:               "success",
		StatusCode:          intPtr(http.StatusOK),
		CandidateID:         uuidPtr(served.ID, served.ID != ""),
		Provider:            stringPtrNil(servedProvider, servedProvider != ""),
		UpstreamModel:       stringPtrNil(served.UpstreamModel, served.UpstreamModel != ""),
		InputTokens:         snapshot.InputTokens,
		OutputTokens:        snapshot.OutputTokens,
		CachedTokens:        snapshot.CachedTokens,
		CacheCreationTokens: snapshot.CacheCreationTokens,
		ReasoningTokens:     snapshot.ReasoningTokens,
		Protocol:            protocol,
		Cost:                cost,
		Currency:            currencyOf(served),
		FirstTokenMS:        firstTokenMS,
		TotalMS:             total,
		UsageSource:         stringPtrNil("computed", snapshot.hasTokens()),
		AttemptTrail:        attemptTrailFromRecords(attempts),
	}); err != nil {
		d.Logger.Warn("finalize responses usage failed", "request_id", requestID, "error", err)
	}
	d.settleBudget(context.WithoutCancel(ctx), keyCtx, requestID, cost, currencyOf(served))
}

func (d Deps) handleResponsesStreamPreCommitFailure(r *http.Request, w http.ResponseWriter, requestID uuid.UUID,
	keyCtx apiKeyContext, decision RouteDecision, attempts []AttemptRecord, lastErr *schemas.BifrostError,
	lastTTFT bool, clientAborted bool, start time.Time, protocol string) {
	if clientAborted {
		d.recordStreamInterruptedWithProtocol(r.Context(), requestID, candidate{}, attempts, start, nil, usageSnapshot{}, protocol)
		return
	}
	if lastTTFT {
		d.recordChainFailureWithProtocol(r.Context(), &keyCtx, requestID, decision.Chain, attempts, lastErr, start, nil, usageSnapshot{}, protocol)
		writeProtocolError(w, r, http.StatusGatewayTimeout, "timeout",
			"Upstream did not return the first token within first_token_timeout_ms.",
			apiErrorOptions{Reason: "first_token_timeout"})
		return
	}
	d.recordChainFailureWithProtocol(r.Context(), &keyCtx, requestID, decision.Chain, attempts, lastErr, start, nil, usageSnapshot{}, protocol)
	writeProtocolBifrostError(w, r, lastErr)
}

func (d Deps) recordStreamInterruptedWithProtocol(ctx context.Context, requestID uuid.UUID, winningCand candidate,
	attempts []AttemptRecord, start time.Time, firstTokenMS *int, snapshot usageSnapshot, protocol string) {
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
		Protocol:            protocol,
		Currency:            currencyOf(winningCand),
		UsageSource:         stringPtrNil("computed", snapshot.hasTokens()),
		AttemptTrail:        attemptTrailFromRecords(attempts),
	}); err != nil {
		d.Logger.Warn("finalize interrupted responses usage failed", "request_id", requestID, "error", err)
	}
}

var extraFieldsKeyPattern = []byte(`"extra_fields"`)

// sanitizeOpenAIResponsesPayload marshals Bifrost OpenAI Responses / Compaction structs
// without internal "extra_fields" metadata leaked to downstream Codex/OpenAI clients.
func sanitizeOpenAIResponsesPayload(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(raw, extraFieldsKeyPattern) {
		return raw, nil
	}
	cleaned, err := sjson.DeleteBytes(raw, "extra_fields")
	if err != nil {
		return raw, nil
	}
	cleaned, err = sjson.DeleteBytes(cleaned, "response.extra_fields")
	if err != nil {
		return raw, nil
	}
	return cleaned, nil
}

func commitSSEHeaders(w http.ResponseWriter) (http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// Clear http.Server.WriteTimeout once SSE headers are flushed so long coding sessions stay open.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	flusher.Flush()
	return flusher, true
}

func isUnsupportedOpErr(err *schemas.BifrostError) bool {
	if err == nil {
		return false
	}
	status, _, _, msg := extractBifrostErrorInfo(err)
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented {
		return true
	}
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "not allowed") || strings.Contains(lower, "not supported") || strings.Contains(lower, "unsupported")
}

var _ = errors.Is
