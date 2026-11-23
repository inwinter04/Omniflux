package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/inwinter04/omniflux/internal/auth"
)

type resourceStatusRequest struct {
	Status string `json:"status"`
}

func parseResourceStatus(w http.ResponseWriter, r *http.Request, d ServerDeps) (string, bool) {
	var body resourceStatusRequest
	if !d.decodeJSON(w, r, &body) {
		return "", false
	}
	if body.Status != "active" && body.Status != "disabled" {
		adminError(w, http.StatusBadRequest, "RESOURCE_STATUS_INVALID", "状态仅支持启用或停用", nil)
		return "", false
	}
	return body.Status, true
}

func (d ServerDeps) UpdateProviderAccountStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_ID_INVALID", "账号 ID 不合法", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	result, err := d.Pool.Exec(r.Context(), `UPDATE provider_accounts SET status = $3, updated_at = now() WHERE id = $1 AND organization_id = $2`,
		accountID, principal.OrganizationID, status)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_STATUS_FAILED", "更新账号状态失败", nil)
		return
	}
	if result.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "账号不存在或不属于当前组织", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
		VALUES ($1,$2,'user','provider_account.status_changed','provider_account',$3,$4)
	`, principal.OrganizationID, principal.UserID, accountID.String(), auditJSON(map[string]any{"status": status}))
	if !d.reloadEngine(w, r) {
		return
	}
	adminOK(w, nil)
}

func (d ServerDeps) UpdateModelServiceStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_ID_INVALID", "模型服务 ID 不合法", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	result, err := d.Pool.Exec(r.Context(), `UPDATE model_services SET status = $3, updated_at = now() WHERE id = $1 AND organization_id = $2`,
		serviceID, principal.OrganizationID, status)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_STATUS_FAILED", "更新模型服务状态失败", nil)
		return
	}
	if result.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "SERVICE_NOT_FOUND", "模型服务不存在或不属于当前组织", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
		VALUES ($1,$2,'user','model_service.status_changed','model_service',$3,$4)
	`, principal.OrganizationID, principal.UserID, serviceID.String(), auditJSON(map[string]any{"status": status}))
	adminOK(w, nil)
}

func (d ServerDeps) UpdateModelCandidateStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	candidateID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "CANDIDATE_ID_INVALID", "候选模型 ID 不合法", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	result, err := d.Pool.Exec(r.Context(), `
		UPDATE model_candidates SET status = $3 WHERE id = $1 AND model_service_id IN (
			SELECT id FROM model_services WHERE organization_id = $2
		)
	`, candidateID, principal.OrganizationID, status)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "CANDIDATE_STATUS_FAILED", "更新候选模型状态失败", nil)
		return
	}
	if result.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "CANDIDATE_NOT_FOUND", "候选模型不存在或不属于当前组织", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
		VALUES ($1,$2,'user','model_candidate.status_changed','model_candidate',$3,$4)
	`, principal.OrganizationID, principal.UserID, candidateID.String(), auditJSON(map[string]any{"status": status}))
	adminOK(w, nil)
}

func (d ServerDeps) UpdateProjectStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROJECT_ID_INVALID", "项目 ID 不合法", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	result, err := d.Pool.Exec(r.Context(), `UPDATE projects SET status = $3, updated_at = now() WHERE id = $1 AND organization_id = $2`, id, principal.OrganizationID, status)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_STATUS_FAILED", "更新项目状态失败", nil)
		return
	}
	if result.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在或不属于当前组织", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state) VALUES ($1,$2,'user','project.status_changed','project',$3,$4)`, principal.OrganizationID, principal.UserID, id.String(), auditJSON(map[string]any{"status": status}))
	adminOK(w, nil)
}

func (d ServerDeps) UpdateApplicationStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "APPLICATION_ID_INVALID", "应用 ID 不合法", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	result, err := d.Pool.Exec(r.Context(), `UPDATE applications SET status = $3, updated_at = now() WHERE id = $1 AND project_id IN (SELECT id FROM projects WHERE organization_id = $2)`, id, principal.OrganizationID, status)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_STATUS_FAILED", "更新应用状态失败", nil)
		return
	}
	if result.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "APPLICATION_NOT_FOUND", "应用不存在或不属于当前组织", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state) VALUES ($1,$2,'user','application.status_changed','application',$3,$4)`, principal.OrganizationID, principal.UserID, id.String(), auditJSON(map[string]any{"status": status}))
	adminOK(w, nil)
}

func (d ServerDeps) UpdateRoutePolicyStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "路由策略 ID 不合法", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	result, err := d.Pool.Exec(r.Context(), `UPDATE route_policies SET status = $3, updated_at = now() WHERE id = $1 AND organization_id = $2`, id, principal.OrganizationID, status)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_STATUS_FAILED", "更新路由策略状态失败", nil)
		return
	}
	if result.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "路由策略不存在或不属于当前组织", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state) VALUES ($1,$2,'user','route_policy.status_changed','route_policy',$3,$4)`, principal.OrganizationID, principal.UserID, id.String(), auditJSON(map[string]any{"status": status}))
	adminOK(w, nil)
}
