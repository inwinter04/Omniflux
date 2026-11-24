package httpx

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

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
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, name FROM departments
		WHERE organization_id = $1 ORDER BY created_at ASC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "DEPARTMENT_LIST_FAILED", "failed-to-query-departments", nil)
		return
	}
	defer rows.Close()
	list := []departmentResponse{}
	for rows.Next() {
		var item departmentResponse
		var id uuid.UUID
		if err := rows.Scan(&id, &item.Name); err != nil {
			adminError(w, http.StatusInternalServerError, "DEPARTMENT_LIST_FAILED", "failed-to-query-departments", nil)
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
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	var body createProjectRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "PROJECT_NAME_REQUIRED", "enter-a-project-name", nil)
		return
	}
	departmentID, err := uuid.Parse(body.DepartmentID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROJECT_DEPARTMENT_INVALID", "invalid-department-id-j8crm", nil)
		return
	}
	var deptCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM departments WHERE id = $1 AND organization_id = $2
	`, departmentID, principal.OrganizationID).Scan(&deptCount); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_CREATE_FAILED", "failed-to-create-the-project", nil)
		return
	}
	if deptCount == 0 {
		adminError(w, http.StatusBadRequest, "PROJECT_DEPARTMENT_NOT_FOUND", "department-does-not-exist-or-does", nil)
		return
	}
	projectID := uuid.New()
	err = d.execWithAudit(r.Context(), `
		INSERT INTO projects(id, organization_id, department_id, name)
		VALUES ($1,$2,$3,$4)
	`, []any{projectID, principal.OrganizationID, departmentID, body.Name},
		principal.OrganizationID, principal.UserID, "project.created", "project", projectID.String(),
		nil, map[string]any{"name": body.Name})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "projects_organization_id_name_key" {
			adminError(w, http.StatusConflict, "PROJECT_NAME_EXISTS", "a-project-with-the-same-name", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PROJECT_CREATE_FAILED", "failed-to-create-the-project", nil)
		return
	}
	adminOK(w, map[string]string{"id": projectID.String()})
}

// DeleteProject 删除项目;存在关联应用、API Key 或用量记录时拒绝,避免级联删除业务数据。
func (d ServerDeps) DeleteProject(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROJECT_ID_INVALID", "invalid-project-id", nil)
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1 AND organization_id = $2)
	`, projectID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_DELETE_FAILED", "failed-to-delete-the-project", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project-does-not-exist-or-does", nil)
		return
	}
	var appCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM applications WHERE project_id = $1
	`, projectID).Scan(&appCount); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_DELETE_FAILED", "failed-to-delete-the-project", nil)
		return
	}
	if appCount > 0 {
		adminError(w, http.StatusConflict, "PROJECT_HAS_APPLICATIONS",
			"project-has-applications", nil)
		return
	}
	var keyCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM api_keys WHERE project_id = $1 AND deleted_at IS NULL
	`, projectID).Scan(&keyCount); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_DELETE_FAILED", "failed-to-delete-the-project", nil)
		return
	}
	if keyCount > 0 {
		adminError(w, http.StatusConflict, "PROJECT_HAS_API_KEYS",
			"project-has-api-keys", nil)
		return
	}
	var usageCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM usage_records WHERE project_id = $1
	`, projectID).Scan(&usageCount); err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_DELETE_FAILED", "failed-to-delete-the-project", nil)
		return
	}
	if usageCount > 0 {
		adminError(w, http.StatusConflict, "PROJECT_HAS_USAGE",
			"project-has-usage", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		// 资源删除后已查不到名称,审计里记下它,审计列表才能显示“删的是什么”。
		var name string
		if err := tx.QueryRow(r.Context(), `DELETE FROM projects WHERE id = $1 RETURNING name`, projectID).Scan(&name); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"project.deleted", "project", projectID.String(), map[string]any{"name": name}, nil)
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_DELETE_FAILED", "failed-to-delete-the-project", nil)
		return
	}
	adminOK(w, nil)
}

// CreateApplication 在指定项目下创建应用。
func (d ServerDeps) CreateApplication(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	var body createApplicationRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "APPLICATION_NAME_REQUIRED", "enter-an-application-name", nil)
		return
	}
	projectID, err := uuid.Parse(body.ProjectID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "APPLICATION_PROJECT_INVALID", "invalid-project-id-bcvapn", nil)
		return
	}
	var projectCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM projects WHERE id = $1 AND organization_id = $2
	`, projectID, principal.OrganizationID).Scan(&projectCount); err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_CREATE_FAILED", "failed-to-create-the-application", nil)
		return
	}
	if projectCount == 0 {
		adminError(w, http.StatusBadRequest, "APPLICATION_PROJECT_NOT_FOUND", "project-does-not-exist-or-does", nil)
		return
	}
	applicationID := uuid.New()
	err = d.execWithAudit(r.Context(), `
		INSERT INTO applications(id, organization_id, project_id, name)
		VALUES ($1,$2,$3,$4)
	`, []any{applicationID, principal.OrganizationID, projectID, body.Name},
		principal.OrganizationID, principal.UserID, "application.created", "application", applicationID.String(),
		nil, map[string]any{"name": body.Name})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "applications_project_id_name_key" {
			adminError(w, http.StatusConflict, "APPLICATION_NAME_EXISTS", "an-application-with-the-same-name", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "APPLICATION_CREATE_FAILED", "failed-to-create-the-application", nil)
		return
	}
	adminOK(w, map[string]string{"id": applicationID.String()})
}

// DeleteApplication 删除应用;存在关联 API Key 或用量记录时拒绝。
func (d ServerDeps) DeleteApplication(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	applicationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "APPLICATION_ID_INVALID", "invalid-application-id", nil)
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
		adminError(w, http.StatusInternalServerError, "APPLICATION_DELETE_FAILED", "failed-to-delete-the-application", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "APPLICATION_NOT_FOUND", "application-does-not-exist-or-does", nil)
		return
	}
	var keyCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM api_keys WHERE application_id = $1 AND deleted_at IS NULL
	`, applicationID).Scan(&keyCount); err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_DELETE_FAILED", "failed-to-delete-the-application", nil)
		return
	}
	if keyCount > 0 {
		adminError(w, http.StatusConflict, "APPLICATION_HAS_API_KEYS",
			"application-has-api-keys", nil)
		return
	}
	var usageCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM usage_records WHERE application_id = $1
	`, applicationID).Scan(&usageCount); err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_DELETE_FAILED", "failed-to-delete-the-application", nil)
		return
	}
	if usageCount > 0 {
		adminError(w, http.StatusConflict, "APPLICATION_HAS_USAGE",
			"application-has-usage", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		// 资源删除后已查不到名称,审计里记下它,审计列表才能显示“删的是什么”。
		var name string
		if err := tx.QueryRow(r.Context(), `DELETE FROM applications WHERE id = $1 RETURNING name`, applicationID).Scan(&name); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"application.deleted", "application", applicationID.String(), map[string]any{"name": name}, nil)
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_DELETE_FAILED", "failed-to-delete-the-application", nil)
		return
	}
	adminOK(w, nil)
}

type updateProjectRequest struct {
	Name string `json:"name"`
}

// UpdateProject 更新项目名称。
func (d ServerDeps) UpdateProject(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	var body updateProjectRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "PROJECT_NAME_REQUIRED", "enter-a-project-name", nil)
		return
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROJECT_ID_INVALID", "invalid-project-id", nil)
		return
	}
	var oldName string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT name FROM projects WHERE id = $1 AND organization_id = $2
	`, projectID, principal.OrganizationID).Scan(&oldName)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_UPDATE_FAILED", "failed-to-update-the-project", nil)
		return
	}
	err = d.execWithAudit(r.Context(), `
		UPDATE projects SET name = $3, updated_at = now() WHERE id = $1 AND organization_id = $2
	`, []any{projectID, principal.OrganizationID, body.Name},
		principal.OrganizationID, principal.UserID, "project.updated", "project", projectID.String(),
		map[string]any{"name": oldName}, map[string]any{"name": body.Name})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "projects_organization_id_name_key" {
			adminError(w, http.StatusConflict, "PROJECT_NAME_EXISTS", "a-project-with-the-same-name", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PROJECT_UPDATE_FAILED", "failed-to-update-the-project", nil)
		return
	}
	adminOK(w, nil)
}

type updateApplicationRequest struct {
	Name string `json:"name"`
}

// UpdateApplication 更新应用名称。
func (d ServerDeps) UpdateApplication(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	var body updateApplicationRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "APPLICATION_NAME_REQUIRED", "enter-an-application-name", nil)
		return
	}
	applicationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "APPLICATION_ID_INVALID", "invalid-application-id", nil)
		return
	}
	var oldName string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT a.name FROM applications a
		JOIN projects p ON p.id = a.project_id
		WHERE a.id = $1 AND p.organization_id = $2
	`, applicationID, principal.OrganizationID).Scan(&oldName)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "APPLICATION_NOT_FOUND", "application-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_UPDATE_FAILED", "failed-to-update-the-application", nil)
		return
	}
	err = d.execWithAudit(r.Context(), `
		UPDATE applications SET name = $2, updated_at = now() WHERE id = $1
	`, []any{applicationID, body.Name},
		principal.OrganizationID, principal.UserID, "application.updated", "application", applicationID.String(),
		map[string]any{"name": oldName}, map[string]any{"name": body.Name})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "applications_project_id_name_key" {
			adminError(w, http.StatusConflict, "APPLICATION_NAME_EXISTS", "an-application-with-the-same-name", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "APPLICATION_UPDATE_FAILED", "failed-to-update-the-application", nil)
		return
	}
	adminOK(w, nil)
}

var _ = pgx.ErrNoRows

type createDepartmentRequest struct {
	Name string `json:"name"`
}

// CreateDepartment 创建部门;需要组织管理员权限。
func (d ServerDeps) CreateDepartment(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	var body createDepartmentRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "DEPARTMENT_NAME_REQUIRED", "enter-a-department-name", nil)
		return
	}
	departmentID := uuid.New()
	err := d.execWithAudit(r.Context(), `
		INSERT INTO departments(id, organization_id, name) VALUES ($1,$2,$3)
	`, []any{departmentID, principal.OrganizationID, body.Name},
		principal.OrganizationID, principal.UserID, "department.created", "department", departmentID.String(),
		nil, map[string]any{"name": body.Name})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "departments_organization_id_name_key" {
			adminError(w, http.StatusConflict, "DEPARTMENT_NAME_EXISTS", "a-department-with-the-same-name", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "DEPARTMENT_CREATE_FAILED", "failed-to-create-the-department", nil)
		return
	}
	adminOK(w, map[string]string{"id": departmentID.String()})
}

// UpdateDepartment 更新部门名称。
func (d ServerDeps) UpdateDepartment(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	var body createDepartmentRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "DEPARTMENT_NAME_REQUIRED", "enter-a-department-name", nil)
		return
	}
	departmentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "DEPARTMENT_ID_INVALID", "invalid-department-id", nil)
		return
	}
	var oldName string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT name FROM departments WHERE id = $1 AND organization_id = $2
	`, departmentID, principal.OrganizationID).Scan(&oldName)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "DEPARTMENT_NOT_FOUND", "department-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "DEPARTMENT_UPDATE_FAILED", "failed-to-update-the-department", nil)
		return
	}
	err = d.execWithAudit(r.Context(), `
		UPDATE departments SET name = $3 WHERE id = $1 AND organization_id = $2
	`, []any{departmentID, principal.OrganizationID, body.Name},
		principal.OrganizationID, principal.UserID, "department.updated", "department", departmentID.String(),
		map[string]any{"name": oldName}, map[string]any{"name": body.Name})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "departments_organization_id_name_key" {
			adminError(w, http.StatusConflict, "DEPARTMENT_NAME_EXISTS", "a-department-with-the-same-name", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "DEPARTMENT_UPDATE_FAILED", "failed-to-update-the-department", nil)
		return
	}
	adminOK(w, nil)
}

// DeleteDepartment 删除部门;存在关联项目时拒绝(FK RESTRICT 保证)。
func (d ServerDeps) DeleteDepartment(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	departmentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "DEPARTMENT_ID_INVALID", "invalid-department-id", nil)
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM departments WHERE id = $1 AND organization_id = $2)
	`, departmentID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "DEPARTMENT_DELETE_FAILED", "failed-to-delete-the-department", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "DEPARTMENT_NOT_FOUND", "department-does-not-exist-or-does", nil)
		return
	}
	var projectCount int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM projects WHERE department_id = $1
	`, departmentID).Scan(&projectCount); err != nil {
		adminError(w, http.StatusInternalServerError, "DEPARTMENT_DELETE_FAILED", "failed-to-delete-the-department", nil)
		return
	}
	if projectCount > 0 {
		adminError(w, http.StatusConflict, "DEPARTMENT_HAS_PROJECTS",
			"department-has-projects", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		// 资源删除后已查不到名称,审计里记下它,审计列表才能显示“删的是什么”。
		var name string
		if err := tx.QueryRow(r.Context(), `DELETE FROM departments WHERE id = $1 RETURNING name`, departmentID).Scan(&name); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"department.deleted", "department", departmentID.String(), map[string]any{"name": name}, nil)
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "DEPARTMENT_DELETE_FAILED", "failed-to-delete-the-department", nil)
		return
	}
	adminOK(w, nil)
}

type organizationResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

type createOrganizationRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type updateOrganizationRequest struct {
	Name *string `json:"name"`
}

// organizationSlugPattern 约束 slug 为小写字母/数字/连字符,保证 URL 安全且跨系统兼容。
var organizationSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// isPlatformAdmin 判断当前账号是否为平台管理员(可跨组织管理组织与用户归属)。
func isPlatformAdmin(principal auth.Principal) bool {
	return principal.Role == "platform_admin"
}

// Organizations 返回组织列表:platform_admin 可见全部组织,其余角色仅可见自己所在组织。
func (d ServerDeps) Organizations(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	// platform_admin 返回全部组织;其余角色仅能看到自己所在组织。
	query := `SELECT id, name, slug, created_at FROM organizations`
	args := []any{}
	if !isPlatformAdmin(principal) {
		query += " WHERE id = $1"
		args = append(args, principal.OrganizationID)
	}
	query += " ORDER BY created_at ASC"
	rows, err := d.Pool.Query(r.Context(), query, args...)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "ORG_LIST_FAILED", "failed-to-query-organizations", nil)
		return
	}
	defer rows.Close()
	list := []organizationResponse{}
	for rows.Next() {
		var item organizationResponse
		var id uuid.UUID
		if err := rows.Scan(&id, &item.Name, &item.Slug, &item.CreatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "ORG_LIST_FAILED", "failed-to-query-organizations", nil)
			return
		}
		item.ID = id.String()
		list = append(list, item)
	}
	adminOK(w, list)
}

// CreateOrganization 创建组织;仅 platform_admin 可调用。
// 新组织初始没有任何成员与业务数据,需通过用户管理将用户归属到该组织。
func (d ServerDeps) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminFail(w, http.StatusUnauthorized, "authentication-required")
		return
	}
	if !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	var body createOrganizationRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Slug = strings.TrimSpace(body.Slug)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "ORG_NAME_REQUIRED", "enter-an-organization-name", nil)
		return
	}
	if !organizationSlugPattern.MatchString(body.Slug) {
		adminError(w, http.StatusBadRequest, "ORG_SLUG_INVALID",
			"org-slug-invalid", nil)
		return
	}
	orgID := uuid.New()
	err := d.execWithAudit(r.Context(), `
		INSERT INTO organizations(id, name, slug) VALUES ($1,$2,$3)
	`, []any{orgID, body.Name, body.Slug},
		principal.OrganizationID, principal.UserID, "organization.created", "organization", orgID.String(),
		nil, map[string]any{"name": body.Name, "slug": body.Slug})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "organizations_slug_key" {
			adminError(w, http.StatusConflict, "ORG_SLUG_EXISTS", "this-slug-is-already-in-use", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "ORG_CREATE_FAILED", "failed-to-create-the-organization", nil)
		return
	}
	adminOK(w, map[string]string{"id": orgID.String()})
}

// UpdateOrganization 修改组织名称;platform_admin 可改任意组织,
// 组织内 org_admin 及以上仅可改自己所在组织。slug 创建后不可变更。
func (d ServerDeps) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminFail(w, http.StatusUnauthorized, "authentication-required")
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "ORG_ID_INVALID", "invalid-organization-id", nil)
		return
	}
	if !isPlatformAdmin(principal) {
		if d.requireAdmin(w, principal) != nil {
			return
		}
		if orgID != principal.OrganizationID {
			adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "you-can-only-modify-your-own", nil)
			return
		}
	}
	var body updateOrganizationRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	if body.Name == nil || strings.TrimSpace(*body.Name) == "" {
		adminError(w, http.StatusBadRequest, "ORG_NAME_REQUIRED", "enter-an-organization-name", nil)
		return
	}
	name := strings.TrimSpace(*body.Name)
	var oldName string
	err = d.Pool.QueryRow(r.Context(), `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&oldName)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "ORG_NOT_FOUND", "organization-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "ORG_UPDATE_FAILED", "failed-to-update-the-organization", nil)
		return
	}
	err = d.execWithAudit(r.Context(), `
		UPDATE organizations SET name = $2 WHERE id = $1
	`, []any{orgID, name},
		principal.OrganizationID, principal.UserID, "organization.updated", "organization", orgID.String(),
		map[string]any{"name": oldName}, map[string]any{"name": name})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "ORG_UPDATE_FAILED", "failed-to-update-the-organization", nil)
		return
	}
	adminOK(w, nil)
}
