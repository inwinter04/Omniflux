package dataplane

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/providers/gemini"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

// GeminiListModels handles GET /v1beta/models (Gemini CLI / Google GenAI SDK model discovery).
func (d Deps) GeminiListModels(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		writeProtocolError(w, r, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide x-goog-api-key or ?key=<key>.")
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT s.name FROM model_services s
		WHERE s.organization_id = $1 AND s.status = 'active'
		  AND EXISTS (
		    SELECT 1 FROM model_service_grants g
		    WHERE g.model_service_id = s.id AND g.organization_id = $1
		      AND g.actions && ARRAY['read','invoke']::text[]
		      AND ((g.subject_type = 'organization' AND g.subject_id = $1)
		        OR (g.subject_type = 'project' AND g.subject_id = $2)
		        OR (g.subject_type = 'department' AND g.subject_id = (
		          SELECT p.department_id FROM projects p WHERE p.id = $2 AND p.organization_id = $1
		        )))
		  )
		ORDER BY s.name ASC
	`, keyCtx.OrganizationID, keyCtx.ProjectID)
	if err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to query model list.")
		return
	}
	defer rows.Close()

	type geminiModelEntry struct {
		Name                       string   `json:"name"`
		DisplayName                string   `json:"displayName"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	}
	models := []geminiModelEntry{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to query model list.")
			return
		}
		models = append(models, geminiModelEntry{
			Name:                       "models/" + name,
			DisplayName:                name,
			SupportedGenerationMethods: []string{"generateContent", "streamGenerateContent", "countTokens"},
		})
	}
	if err := rows.Err(); err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to query model list.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func parseGeminiModelAction(urlPath string) (model string, action string, ok bool) {
	trimmed := strings.TrimPrefix(urlPath, "/v1beta/models/")
	trimmed = strings.TrimPrefix(trimmed, "models/")
	idx := strings.LastIndex(trimmed, ":")
	if idx <= 0 || idx >= len(trimmed)-1 {
		return "", "", false
	}
	model = strings.TrimSpace(trimmed[:idx])
	action = strings.TrimSpace(trimmed[idx+1:])
	if model == "" {
		return "", "", false
	}
	return model, action, true
}

// GeminiModelAction handles POST /v1beta/models/{model}:{action}
// supporting :generateContent, :streamGenerateContent (?alt=sse or JSON array), and :countTokens.
func (d Deps) GeminiModelAction(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		writeProtocolError(w, r, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide x-goog-api-key or ?key=<key>.")
		return
	}

	requestedModel, action, parsed := parseGeminiModelAction(r.URL.Path)
	if !parsed {
		writeProtocolError(w, r, http.StatusNotFound, "not_found", "Invalid Gemini model action path.",
			apiErrorOptions{Reason: "invalid_model_action"})
		return
	}
	switch action {
	case "generateContent", "streamGenerateContent", "countTokens":
	default:
		writeProtocolError(w, r, http.StatusNotFound, "unsupported_action", "Unsupported Gemini model action.",
			apiErrorOptions{Reason: "unsupported_gemini_action", Params: map[string]any{"action": action}})
		return
	}

	var wireReq gemini.GeminiGenerationRequest
	if _, ok := readJSONBody(w, r, &wireReq); !ok {
		return
	}
	wireReq.Model = requestedModel
	wireReq.Stream = action == "streamGenerateContent"
	wireReq.IsCountTokens = action == "countTokens"

	bctx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
	bifrostReq := wireReq.ToBifrostResponsesRequest(bctx)
	if bifrostReq == nil {
		writeProtocolError(w, r, http.StatusBadRequest, "invalid_request", "Invalid Gemini generation payload.",
			apiErrorOptions{Reason: "invalid_parameters"})
		return
	}

	if action == "countTokens" {
		d.handleGeminiCountTokens(r, w, keyCtx, requestedModel, bifrostReq)
		return
	}

	start := time.Now()
	reqCtx := ExtractResponsesRouteContext(bifrostReq, wireReq.Stream, ClientProtocolGeminiNative, keyCtx, r.Header, "")
	svc, found, loadErr := d.loadService(r.Context(), keyCtx.OrganizationID, requestedModel)
	strategy := "auto"
	if loadErr == nil && found {
		strategy = svc.Policy.SessionAffinity.KeyStrategy
	}
	reqCtx.SessionKey = DeriveSessionKey(r, "", "", "", strategy)

	svc, decision, ok := d.resolveServiceAndRoute(w, r, keyCtx, requestedModel, reqCtx, "gemini_"+action)
	if !ok {
		return
	}

	requestID := uuid.New()
	if err := d.recordPendingDecision(r.Context(), requestID, &keyCtx, nil, "api", svc, decision); err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to record routing decision.")
		return
	}

	if !wireReq.Stream {
		d.handleGeminiNonStream(r, w, requestID, keyCtx, svc, decision, requestedModel, bifrostReq, start)
		return
	}
	d.handleGeminiStream(r, w, requestID, keyCtx, svc, decision, requestedModel, bifrostReq, start)
}

func (d Deps) handleGeminiCountTokens(r *http.Request, w http.ResponseWriter, keyCtx apiKeyContext,
	requestedModel string, bifrostReq *schemas.BifrostResponsesRequest) {
	reqCtx := ExtractResponsesRouteContext(bifrostReq, false, ClientProtocolGeminiNative, keyCtx, r.Header, "")
	svc, found, err := d.loadService(r.Context(), keyCtx.OrganizationID, requestedModel)
	if err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to resolve model service.")
		return
	}
	if !found {
		writeProtocolError(w, r, http.StatusNotFound, "model_not_found", "Model service does not exist or is not enabled.",
			apiErrorOptions{Reason: "model_not_found", Param: "model", Params: map[string]any{"model": requestedModel}})
		return
	}
	allowed, err := d.hasModelServiceAccess(r.Context(), keyCtx.OrganizationID, keyCtx.ProjectID, svc.ID, "invoke")
	if err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "Failed to check model service access.")
		return
	}
	if !allowed {
		d.auditDeniedModelAccess(r, keyCtx, svc.ID, "gemini_count_tokens")
		writeProtocolError(w, r, http.StatusForbidden, "model_access_denied", "The current project is not allowed to invoke this model service.")
		return
	}

	scores := d.loadCandidateHealthWithBreaker(r.Context(), keyCtx.OrganizationID, svc.Candidates, svc.Policy.CircuitBreaker)
	decision := EvaluateRoute(svc.Candidates, svc.Policy, reqCtx, scores, d.fxRatesSnapshot(), 0)
	for _, cand := range decision.Chain {
		provider, pErr := bifrostx.EngineProviderKey(cand.AccountID, cand.Provider, cand.APIProtocol)
		if pErr != nil {
			continue
		}
		candReq := cloneResponsesRequestForCandidate(bifrostReq, provider, cand.UpstreamModel)
		cctx := schemas.NewBifrostContext(r.Context(), time.Now().Add(15*time.Second))
		cctx.SetValue(schemas.BifrostContextKeyAPIKeyID, cand.AccountID)
		if resp, bErr := d.Engine.Client().CountTokensRequest(cctx, candReq); bErr == nil && resp != nil {
			writeJSON(w, http.StatusOK, map[string]any{"totalTokens": resp.InputTokens})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"totalTokens": reqCtx.EstimatedInputTokens})
}

func (d Deps) handleGeminiNonStream(r *http.Request, w http.ResponseWriter, requestID uuid.UUID,
	keyCtx apiKeyContext, svc resolvedService, decision RouteDecision, requestedModel string,
	bifrostReq *schemas.BifrostResponsesRequest, start time.Time) {
	served, resp, attempts, bifrostErr := d.executeResponsesNonStreamChain(r.Context(), svc, decision.Chain, bifrostReq, nil)
	if bifrostErr != nil {
		d.recordChainFailureWithProtocol(r.Context(), &keyCtx, requestID, decision.Chain, attempts, bifrostErr, start, nil, usageSnapshot{}, ClientProtocolGeminiNative)
		writeProtocolBifrostError(w, r, bifrostErr)
		return
	}

	out := gemini.ToGeminiResponsesResponse(resp)
	if out == nil {
		writeProtocolError(w, r, http.StatusBadGateway, "upstream_error", "Failed to convert upstream response into Gemini format.")
		return
	}
	if requestedModel != "" {
		out.ModelVersion = requestedModel
	}
	writeJSON(w, http.StatusOK, out)

	snap := usageFromResponses(resp.Usage)
	d.finalizeResponsesUsage(r.Context(), requestID, keyCtx, svc, decision, served, attempts, snap, ClientProtocolGeminiNative, start, nil)
}

func (d Deps) handleGeminiStream(r *http.Request, w http.ResponseWriter, requestID uuid.UUID,
	keyCtx apiKeyContext, svc resolvedService, decision RouteDecision, requestedModel string,
	bifrostReq *schemas.BifrostResponsesRequest, start time.Time) {
	winner, attempts, lastErr, lastTTFT, clientAborted := d.openResponsesStreamWinner(r, svc, decision, bifrostReq, nil)
	if winner == nil {
		d.handleResponsesStreamPreCommitFailure(r, w, requestID, keyCtx, decision, attempts, lastErr, lastTTFT, clientAborted, start, ClientProtocolGeminiNative)
		return
	}
	defer winner.Cancel()

	useSSE := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("alt")), "sse")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProtocolError(w, r, http.StatusInternalServerError, "internal_error", "The current connection does not support streaming responses.",
			apiErrorOptions{Reason: "streaming_unsupported"})
		return
	}
	if useSSE {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	if !useSSE {
		_, _ = w.Write([]byte("[\n"))
	}
	flusher.Flush()

	elapsedFirst := int(time.Since(start).Milliseconds())
	firstTokenMS := &elapsedFirst
	snapshot := winner.Snapshot
	attempts = winner.Attempts
	state := gemini.NewBifrostToGeminiStreamState()
	wroteAnyChunk := false

	emitEvent := func(ev *schemas.BifrostResponsesStreamResponse) bool {
		if ev == nil {
			return true
		}
		if ev.Response != nil && ev.Response.Usage != nil {
			mergeUsageSnapshot(&snapshot, usageFromResponses(ev.Response.Usage))
		}
		out := gemini.ToGeminiResponsesStreamResponse(ev, state)
		if out == nil {
			return true
		}
		if requestedModel != "" {
			out.ModelVersion = requestedModel
		}
		payload, err := json.Marshal(out)
		if err != nil {
			d.Logger.Warn("marshal gemini stream chunk failed", "request_id", requestID, "error", err)
			return false
		}
		if useSSE {
			if _, err := w.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
				return false
			}
		} else {
			prefix := ""
			if wroteAnyChunk {
				prefix = ",\n"
			}
			if _, err := w.Write([]byte(prefix + string(payload))); err != nil {
				return false
			}
		}
		wroteAnyChunk = true
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
		d.recordStreamInterruptedWithProtocol(r.Context(), requestID, winner.Candidate, attempts, start, firstTokenMS, snapshot, ClientProtocolGeminiNative)
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
		errFrame, _ := json.Marshal(map[string]any{
			"error": map[string]any{
				"code":       http.StatusBadGateway,
				"status":     "INTERNAL",
				"error_code": "upstream_error",
				"reason":     "upstream_request_failed",
				"message":    "Upstream request failed.",
				"detail":     errMsg,
				"request_id": requestID.String(),
			},
		})
		if useSSE {
			_, _ = w.Write([]byte("data: " + string(errFrame) + "\n\n"))
		} else {
			prefix := ""
			if wroteAnyChunk {
				prefix = ",\n"
			}
			_, _ = w.Write([]byte(prefix + string(errFrame) + "\n]"))
		}
		flusher.Flush()
		d.recordChainFailureWithProtocol(r.Context(), &keyCtx, requestID, decision.Chain, attempts, streamErr, start, firstTokenMS, snapshot, ClientProtocolGeminiNative)
		return
	}

	if !useSSE {
		_, _ = w.Write([]byte("\n]"))
		flusher.Flush()
	}

	d.Breaker().RecordSuccess(r.Context(), winner.Candidate.ID)
	attempts = append(attempts, AttemptRecord{
		CandidateID: winner.Candidate.ID, Provider: winner.Candidate.Provider, UpstreamModel: winner.Candidate.UpstreamModel,
		StatusCode: http.StatusOK, LatencyMS: int(time.Since(start).Milliseconds()), Outcome: "success",
	})
	d.finalizeResponsesUsage(r.Context(), requestID, keyCtx, svc, decision, winner.Candidate, attempts, snapshot, ClientProtocolGeminiNative, start, firstTokenMS)
}
