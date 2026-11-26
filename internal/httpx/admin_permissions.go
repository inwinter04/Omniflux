package httpx

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/inwinter04/omniflux/internal/auth"
)

type permissionGroupResponse struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Permissions    []string  `json:"permissions"`
	MemberCount    int64     `json:"member_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type createPermissionGroupRequest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Permissions    []string `json:"permissions"`
	OrganizationID string   `json:"organization_id"`
}

type updatePermissionGroupRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type userPermissionsResponse struct {
	UserID               string   `json:"user_id"`
	Email                string   `json:"email"`
	Role                 string   `json:"role"`
	PermissionGroupID    *string  `json:"permission_group_id"`
	PermissionGroupName  *string  `json:"permission_group_name"`
	BasePermissions      []string `json:"base_permissions"`
	Allow                []string `json:"allow"`
	Deny                 []string `json:"deny"`
	EffectivePermissions []string `json:"effective_permissions"`
}

type updateUserPermissionsRequest struct {
	Role              *string  `json:"role"`
	PermissionGroupID *string  `json:"permission_group_id"`
	Allow             []string `json:"allow"`
	Deny              []string `json:"deny"`
}

// PermissionsCatalog 返回平台权限节点目录与内置系统预设。
func (d ServerDeps) PermissionsCatalog(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAnyPermission(w, principal,
		auth.PermUserRead, auth.PermUserManage,
		auth.PermPermissionGroupRead, auth.PermPermissionGroupManage,
	) != nil {
		return
	}
	adminOK(w, map[string]any{
		"modules": auth.PermissionCatalog(),
		"presets": auth.SystemPresets(),
	})
}

// PermissionGroups 返回当前组织的自定义权限组预设列表。
func (d ServerDeps) PermissionGroups(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAnyPermission(w, principal,
		auth.PermPermissionGroupRead, auth.PermUserRead, auth.PermUserManage,
	) != nil {
		return
	}
	listOrgID := principal.OrganizationID
	if isPlatformAdmin(principal) {
		if orgParam := strings.TrimSpace(r.URL.Query().Get("organization_id")); orgParam != "" {
			parsed, err := uuid.Parse(orgParam)
			if err != nil {
				adminError(w, http.StatusBadRequest, "USER_ORG_INVALID", "invalid-organization-id-t4rbtt", nil)
				return
			}
			listOrgID = parsed
		}
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT pg.id, pg.organization_id, pg.name, pg.description, pg.permissions, pg.created_at, pg.updated_at,
		       (SELECT count(*) FROM users u WHERE u.permission_group_id = pg.id) AS member_count
		FROM permission_groups pg
		WHERE pg.organization_id = $1
		ORDER BY pg.created_at ASC
	`, listOrgID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PERMISSION_GROUP_LIST_FAILED", "failed-to-query-permission-groups", nil)
		return
	}
	defer rows.Close()
	list := []permissionGroupResponse{}
	for rows.Next() {
		var item permissionGroupResponse
		var id, orgID uuid.UUID
		if err := rows.Scan(&id, &orgID, &item.Name, &item.Description, &item.Permissions, &item.CreatedAt, &item.UpdatedAt, &item.MemberCount); err != nil {
			adminError(w, http.StatusInternalServerError, "PERMISSION_GROUP_LIST_FAILED", "failed-to-query-permission-groups", nil)
			return
		}
		item.ID = id.String()
		item.OrganizationID = orgID.String()
		item.Permissions = auth.NormalizePermissions(item.Permissions)
		list = append(list, item)
	}
	adminOK(w, list)
}

// CreatePermissionGroup 创建组织级自定义权限组预设。
func (d ServerDeps) CreatePermissionGroup(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermPermissionGroupManage) != nil {
		return
	}
	var body createPermissionGroupRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		adminError(w, http.StatusBadRequest, "PERMISSION_GROUP_NAME_REQUIRED", "enter-a-permission-group-name", nil)
		return
	}
	description := strings.TrimSpace(body.Description)
	normalized, okPerms := validatePermissionList(w, body.Permissions)
	if !okPerms {
		return
	}
	if !ensureWithinPrincipalPermissions(w, principal, normalized) {
		return
	}
	targetOrgID, orgOK := d.inviteTargetOrganization(w, r, principal, strings.TrimSpace(body.OrganizationID))
	if !orgOK {
		return
	}
	groupID := uuid.New()
	err := d.execWithAudit(r.Context(), `
		INSERT INTO permission_groups(id, organization_id, name, description, permissions, created_by)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, []any{groupID, targetOrgID, name, description, normalized, principal.UserID},
		targetOrgID, principal.UserID, "permission_group.created", "permission_group", groupID.String(),
		nil, map[string]any{"name": name, "description": description, "permissions": normalized})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "permission_groups_organization_id_name_key" {
			adminError(w, http.StatusConflict, "PERMISSION_GROUP_NAME_EXISTS", "a-permission-group-with-the-same-name", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PERMISSION_GROUP_CREATE_FAILED", "failed-to-create-permission-group", nil)
		return
	}
	adminOK(w, map[string]string{"id": groupID.String()})
}

// UpdatePermissionGroup 修改自定义权限组名称、说明与权限节点。
func (d ServerDeps) UpdatePermissionGroup(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermPermissionGroupManage) != nil {
		return
	}
	groupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PERMISSION_GROUP_ID_INVALID", "invalid-permission-group-id", nil)
		return
	}
	var body updatePermissionGroupRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		adminError(w, http.StatusBadRequest, "PERMISSION_GROUP_NAME_REQUIRED", "enter-a-permission-group-name", nil)
		return
	}
	description := strings.TrimSpace(body.Description)
	normalized, okPerms := validatePermissionList(w, body.Permissions)
	if !okPerms {
		return
	}
	if !ensureWithinPrincipalPermissions(w, principal, normalized) {
		return
	}
	var currentOrgID uuid.UUID
	var oldName, oldDesc string
	var oldPerms []string
	if isPlatformAdmin(principal) {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT organization_id, name, description, permissions FROM permission_groups WHERE id = $1
		`, groupID).Scan(&currentOrgID, &oldName, &oldDesc, &oldPerms)
	} else {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT organization_id, name, description, permissions FROM permission_groups WHERE id = $1 AND organization_id = $2
		`, groupID, principal.OrganizationID).Scan(&currentOrgID, &oldName, &oldDesc, &oldPerms)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PERMISSION_GROUP_NOT_FOUND", "permission-group-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PERMISSION_GROUP_UPDATE_FAILED", "failed-to-update-permission-group", nil)
		return
	}
	if !ensureWithinPrincipalPermissions(w, principal, oldPerms) {
		return
	}
	err = d.execWithAudit(r.Context(), `
		UPDATE permission_groups
		SET name = $3, description = $4, permissions = $5, updated_at = now()
		WHERE id = $1 AND organization_id = $2
	`, []any{groupID, currentOrgID, name, description, normalized},
		currentOrgID, principal.UserID, "permission_group.updated", "permission_group", groupID.String(),
		map[string]any{"name": oldName, "description": oldDesc, "permissions": auth.NormalizePermissions(oldPerms)},
		map[string]any{"name": name, "description": description, "permissions": normalized})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "permission_groups_organization_id_name_key" {
			adminError(w, http.StatusConflict, "PERMISSION_GROUP_NAME_EXISTS", "a-permission-group-with-the-same-name", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PERMISSION_GROUP_UPDATE_FAILED", "failed-to-update-permission-group", nil)
		return
	}
	adminOK(w, nil)
}

// DeletePermissionGroup 删除未被成员使用的自定义权限组预设。
func (d ServerDeps) DeletePermissionGroup(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermPermissionGroupManage) != nil {
		return
	}
	groupID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PERMISSION_GROUP_ID_INVALID", "invalid-permission-group-id", nil)
		return
	}
	var currentOrgID uuid.UUID
	var oldName string
	var oldPerms []string
	if isPlatformAdmin(principal) {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT organization_id, name, permissions FROM permission_groups WHERE id = $1
		`, groupID).Scan(&currentOrgID, &oldName, &oldPerms)
	} else {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT organization_id, name, permissions FROM permission_groups WHERE id = $1 AND organization_id = $2
		`, groupID, principal.OrganizationID).Scan(&currentOrgID, &oldName, &oldPerms)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PERMISSION_GROUP_NOT_FOUND", "permission-group-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PERMISSION_GROUP_DELETE_FAILED", "failed-to-delete-permission-group", nil)
		return
	}
	if !ensureWithinPrincipalPermissions(w, principal, oldPerms) {
		return
	}
	var memberCount int
	if err := d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE permission_group_id = $1`, groupID).Scan(&memberCount); err != nil {
		adminError(w, http.StatusInternalServerError, "PERMISSION_GROUP_DELETE_FAILED", "failed-to-delete-permission-group", nil)
		return
	}
	if memberCount > 0 {
		adminError(w, http.StatusConflict, "PERMISSION_GROUP_IN_USE", "permission-group-is-in-use", nil)
		return
	}
	err = d.execWithAudit(r.Context(), `
		DELETE FROM permission_groups WHERE id = $1 AND organization_id = $2
	`, []any{groupID, currentOrgID},
		currentOrgID, principal.UserID, "permission_group.deleted", "permission_group", groupID.String(),
		map[string]any{"name": oldName, "permissions": auth.NormalizePermissions(oldPerms)}, nil)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PERMISSION_GROUP_NOT_FOUND", "permission-group-does-not-exist", nil)
		return
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			adminError(w, http.StatusConflict, "PERMISSION_GROUP_IN_USE", "permission-group-is-in-use", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PERMISSION_GROUP_DELETE_FAILED", "failed-to-delete-permission-group", nil)
		return
	}
	adminOK(w, nil)
}

// UserPermissions 查询成员当前身份/权限组及个人节点覆盖详情。
func (d ServerDeps) UserPermissions(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermUserRead) != nil {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "USER_ID_INVALID", "invalid-user-id", nil)
		return
	}
	var email, role string
	var groupID uuid.NullUUID
	var groupName string
	var groupPerms []string
	if isPlatformAdmin(principal) {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT u.email, u.role, u.permission_group_id, COALESCE(pg.name, ''), COALESCE(pg.permissions, '{}'::text[])
			FROM users u
			LEFT JOIN permission_groups pg ON pg.id = u.permission_group_id
			WHERE u.id = $1
		`, userID).Scan(&email, &role, &groupID, &groupName, &groupPerms)
	} else {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT u.email, u.role, u.permission_group_id, COALESCE(pg.name, ''), COALESCE(pg.permissions, '{}'::text[])
			FROM users u
			LEFT JOIN permission_groups pg ON pg.id = u.permission_group_id
			WHERE u.id = $1 AND u.organization_id = $2
		`, userID, principal.OrganizationID).Scan(&email, &role, &groupID, &groupName, &groupPerms)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "USER_NOT_FOUND", "user-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_PERMISSION_READ_FAILED", "failed-to-query-user-permissions", nil)
		return
	}
	allow, deny, err := loadUserOverrides(r.Context(), d.Pool, userID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_PERMISSION_READ_FAILED", "failed-to-query-user-permissions", nil)
		return
	}
	base := auth.PresetPermissions(role)
	if groupID.Valid {
		base = auth.NormalizePermissions(groupPerms)
	}
	effectiveMap := auth.ResolveEffectivePermissions(role, groupPerms, groupID.Valid, allow, deny)
	res := userPermissionsResponse{
		UserID:               userID.String(),
		Email:                email,
		Role:                 role,
		BasePermissions:      base,
		Allow:                auth.NormalizePermissions(allow),
		Deny:                 auth.NormalizePermissions(deny),
		EffectivePermissions: permissionMapToSlice(effectiveMap),
	}
	if groupID.Valid {
		gid := groupID.UUID.String()
		res.PermissionGroupID = &gid
		res.PermissionGroupName = &groupName
	}
	adminOK(w, res)
}

// UpdateUserPermissions 更新成员身份/自定义权限组与个人节点 Allow/Deny 覆盖。
func (d ServerDeps) UpdateUserPermissions(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermUserManage) != nil {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "USER_ID_INVALID", "invalid-user-id", nil)
		return
	}
	if userID == principal.UserID {
		adminError(w, http.StatusBadRequest, "USER_SELF_PERMISSION_EDIT", "user-self-permission-edit", nil)
		return
	}
	var body updateUserPermissionsRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	allowList, allowOK := validatePermissionList(w, body.Allow)
	if !allowOK {
		return
	}
	denyList, denyOK := validatePermissionList(w, body.Deny)
	if !denyOK {
		return
	}
	denySet := make(map[string]bool, len(denyList))
	for _, code := range denyList {
		denySet[code] = true
	}
	for _, code := range allowList {
		if denySet[code] {
			adminError(w, http.StatusBadRequest, "PERMISSION_CONFLICT", "permission-allow-deny-conflict", nil)
			return
		}
	}

	var parsedGroupID *uuid.UUID
	if body.PermissionGroupID != nil && strings.TrimSpace(*body.PermissionGroupID) != "" {
		gid, parseErr := uuid.Parse(strings.TrimSpace(*body.PermissionGroupID))
		if parseErr != nil {
			adminError(w, http.StatusBadRequest, "PERMISSION_GROUP_ID_INVALID", "invalid-permission-group-id", nil)
			return
		}
		parsedGroupID = &gid
	}
	if parsedGroupID == nil && body.Role != nil {
		role := strings.TrimSpace(*body.Role)
		if !assignableRoles[role] {
			adminError(w, http.StatusBadRequest, "USER_ROLE_INVALID", "roles-support-only-org-admin-project", nil)
			return
		}
		body.Role = &role
	}

	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var curRole string
		var curOrgID uuid.UUID
		var curGroupID uuid.NullUUID
		var curGroupPerms []string
		var scanErr error
		if isPlatformAdmin(principal) {
			scanErr = tx.QueryRow(r.Context(), `
				SELECT u.role, u.organization_id, u.permission_group_id, COALESCE(pg.permissions, '{}'::text[])
				FROM users u
				LEFT JOIN permission_groups pg ON pg.id = u.permission_group_id
				WHERE u.id = $1 FOR UPDATE OF u
			`, userID).Scan(&curRole, &curOrgID, &curGroupID, &curGroupPerms)
		} else {
			scanErr = tx.QueryRow(r.Context(), `
				SELECT u.role, u.organization_id, u.permission_group_id, COALESCE(pg.permissions, '{}'::text[])
				FROM users u
				LEFT JOIN permission_groups pg ON pg.id = u.permission_group_id
				WHERE u.id = $1 AND u.organization_id = $2 FOR UPDATE OF u
			`, userID, principal.OrganizationID).Scan(&curRole, &curOrgID, &curGroupID, &curGroupPerms)
		}
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return pgx.ErrNoRows
		}
		if scanErr != nil {
			return scanErr
		}
		if curRole == "org_owner" || curRole == "platform_admin" {
			return errOwnerProtected
		}

		curAllow, curDeny, loadErr := loadUserOverrides(r.Context(), tx, userID)
		if loadErr != nil {
			return loadErr
		}
		curEffective := permissionMapToSlice(auth.ResolveEffectivePermissions(curRole, curGroupPerms, curGroupID.Valid, curAllow, curDeny))
		if !withinPrincipalPermissions(principal, curEffective) {
			return errPermissionEscalation
		}

		newRole := curRole
		var newGroupID any
		var basePerms []string
		hasGroup := false
		if parsedGroupID != nil {
			var groupPerms []string
			groupErr := tx.QueryRow(r.Context(), `
				SELECT permissions FROM permission_groups WHERE id = $1 AND organization_id = $2
			`, *parsedGroupID, curOrgID).Scan(&groupPerms)
			if errors.Is(groupErr, pgx.ErrNoRows) {
				return errPermissionGroupNotFound
			}
			if groupErr != nil {
				return groupErr
			}
			newRole = "viewer"
			newGroupID = *parsedGroupID
			basePerms = auth.NormalizePermissions(groupPerms)
			hasGroup = true
		} else {
			if body.Role != nil {
				newRole = *body.Role
			}
			basePerms = auth.PresetPermissions(newRole)
		}

		baseSet := make(map[string]bool, len(basePerms))
		for _, code := range basePerms {
			baseSet[code] = true
		}
		cleanAllow := make([]string, 0, len(allowList))
		for _, code := range allowList {
			if !baseSet[code] {
				cleanAllow = append(cleanAllow, code)
			}
		}
		cleanDeny := make([]string, 0, len(denyList))
		for _, code := range denyList {
			if baseSet[code] {
				cleanDeny = append(cleanDeny, code)
			}
		}

		nextEffectiveMap := auth.ResolveEffectivePermissions(newRole, basePerms, hasGroup, cleanAllow, cleanDeny)
		nextEffective := permissionMapToSlice(nextEffectiveMap)
		if !withinPrincipalPermissions(principal, nextEffective) {
			return errPermissionEscalation
		}

		if _, err := tx.Exec(r.Context(), `
			UPDATE users SET role = $2, permission_group_id = $3, updated_at = now()
			WHERE id = $1
		`, userID, newRole, newGroupID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM user_permission_overrides WHERE user_id = $1`, userID); err != nil {
			return err
		}
		for _, code := range cleanAllow {
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO user_permission_overrides(user_id, permission, effect) VALUES ($1,$2,'allow')
			`, userID, code); err != nil {
				return err
			}
		}
		for _, code := range cleanDeny {
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO user_permission_overrides(user_id, permission, effect) VALUES ($1,$2,'deny')
			`, userID, code); err != nil {
				return err
			}
		}

		return writeAuditEvent(r.Context(), tx, curOrgID, principal.UserID, "user.permissions_updated", "user", userID.String(),
			map[string]any{"role": curRole, "permission_group_id": nullUUIDString(curGroupID), "allow": curAllow, "deny": curDeny},
			map[string]any{"role": newRole, "permission_group_id": uuidPtrString(parsedGroupID), "allow": cleanAllow, "deny": cleanDeny})
	})
	if errors.Is(err, errOwnerProtected) {
		adminError(w, http.StatusForbidden, "USER_OWNER_PROTECTED", "the-organization-owner-account-cannot-be", nil)
		return
	}
	if errors.Is(err, errPermissionEscalation) {
		adminError(w, http.StatusForbidden, "PERMISSION_ESCALATION_DENIED", "permission-escalation-denied", nil)
		return
	}
	if errors.Is(err, errPermissionGroupNotFound) {
		adminError(w, http.StatusBadRequest, "PERMISSION_GROUP_NOT_FOUND", "permission-group-does-not-exist", nil)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "USER_NOT_FOUND", "user-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "USER_PERMISSION_UPDATE_FAILED", "failed-to-update-user-permissions", nil)
		return
	}
	adminOK(w, nil)
}

var (
	errPermissionEscalation    = errors.New("permission escalation denied")
	errPermissionGroupNotFound = errors.New("permission group not found")
)

func validatePermissionList(w http.ResponseWriter, perms []string) ([]string, bool) {
	for _, code := range perms {
		if !auth.IsValidPermission(code) {
			adminError(w, http.StatusBadRequest, "PERMISSION_INVALID", "invalid-permission-code", map[string]any{"permission": code})
			return nil, false
		}
	}
	return auth.NormalizePermissions(perms), true
}

func withinPrincipalPermissions(principal auth.Principal, perms []string) bool {
	if isPlatformAdmin(principal) || principal.Role == "org_owner" {
		return true
	}
	for _, code := range perms {
		if !principal.HasPermission(code) {
			return false
		}
	}
	return true
}

func ensureWithinPrincipalPermissions(w http.ResponseWriter, principal auth.Principal, perms []string) bool {
	if withinPrincipalPermissions(principal, perms) {
		return true
	}
	adminError(w, http.StatusForbidden, "PERMISSION_ESCALATION_DENIED", "permission-escalation-denied", nil)
	return false
}

func loadUserOverrides(ctx context.Context, q rowQuerier, userID uuid.UUID) ([]string, []string, error) {
	rows, err := q.Query(ctx, `SELECT permission, effect FROM user_permission_overrides WHERE user_id = $1`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var allow, deny []string
	for rows.Next() {
		var perm, effect string
		if err := rows.Scan(&perm, &effect); err != nil {
			return nil, nil, err
		}
		switch effect {
		case "allow":
			allow = append(allow, perm)
		case "deny":
			deny = append(deny, perm)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return allow, deny, nil
}

func permissionMapToSlice(perms map[string]bool) []string {
	out := make([]string, 0, len(perms))
	for _, code := range auth.AllPermissionCodes() {
		if perms[code] {
			out = append(out, code)
		}
	}
	return out
}

func nullUUIDString(value uuid.NullUUID) any {
	if !value.Valid {
		return nil
	}
	return value.UUID.String()
}
