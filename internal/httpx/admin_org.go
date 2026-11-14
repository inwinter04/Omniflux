package httpx

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/inwinter04/omniflux/internal/auth"
)

type departmentResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type createProjectRequest struct {
	Name         string `json:"name"`
	DepartmentID string `json:"department_id"`
}

type createApplicationRequest struct {
	Name      string `json:"name"`
	ProjectID string `json:"project_id"`
}

// Departments 返回当前组织的部门列表,供项目创建与展示使用。
func (d ServerDeps) Departments(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, name FROM departments
		WHERE organization_id = $1 ORDER BY created_at ASC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "DEPARTMENT_LIST_FAILED", "无法查询部门", nil)
		return
	}
	defer rows.Close()
	list := []departmentResponse{}
	for rows.Next() {
		var item departmentResponse
		var id uuid.UUID
		if err := rows.Scan(&id, &item.Name); err != nil {
			adminError(w, http.StatusInternalServerError, "DEPARTMENT_LIST_FAILED", "无法查询部门", nil)
			return
		}
		item.ID = id.String()
		list = append(list, item)
	}
	adminOK(w, list)
}

// CreateProject 在指定部门下创建项目。
func (d ServerDeps) CreateProject(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	var body createProjectRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "PROJECT_NAME_REQUIRED", "请填写项目名称", nil)
		return
	}
	departmentID, err := uuid.Parse(body.DepartmentID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROJECT_DEPARTMENT_INVALID", "department_id 不合法", nil)
		return
	}
	var deptCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM departments WHERE id = $1 AND organization_id = $2
	`, departmentID, principal.OrganizationID).Scan(&deptCount); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_CREATE_FAILED", "创建项目失败", nil)
		return
	}
	if deptCount == 0 {
		adminError(w, http.StatusBadRequest, "PROJECT_DEPARTMENT_NOT_FOUND", "部门不存在或不属于当前组织", nil)
		return
	}
	projectID := uuid.New()
	if _, err := d.Pool.Exec(r.Context(), `
		INSERT INTO projects(id, organization_id, department_id, name)
		VALUES ($1,$2,$3,$4)
	`, projectID, principal.OrganizationID, departmentID, body.Name); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "projects_organization_id_name_key" {
			adminError(w, http.StatusConflict, "PROJECT_NAME_EXISTS", "同名项目已存在", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PROJECT_CREATE_FAILED", "创建项目失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
		VALUES ($1,$2,'user','project.created','project',$3,$4)
	`, principal.OrganizationID, principal.UserID, projectID.String(), auditJSON(map[string]any{"name": body.Name}))
	adminOK(w, map[string]string{"id": projectID.String()})
}

// DeleteProject 删除项目;存在关联应用、API Key 或用量记录时拒绝,避免级联删除业务数据。
func (d ServerDeps) DeleteProject(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROJECT_ID_INVALID", "项目 ID 不合法", nil)
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1 AND organization_id = $2)
	`, projectID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_DELETE_FAILED", "删除项目失败", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在或不属于当前组织", nil)
		return
	}
	var appCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM applications WHERE project_id = $1
	`, projectID).Scan(&appCount); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_DELETE_FAILED", "删除项目失败", nil)
		return
	}
	if appCount > 0 {
		adminError(w, http.StatusConflict, "PROJECT_HAS_APPLICATIONS",
			"项目下存在应用,请先删除应用后再删除项目", nil)
		return
	}
	var keyCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM api_keys WHERE project_id = $1 AND deleted_at IS NULL
	`, projectID).Scan(&keyCount); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_DELETE_FAILED", "删除项目失败", nil)
		return
	}
	if keyCount > 0 {
		adminError(w, http.StatusConflict, "PROJECT_HAS_API_KEYS",
			"项目下存在 API Key,请先禁用并移除后再删除项目", nil)
		return
	}
	var usageCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM usage_records WHERE project_id = $1
	`, projectID).Scan(&usageCount); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_DELETE_FAILED", "删除项目失败", nil)
		return
	}
	if usageCount > 0 {
		adminError(w, http.StatusConflict, "PROJECT_HAS_USAGE",
			"项目存在用量记录,为保留审计数据无法删除", nil)
		return
	}
	if _, err := d.Pool.Exec(r.Context(), `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_DELETE_FAILED", "删除项目失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id)
		VALUES ($1,$2,'user','project.deleted','project',$3)
	`, principal.OrganizationID, principal.UserID, projectID.String())
	adminOK(w, nil)
}

// CreateApplication 在指定项目下创建应用。
func (d ServerDeps) CreateApplication(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	var body createApplicationRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "APPLICATION_NAME_REQUIRED", "请填写应用名称", nil)
		return
	}
	projectID, err := uuid.Parse(body.ProjectID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "APPLICATION_PROJECT_INVALID", "project_id 不合法", nil)
		return
	}
	var projectCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM projects WHERE id = $1 AND organization_id = $2
	`, projectID, principal.OrganizationID).Scan(&projectCount); err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_CREATE_FAILED", "创建应用失败", nil)
		return
	}
	if projectCount == 0 {
		adminError(w, http.StatusBadRequest, "APPLICATION_PROJECT_NOT_FOUND", "项目不存在或不属于当前组织", nil)
		return
	}
	applicationID := uuid.New()
	if _, err := d.Pool.Exec(r.Context(), `
		INSERT INTO applications(id, organization_id, project_id, name)
		VALUES ($1,$2,$3,$4)
	`, applicationID, principal.OrganizationID, projectID, body.Name); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "applications_project_id_name_key" {
			adminError(w, http.StatusConflict, "APPLICATION_NAME_EXISTS", "该项目下同名应用已存在", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "APPLICATION_CREATE_FAILED", "创建应用失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
		VALUES ($1,$2,'user','application.created','application',$3,$4)
	`, principal.OrganizationID, principal.UserID, applicationID.String(), auditJSON(map[string]any{"name": body.Name}))
	adminOK(w, map[string]string{"id": applicationID.String()})
}

// DeleteApplication 删除应用;存在关联 API Key 或用量记录时拒绝。
func (d ServerDeps) DeleteApplication(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	applicationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "APPLICATION_ID_INVALID", "应用 ID 不合法", nil)
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM applications a
			JOIN projects p ON p.id = a.project_id
			WHERE a.id = $1 AND p.organization_id = $2
		)
	`, applicationID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_DELETE_FAILED", "删除应用失败", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "APPLICATION_NOT_FOUND", "应用不存在或不属于当前组织", nil)
		return
	}
	var keyCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM api_keys WHERE application_id = $1 AND deleted_at IS NULL
	`, applicationID).Scan(&keyCount); err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_DELETE_FAILED", "删除应用失败", nil)
		return
	}
	if keyCount > 0 {
		adminError(w, http.StatusConflict, "APPLICATION_HAS_API_KEYS",
			"应用下存在 API Key,请先禁用并移除后再删除应用", nil)
		return
	}
	var usageCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM usage_records WHERE application_id = $1
	`, applicationID).Scan(&usageCount); err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_DELETE_FAILED", "删除应用失败", nil)
		return
	}
	if usageCount > 0 {
		adminError(w, http.StatusConflict, "APPLICATION_HAS_USAGE",
			"应用存在用量记录,为保留审计数据无法删除", nil)
		return
	}
	if _, err := d.Pool.Exec(r.Context(), `DELETE FROM applications WHERE id = $1`, applicationID); err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_DELETE_FAILED", "删除应用失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id)
		VALUES ($1,$2,'user','application.deleted','application',$3)
	`, principal.OrganizationID, principal.UserID, applicationID.String())
	adminOK(w, nil)
}

type updateProjectRequest struct {
	Name string `json:"name"`
}

// UpdateProject 更新项目名称。
func (d ServerDeps) UpdateProject(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	var body updateProjectRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "PROJECT_NAME_REQUIRED", "请填写项目名称", nil)
		return
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROJECT_ID_INVALID", "项目 ID 不合法", nil)
		return
	}
	var oldName string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT name FROM projects WHERE id = $1 AND organization_id = $2
	`, projectID, principal.OrganizationID).Scan(&oldName)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在或不属于当前组织", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_UPDATE_FAILED", "更新项目失败", nil)
		return
	}
	if _, err := d.Pool.Exec(r.Context(), `
		UPDATE projects SET name = $3, updated_at = now() WHERE id = $1 AND organization_id = $2
	`, projectID, principal.OrganizationID, body.Name); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "projects_organization_id_name_key" {
			adminError(w, http.StatusConflict, "PROJECT_NAME_EXISTS", "同名项目已存在", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PROJECT_UPDATE_FAILED", "更新项目失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
		VALUES ($1,$2,'user','project.updated','project',$3,$4,$5)
	`, principal.OrganizationID, principal.UserID, projectID.String(), auditJSON(map[string]any{"name": oldName}), auditJSON(map[string]any{"name": body.Name}))
	adminOK(w, nil)
}

type updateApplicationRequest struct {
	Name string `json:"name"`
}

// UpdateApplication 更新应用名称。
func (d ServerDeps) UpdateApplication(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	var body updateApplicationRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "APPLICATION_NAME_REQUIRED", "请填写应用名称", nil)
		return
	}
	applicationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "APPLICATION_ID_INVALID", "应用 ID 不合法", nil)
		return
	}
	var oldName string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT a.name FROM applications a
		JOIN projects p ON p.id = a.project_id
		WHERE a.id = $1 AND p.organization_id = $2
	`, applicationID, principal.OrganizationID).Scan(&oldName)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "APPLICATION_NOT_FOUND", "应用不存在或不属于当前组织", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_UPDATE_FAILED", "更新应用失败", nil)
		return
	}
	if _, err := d.Pool.Exec(r.Context(), `
		UPDATE applications SET name = $2, updated_at = now() WHERE id = $1
	`, applicationID, body.Name); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "applications_project_id_name_key" {
			adminError(w, http.StatusConflict, "APPLICATION_NAME_EXISTS", "该项目下同名应用已存在", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "APPLICATION_UPDATE_FAILED", "更新应用失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
		VALUES ($1,$2,'user','application.updated','application',$3,$4,$5)
	`, principal.OrganizationID, principal.UserID, applicationID.String(), auditJSON(map[string]any{"name": oldName}), auditJSON(map[string]any{"name": body.Name}))
	adminOK(w, nil)
}

var _ = pgx.ErrNoRows



