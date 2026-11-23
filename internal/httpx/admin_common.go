package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/inwinter04/omniflux/internal/auth"
)

// requireAdmin 校验当前 principal 至少是 org_admin;不足时写出 403。
func (d ServerDeps) requireAdmin(w http.ResponseWriter, principal auth.Principal) error {
	if err := auth.RequireRole(principal, "org_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "organization-admin-permission-is-required", nil)
		return err
	}
	return nil
}

// requireViewer 校验至少是 viewer,用于只读接口。
func (d ServerDeps) requireViewer(w http.ResponseWriter, principal auth.Principal) error {
	if err := auth.RequireRole(principal, "viewer"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return err
	}
	return nil
}

// requireProjectAdmin 校验至少是 project_admin,用于业务资源写接口。
func (d ServerDeps) requireProjectAdmin(w http.ResponseWriter, principal auth.Principal) error {
	if err := auth.RequireRole(principal, "project_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "project-admin-permission-is-required", nil)
		return err
	}
	return nil
}

// requireOwner 校验至少是 org_owner,用于用户管理等最高权限接口。
func (d ServerDeps) requireOwner(w http.ResponseWriter, principal auth.Principal) error {
	if err := auth.RequireRole(principal, "org_owner"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "organization-owner-permission-is-required", nil)
		return err
	}
	return nil
}

// decodeJSON 解析请求体;失败时写出 400。
func (d ServerDeps) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		adminError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid-request-body", nil, err.Error())
		return false
	}
	return true
}

// nullableIntPtr 把可选限流指针转成可空参数。
func nullableIntPtr(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
