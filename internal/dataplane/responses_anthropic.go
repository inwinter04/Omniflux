package dataplane

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/providers/anthropic"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

func buildAnthropicContextPreparer(req *anthropic.AnthropicMessageRequest, inboundBeta string) attemptContextPreparer {
	return func(bctx *schemas.BifrostContext, cand candidate) {
		if bifrostx.EffectiveProtocol(cand.Provider, cand.APIProtocol) != bifrostx.ProtocolAnthropicMessages {
			return
		}
		if err := anthropic.AddMissingBetaHeadersToContext(bctx, req, schemas.Anthropic); err != nil {
			// Beta header helper failure is non-fatal for forwarding.
			_ = err
		}
		if strings.TrimSpace(inboundBeta) != "" {
			extra := map[string][]string{"anthropic-beta": {strings.TrimSpace(inboundBeta)}}
			bctx.SetValue(schemas.BifrostContextKeyExtraHeaders, extra)
		}
	}
}

// AnthropicMessages handles POST /v1/messages (Claude Code & Anthropic SDK).
func (d Deps) AnthropicMessages(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		writeProtocolError(w, r, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide x-api-key or Authorization: Bearer <key>.")
		return
	}
	var wireReq anthropic.AnthropicMessageRequest
	if _, ok := readJSONBody(w, r, &wireReq); !ok {
		return
	}
	requestedModel := strings.TrimSpace(wireReq.Model)
	if requestedModel == "" {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "The model parameter is required.",
			apiErrorOptions{Reason: "missing_model", Param: "model"})
		return
	}
	if len(wireReq.Messages) == 0 {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "The messages parameter is required.",
			apiErrorOptions{Reason: "missing_messages", Param: "messages"})
		return
	}

	bctx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
	bifrostReq := wireReq.ToBifrostResponsesRequest(bctx)
	if bifrostReq == nil {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "Invalid Anthropic messages payload.",
			apiErrorOptions{Reason: "invalid_parameters"})
		return
	}

	start := time.Now()
	isStream := wireReq.IsStreamingRequested()
	var metaUserID string
	if wireReq.Metadata != nil && wireReq.Metadata.UserID != nil {
		metaUserID = *wireReq.Metadata.UserID
	}

	reqCtx := ExtractResponsesRouteContext(bifrostReq, isStream, ClientProtocolAnthropicMessages, keyCtx, r.Header, "")
	svc, found, allowed, loadErr := d.resolveAuthorizedTarget(r.Context(), keyCtx, requestedModel, "invoke")
	strategy := "auto"
	if loadErr == nil && found && allowed {
		strategy = svc.Policy.SessionAffinity.KeyStrategy
	}
	reqCtx.SessionKey = DeriveSessionKey(r, "", "", metaUserID, strategy)

	svc, decision, ok := d.resolveServiceAndRoute(w, r, keyCtx, requestedModel, reqCtx, "anthropic_messages")
	if !ok {
		return
	}

	requestID := uuid.New()
	if err := d.recordPendingDecision(r.Context(), requestID, &keyCtx, nil, "api", svc, decision); err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to record routing decision.")
		return
	}

	preparer := buildAnthropicContextPreparer(&wireReq, r.Header.Get("anthropic-beta"))
	if !isStream {
		d.handleAnthropicNonStream(r, w, requestID, keyCtx, svc, decision, requestedModel, bifrostReq, preparer, start)
		return
	}
	d.handleAnthropicStream(r, w, requestID, keyCtx, svc, decision, requestedModel, bifrostReq, preparer, start)
}

func (d Deps) handleAnthropicNonStream(r *http.Request, w http.ResponseWriter, requestID uuid.UUID,
	keyCtx apiKeyContext, svc resolvedService, decision RouteDecision, requestedModel string,
	bifrostReq *schemas.BifrostResponsesRequest, preparer attemptContextPreparer, start time.Time) {
	served, resp, attempts, bifrostErr := d.executeResponsesNonStreamChain(r.Context(), svc, decision.Chain, bifrostReq, preparer)
	if bifrostErr != nil {
		d.recordChainFailureWithProtocol(r.Context(), &keyCtx, requestID, decision.Chain, attempts, bifrostErr, start, nil, usageSnapshot{}, ClientProtocolAnthropicMessages)
		writeProtocolBifrostError(w, r, bifrostErr)
		return
	}

	convCtx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
	out := anthropic.ToAnthropicResponsesResponse(convCtx, resp)
	if out == nil {
		writeProtocolError(w, r, http.StatusBadGateway, "upstream_error", "Failed to convert upstream response into Anthropic format.")
		return
	}
	if requestedModel != "" {
		out.Model = requestedModel
	}
	out.ExtraFields = nil
	writeJSON(w, http.StatusOK, out)

	snap := usageFromResponses(resp.Usage)
	d.finalizeResponsesUsage(r.Context(), requestID, keyCtx, svc, decision, served, attempts, snap, ClientProtocolAnthropicMessages, start, nil)
}

func (d Deps) handleAnthropicStream(r *http.Request, w http.ResponseWriter, requestID uuid.UUID,
	keyCtx apiKeyContext, svc resolvedService, decision RouteDecision, requestedModel string,
	bifrostReq *schemas.BifrostResponsesRequest, preparer attemptContextPreparer, start time.Time) {
	winner, attempts, lastErr, lastTTFT, clientAborted := d.openResponsesStreamWinner(r, svc, decision, bifrostReq, preparer)
	if winner == nil {
		d.handleResponsesStreamPreCommitFailure(r, w, requestID, keyCtx, decision, attempts, lastErr, lastTTFT, clientAborted, start, ClientProtocolAnthropicMessages)
		return
	}
	defer winner.Cancel()

	flusher, ok := commitSSEHeaders(w)
	if !ok {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "The current connection does not support streaming responses.",
			apiErrorOptions{Reason: "streaming_unsupported"})
		return
	}

	elapsedFirst := int(time.Since(start).Milliseconds())
	firstTokenMS := &elapsedFirst
	snapshot := winner.Snapshot
	attempts = winner.Attempts
	convCtx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
	sawTerminalEvent := false

	emitResponsesEvent := func(ev *schemas.BifrostResponsesStreamResponse) bool {
		if ev == nil {
			return true
		}
		if isResponsesTerminalEvent(ev) {
			sawTerminalEvent = true
		}
		if ev.Response != nil && ev.Response.Usage != nil {
			mergeUsageSnapshot(&snapshot, usageFromResponses(ev.Response.Usage))
		}
		frames := anthropic.ToAnthropicResponsesStreamResponse(convCtx, ev)
		for _, frame := range frames {
			if frame == nil {
				continue
			}
			if frame.Message != nil && requestedModel != "" {
				frame.Message.Model = requestedModel
			}
			payload, err := json.Marshal(frame)
			if err != nil {
				d.Logger.Warn("marshal anthropic stream frame failed", "request_id", requestID, "error", err)
				return false
			}
			msg := "event: " + string(frame.Type) + "\ndata: " + string(payload) + "\n\n"
			if _, err := w.Write([]byte(msg)); err != nil {
				return false
			}
		}
		flusher.Flush()
		return true
	}

	interrupted := false
	for _, ev := range winner.Buffered {
		if !emitResponsesEvent(ev) {
			interrupted = true
			break
		}
	}

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
			case chunk, open := <-winner.Stream:
				if !open {
					break streamLoop
				}
				keepalive.Reset(sseKeepaliveInterval)
				if chunk.BifrostError != nil {
					streamErr = chunk.BifrostError
					break streamLoop
				}
				if chunk.BifrostResponsesStreamResponse != nil {
					if !emitResponsesEvent(chunk.BifrostResponsesStreamResponse) {
						interrupted = true
						break streamLoop
					}
				}
			}
		}
	}

	if interrupted {
		attempts = append(attempts, AttemptRecord{
			CandidateID: winner.Candidate.ID, Provider: winner.Candidate.Provider, UpstreamModel: winner.Candidate.UpstreamModel,
			LatencyMS: int(time.Since(start).Milliseconds()), Outcome: "interrupted",
		})
		d.recordStreamInterruptedWithProtocol(r.Context(), requestID, winner.Candidate, attempts, start, firstTokenMS, snapshot, ClientProtocolAnthropicMessages)
		return
	}

	if streamErr == nil && !sawTerminalEvent {
		streamErr = &schemas.BifrostError{
			StatusCode: intPtr(http.StatusBadGateway),
			Type:       stringPtr("upstream_stream_closed"),
			Error:      &schemas.ErrorField{Message: "Upstream stream closed before completion."},
		}
		d.Logger.Warn("anthropic stream closed without terminal event",
			"request_id", requestID,
			"provider", winner.Candidate.Provider,
			"upstream_model", winner.Candidate.UpstreamModel,
			"duration_ms", time.Since(start).Milliseconds())
	}

	if streamErr != nil {
		statusCode, errCode, errType, errMsg := extractBifrostErrorInfo(streamErr)
		d.Breaker().RecordFailure(r.Context(), winner.Candidate.ID, statusCode, errCode, errType, svc.Policy.RetryPolicy, svc.Policy.CircuitBreaker)
		attempts = append(attempts, AttemptRecord{
			CandidateID: winner.Candidate.ID, Provider: winner.Candidate.Provider, UpstreamModel: winner.Candidate.UpstreamModel,
			StatusCode: statusCode, ErrorType: errType, ErrorMessage: errMsg,
			LatencyMS: int(time.Since(start).Milliseconds()), Outcome: "failed",
		})
		if errSSE := anthropic.ToAnthropicResponsesStreamError(streamErr); errSSE != "" {
			_, _ = w.Write([]byte(errSSE))
			flusher.Flush()
		}
		d.recordChainFailureWithProtocol(r.Context(), &keyCtx, requestID, decision.Chain, attempts, streamErr, start, firstTokenMS, snapshot, ClientProtocolAnthropicMessages)
		return
	}

	d.Breaker().RecordSuccess(r.Context(), winner.Candidate.ID)
	attempts = append(attempts, AttemptRecord{
		CandidateID: winner.Candidate.ID, Provider: winner.Candidate.Provider, UpstreamModel: winner.Candidate.UpstreamModel,
		StatusCode: http.StatusOK, LatencyMS: int(time.Since(start).Milliseconds()), Outcome: "success",
	})
	d.finalizeResponsesUsage(r.Context(), requestID, keyCtx, svc, decision, winner.Candidate, attempts, snapshot, ClientProtocolAnthropicMessages, start, firstTokenMS)
}

// AnthropicCountTokens handles POST /v1/messages/count_tokens (Claude Code token counting preflight).
func (d Deps) AnthropicCountTokens(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		writeProtocolError(w, r, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide x-api-key or Authorization: Bearer <key>.")
		return
	}
	var wireReq anthropic.AnthropicMessageRequest
	if _, ok := readJSONBody(w, r, &wireReq); !ok {
		return
	}
	requestedModel := strings.TrimSpace(wireReq.Model)
	if requestedModel == "" {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "The model parameter is required.",
			apiErrorOptions{Reason: "missing_model", Param: "model"})
		return
	}

	bctx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
	bifrostReq := wireReq.ToBifrostResponsesRequest(bctx)
	if bifrostReq == nil {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "Invalid count_tokens payload.",
			apiErrorOptions{Reason: "invalid_parameters"})
		return
	}
	reqCtx := ExtractResponsesRouteContext(bifrostReq, false, ClientProtocolAnthropicMessages, keyCtx, r.Header, "")

	svc, found, allowed, err := d.resolveAuthorizedTarget(r.Context(), keyCtx, requestedModel, "invoke")
	if err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to resolve model service.")
		return
	}
	if !found {
		writeProtocolError(w, r, http.StatusNotFound, "model_not_found", "Model service does not exist or is not enabled.",
			apiErrorOptions{Reason: "model_not_found", Param: "model", Params: map[string]any{"model": requestedModel}})
		return
	}
	if !allowed {
		if svc.ID != uuid.Nil {
			d.auditDeniedModelAccess(r, keyCtx, svc.ID, "anthropic_count_tokens")
		}
		writeProtocolError(w, r, http.StatusForbidden, "model_access_denied", "The current project is not allowed to invoke this model service.")
		return
	}

	scores := d.loadCandidateHealthWithBreaker(r.Context(), keyCtx.OrganizationID, svc.Candidates, svc.Policy.CircuitBreaker)
	decision := EvaluateRoute(svc.Candidates, svc.Policy, reqCtx, scores, d.fxRatesSnapshot(), 0)
	if len(decision.Chain) == 0 {
		writeProtocolError(w, r, http.StatusNotFound, "no_eligible_candidates", "No upstream model candidate matches the route policy constraints.")
		return
	}

	for _, cand := range decision.Chain {
		provider, pErr := bifrostx.EngineProviderKey(cand.AccountID, cand.Provider, cand.APIProtocol)
		if pErr != nil {
			continue
		}
		candReq := cloneResponsesRequestForCandidate(bifrostReq, provider, cand.UpstreamModel)
		cctx := schemas.NewBifrostContext(r.Context(), time.Now().Add(15*time.Second))
		cctx.SetValue(schemas.BifrostContextKeyAPIKeyID, cand.AccountID)
		if resp, bErr := d.Engine.Client().CountTokensRequest(cctx, candReq); bErr == nil && resp != nil {
			writeJSON(w, http.StatusOK, map[string]any{"input_tokens": resp.InputTokens})
			return
		}
	}

	// Fallback to local estimation when upstream protocol does not implement native count_tokens (e.g. openai_chat).
	writeJSON(w, http.StatusOK, map[string]any{"input_tokens": reqCtx.EstimatedInputTokens})
}
