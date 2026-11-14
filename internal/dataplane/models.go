package dataplane

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// ListModels 处理 GET /v1/models,返回当前组织已启用的模型服务,格式对齐 OpenAI。
func (d Deps) ListModels(w http.ResponseWriter, r *http.Request) {
	keyCtx, ok := keyFromContext(r.Context())
	if !ok {
		apiError(w, http.StatusUnauthorized, "missing_api_key", "缺少 API Key")
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT name, created_at FROM model_services
		WHERE organization_id = $1 AND status = 'active'
		ORDER BY name ASC
	`, keyCtx.OrganizationID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "internal_error", "模型列表查询失败")
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
			apiError(w, http.StatusInternalServerError, "internal_error", "模型列表查询失败")
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
		apiError(w, http.StatusInternalServerError, "internal_error", "模型列表查询失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(body)
}

var _ = uuid.Nil
