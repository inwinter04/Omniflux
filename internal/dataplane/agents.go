package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/asset"
	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/maximhq/bifrost/core/schemas"
)

const maxAgentChatBodyBytes = 1024 * 1024

type agentResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	VersionID   string    `json:"version_id"`
	Version     int       `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type agentDetailResponse struct {
	agentResponse
	Spec asset.AgentSpec `json:"spec"`
}

type agentChatAPIRequest struct {
	SessionID string         `json:"session_id"`
	Variables map[string]any `json:"variables"`
	Message   string         `json:"message"`
	Stream    bool           `json:"stream"`
}

func (d Deps) ListAgents(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT a.id, a.name, a.description, v.id, v.version, a.updated_at
		FROM assets a JOIN asset_versions v ON v.id = a.current_version_id
		WHERE a.organization_id = $1 AND a.asset_type = 'agent' AND a.status = 'published'
		  AND EXISTS (
		    SELECT 1 FROM asset_grants g WHERE g.asset_id = a.id AND g.organization_id = $1
		      AND g.actions && ARRAY['read','invoke']::text[]
		      AND ((g.subject_type = 'organization' AND g.subject_id = $1)
		        OR (g.subject_type = 'project' AND g.subject_id = $2)
		        OR (g.subject_type = 'department' AND g.subject_id = (
		          SELECT p.department_id FROM projects p WHERE p.id = $2 AND p.organization_id = $1
		        )))
		  )
		ORDER BY a.name ASC
	`, keyCtx.OrganizationID, keyCtx.ProjectID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to query agent.")
		return
	}
	defer rows.Close()
	result := []agentResponse{}
	for rows.Next() {
		var item agentResponse
		var id, versionID uuid.UUID
		if err := rows.Scan(&id, &item.Name, &item.Description, &versionID, &item.Version, &item.UpdatedAt); err != nil {
			apiError(w, http.StatusInternalServerError, "internal_error", "Failed to read agent.")
			return
		}
		item.ID, item.VersionID = id.String(), versionID.String()
		result = append(result, item)
	}
	writePromptJSON(w, http.StatusOK, result)
}

func (d Deps) GetAgent(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	item, rawSpec, err := d.loadAgent(r, keyCtx, chi.URLParam(r, "name"), "read")
	if errors.Is(err, pgx.ErrNoRows) {
		apiError(w, http.StatusNotFound, "agent_not_found", "Agent does not exist, is not published, or is not readable by the current project.")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to query agent.")
		return
	}
	spec, err := asset.ParseAgentSpec(rawSpec)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Agent configuration is invalid.")
		return
	}
	writePromptJSON(w, http.StatusOK, agentDetailResponse{agentResponse: item, Spec: spec})
}

func (d Deps) ChatAgent(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	var body agentChatAPIRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAgentChatBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", "Request body format is invalid.", apiErrorOptions{Reason: "invalid_json", Detail: err.Error()})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		apiError(w, http.StatusBadRequest, "invalid_request", "Request body must contain exactly one JSON object.", apiErrorOptions{Reason: "invalid_json"})
		return
	}
	name := chi.URLParam(r, "name")
	item, rawSpec, err := d.loadAgent(r, keyCtx, name, "invoke")
	if errors.Is(err, pgx.ErrNoRows) {
		d.auditDeniedAgentChat(r, keyCtx, name)
		apiError(w, http.StatusForbidden, "agent_access_denied", "Agent does not exist, is not published, or is not invocable by the current project.")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to query agent.")
		return
	}
	agentSpec, err := asset.ParseAgentSpec(rawSpec)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Agent configuration is invalid.")
		return
	}
	serviceID := uuid.MustParse(agentSpec.ModelServiceID)
	allowed, err := d.hasModelServiceAccess(r.Context(), keyCtx.OrganizationID, keyCtx.ProjectID, serviceID, "invoke")
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to check model service access.")
		return
	}
	if !allowed {
		apiError(w, http.StatusForbidden, "model_access_denied", "The current project is not allowed to invoke the model service bound to this agent.")
		return
	}
	if !d.checkBudgets(w, r, keyCtx) {
		return
	}
	agentID := uuid.MustParse(item.ID)
	sessionID := uuid.Nil
	if body.SessionID != "" {
		sessionID, err = uuid.Parse(body.SessionID)
		if err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request", "The session_id parameter is invalid.", apiErrorOptions{Reason: "invalid_session_id", Param: "session_id"})
			return
		}
	}
	run, err := d.StartAgentRun(r.Context(), AgentRunStart{
		OrganizationID: keyCtx.OrganizationID, AssetID: agentID, SessionID: sessionID,
		Variables: body.Variables, Message: body.Message, PublishedOnly: true,
		Actor: AgentActor{Channel: "api", ProjectID: &keyCtx.ProjectID, ApplicationID: &keyCtx.ApplicationID, APIKeyID: &keyCtx.KeyID},
	})
	if err != nil {
		writeAgentAPIError(w, err)
		return
	}
	var serviceName string
	if err := d.Pool.QueryRow(r.Context(), `SELECT name FROM model_services WHERE id = $1 AND organization_id = $2 AND status = 'active'`, serviceID, keyCtx.OrganizationID).Scan(&serviceName); err != nil {
		_ = d.FinishAgentRun(r.Context(), run, "failed", DebugResult{}, errors.New("Model service does not exist or is not enabled."))
		apiError(w, http.StatusConflict, "agent_model_unavailable", "The model service bound to the agent does not exist or is not enabled.", apiErrorOptions{Reason: "agent_model_unavailable"})
		return
	}
	svc, found, err := d.loadService(r.Context(), keyCtx.OrganizationID, serviceName)
	if err != nil || !found {
		_ = d.FinishAgentRun(r.Context(), run, "failed", DebugResult{}, errors.New("Model service has no available candidates."))
		apiError(w, http.StatusConflict, "agent_model_unavailable", "The model service bound to the agent has no available candidates.", apiErrorOptions{Reason: "agent_model_no_candidates"})
		return
	}
	chain := selectChain(svc.Candidates, svc.Policy, d.loadCandidateHealth(r.Context(), keyCtx.OrganizationID, svc.Candidates))
	primary := chain[0]
	provider, err := bifrostx.MapProvider(primary.Provider)
	if err != nil {
		_ = d.FinishAgentRun(r.Context(), run, "failed", DebugResult{}, err)
		apiError(w, http.StatusInternalServerError, "internal_error", "Candidate provider is not supported.", apiErrorOptions{Reason: "unsupported_provider"})
		return
	}
	if err := d.recordPending(r.Context(), run.RunID, keyCtx, svc, chain); err != nil {
		_ = d.FinishAgentRun(r.Context(), run, "failed", DebugResult{}, errors.New("Failed to record routing decision."))
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to record routing decision.")
		return
	}
	request := &schemas.BifrostChatRequest{
		Provider: provider, Model: primary.UpstreamModel, Input: run.Input,
		Params: AgentChatParameters(run.Spec, run.PromptSpec), Fallbacks: fallbackSpecs(chain),
	}
	requestCtx, cancelRequest := context.WithCancel(r.Context())
	defer cancelRequest()
	timeout := time.Duration(svc.Policy.ProviderTimeoutMS) * time.Millisecond
	start := time.Now()
	bctx := schemas.NewBifrostContext(requestCtx, time.Now().Add(timeout))
	runStatus := "failed"
	observer := &completionObserver{onComplete: func(status string, result DebugResult, runErr error) {
		runStatus = status
		if err := d.FinishAgentRun(r.Context(), run, status, result, runErr); err != nil {
			d.Logger.Warn("finalize agent run failed", "run_id", run.RunID, "error", err)
		}
	}}
	w.Header().Set("X-Agent-Session-Id", run.SessionID.String())
	w.Header().Set("X-Agent-Run-Id", run.RunID.String())
	w.Header().Set("X-Agent-Version", strconv.Itoa(run.Version))
	w.Header().Set("X-Agent-Version-Id", run.VersionID.String())
	if body.Stream {
		var firstTokenSeen atomic.Bool
		if svc.Policy.FirstTokenTimeoutMS > 0 {
			d.startFirstTokenWatchdog(requestCtx, cancelRequest, &firstTokenSeen,
				time.Duration(svc.Policy.FirstTokenTimeoutMS)*time.Millisecond, run.RunID)
		}
		d.handleStreamObserved(bctx, requestCtx, r, w, run.RunID, keyCtx, chain, name, request, start, &firstTokenSeen, observer)
	} else {
		d.handleNonStreamObserved(bctx, r, w, run.RunID, chain, name, request, start, observer)
	}
	d.auditAgentChat(r, keyCtx, agentID, run, runStatus)
}

func (d Deps) loadAgent(r *http.Request, keyCtx apiKeyContext, name, action string) (agentResponse, json.RawMessage, error) {
	var item agentResponse
	var id, versionID uuid.UUID
	var rawSpec json.RawMessage
	err := d.Pool.QueryRow(r.Context(), `
		SELECT a.id, a.name, a.description, v.id, v.version, a.updated_at, v.spec
		FROM assets a JOIN asset_versions v ON v.id = a.current_version_id
		WHERE a.organization_id = $1 AND a.name = $2 AND a.asset_type = 'agent' AND a.status = 'published'
		  AND EXISTS (
		    SELECT 1 FROM asset_grants g WHERE g.asset_id = a.id AND g.organization_id = $1
		      AND g.actions @> ARRAY[$3]::text[]
		      AND ((g.subject_type = 'organization' AND g.subject_id = $1)
		        OR (g.subject_type = 'project' AND g.subject_id = $4)
		        OR (g.subject_type = 'department' AND g.subject_id = (
		          SELECT p.department_id FROM projects p WHERE p.id = $4 AND p.organization_id = $1
		        )))
		  )
	`, keyCtx.OrganizationID, name, action, keyCtx.ProjectID).Scan(&id, &item.Name, &item.Description,
		&versionID, &item.Version, &item.UpdatedAt, &rawSpec)
	item.ID, item.VersionID = id.String(), versionID.String()
	return item, rawSpec, err
}

func writeAgentAPIError(w http.ResponseWriter, err error) {
	var inputErr *AgentInputError
	if errors.As(err, &inputErr) {
		status := http.StatusBadRequest
		if inputErr.Code == "agent_session_not_found" || inputErr.Code == "agent_version_not_found" {
			status = http.StatusNotFound
		}
		apiError(w, status, "agent_run_invalid", inputErr.Message, apiErrorOptions{Reason: inputErr.Code})
		return
	}
	apiError(w, http.StatusInternalServerError, "internal_error", "Failed to start agent run.")
}

func (d Deps) auditAgentChat(r *http.Request, keyCtx apiKeyContext, agentID uuid.UUID, run AgentRunContext, status string) {
	result := "failure"
	if status == "success" {
		result = "success"
	}
	_, _ = d.Pool.Exec(context.WithoutCancel(r.Context()), `
		INSERT INTO audit_events(organization_id, actor_type, action, resource_type, resource_id, result, after_state, request_id)
		VALUES ($1,'api_key','agent.executed','asset',$2,$3,$4,$5)
	`, keyCtx.OrganizationID, agentID.String(), result, auditDataJSON(map[string]any{
		"api_key_id": keyCtx.KeyID.String(), "project_id": keyCtx.ProjectID.String(), "application_id": keyCtx.ApplicationID.String(),
		"session_id": run.SessionID.String(), "version_id": run.VersionID.String(), "version": run.Version,
	}), run.RunID)
}

func (d Deps) auditDeniedAgentChat(r *http.Request, keyCtx apiKeyContext, name string) {
	var assetID string
	if err := d.Pool.QueryRow(r.Context(), `SELECT id FROM assets WHERE organization_id = $1 AND name = $2 AND asset_type = 'agent'`,
		keyCtx.OrganizationID, name).Scan(&assetID); err != nil {
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_type, action, resource_type, resource_id, result, after_state, request_id)
		VALUES ($1,'api_key','agent.executed','asset',$2,'failure',$3,$4)
	`, keyCtx.OrganizationID, assetID, auditDataJSON(map[string]any{
		"api_key_id": keyCtx.KeyID.String(), "project_id": keyCtx.ProjectID.String(), "reason": "not_published_or_invoke_grant_missing",
	}), uuid.New())
}
