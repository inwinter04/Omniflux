package httpx

import (
	"encoding/json"
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
	// Expired 表示启用中但已过有效期;数据面会拒绝这类 Key,管理台不能继续显示为“有效”。
	Expired    bool       `json:"expired"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
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
		adminError(w, http.StatusInternalServerError, "PROJECT_LIST_FAILED", "failed-to-query-projects", nil)
		return
	}
	defer rows.Close()
	list := []projectResponse{}
	for rows.Next() {
		var item projectResponse
		var id, deptID uuid.UUID
		if err := rows.Scan(&id, &item.Name, &deptID, &item.Status); err != nil {
			adminError(w, http.StatusInternalServerError, "PROJECT_LIST_FAILED", "failed-to-query-projects", nil)
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
		adminError(w, http.StatusInternalServerError, "APPLICATION_LIST_FAILED", "failed-to-query-applications", nil)
		return
	}
	defer rows.Close()
	list := []applicationResponse{}
	for rows.Next() {
		var item applicationResponse
		var id, projectID uuid.UUID
		if err := rows.Scan(&id, &projectID, &item.Name, &item.Status); err != nil {
			adminError(w, http.StatusInternalServerError, "APPLICATION_LIST_FAILED", "failed-to-query-applications", nil)
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
		adminError(w, http.StatusInternalServerError, "APIKEY_LIST_FAILED", "failed-to-query-api-keys", nil)
		return
	}
	defer rows.Close()
	list := []apiKeyResponse{}
	for rows.Next() {
		var item apiKeyResponse
		var id, projectID, applicationID uuid.UUID
		if err := rows.Scan(&id, &item.Name, &item.KeyPrefix, &projectID, &applicationID,
			&item.RateLimitPerMin, &item.Status, &item.ExpiresAt, &item.LastUsedAt, &item.CreatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "APIKEY_LIST_FAILED", "failed-to-query-api-keys", nil)
			return
		}
		item.ID = id.String()
		item.ProjectID = projectID.String()
		item.ApplicationID = applicationID.String()
		item.Expired = item.Status == "active" && item.ExpiresAt != nil && !item.ExpiresAt.After(time.Now())
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
		adminError(w, http.StatusBadRequest, "APIKEY_NAME_REQUIRED", "enter-an-api-key-name", nil)
		return
	}
	projectID, err := uuid.Parse(body.ProjectID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "APIKEY_PROJECT_INVALID", "invalid-project-id-bcvapn", nil)
		return
	}
	applicationID, err := uuid.Parse(body.ApplicationID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "APIKEY_APPLICATION_INVALID", "invalid-application-id-7cby78", nil)
		return
	}
	// 项目必须属于当前组织,应用必须挂在同一项目下。
	var count int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM projects WHERE id = $1 AND organization_id = $2
	`, projectID, principal.OrganizationID).Scan(&count); err != nil || count == 0 {
		adminError(w, http.StatusBadRequest, "APIKEY_PROJECT_NOT_FOUND", "project-does-not-exist-or-does", nil)
		return
	}
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM applications WHERE id = $1 AND project_id = $2
	`, applicationID, projectID).Scan(&count); err != nil || count == 0 {
		adminError(w, http.StatusBadRequest, "APIKEY_APPLICATION_MISMATCH", "application-does-not-exist-or-does-g14dqm", nil)
		return
	}
	if body.ExpiresAt != nil && !body.ExpiresAt.After(time.Now()) {
		adminError(w, http.StatusBadRequest, "APIKEY_EXPIRES_INVALID", "expiration-time-must-be-later-than", nil)
		if body.RateLimitPerMin != nil && *body.RateLimitPerMin <= 0 {
			adminError(w, http.StatusBadRequest, "APIKEY_RATE_LIMIT_INVALID", "rate-limit-per-min-must-be", nil)
			return
		}
		return
	}
	generated, err := apikeys.Generate()
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_CREATE_FAILED", "failed-to-generate-the-api-key", nil)
		return
	}
	secretHash := apikeys.Hash(d.APIKeyPepper, generated.Full)
	keyID := uuid.New()
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO api_keys(id, organization_id, project_id, application_id, created_by, name, key_prefix, secret_hash, expires_at, rate_limit_per_min)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`, keyID, principal.OrganizationID, projectID, applicationID, principal.UserID,
			body.Name, generated.Prefix, secretHash, body.ExpiresAt, body.RateLimitPerMin); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"api_key.created", "api_key", keyID.String(), nil, map[string]any{
				"name": body.Name, "project_id": projectID.String(), "application_id": applicationID.String(),
				"expires_at": body.ExpiresAt, "rate_limit_per_min": body.RateLimitPerMin,
			})
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_CREATE_FAILED", "failed-to-create-the-api-key", nil)
		return
	}
	adminOK(w, map[string]string{
		"id":  keyID.String(),
		"key": generated.Full,
	})
}

type updateAPIKeyRequest struct {
	Status *string `json:"status"`

	// RateLimitPerMin 用 RawMessage 接收,才能区分“未传(保持不变)”与“传 null(清除限流)”。
	RateLimitPerMin json.RawMessage `json:"rate_limit_per_min"`
}

// UpdateAPIKeyStatus 更新 API Key 的状态与限流阈值;禁用立即停止认证通过。
// UpdateAPIKeyStatus 更新 API Key 的状态与 Key 级限流。
// 两个字段都是“传了才改”:未传保持原值,rate_limit_per_min 传 null 表示清除限流。
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
		adminError(w, http.StatusBadRequest, "APIKEY_STATUS_INVALID", "status-supports-only-active-or-disabled", nil)
		return
	}
	rateLimitProvided := len(body.RateLimitPerMin) > 0
	var requestedRateLimit *int
	if rateLimitProvided && string(body.RateLimitPerMin) != "null" {
		var parsed int
		if err := json.Unmarshal(body.RateLimitPerMin, &parsed); err != nil || parsed <= 0 {
			adminError(w, http.StatusBadRequest, "APIKEY_RATE_LIMIT_INVALID", "rate-limit-per-min-must-be", nil)
			return
		}
		requestedRateLimit = &parsed
	}
	if body.Status == nil && !rateLimitProvided {
		adminError(w, http.StatusBadRequest, "APIKEY_NOTHING_TO_UPDATE", "provide-at-least-status-or-rate", nil)
		return
	}
	keyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "APIKEY_INVALID_ID", "invalid-api-key-id", nil)
		return
	}
	var currentStatus string
	var currentRateLimit *int
	err = d.Pool.QueryRow(r.Context(), `
		SELECT status, rate_limit_per_min FROM api_keys WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL
	`, keyID, principal.OrganizationID).Scan(&currentStatus, &currentRateLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "APIKEY_NOT_FOUND", "api-key-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_STATUS_FAILED", "failed-to-update-the-api-key", nil)
		return
	}
	// 生效后的值:未传的字段沿用原值;审计快照记录生效值,而不是请求里的空值。
	newStatus := currentStatus
	if body.Status != nil {
		newStatus = *body.Status
	}
	newRateLimit := currentRateLimit
	if rateLimitProvided {
		newRateLimit = requestedRateLimit
	}
	if newStatus == currentStatus && optionalIntEqual(newRateLimit, currentRateLimit) {
		adminOK(w, map[string]string{"status": newStatus})
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `
			UPDATE api_keys SET status = $3, rate_limit_per_min = $4, updated_at = now()
			WHERE id = $1 AND organization_id = $2
		`, keyID, principal.OrganizationID, newStatus, nullableIntPtr(newRateLimit))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"api_key.updated", "api_key", keyID.String(),
			map[string]any{"status": currentStatus, "rate_limit_per_min": currentRateLimit},
			map[string]any{"status": newStatus, "rate_limit_per_min": newRateLimit})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "APIKEY_NOT_FOUND", "api-key-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_STATUS_FAILED", "failed-to-update-the-api-key", nil)
		return
	}
	adminOK(w, map[string]string{"status": newStatus})
}

// DeleteAPIKey 软删除 API Key,立即停止认证通过且不再出现在列表中。
func (d ServerDeps) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	keyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "APIKEY_INVALID_ID", "invalid-api-key-id", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		// 软删除后名称仍可查到,但保持与其它删除一致:审计里记下被删 Key 的名称。
		var name string
		if err := tx.QueryRow(r.Context(), `
			UPDATE api_keys SET deleted_at = now(), updated_at = now()
			WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL
			RETURNING name
		`, keyID, principal.OrganizationID).Scan(&name); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"api_key.deleted", "api_key", keyID.String(), map[string]any{"name": name}, nil)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "APIKEY_NOT_FOUND", "api-key-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APIKEY_DELETE_FAILED", "failed-to-delete-the-api-key", nil)
		return
	}
	adminOK(w, nil)
}
