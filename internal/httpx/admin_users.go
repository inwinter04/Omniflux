package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
)

// 可分配的角色:org_owner 只在 bootstrap 产生,不允许通过用户管理授予。
var assignableRoles = map[string]bool{
	"org_admin": true, "project_admin": true, "developer": true, "viewer": true,
}

type userResponse struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	Role       string  `json:"role"`
	Status     string  `json:"status"`
	LastUsedAt *string `json:"last_login_at"`
	CreatedAt  string  `json:"created_at"`
}

type createUserRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	Role           string `json:"role"`
	OrganizationID string `json:"organization_id"`
}

type updateUserRequest struct {
	Role           *string `json:"role"`
	Status         *string `json:"status"`
	Password       *string `json:"password"`
	OrganizationID *string `json:"organization_id"`
}

func timeStringPtr(src any) (*string, error) {
	switch v := src.(type) {
	case nil:
		return nil, nil
	case time.Time:
		s := v.Format(time.RFC3339)
		return &s, nil
	default:
		return nil, fmt.Errorf("unexpected time type %T", src)
	}
}

// Users 返回当前组织用户列表;需要组织所有者权限。
func (d ServerDeps) Users(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireOwner(w, principal) != nil {
		return
	}
	// platform_admin 可通过 organization_id 参数查看任意组织的用户;默认只看自己组织。
	listOrgID := principal.OrganizationID
	if isPlatformAdmin(principal) {
		if orgParam := r.URL.Query().Get("organization_id"); orgParam != "" {
			parsed, err := uuid.Parse(orgParam)
			if err != nil {
				adminError(w, http.StatusBadRequest, "USER_ORG_INVALID", "invalid-organization-id-t4rbtt", nil)
				return
			}
			listOrgID = parsed
		}
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, email, role, status, last_login_at, created_at
		FROM users WHERE organization_id = $1 ORDER BY created_at ASC
	`, listOrgID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_LIST_FAILED", "failed-to-query-users", nil)
		return
	}
	defer rows.Close()
	list := []userResponse{}
	for rows.Next() {
		var item userResponse
		var id uuid.UUID
		var lastLoginSrc, createdSrc any
		if err := rows.Scan(&id, &item.Email, &item.Role, &item.Status, &lastLoginSrc, &createdSrc); err != nil {
			adminError(w, http.StatusInternalServerError, "USER_LIST_FAILED", "failed-to-query-users", nil)
			return
		}
		item.ID = id.String()
		lastLogin, err := timeStringPtr(lastLoginSrc)
		if err != nil {
			adminError(w, http.StatusInternalServerError, "USER_LIST_FAILED", "failed-to-query-users", nil)
			return
		}
		created, err := timeStringPtr(createdSrc)
		if err != nil {
			adminError(w, http.StatusInternalServerError, "USER_LIST_FAILED", "failed-to-query-users", nil)
			return
		}
		item.LastUsedAt = lastLogin
		item.CreatedAt = *created
		list = append(list, item)
	}
	adminOK(w, list)
}

// CreateUser 在当前组织创建管理员/成员账号;需要组织所有者权限。
func (d ServerDeps) CreateUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireOwner(w, principal) != nil {
		return
	}
	var body createUserRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if email == "" || !strings.Contains(email, "@") {
		adminError(w, http.StatusBadRequest, "USER_EMAIL_INVALID", "invalid-email-format", nil)
		return
	}
	if len(body.Password) < 16 {
		adminError(w, http.StatusBadRequest, "USER_PASSWORD_WEAK", "password-must-be-at-least-16", nil)
		return
	}
	body.Role = strings.TrimSpace(body.Role)
	if !assignableRoles[body.Role] {
		adminError(w, http.StatusBadRequest, "USER_ROLE_INVALID", "roles-support-only-org-admin-project", nil)
		return
	}
	// platform_admin 可把新用户直接归属到任意组织;其余角色只能在当前组织创建。
	targetOrgID := principal.OrganizationID
	if body.OrganizationID != "" {
		if !isPlatformAdmin(principal) {
			adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "only-platform-admins-can-assign-a", nil)
			return
		}
		parsed, err := uuid.Parse(body.OrganizationID)
		if err != nil {
			adminError(w, http.StatusBadRequest, "USER_ORG_INVALID", "invalid-organization-id-t4rbtt", nil)
			return
		}
		var orgCount int
		if err := d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM organizations WHERE id = $1`, parsed).Scan(&orgCount); err != nil {
			adminError(w, http.StatusInternalServerError, "USER_CREATE_FAILED", "failed-to-create-the-user", nil)
			return
		}
		if orgCount == 0 {
			adminError(w, http.StatusBadRequest, "USER_ORG_NOT_FOUND", "target-organization-does-not-exist", nil)
			return
		}
		targetOrgID = parsed
	}
	var count int
	if err := d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE email = $1`, email).Scan(&count); err != nil {
		adminError(w, http.StatusInternalServerError, "USER_CREATE_FAILED", "failed-to-create-the-user", nil)
		return
	}
	if count > 0 {
		adminError(w, http.StatusConflict, "USER_EMAIL_EXISTS", "this-email-is-already-registered", nil)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_CREATE_FAILED", "failed-to-create-the-user", nil)
		return
	}
	userID := uuid.New()
	err = d.execWithAudit(r.Context(), `
		INSERT INTO users(id, organization_id, email, password_hash, role)
		VALUES ($1,$2,$3,$4,$5)
	`, []any{userID, targetOrgID, email, hash, body.Role},
		targetOrgID, principal.UserID, "user.created", "user", userID.String(),
		nil, map[string]any{"email": email, "role": body.Role})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_CREATE_FAILED", "failed-to-create-the-user", nil)
		return
	}
	adminOK(w, map[string]string{"id": userID.String()})
}

// UpdateUser 修改角色/状态/重置密码;org_owner 账号不可修改,也不能禁用自己或改自己的角色。
func (d ServerDeps) UpdateUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireOwner(w, principal) != nil {
		return
	}
	var body updateUserRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "USER_ID_INVALID", "invalid-user-id", nil)
		return
	}
	// platform_admin 可把用户移动到任意组织;其余角色不允许调整归属。
	var targetOrgID *uuid.UUID
	if body.OrganizationID != nil {
		if !isPlatformAdmin(principal) {
			adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "only-platform-admins-can-change-a", nil)
			return
		}
		parsed, err := uuid.Parse(*body.OrganizationID)
		if err != nil {
			adminError(w, http.StatusBadRequest, "USER_ORG_INVALID", "invalid-organization-id-t4rbtt", nil)
			return
		}
		var orgCount int
		if err := d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM organizations WHERE id = $1`, parsed).Scan(&orgCount); err != nil {
			adminError(w, http.StatusInternalServerError, "USER_UPDATE_FAILED", "failed-to-update-the-user", nil)
			return
		}
		if orgCount == 0 {
			adminError(w, http.StatusBadRequest, "USER_ORG_NOT_FOUND", "target-organization-does-not-exist", nil)
			return
		}
		targetOrgID = &parsed
	}
	if userID == principal.UserID && body.Status != nil && *body.Status != "active" {
		adminError(w, http.StatusBadRequest, "USER_SELF_DISABLE", "you-cannot-disable-your-own-account", nil)
		return
	}
	if userID == principal.UserID && body.Role != nil {
		adminError(w, http.StatusBadRequest, "USER_SELF_ROLE", "you-cannot-change-your-own-role", nil)
		return
	}
	var current struct {
		Role   string
		Status string
		OrgID  uuid.UUID
	}
	if isPlatformAdmin(principal) {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT role, status, organization_id FROM users WHERE id = $1
		`, userID).Scan(&current.Role, &current.Status, &current.OrgID)
	} else {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT role, status, organization_id FROM users WHERE id = $1 AND organization_id = $2
		`, userID, principal.OrganizationID).Scan(&current.Role, &current.Status, &current.OrgID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "USER_NOT_FOUND", "user-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_UPDATE_FAILED", "failed-to-update-the-user", nil)
		return
	}
	if current.Role == "org_owner" && !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "USER_OWNER_PROTECTED", "the-organization-owner-account-cannot-be", nil)
		return
	}
	if body.Role != nil && !assignableRoles[*body.Role] {
		adminError(w, http.StatusBadRequest, "USER_ROLE_INVALID", "roles-support-only-org-admin-project", nil)
		return
	}
	if body.Status != nil && *body.Status != "active" && *body.Status != "disabled" {
		adminError(w, http.StatusBadRequest, "USER_STATUS_INVALID", "status-supports-only-active-or-disabled", nil)
		return
	}
	if body.Password != nil && len(*body.Password) < 16 {
		adminError(w, http.StatusBadRequest, "USER_PASSWORD_WEAK", "password-must-be-at-least-16", nil)
		return
	}
	var hash any
	if body.Password != nil {
		newHash, hashErr := auth.HashPassword(*body.Password)
		if hashErr != nil {
			adminError(w, http.StatusInternalServerError, "USER_UPDATE_FAILED", "failed-to-update-the-user", nil)
			return
		}
		hash = newHash
	}
	// 禁用即吊销全部会话,保证立即失效;移动组织不吊销会话,principal 每次请求重建,下一请求即生效。
	// 审计挂在用户归属组织(移动后)下,便于目标组织追溯成员变更。
	auditOrgID := current.OrgID
	if targetOrgID != nil {
		auditOrgID = *targetOrgID
	}
	// 审计快照记录生效后的值:未传的字段沿用原值,而不是请求里的空值。
	newRole, newStatus := current.Role, current.Status
	if body.Role != nil {
		newRole = *body.Role
	}
	if body.Status != nil {
		newStatus = *body.Status
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var execErr error
		if isPlatformAdmin(principal) {
			_, execErr = tx.Exec(r.Context(), `
				UPDATE users SET
					role = COALESCE($2, role),
					status = COALESCE($3, status),
					password_hash = COALESCE($4, password_hash),
					organization_id = COALESCE($5, organization_id),
					failed_login_count = 0, locked_until = NULL, updated_at = now()
				WHERE id = $1
			`, userID, body.Role, body.Status, hash, targetOrgID)
		} else {
			_, execErr = tx.Exec(r.Context(), `
				UPDATE users SET
					role = COALESCE($3, role),
					status = COALESCE($4, status),
					password_hash = COALESCE($5, password_hash),
					failed_login_count = 0, locked_until = NULL, updated_at = now()
				WHERE id = $1 AND organization_id = $2
			`, userID, principal.OrganizationID, body.Role, body.Status, hash)
		}
		if execErr != nil {
			return execErr
		}
		if body.Status != nil && *body.Status == "disabled" {
			if _, err := tx.Exec(r.Context(), `
				UPDATE sessions SET revoked_at = now()
				WHERE user_id = $1 AND revoked_at IS NULL
			`, userID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
			VALUES ($1,$2,'user','user.updated','user',$3,$4,$5)
		`, auditOrgID, principal.UserID, userID.String(),
			auditJSON(map[string]any{"role": current.Role, "status": current.Status, "password_reset": false}),
			auditJSON(map[string]any{"role": newRole, "status": newStatus, "password_reset": body.Password != nil}))
		return err
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_UPDATE_FAILED", "failed-to-update-the-user", nil)
		return
	}
	adminOK(w, nil)
}
