package dataplane

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/providers/openai"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

// Responses handles POST /v1/responses (Codex CLI & OpenAI Responses API).
func (d Deps) Responses(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		writeProtocolError(w, r, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	var wireReq openai.OpenAIResponsesRequest
	if _, ok := readJSONBody(w, r, &wireReq); !ok {
		return
	}
	if strings.TrimSpace(wireReq.Model) == "" {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "The model parameter is required.",
			apiErrorOptions{Reason: "missing_model", Param: "model"})
		return
	}

	bctx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
	bifrostReq := wireReq.ToBifrostResponsesRequest(bctx)
	if bifrostReq == nil {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "Invalid Responses API payload.",
			apiErrorOptions{Reason: "invalid_parameters"})
		return
	}

	start := time.Now()
	requestedModel := strings.TrimSpace(wireReq.Model)
	isStream := wireReq.IsStreamingRequested()

	var promptCacheKey, prevRespID, metaUserID string
	if bifrostReq.Params != nil {
		if bifrostReq.Params.PromptCacheKey != nil {
			promptCacheKey = *bifrostReq.Params.PromptCacheKey
		}
		if bifrostReq.Params.PreviousResponseID != nil {
			prevRespID = *bifrostReq.Params.PreviousResponseID
		}
		if bifrostReq.Params.Metadata != nil {
			if uid, exists := (*bifrostReq.Params.Metadata)["user_id"]; exists {
				metaUserID = fmt.Sprint(uid)
			}
		}
	}

	reqCtx := ExtractResponsesRouteContext(bifrostReq, isStream, ClientProtocolOpenAIResponses, keyCtx, r.Header, "")
	svc, found, loadErr := d.loadService(r.Context(), keyCtx.OrganizationID, requestedModel)
	strategy := "auto"
	if loadErr == nil && found {
		strategy = svc.Policy.SessionAffinity.KeyStrategy
	}
	reqCtx.SessionKey = DeriveSessionKey(r, promptCacheKey, prevRespID, metaUserID, strategy)

	svc, decision, ok := d.resolveServiceAndRoute(w, r, keyCtx, requestedModel, reqCtx, "responses")
	if !ok {
		return
	}

	requestID := uuid.New()
	if err := d.recordPendingDecision(r.Context(), requestID, &keyCtx, nil, "api", svc, decision); err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to record routing decision.")
		return
	}

	if !isStream {
		d.handleResponsesNonStream(r, w, requestID, keyCtx, svc, decision, requestedModel, bifrostReq, start)
		return
	}
	d.handleResponsesStream(r, w, requestID, keyCtx, svc, decision, requestedModel, bifrostReq, start)
}

func (d Deps) handleResponsesNonStream(r *http.Request, w http.ResponseWriter, requestID uuid.UUID,
	keyCtx apiKeyContext, svc resolvedService, decision RouteDecision, requestedModel string,
	bifrostReq *schemas.BifrostResponsesRequest, start time.Time) {
	served, resp, attempts, bifrostErr := d.executeResponsesNonStreamChain(r.Context(), svc, decision.Chain, bifrostReq, nil)
	if bifrostErr != nil {
		d.recordChainFailureWithProtocol(r.Context(), &keyCtx, requestID, decision.Chain, attempts, bifrostErr, start, nil, usageSnapshot{}, ClientProtocolOpenAIResponses)
		writeProtocolBifrostError(w, r, bifrostErr)
		return
	}
	resp = resp.WithDefaults()
	if requestedModel != "" {
		resp.Model = requestedModel
	}
	payload, err := sanitizeOpenAIResponsesPayload(resp)
	if err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to serialize response payload.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)

	snap := usageFromResponses(resp.Usage)
	d.finalizeResponsesUsage(r.Context(), requestID, keyCtx, svc, decision, served, attempts, snap, ClientProtocolOpenAIResponses, start, nil)
}

func (d Deps) handleResponsesStream(r *http.Request, w http.ResponseWriter, requestID uuid.UUID,
	keyCtx apiKeyContext, svc resolvedService, decision RouteDecision, requestedModel string,
	bifrostReq *schemas.BifrostResponsesRequest, start time.Time) {
	winner, attempts, lastErr, lastTTFT, clientAborted := d.openResponsesStreamWinner(r, svc, decision, bifrostReq, nil)
	if winner == nil {
		d.handleResponsesStreamPreCommitFailure(r, w, requestID, keyCtx, decision, attempts, lastErr, lastTTFT, clientAborted, start, ClientProtocolOpenAIResponses)
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

	emitEvent := func(ev *schemas.BifrostResponsesStreamResponse) bool {
		if ev == nil {
			return true
		}
		if ev.Response != nil {
			if requestedModel != "" {
				ev.Response.Model = requestedModel
			}
			if ev.Response.Usage != nil {
				mergeUsageSnapshot(&snapshot, usageFromResponses(ev.Response.Usage))
			}
		}
		payload, err := sanitizeOpenAIResponsesPayload(ev)
		if err != nil {
			d.Logger.Warn("marshal responses sse event failed", "request_id", requestID, "error", err)
			return false
		}
		frame := fmt.Sprintf("event: %s\ndata: %s\n\n", ev.Type, payload)
		if _, err := w.Write([]byte(frame)); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	interrupted := false
	for _, ev := range winner.Buffered {
		if !emitEvent(ev) {
			interrupted = true
			break
		}
	}

	var streamErr *schemas.BifrostError
	if !interrupted {
	streamLoop:
		for {
			select {
			case <-r.Context().Done():
				interrupted = true
				break streamLoop
			case chunk, open := <-winner.Stream:
				if !open {
					break streamLoop
				}
				if chunk.BifrostError != nil {
					streamErr = chunk.BifrostError
					break streamLoop
				}
				if chunk.BifrostResponsesStreamResponse != nil {
					if !emitEvent(chunk.BifrostResponsesStreamResponse) {
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
		d.recordStreamInterruptedWithProtocol(r.Context(), requestID, winner.Candidate, attempts, start, firstTokenMS, snapshot, ClientProtocolOpenAIResponses)
		return
	}

	if streamErr != nil {
		statusCode, errCode, errType, errMsg := extractBifrostErrorInfo(streamErr)
		d.Breaker().RecordFailure(r.Context(), winner.Candidate.ID, statusCode, errCode, errType, svc.Policy.RetryPolicy, svc.Policy.CircuitBreaker)
		attempts = append(attempts, AttemptRecord{
			CandidateID: winner.Candidate.ID, Provider: winner.Candidate.Provider, UpstreamModel: winner.Candidate.UpstreamModel,
			StatusCode: statusCode, ErrorType: errType, ErrorMessage: errMsg,
			LatencyMS: int(time.Since(start).Milliseconds()), Outcome: "failed",
		})
		errPayload := apiErrorEvent(http.StatusBadGateway, "upstream_error", "Upstream request failed.", requestID.String(),
			apiErrorOptions{Type: "upstream_error", Reason: "upstream_request_failed", Detail: errMsg, Params: map[string]any{"upstream_type": errType}})
		_, _ = w.Write([]byte("event: error\ndata: " + string(errPayload) + "\n\n"))
		flusher.Flush()
		d.recordChainFailureWithProtocol(r.Context(), &keyCtx, requestID, decision.Chain, attempts, streamErr, start, firstTokenMS, snapshot, ClientProtocolOpenAIResponses)
		return
	}

	d.Breaker().RecordSuccess(r.Context(), winner.Candidate.ID)
	attempts = append(attempts, AttemptRecord{
		CandidateID: winner.Candidate.ID, Provider: winner.Candidate.Provider, UpstreamModel: winner.Candidate.UpstreamModel,
		StatusCode: http.StatusOK, LatencyMS: int(time.Since(start).Milliseconds()), Outcome: "success",
	})
	d.finalizeResponsesUsage(r.Context(), requestID, keyCtx, svc, decision, winner.Candidate, attempts, snapshot, ClientProtocolOpenAIResponses, start, firstTokenMS)
}

// ResponsesCompact handles POST /v1/responses/compact (Codex CLI context compaction).
func (d Deps) ResponsesCompact(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		writeProtocolError(w, r, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	var wireReq openai.OpenAICompactionRequest
	if _, ok := readJSONBody(w, r, &wireReq); !ok {
		return
	}
	if strings.TrimSpace(wireReq.Model) == "" {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "The model parameter is required.",
			apiErrorOptions{Reason: "missing_model", Param: "model"})
		return
	}

	bctx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
	compactReq := wireReq.ToBifrostCompactionRequest(bctx)
	if compactReq == nil {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "Invalid compaction payload.",
			apiErrorOptions{Reason: "invalid_parameters"})
		return
	}

	requestedModel := strings.TrimSpace(wireReq.Model)
	var promptCacheKey, prevRespID string
	if compactReq.PromptCacheKey != nil {
		promptCacheKey = *compactReq.PromptCacheKey
	}
	if compactReq.PreviousResponseID != nil {
		prevRespID = *compactReq.PreviousResponseID
	}

	syntheticReq := &schemas.BifrostResponsesRequest{
		Model: requestedModel,
		Input: compactReq.Input,
	}
	reqCtx := ExtractResponsesRouteContext(syntheticReq, false, ClientProtocolOpenAIResponses, keyCtx, r.Header,
		DeriveSessionKey(r, promptCacheKey, prevRespID, "", "auto"))

	svc, decision, ok := d.resolveServiceAndRoute(w, r, keyCtx, requestedModel, reqCtx, "responses_compact")
	if !ok {
		return
	}

	start := time.Now()
	requestID := uuid.New()
	if err := d.recordPendingDecision(r.Context(), requestID, &keyCtx, nil, "api", svc, decision); err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to record routing decision.")
		return
	}

	perTimeout := time.Duration(svc.Policy.ProviderTimeoutMS) * time.Millisecond
	if perTimeout <= 0 {
		perTimeout = 60 * time.Second
	}
	var attempts []AttemptRecord
	var lastErr *schemas.BifrostError

	for _, cand := range decision.Chain {
		if bifrostx.EffectiveProtocol(cand.Provider, cand.APIProtocol) != bifrostx.ProtocolOpenAIResponses {
			attempts = append(attempts, AttemptRecord{
				CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
				ErrorClass: "unsupported_operation", ErrorMessage: "candidate does not expose OpenAI Responses compaction", Outcome: "failed",
			})
			continue
		}
		provider, err := bifrostx.EngineProviderKey(cand.AccountID, cand.Provider, cand.APIProtocol)
		if err != nil {
			continue
		}
		attemptStart := time.Now()
		attemptCtx, cancel := context.WithTimeout(r.Context(), perTimeout)
		cctx := schemas.NewBifrostContext(attemptCtx, time.Now().Add(perTimeout))
		cctx.SetValue(schemas.BifrostContextKeyAPIKeyID, cand.AccountID)
		candCompact := *compactReq
		candCompact.Provider = provider
		candCompact.Model = cand.UpstreamModel
		candCompact.Fallbacks = nil

		resp, bifrostErr := d.Engine.Client().CompactionRequest(cctx, &candCompact)
		cancel()
		attemptMS := int(time.Since(attemptStart).Milliseconds())

		if bifrostErr == nil && resp != nil {
			d.Breaker().RecordSuccess(r.Context(), cand.ID)
			attempts = append(attempts, AttemptRecord{
				CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
				StatusCode: http.StatusOK, LatencyMS: attemptMS, Outcome: "success",
			})
			resp = resp.WithDefaults()
			if requestedModel != "" {
				resp.Model = requestedModel
			}
			payload, serErr := sanitizeOpenAIResponsesPayload(resp)
			if serErr != nil {
				writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to serialize compaction response.")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)

			snap := usageFromResponses(resp.Usage)
			d.finalizeResponsesUsage(r.Context(), requestID, keyCtx, svc, decision, cand, attempts, snap, ClientProtocolOpenAIResponses, start, nil)
			return
		}
		lastErr = bifrostErr
		statusCode, errCode, errType, errMsg := extractBifrostErrorInfo(bifrostErr)
		if !isUnsupportedOpErr(bifrostErr) {
			d.Breaker().RecordFailure(r.Context(), cand.ID, statusCode, errCode, errType, svc.Policy.RetryPolicy, svc.Policy.CircuitBreaker)
		}
		attempts = append(attempts, AttemptRecord{
			CandidateID: cand.ID, Provider: cand.Provider, UpstreamModel: cand.UpstreamModel,
			StatusCode: statusCode, ErrorType: errType, ErrorMessage: errMsg,
			LatencyMS: attemptMS, Outcome: "failed",
		})
	}

	if lastErr == nil {
		lastErr = &schemas.BifrostError{
			StatusCode: intPtr(http.StatusNotImplemented),
			Type:       stringPtr("invalid_request_error"),
			Error:      &schemas.ErrorField{Message: "No upstream candidate supports /v1/responses/compact."},
		}
	}
	d.recordChainFailureWithProtocol(r.Context(), &keyCtx, requestID, decision.Chain, attempts, lastErr, start, nil, usageSnapshot{}, ClientProtocolOpenAIResponses)
	writeProtocolBifrostError(w, r, lastErr)
}
