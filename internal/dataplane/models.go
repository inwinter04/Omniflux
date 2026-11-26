package dataplane

import (
	"net/http"
	"time"

	"github.com/google/uuid"
)

// ListModels 处理 GET /v1/models,返回当前组织已启用的模型服务,格式对齐 OpenAI。
func (d Deps) ListModels(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "Missing API key. Provide Authorization: Bearer <key>.")
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT s.name, s.created_at FROM model_services s
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
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to query model list.")
		return
	}
	defer rows.Close()
	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	data := []modelEntry{}
	for rows.Next() {
		var name string
		var createdAt time.Time
		if err := rows.Scan(&name, &createdAt); err != nil {
			apiError(w, http.StatusInternalServerError, "internal_error", "Failed to query model list.")
			return
		}
		data = append(data, modelEntry{
			ID:      name,
			Object:  "model",
			Created: createdAt.Unix(),
			OwnedBy: "organization",
		})
	}
	if err := rows.Err(); err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "Failed to query model list.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

var _ = uuid.Nil
