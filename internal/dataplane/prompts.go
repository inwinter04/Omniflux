package dataplane

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/asset"
)

const maxPromptRenderBodyBytes = 1024 * 1024

type promptTemplateResponse struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	VersionID      string    `json:"version_id"`
	Version        int       `json:"version"`
	ModelServiceID string    `json:"model_service_id"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type promptTemplateDetailResponse struct {
	promptTemplateResponse
	Spec asset.PromptSpec `json:"spec"`
}

type renderPromptTemplateRequest struct {
	Variables map[string]any `json:"variables"`
}

func (d Deps) ListPromptTemplates(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT a.id, a.name, a.description, v.id, v.version,
		       COALESCE(v.spec->>'model_service_id', ''), a.updated_at
		FROM assets a
		JOIN asset_versions v ON v.id = a.current_version_id
		WHERE a.organization_id = $1 AND a.asset_type = 'prompt_template' AND a.status = 'published'
		  AND EXISTS (
		    SELECT 1 FROM asset_grants g
		    WHERE g.asset_id = a.id AND g.organization_id = $1
		      AND g.actions && ARRAY['read','invoke']::text[]
		      AND (
		        (g.subject_type = 'organization' AND g.subject_id = $1)
		        OR (g.subject_type = 'project' AND g.subject_id = $2)
		        OR (g.subject_type = 'department' AND g.subject_id = (
		          SELECT p.department_id FROM projects p WHERE p.id = $2 AND p.organization_id = $1
		        ))
		      )
		  )
		ORDER BY a.name ASC
	`, keyCtx.OrganizationID, keyCtx.ProjectID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to query prompt template.")
		return
	}
	defer rows.Close()
	list := []promptTemplateResponse{}
	for rows.Next() {
		var item promptTemplateResponse
		var id, versionID uuid.UUID
		if err := rows.Scan(&id, &item.Name, &item.Description, &versionID, &item.Version, &item.ModelServiceID, &item.UpdatedAt); err != nil {
			apiError(w, http.StatusInternalServerError, "internal_error", "Failed to read prompt template.")
			return
		}
		item.ID = id.String()
		item.VersionID = versionID.String()
		list = append(list, item)
	}
	writePromptJSON(w, http.StatusOK, list)
}

func (d Deps) GetPromptTemplate(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	item, rawSpec, err := d.loadPromptTemplate(r, keyCtx, chi.URLParam(r, "name"), "read")
	if errors.Is(err, pgx.ErrNoRows) {
		apiError(w, http.StatusNotFound, "prompt_template_not_found", "Prompt template does not exist, is not published, or is not readable by the current project.")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to query prompt template.")
		return
	}
	spec, err := asset.ParsePromptSpec(rawSpec)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Prompt template configuration is invalid.")
		return
	}
	writePromptJSON(w, http.StatusOK, promptTemplateDetailResponse{promptTemplateResponse: item, Spec: spec})
}

func (d Deps) RenderPromptTemplate(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	var body renderPromptTemplateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPromptRenderBodyBytes))
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
	item, rawSpec, err := d.loadPromptTemplate(r, keyCtx, name, "invoke")
	if errors.Is(err, pgx.ErrNoRows) {
		d.auditDeniedPromptRender(r, keyCtx, name)
		apiError(w, http.StatusForbidden, "prompt_template_access_denied", "Prompt template does not exist, is not published, or is not invocable by the current project.")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to query prompt template.")
		return
	}
	spec, err := asset.ParsePromptSpec(rawSpec)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Prompt template configuration is invalid.")
		return
	}
	rendered, err := asset.RenderPrompt(spec, body.Variables)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request", "Prompt render input is invalid.", apiErrorOptions{Reason: "prompt_render_invalid"})
		return
	}
	requestID := uuid.New()
	if _, err := d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_type, action, resource_type, resource_id, result, after_state, request_id)
		VALUES ($1,'api_key','prompt_template.invoked','asset',$2,'success',$3,$4)
	`, keyCtx.OrganizationID, item.ID, auditDataJSON(map[string]any{
		"api_key_id": keyCtx.KeyID.String(), "project_id": keyCtx.ProjectID.String(),
		"application_id": keyCtx.ApplicationID.String(), "version_id": item.VersionID,
		"version": item.Version, "model_service_id": item.ModelServiceID,
	}), requestID); err != nil {
		apiError(w, http.StatusInternalServerError, "audit_error", "Failed to write the invocation audit record. Please retry.")
		return
	}
	writePromptJSON(w, http.StatusOK, map[string]any{
		"id": item.ID, "name": item.Name, "version_id": item.VersionID, "version": item.Version,
		"model_service_id": item.ModelServiceID, "rendered": rendered, "request_id": requestID.String(),
	})
}

func (d Deps) loadPromptTemplate(r *http.Request, keyCtx apiKeyContext, name, action string) (promptTemplateResponse, json.RawMessage, error) {
	var item promptTemplateResponse
	var id, versionID uuid.UUID
	var rawSpec json.RawMessage
	err := d.Pool.QueryRow(r.Context(), `
		SELECT a.id, a.name, a.description, v.id, v.version,
		       COALESCE(v.spec->>'model_service_id', ''), a.updated_at, v.spec
		FROM assets a
		JOIN asset_versions v ON v.id = a.current_version_id
		WHERE a.organization_id = $1 AND a.name = $2 AND a.asset_type = 'prompt_template' AND a.status = 'published'
		  AND EXISTS (
		    SELECT 1 FROM asset_grants g
		    WHERE g.asset_id = a.id AND g.organization_id = $1
		      AND g.actions @> ARRAY[$3]::text[]
		      AND (
		        (g.subject_type = 'organization' AND g.subject_id = $1)
		        OR (g.subject_type = 'project' AND g.subject_id = $4)
		        OR (g.subject_type = 'department' AND g.subject_id = (
		          SELECT p.department_id FROM projects p WHERE p.id = $4 AND p.organization_id = $1
		        ))
		      )
		  )
	`, keyCtx.OrganizationID, name, action, keyCtx.ProjectID).Scan(&id, &item.Name, &item.Description,
		&versionID, &item.Version, &item.ModelServiceID, &item.UpdatedAt, &rawSpec)
	item.ID = id.String()
	item.VersionID = versionID.String()
	return item, rawSpec, err
}

func (d Deps) auditDeniedPromptRender(r *http.Request, keyCtx apiKeyContext, name string) {
	var assetID string
	var versionID *string
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT a.id, a.current_version_id::text FROM assets a
		WHERE a.organization_id = $1 AND a.name = $2 AND a.asset_type = 'prompt_template'
	`, keyCtx.OrganizationID, name).Scan(&assetID, &versionID); err != nil {
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_type, action, resource_type, resource_id, result, after_state, request_id)
		VALUES ($1,'api_key','prompt_template.invoked','asset',$2,'failure',$3,$4)
	`, keyCtx.OrganizationID, assetID, auditDataJSON(map[string]any{
		"api_key_id": keyCtx.KeyID.String(), "project_id": keyCtx.ProjectID.String(),
		"application_id": keyCtx.ApplicationID.String(), "version_id": versionID,
		"reason": "not_published_or_invoke_grant_missing",
	}), uuid.New())
}

func auditDataJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func writePromptJSON(w http.ResponseWriter, status int, value any) {
	var output bytes.Buffer
	if err := json.NewEncoder(&output).Encode(value); err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to serialize response.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(output.Bytes())
}
