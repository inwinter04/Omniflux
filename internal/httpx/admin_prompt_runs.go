package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/asset"
	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/dataplane"
)

type promptRunRequest struct {
	VersionID       string         `json:"version_id"`
	Variables       map[string]any `json:"variables"`
	ModelServiceIDs []string       `json:"model_service_ids"`
	Stream          bool           `json:"stream"`
}

type promptRunResponse struct {
	ID               string          `json:"id"`
	GroupID          string          `json:"group_id"`
	Kind             string          `json:"kind"`
	Status           string          `json:"status"`
	Version          int             `json:"version"`
	VersionID        string          `json:"version_id"`
	ModelServiceID   string          `json:"model_service_id"`
	ModelServiceName string          `json:"model_service_name"`
	RenderedPrompt   string          `json:"rendered_prompt"`
	Response         string          `json:"response"`
	StructuredOutput json.RawMessage `json:"structured_output,omitempty"`
	SchemaValid      *bool           `json:"schema_valid"`
	Error            string          `json:"error,omitempty"`
	Provider         string          `json:"provider"`
	UpstreamModel    string          `json:"upstream_model"`
	InputTokens      int64           `json:"input_tokens"`
	OutputTokens     int64           `json:"output_tokens"`
	CachedTokens     int64           `json:"cached_tokens"`
	ReasoningTokens  int64           `json:"reasoning_tokens"`
	Cost             float64         `json:"cost"`
	CostCurrency     string          `json:"cost_currency"`
	FirstTokenMS     *int            `json:"first_token_ms"`
	TotalMS          int             `json:"total_ms"`
	Score            *float64        `json:"score"`
	Matched          *bool           `json:"matched"`
	EvaluationSetID  *string         `json:"evaluation_set_id"`
	EvaluationCaseID *string         `json:"evaluation_case_id"`
	CreatedAt        time.Time       `json:"created_at"`
}

type promptRunContext struct {
	spec       asset.PromptSpec
	versionID  uuid.UUID
	version    int
	rendered   string
	serviceIDs []uuid.UUID
}

func (d ServerDeps) RunPromptAsset(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "PROMPT_RUN_FAILED", "model-run-failed")
		return
	}
	var body promptRunRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	runContext, err := d.preparePromptRun(r.Context(), principal, assetID, body.VersionID, body.Variables, body.ModelServiceIDs)
	if err != nil {
		writeAssetError(w, err, "PROMPT_RUN_FAILED", "model-run-failed")
		return
	}
	kind := "single"
	if len(runContext.serviceIDs) > 1 {
		kind = "compare"
	}
	if body.Stream {
		if len(runContext.serviceIDs) != 1 {
			adminError(w, http.StatusBadRequest, "PROMPT_STREAM_SINGLE_MODEL", "select-one-model-service-for-streaming", nil)
			return
		}
		d.streamPromptRun(w, r, principal, assetID, runContext)
		return
	}
	groupID := uuid.New()
	results := make([]promptRunResponse, len(runContext.serviceIDs))
	semaphore := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for index := range runContext.serviceIDs {
		index := index
		semaphore <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-semaphore }()
			results[index] = d.executePromptRun(r.Context(), principal, groupID, kind, assetID, runContext, runContext.serviceIDs[index], nil)
		}()
	}
	wg.Wait()
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
		VALUES ($1,$2,'user','prompt_run.executed','asset',$3,$4)
	`, principal.OrganizationID, principal.UserID, assetID.String(), auditJSON(map[string]any{"group_id": groupID.String(), "kind": kind, "runs": len(results), "version": runContext.version}))
	adminOK(w, map[string]any{"group_id": groupID.String(), "kind": kind, "runs": results})
}

func (d ServerDeps) preparePromptRun(ctx context.Context, principal auth.Principal, assetID uuid.UUID, versionID string, variables map[string]any, modelIDs []string) (promptRunContext, error) {
	var result promptRunContext
	var rawSpec json.RawMessage
	var err error
	if versionID != "" {
		result.versionID, err = uuid.Parse(versionID)
		if err != nil {
			return result, assetError(http.StatusBadRequest, "ASSET_VERSION_ID_INVALID", "invalid-version-id")
		}
		err = d.Pool.QueryRow(ctx, `
			SELECT v.id, v.version, v.spec FROM asset_versions v
			JOIN assets a ON a.id = v.asset_id
			WHERE v.id = $1 AND v.asset_id = $2 AND a.organization_id = $3
		`, result.versionID, assetID, principal.OrganizationID).Scan(&result.versionID, &result.version, &rawSpec)
	} else {
		err = d.Pool.QueryRow(ctx, `
			SELECT v.id, v.version, v.spec FROM asset_versions v
			JOIN assets a ON a.id = v.asset_id
			WHERE v.asset_id = $1 AND a.organization_id = $2 AND v.version = a.latest_version
		`, assetID, principal.OrganizationID).Scan(&result.versionID, &result.version, &rawSpec)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return result, assetError(http.StatusNotFound, "ASSET_VERSION_NOT_FOUND", "version-does-not-exist-or-does")
	}
	if err != nil {
		return result, err
	}
	result.spec, err = asset.ParsePromptSpec(rawSpec)
	if err != nil {
		return result, assetError(http.StatusBadRequest, "PROMPT_SPEC_INVALID", "promptSpecInvalid")
	}
	result.rendered, err = asset.RenderPrompt(result.spec, variables)
	if err != nil {
		return result, assetError(http.StatusBadRequest, "PROMPT_RENDER_INVALID", "prompt-render-invalid")
	}
	if len(modelIDs) == 0 {
		if result.spec.ModelServiceID == "" {
			return result, assetError(http.StatusBadRequest, "PROMPT_MODEL_REQUIRED", "select-at-least-one-model-service")
		}
		modelIDs = []string{result.spec.ModelServiceID}
	}
	if len(modelIDs) > 4 {
		return result, assetError(http.StatusBadRequest, "PROMPT_MODEL_LIMIT", "at-most-4-model-services-can")
	}
	seen := map[uuid.UUID]bool{}
	for _, value := range modelIDs {
		id, parseErr := uuid.Parse(value)
		if parseErr != nil {
			return result, assetError(http.StatusBadRequest, "SERVICE_ID_INVALID", "invalid-model-service-id")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		result.serviceIDs = append(result.serviceIDs, id)
	}
	var count int
	if err := d.Pool.QueryRow(ctx, `SELECT count(*) FROM model_services WHERE id = ANY($1) AND organization_id = $2 AND status = 'active'`, result.serviceIDs, principal.OrganizationID).Scan(&count); err != nil {
		return result, err
	}
	if count != len(result.serviceIDs) {
		return result, assetError(http.StatusBadRequest, "SERVICE_NOT_FOUND", "model-service-does-not-exist-is")
	}
	return result, nil
}

func (d ServerDeps) executePromptRun(ctx context.Context, principal auth.Principal, groupID uuid.UUID, kind string, assetID uuid.UUID, runContext promptRunContext, serviceID uuid.UUID, evaluation *evaluationRunTarget) promptRunResponse {
	runID := uuid.New()
	response := promptRunResponse{ID: runID.String(), GroupID: groupID.String(), Kind: kind, Status: "running", Version: runContext.version, VersionID: runContext.versionID.String(), ModelServiceID: serviceID.String(), RenderedPrompt: runContext.rendered, CreatedAt: time.Now()}
	var evaluationSetID, evaluationCaseID *uuid.UUID
	if evaluation != nil {
		response.Kind = "evaluation"
		evaluationSetID, evaluationCaseID = &evaluation.SetID, &evaluation.CaseID
		response.EvaluationSetID = stringUUIDPtr(evaluation.SetID)
		response.EvaluationCaseID = stringUUIDPtr(evaluation.CaseID)
	}
	if _, err := d.Pool.Exec(ctx, `
		INSERT INTO prompt_runs(id, group_id, organization_id, asset_id, version_id, evaluation_set_id, evaluation_case_id,
		                        actor_user_id, kind, model_service_id, rendered_prompt)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, runID, groupID, principal.OrganizationID, assetID, runContext.versionID, evaluationSetID, evaluationCaseID,
		principal.UserID, response.Kind, serviceID, runContext.rendered); err != nil {
		response.Status = "failed"
		response.Error = "failed-to-create-a-run-record"
		return response
	}
	plane := dataplane.Deps{Logger: d.Logger, Pool: d.Pool, Engine: d.Engine}
	result, err := plane.DebugCompletion(ctx, principal.OrganizationID, serviceID, runContext.rendered, runContext.spec.OutputSchema)
	if err != nil {
		response.Status = "failed"
		response.Error = err.Error()
	} else {
		response.Status = "success"
		response.Response = result.Response
		response.Provider, response.UpstreamModel = result.Provider, result.UpstreamModel
		response.InputTokens, response.OutputTokens = result.InputTokens, result.OutputTokens
		response.CachedTokens, response.ReasoningTokens = result.CachedTokens, result.ReasoningTokens
		response.Cost, response.FirstTokenMS, response.TotalMS = result.Cost, result.FirstTokenMS, result.TotalMS
		response.CostCurrency = result.Currency
		if len(runContext.spec.OutputSchema) > 0 {
			structured, validationErr := asset.ValidateStructuredOutput(runContext.spec, result.Response)
			valid := validationErr == nil
			response.SchemaValid = &valid
			if validationErr != nil {
				response.Status = "failed"
				response.Error = validationErr.Error()
			} else if raw, marshalErr := json.Marshal(structured); marshalErr == nil {
				response.StructuredOutput = raw
			}
		}
	}
	if evaluation != nil && response.Status == "success" {
		matched, score := evaluatePromptOutput(evaluation.MatchType, evaluation.Expected, response.Response)
		response.Matched, response.Score = &matched, &score
	}
	if err := d.finalizePromptRun(ctx, runID, response); err != nil {
		response.Status = "failed"
		response.Error = "prompt_run_finalize_failed"
	}
	return response
}

func (d ServerDeps) finalizePromptRun(ctx context.Context, runID uuid.UUID, response promptRunResponse) error {
	if response.CostCurrency == "" {
		_ = d.Pool.QueryRow(context.WithoutCancel(ctx), `SELECT cost_currency FROM model_candidates WHERE model_service_id = $1 AND retired_at IS NULL LIMIT 1`, response.ModelServiceID).Scan(&response.CostCurrency)
	}
	if response.CostCurrency == "" {
		response.CostCurrency = "USD"
	}
	_, err := d.Pool.Exec(context.WithoutCancel(ctx), `
		UPDATE prompt_runs SET status=$2, response_text=$3, structured_output=$4, schema_valid=$5, error_message=$6,
		                       provider=$7, upstream_model=$8, input_tokens=$9, output_tokens=$10, cached_tokens=$11,
		                       reasoning_tokens=$12, cost=$13, first_token_ms=$14, total_ms=$15, score=$16, matched=$17, cost_currency=$18, finished_at=now()
		WHERE id=$1
	`, runID, response.Status, response.Response, response.StructuredOutput, response.SchemaValid, nullableRunError(response.Error),
		response.Provider, response.UpstreamModel, response.InputTokens, response.OutputTokens, response.CachedTokens,
		response.ReasoningTokens, response.Cost, response.FirstTokenMS, response.TotalMS, response.Score, response.Matched, response.CostCurrency)
	if err != nil {
		d.Logger.Warn("finalize prompt run failed", "run_id", runID, "error", err)
	}
	return err
}

func (d ServerDeps) streamPromptRun(w http.ResponseWriter, r *http.Request, principal auth.Principal, assetID uuid.UUID, runContext promptRunContext) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		adminError(w, http.StatusInternalServerError, "PROMPT_STREAM_UNSUPPORTED", "the-current-connection-does-not-support", nil)
		return
	}
	groupID, runID := uuid.New(), uuid.New()
	serviceID := runContext.serviceIDs[0]
	if _, err := d.Pool.Exec(r.Context(), `
		INSERT INTO prompt_runs(id, group_id, organization_id, asset_id, version_id, actor_user_id, kind, model_service_id, rendered_prompt)
		VALUES ($1,$2,$3,$4,$5,$6,'single',$7,$8)
	`, runID, groupID, principal.OrganizationID, assetID, runContext.versionID, principal.UserID, serviceID, runContext.rendered); err != nil {
		adminError(w, http.StatusInternalServerError, "PROMPT_RUN_CREATE_FAILED", "failed-to-create-a-run-record", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	writeEvent := func(event string, payload any) bool {
		raw, _ := json.Marshal(payload)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	writeEvent("started", map[string]any{"run_id": runID.String(), "group_id": groupID.String(), "version": runContext.version})
	plane := dataplane.Deps{Logger: d.Logger, Pool: d.Pool, Engine: d.Engine}
	response := promptRunResponse{ID: runID.String(), GroupID: groupID.String(), Kind: "single", Version: runContext.version, VersionID: runContext.versionID.String(), ModelServiceID: serviceID.String(), Status: "running"}
	events, err := plane.DebugCompletionStream(r.Context(), principal.OrganizationID, serviceID, runContext.rendered, runContext.spec.OutputSchema)
	if err != nil {
		response.Status, response.Error = "failed", err.Error()
		d.finalizePromptRun(r.Context(), runID, response)
		writeEvent("error", response)
		return
	}
	var content strings.Builder
	for event := range events {
		if event.Err != nil {
			response.Status, response.Error = "failed", event.Err.Error()
			response.Response = content.String()
			d.finalizePromptRun(r.Context(), runID, response)
			writeEvent("error", response)
			return
		}
		if event.Delta != "" {
			content.WriteString(event.Delta)
			if !writeEvent("delta", map[string]string{"delta": event.Delta}) {
				response.Status, response.Response = "interrupted", content.String()
				d.finalizePromptRun(r.Context(), runID, response)
				return
			}
		}
		if event.Result != nil {
			result := event.Result
			response.Status, response.Response = "success", result.Response
			response.Provider, response.UpstreamModel = result.Provider, result.UpstreamModel
			response.InputTokens, response.OutputTokens = result.InputTokens, result.OutputTokens
			response.CachedTokens, response.ReasoningTokens = result.CachedTokens, result.ReasoningTokens
			response.Cost, response.FirstTokenMS, response.TotalMS = result.Cost, result.FirstTokenMS, result.TotalMS
			response.CostCurrency = result.Currency
			if len(runContext.spec.OutputSchema) > 0 {
				structured, validationErr := asset.ValidateStructuredOutput(runContext.spec, result.Response)
				valid := validationErr == nil
				response.SchemaValid = &valid
				if validationErr != nil {
					response.Status, response.Error = "failed", validationErr.Error()
				} else if raw, marshalErr := json.Marshal(structured); marshalErr == nil {
					response.StructuredOutput = raw
				}
			}
		}
	}
	if response.Status == "running" {
		response.Status, response.Response = "interrupted", content.String()
	}
	if err := d.finalizePromptRun(r.Context(), runID, response); err != nil {
		d.Logger.Error("finalize prompt run failed", "run_id", runID, "error", err)
		response.Status, response.Error = "failed", "failed to persist prompt run"
		writeEvent("error", response)
		return
	}
	writeEvent("result", response)
}

func (d ServerDeps) PromptRunHistory(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "PROMPT_RUN_HISTORY_FAILED", "failed-to-query-run-records")
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT r.id, r.group_id, r.kind, r.status, v.version, r.version_id, r.model_service_id, s.name,
		       r.rendered_prompt, r.response_text, r.structured_output, r.schema_valid, COALESCE(r.error_message, ''),
		       COALESCE(r.provider, ''), COALESCE(r.upstream_model, ''), r.input_tokens, r.output_tokens, r.cached_tokens,
		       r.reasoning_tokens, r.cost, r.cost_currency, r.first_token_ms, r.total_ms, r.score, r.matched, r.evaluation_set_id, r.evaluation_case_id, r.created_at
		FROM prompt_runs r
		JOIN model_services s ON s.id = r.model_service_id
		JOIN asset_versions v ON v.id = r.version_id
		WHERE r.organization_id = $1 AND r.asset_id = $2
		ORDER BY r.created_at DESC LIMIT 100
	`, principal.OrganizationID, assetID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROMPT_RUN_HISTORY_FAILED", "failed-to-query-run-records", nil)
		return
	}
	defer rows.Close()
	results := []promptRunResponse{}
	for rows.Next() {
		var item promptRunResponse
		var id, groupID, versionID, serviceID uuid.UUID
		if err := rows.Scan(&id, &groupID, &item.Kind, &item.Status, &item.Version, &versionID, &serviceID, &item.ModelServiceName,
			&item.RenderedPrompt, &item.Response, &item.StructuredOutput, &item.SchemaValid, &item.Error, &item.Provider, &item.UpstreamModel,
			&item.InputTokens, &item.OutputTokens, &item.CachedTokens, &item.ReasoningTokens, &item.Cost, &item.CostCurrency, &item.FirstTokenMS, &item.TotalMS,
			&item.Score, &item.Matched, &item.EvaluationSetID, &item.EvaluationCaseID, &item.CreatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "PROMPT_RUN_HISTORY_FAILED", "failed-to-read-run-records", nil)
			return
		}
		item.ID, item.GroupID, item.VersionID, item.ModelServiceID = id.String(), groupID.String(), versionID.String(), serviceID.String()
		results = append(results, item)
	}
	adminOK(w, results)
}

func nullableRunError(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func stringUUIDPtr(value uuid.UUID) *string {
	text := value.String()
	return &text
}
