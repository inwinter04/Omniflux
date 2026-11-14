package httpx

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/apikeys"
	"github.com/inwinter04/omniflux/internal/auth"
)

type apiKeyResponse struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	KeyPrefix       string     `json:"key_prefix"`
	ProjectID       string     `json:"project_id"`
	ApplicationID   string     `json:"application_id"`
	RateLimitPerMin *int       `json:"rate_limit_per_min"`
	Status          string     `json:"status"`
	ExpiresAt       *time.Time `json:"expires_at"`
	LastUsedAt      *time.Time `json:"last_used_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

type createAPIKeyRequest struct {
	Name            string     `json:"name"`
	ProjectID       string     `json:"project_id"`
	ApplicationID   string     `json:"application_id"`
	ExpiresAt       *time.Time `json:"expires_at"`
	RateLimitPerMin *int       `json:"rate_limit_per_min"`
}

type projectResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DepartmentID string `json:"department_id"`
	Status       string `json:"status"`
}

type applicationResponse struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
}

// Projects 返回当前组织的项目列表,供创建 API Key 时选择。
func (d ServerDeps) Projects(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, name, department_id, status FROM projects
		WHERE organization_id = $1 ORDER BY created_at ASC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_LIST_FAILED", "无法查询项目", nil)
		return
	}
	defer rows.Close()
	list := []projectResponse{}
	for rows.Next() {
		var item projectResponse
		var id, deptID uuid.UUID
		if err := rows.Scan(&id, &item.Name, &deptID, &item.Status); err != nil {
			adminError(w, http.StatusInternalServerError, "PROJECT_LIST_FAILED", "无法查询项目", nil)
			return
		}
		item.ID = id.String()
		item.DepartmentID = deptID.String()
		list = append(list, item)
	}
	adminOK(w, list)
}

// Applications 返回当前组织的应用列表,供创建 API Key 时选择。
func (d ServerDeps) Applications(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT a.id, a.project_id, a.name, a.status FROM applications a
		JOIN projects p ON p.id = a.project_id
		WHERE p.organization_id = $1 ORDER BY a.created_at ASC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_LIST_FAILED", "无法查询应用", nil)
		return
	}
	defer rows.Close()
	list := []applicationResponse{}
	for rows.Next() {
		var item applicationResponse
		var id, projectID uuid.UUID
		if err := rows.Scan(&id, &projectID, &item.Name, &item.Status); err != nil {
			adminError(w, http.StatusInternalServerError, "APPLICATION_LIST_FAILED", "无法查询应用", nil)
			return
		}
		item.ID = id.String()
		item.ProjectID = projectID.String()
		list = append(list, item)
	}
	adminOK(w, list)
}

// APIKeys 返回当前组织的 API Key 列表(不含任何密钥材料)。
func (d ServerDeps) APIKeys(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, name, key_prefix, project_id, application_id, rate_limit_per_min, status, expires_at, last_used_at, created_at
		FROM api_keys WHERE organization_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_LIST_FAILED", "无法查询 API Key", nil)
		return
	}
	defer rows.Close()
	list := []apiKeyResponse{}
	for rows.Next() {
		var item apiKeyResponse
		var id, projectID, applicationID uuid.UUID
		if err := rows.Scan(&id, &item.Name, &item.KeyPrefix, &projectID, &applicationID,
			&item.RateLimitPerMin, &item.Status, &item.ExpiresAt, &item.LastUsedAt, &item.CreatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "APIKEY_LIST_FAILED", "无法查询 API Key", nil)
			return
		}
		item.ID = id.String()
		item.ProjectID = projectID.String()
		item.ApplicationID = applicationID.String()
		list = append(list, item)
	}
	adminOK(w, list)
}

// CreateAPIKey 创建 API Key;明文只在本次响应中返回一次。
func (d ServerDeps) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	var body createAPIKeyRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "APIKEY_NAME_REQUIRED", "请填写 API Key 名称", nil)
		return
	}
	projectID, err := uuid.Parse(body.ProjectID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "APIKEY_PROJECT_INVALID", "project_id 不合法", nil)
		return
	}
	applicationID, err := uuid.Parse(body.ApplicationID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "APIKEY_APPLICATION_INVALID", "application_id 不合法", nil)
		return
	}
	// 项目必须属于当前组织,应用必须挂在同一项目下。
	var count int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM projects WHERE id = $1 AND organization_id = $2
	`, projectID, principal.OrganizationID).Scan(&count); err != nil || count == 0 {
		adminError(w, http.StatusBadRequest, "APIKEY_PROJECT_NOT_FOUND", "项目不存在或不属于当前组织", nil)
		return
	}
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM applications WHERE id = $1 AND project_id = $2
	`, applicationID, projectID).Scan(&count); err != nil || count == 0 {
		adminError(w, http.StatusBadRequest, "APIKEY_APPLICATION_MISMATCH", "应用不存在或不属于所选项目", nil)
		return
	}
	if body.ExpiresAt != nil && !body.ExpiresAt.After(time.Now()) {
		adminError(w, http.StatusBadRequest, "APIKEY_EXPIRES_INVALID", "过期时间必须晚于当前时间", nil)
		if body.RateLimitPerMin != nil && *body.RateLimitPerMin <= 0 {
			adminError(w, http.StatusBadRequest, "APIKEY_RATE_LIMIT_INVALID", "rate_limit_per_min 必须大于 0", nil)
			return
		}
		return
	}
	generated, err := apikeys.Generate()
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_CREATE_FAILED", "生成 API Key 失败", nil)
		return
	}
	secretHash := apikeys.Hash(d.APIKeyPepper, generated.Full)
	keyID := uuid.New()
	if _, err := d.Pool.Exec(r.Context(), `
		INSERT INTO api_keys(id, organization_id, project_id, application_id, created_by, name, key_prefix, secret_hash, expires_at, rate_limit_per_min)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, keyID, principal.OrganizationID, projectID, applicationID, principal.UserID,
		body.Name, generated.Prefix, secretHash, body.ExpiresAt, body.RateLimitPerMin); err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_CREATE_FAILED", "创建 API Key 失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id)
		VALUES ($1,$2,'user','api_key.created','api_key',$3)
	`, principal.OrganizationID, principal.UserID, keyID.String())
	adminOK(w, map[string]string{
		"id":   keyID.String(),
		"key":  generated.Full,
		"hint": "请立即保存,该明文仅出现这一次",
	})
}

type updateAPIKeyRequest struct {
	Status          *string `json:"status"`
	RateLimitPerMin *int    `json:"rate_limit_per_min"`
}

// UpdateAPIKeyStatus 更新 API Key 的状态与限流阈值;禁用立即停止认证通过。
func (d ServerDeps) UpdateAPIKeyStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	var body updateAPIKeyRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	if body.Status != nil && *body.Status != "active" && *body.Status != "disabled" {
		adminError(w, http.StatusBadRequest, "APIKEY_STATUS_INVALID", "status 仅支持 active 或 disabled", nil)
		return
	}
	if body.RateLimitPerMin != nil && *body.RateLimitPerMin <= 0 {
		adminError(w, http.StatusBadRequest, "APIKEY_RATE_LIMIT_INVALID", "rate_limit_per_min 必须大于 0", nil)
		return
	}
	if body.Status == nil && body.RateLimitPerMin == nil {
		adminError(w, http.StatusBadRequest, "APIKEY_NOTHING_TO_UPDATE", "status 与 rate_limit_per_min 至少提供一项", nil)
		return
	}
	keyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "APIKEY_INVALID_ID", "API Key ID 不合法", nil)
		return
	}
	var currentStatus string
	var currentRateLimit *int
	err = d.Pool.QueryRow(r.Context(), `
		SELECT status, rate_limit_per_min FROM api_keys WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL
	`, keyID, principal.OrganizationID).Scan(&currentStatus, &currentRateLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "APIKEY_NOT_FOUND", "API Key 不存在", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_STATUS_FAILED", "更新 API Key 状态失败", nil)
		return
	}
	if body.Status != nil && currentStatus == *body.Status &&
		(body.RateLimitPerMin == nil || (currentRateLimit != nil && *currentRateLimit == *body.RateLimitPerMin)) {
		adminOK(w, map[string]string{"status": currentStatus})
		return
	}
	tag, err := d.Pool.Exec(r.Context(), `
		UPDATE api_keys SET status = COALESCE($3, status), rate_limit_per_min = $4, updated_at = now()
		WHERE id = $1 AND organization_id = $2
	`, keyID, principal.OrganizationID, body.Status, nullableIntPtr(body.RateLimitPerMin))
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_STATUS_FAILED", "更新 API Key 状态失败", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "APIKEY_NOT_FOUND", "API Key 不存在", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
		VALUES ($1,$2,'user','api_key.updated','api_key',$3,$4,$5)
	`, principal.OrganizationID, principal.UserID, keyID.String(),
		auditJSON(map[string]any{"status": currentStatus, "rate_limit_per_min": currentRateLimit}),
		auditJSON(map[string]any{"status": body.Status, "rate_limit_per_min": body.RateLimitPerMin}))
	adminOK(w, map[string]string{"status": currentStatus})
}

// DeleteAPIKey 软删除 API Key,立即停止认证通过且不再出现在列表中。
func (d ServerDeps) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	keyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "APIKEY_INVALID_ID", "API Key ID 不合法", nil)
		return
	}
	tag, err := d.Pool.Exec(r.Context(), `
		UPDATE api_keys SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL
	`, keyID, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_DELETE_FAILED", "删除 API Key 失败", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "APIKEY_NOT_FOUND", "API Key 不存在", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id)
		VALUES ($1,$2,'user','api_key.deleted','api_key',$3)
	`, principal.OrganizationID, principal.UserID, keyID.String())
	adminOK(w, nil)
}
