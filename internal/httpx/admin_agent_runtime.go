package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/dataplane"
)

var errAgentEmptyResponse = errors.New("agent_empty_response")

type agentChatRequest struct {
	VersionID string         `json:"version_id"`
	SessionID string         `json:"session_id"`
	Variables map[string]any `json:"variables"`
	Message   string         `json:"message"`
	Stream    bool           `json:"stream"`
}

type agentRunResponse struct {
	ID               string    `json:"id"`
	SessionID        string    `json:"session_id"`
	Status           string    `json:"status"`
	Version          int       `json:"version"`
	VersionID        string    `json:"version_id"`
	ModelServiceID   string    `json:"model_service_id"`
	ModelServiceName string    `json:"model_service_name"`
	Request          string    `json:"request"`
	Response         string    `json:"response"`
	Error            string    `json:"error,omitempty"`
	Provider         string    `json:"provider"`
	UpstreamModel    string    `json:"upstream_model"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	CachedTokens     int64     `json:"cached_tokens"`
	ReasoningTokens  int64     `json:"reasoning_tokens"`
	Cost             float64   `json:"cost"`
	CostCurrency     string    `json:"cost_currency"`
	FirstTokenMS     *int      `json:"first_token_ms"`
	TotalMS          int       `json:"total_ms"`
	CreatedAt        time.Time `json:"created_at"`
}

type agentSessionResponse struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Version   int             `json:"version"`
	Variables json.RawMessage `json:"variables"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type agentMessageResponse struct {
	ID        string    `json:"id"`
	RunID     *string   `json:"run_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func (d ServerDeps) AgentSessions(w http.ResponseWriter, r *http.Request) {
	principal, assetID, ok := d.agentAdminContext(w, r, false)
	if !ok {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT s.id, s.title, v.version, s.variables, s.created_at, s.updated_at
		FROM agent_sessions s JOIN asset_versions v ON v.id = s.agent_version_id
		WHERE s.organization_id = $1 AND s.asset_id = $2 AND s.channel = 'debug' AND s.created_by = $3
		ORDER BY s.updated_at DESC LIMIT 100
	`, principal.OrganizationID, assetID, principal.UserID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AGENT_SESSION_LIST_FAILED", "failed-to-query-sessions", nil)
		return
	}
	defer rows.Close()
	result := []agentSessionResponse{}
	for rows.Next() {
		item, err := scanAgentSession(rows.Scan)
		if err != nil {
			adminError(w, http.StatusInternalServerError, "AGENT_SESSION_LIST_FAILED", "failed-to-read-sessions", nil)
			return
		}
		result = append(result, item)
	}
	adminOK(w, result)
}

func (d ServerDeps) AgentSessionDetail(w http.ResponseWriter, r *http.Request) {
	principal, assetID, ok := d.agentAdminContext(w, r, false)
	if !ok {
		return
	}
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "AGENT_SESSION_ID_INVALID", "invalid-session-id", nil)
		return
	}
	row := d.Pool.QueryRow(r.Context(), `
		SELECT s.id, s.title, v.version, s.variables, s.created_at, s.updated_at
		FROM agent_sessions s JOIN asset_versions v ON v.id = s.agent_version_id
		WHERE s.id = $1 AND s.organization_id = $2 AND s.asset_id = $3 AND s.channel = 'debug' AND s.created_by = $4
	`, sessionID, principal.OrganizationID, assetID, principal.UserID)
	session, err := scanAgentSession(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "AGENT_SESSION_NOT_FOUND", "session-does-not-exist-or-is", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AGENT_SESSION_DETAIL_FAILED", "failed-to-query-session-details", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, agent_run_id, role, content, created_at FROM agent_messages
		WHERE session_id = $1 ORDER BY created_at ASC, id ASC
	`, sessionID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AGENT_MESSAGE_LIST_FAILED", "failed-to-query-session-messages", nil)
		return
	}
	defer rows.Close()
	messages := []agentMessageResponse{}
	for rows.Next() {
		var item agentMessageResponse
		var id, runID *uuid.UUID
		if err := rows.Scan(&id, &runID, &item.Role, &item.Content, &item.CreatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "AGENT_MESSAGE_LIST_FAILED", "failed-to-read-session-messages", nil)
			return
		}
		if id != nil {
			item.ID = id.String()
		}
		if runID != nil {
			value := runID.String()
			item.RunID = &value
		}
		messages = append(messages, item)
	}
	adminOK(w, map[string]any{"session": session, "messages": messages})
}

func (d ServerDeps) DeleteAgentSession(w http.ResponseWriter, r *http.Request) {
	principal, assetID, ok := d.agentAdminContext(w, r, true)
	if !ok {
		return
	}
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "AGENT_SESSION_ID_INVALID", "invalid-session-id", nil)
		return
	}
	command, err := d.Pool.Exec(r.Context(), `
		DELETE FROM agent_sessions WHERE id = $1 AND organization_id = $2 AND asset_id = $3 AND channel = 'debug' AND created_by = $4
	`, sessionID, principal.OrganizationID, assetID, principal.UserID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AGENT_SESSION_DELETE_FAILED", "failed-to-delete-the-session", nil)
		return
	}
	if command.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "AGENT_SESSION_NOT_FOUND", "session-does-not-exist-or-is", nil)
		return
	}
	adminOK(w, nil)
}

func (d ServerDeps) RunAgent(w http.ResponseWriter, r *http.Request) {
	principal, assetID, ok := d.agentAdminContext(w, r, true)
	if !ok {
		return
	}
	var body agentChatRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	versionID, sessionID, err := parseAgentChatIDs(body)
	if err != nil {
		writeAssetError(w, err, "AGENT_RUN_FAILED", "agent-run-failed")
		return
	}
	plane := dataplane.Deps{Logger: d.Logger, Pool: d.Pool, Engine: d.Engine}
	run, err := plane.StartAgentRun(r.Context(), dataplane.AgentRunStart{
		OrganizationID: principal.OrganizationID, AssetID: assetID, VersionID: versionID, SessionID: sessionID,
		Variables: body.Variables, Message: body.Message,
		Actor: dataplane.AgentActor{Channel: "debug", UserID: &principal.UserID},
	})
	if err != nil {
		writeAgentRuntimeError(w, err)
		return
	}
	params := dataplane.AgentChatParameters(run.Spec, run.PromptSpec)
	if body.Stream {
		d.streamAgentRun(w, r, plane, run, params)
		return
	}
	serviceID, _ := uuid.Parse(run.Spec.ModelServiceID)
	result, runErr := plane.DebugConversation(r.Context(), principal.OrganizationID, serviceID, run.Input, params)
	status := "success"
	if runErr == nil && strings.TrimSpace(result.Response) == "" {
		runErr = errAgentEmptyResponse
	}
	if runErr != nil {
		status = "failed"
	}
	if err := plane.FinishAgentRun(r.Context(), run, status, result, runErr); err != nil {
		adminError(w, http.StatusInternalServerError, "AGENT_RUN_FINALIZE_FAILED", "failed-to-save-the-run-result", nil)
		return
	}
	d.auditAgentRun(r, principal, assetID, run, status)
	response := agentResponseFromResult(run, status, result, runErr)
	if runErr != nil {
		reason := "agentUpstreamFailed"
		detail := runErr.Error()
		if errors.Is(runErr, errAgentEmptyResponse) {
			reason, detail = "agentEmptyResponse", ""
		}
		adminError(w, http.StatusBadGateway, "AGENT_UPSTREAM_FAILED", reason, map[string]any{"run": response}, detail)
		return
	}
	adminOK(w, response)
}

func (d ServerDeps) streamAgentRun(w http.ResponseWriter, r *http.Request, plane dataplane.Deps, run dataplane.AgentRunContext, chatParams *schemas.ChatParameters) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		adminError(w, http.StatusInternalServerError, "AGENT_STREAM_UNSUPPORTED", "the-current-connection-does-not-support", nil)
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
	writeEvent("started", map[string]any{"run_id": run.RunID.String(), "session_id": run.SessionID.String(), "version": run.Version})
	serviceID, _ := uuid.Parse(run.Spec.ModelServiceID)
	events, err := plane.DebugConversationStream(r.Context(), run.OrganizationID, serviceID, run.Input, chatParams)
	if err != nil {
		_ = plane.FinishAgentRun(r.Context(), run, "failed", dataplane.DebugResult{}, err)
		writeEvent("error", map[string]string{"error": err.Error()})
		return
	}
	status := "running"
	var result dataplane.DebugResult
	var content strings.Builder
	for status == "running" {
		select {
		case <-r.Context().Done():
			result.Response = content.String()
			_ = plane.FinishAgentRun(r.Context(), run, "interrupted", result, nil)
			return
		case event, open := <-events:
			if !open {
				status = "interrupted"
				result.Response = content.String()
				continue
			}
			if event.Err != nil {
				result.Response = content.String()
				_ = plane.FinishAgentRun(r.Context(), run, "failed", result, event.Err)
				writeEvent("error", map[string]string{"error": event.Err.Error()})
				return
			}
			if event.Delta != "" {
				content.WriteString(event.Delta)
				if !writeEvent("delta", map[string]string{"delta": event.Delta}) {
					result.Response = content.String()
					_ = plane.FinishAgentRun(r.Context(), run, "interrupted", result, nil)
					return
				}
			}
			if event.Result != nil {
				result = *event.Result
				status = "success"
			}
		}
	}
	if status == "running" {
		status = "interrupted"
		result.Response = content.String()
	}
	var runErr error
	if status == "success" && strings.TrimSpace(result.Response) == "" {
		status = "failed"
		runErr = errAgentEmptyResponse
	}
	_ = plane.FinishAgentRun(r.Context(), run, status, result, runErr)
	writeEvent("result", agentResponseFromResult(run, status, result, runErr))
}

func (d ServerDeps) AgentRunHistory(w http.ResponseWriter, r *http.Request) {
	principal, assetID, ok := d.agentAdminContext(w, r, false)
	if !ok {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT r.id, r.session_id, r.status, v.version, r.version_id, r.model_service_id, s.name,
		       r.request_text, r.response_text, COALESCE(r.error_message, ''), COALESCE(r.provider, ''),
		       COALESCE(r.upstream_model, ''), r.input_tokens, r.output_tokens, r.cached_tokens,
		       r.reasoning_tokens, r.cost, r.cost_currency, r.first_token_ms, r.total_ms, r.created_at
		FROM agent_runs r JOIN asset_versions v ON v.id = r.version_id JOIN model_services s ON s.id = r.model_service_id
		WHERE r.organization_id = $1 AND r.asset_id = $2 ORDER BY r.created_at DESC LIMIT 100
	`, principal.OrganizationID, assetID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "AGENT_RUN_HISTORY_FAILED", "failed-to-query-run-records-94841f", nil)
		return
	}
	defer rows.Close()
	result := []agentRunResponse{}
	for rows.Next() {
		var item agentRunResponse
		var id, sessionID, versionID, serviceID uuid.UUID
		if err := rows.Scan(&id, &sessionID, &item.Status, &item.Version, &versionID, &serviceID, &item.ModelServiceName,
			&item.Request, &item.Response, &item.Error, &item.Provider, &item.UpstreamModel, &item.InputTokens,
			&item.OutputTokens, &item.CachedTokens, &item.ReasoningTokens, &item.Cost, &item.CostCurrency, &item.FirstTokenMS,
			&item.TotalMS, &item.CreatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "AGENT_RUN_HISTORY_FAILED", "failed-to-read-run-records-184tic1", nil)
			return
		}
		item.ID, item.SessionID, item.VersionID, item.ModelServiceID = id.String(), sessionID.String(), versionID.String(), serviceID.String()
		result = append(result, item)
	}
	adminOK(w, result)
}

func (d ServerDeps) agentAdminContext(w http.ResponseWriter, r *http.Request, write bool) (auth.Principal, uuid.UUID, bool) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return principal, uuid.Nil, false
	}
	if write {
		if d.requireProjectAdmin(w, principal) != nil {
			return principal, uuid.Nil, false
		}
	} else if d.requireViewer(w, principal) != nil {
		return principal, uuid.Nil, false
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "AGENT_ID_INVALID", "agent-id-invalid")
		return principal, uuid.Nil, false
	}
	var assetType string
	if err := d.Pool.QueryRow(r.Context(), `SELECT asset_type FROM assets WHERE id = $1 AND organization_id = $2`, assetID, principal.OrganizationID).Scan(&assetType); err != nil || assetType != "agent" {
		adminError(w, http.StatusNotFound, "AGENT_NOT_FOUND", "agent-does-not-exist-or-does", nil)
		return principal, uuid.Nil, false
	}
	return principal, assetID, true
}

func parseAgentChatIDs(body agentChatRequest) (uuid.UUID, uuid.UUID, error) {
	var versionID, sessionID uuid.UUID
	var err error
	if body.VersionID != "" {
		versionID, err = uuid.Parse(body.VersionID)
		if err != nil {
			return versionID, sessionID, assetError(http.StatusBadRequest, "ASSET_VERSION_ID_INVALID", "invalid-version-id")
		}
	}
	if body.SessionID != "" {
		sessionID, err = uuid.Parse(body.SessionID)
		if err != nil {
			return versionID, sessionID, assetError(http.StatusBadRequest, "AGENT_SESSION_ID_INVALID", "invalid-session-id")
		}
	}
	return versionID, sessionID, nil
}

func writeAgentRuntimeError(w http.ResponseWriter, err error) {
	var inputErr *dataplane.AgentInputError
	if errors.As(err, &inputErr) {
		// 数据面的 code 是小写 snake_case,说明是英文句子;管理面统一为大写 code 加 kebab-case 的 reason,英文句子只作为 detail。
		code := strings.ToUpper(inputErr.Code)
		status := http.StatusBadRequest
		if code == "AGENT_SESSION_NOT_FOUND" || code == "AGENT_VERSION_NOT_FOUND" {
			status = http.StatusNotFound
		}
		adminError(w, status, code, strings.ReplaceAll(inputErr.Code, "_", "-"), nil, inputErr.Message)
		return
	}
	adminError(w, http.StatusInternalServerError, "AGENT_RUN_FAILED", "failed-to-start-the-agent-run", nil)
}

func agentResponseFromResult(run dataplane.AgentRunContext, status string, result dataplane.DebugResult, runErr error) agentRunResponse {
	response := agentRunResponse{
		ID: run.RunID.String(), SessionID: run.SessionID.String(), Status: status, Version: run.Version,
		VersionID: run.VersionID.String(), ModelServiceID: run.Spec.ModelServiceID, Request: run.RequestText, Response: result.Response,
		Provider: result.Provider, UpstreamModel: result.UpstreamModel, InputTokens: result.InputTokens,
		OutputTokens: result.OutputTokens, CachedTokens: result.CachedTokens, ReasoningTokens: result.ReasoningTokens,
		Cost: result.Cost, CostCurrency: result.Currency, FirstTokenMS: result.FirstTokenMS, TotalMS: result.TotalMS, CreatedAt: time.Now(),
	}
	if runErr != nil {
		response.Error = runErr.Error()
	}
	return response
}

func (d ServerDeps) auditAgentRun(r *http.Request, principal auth.Principal, assetID uuid.UUID, run dataplane.AgentRunContext, status string) {
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, result, after_state)
		VALUES ($1,$2,'user','agent.executed','asset',$3,$4,$5)
	`, principal.OrganizationID, principal.UserID, assetID.String(), map[string]string{"success": "success", "failed": "failure"}[status], auditJSON(map[string]any{
		"run_id": run.RunID.String(), "session_id": run.SessionID.String(), "version_id": run.VersionID.String(), "version": run.Version,
	}))
}

func scanAgentSession(scan func(dest ...any) error) (agentSessionResponse, error) {
	var item agentSessionResponse
	var id uuid.UUID
	if err := scan(&id, &item.Title, &item.Version, &item.Variables, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	item.ID = id.String()
	return item, nil
}
