package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/inwinter04/omniflux/internal/auth"
)

// requireAdmin 校验当前 principal 至少是 org_admin;不足时写出 403。
func (d ServerDeps) requireAdmin(w http.ResponseWriter, principal auth.Principal) error {
	if err := auth.RequireRole(principal, "org_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "需要组织管理员权限", nil)
		return err
	}
	return nil
}

// decodeJSON 解析请求体;失败时写出 400。
func (d ServerDeps) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		adminError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求体格式不正确: "+err.Error(), nil)
		return false
	}
	return true
}
