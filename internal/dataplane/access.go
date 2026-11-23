package dataplane

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

func (d Deps) hasModelServiceAccess(ctx context.Context, organizationID, projectID, serviceID uuid.UUID, action string) (bool, error) {
	var allowed bool
	err := d.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM model_service_grants g
			WHERE g.model_service_id = $1 AND g.organization_id = $2
			  AND g.actions @> ARRAY[$3]::text[]
			  AND (
			    (g.subject_type = 'organization' AND g.subject_id = $2)
			    OR (g.subject_type = 'project' AND g.subject_id = $4)
			    OR (g.subject_type = 'department' AND g.subject_id = (
			      SELECT p.department_id FROM projects p WHERE p.id = $4 AND p.organization_id = $2
			    ))
			  )
		)
	`, serviceID, organizationID, action, projectID).Scan(&allowed)
	return allowed, err
}

// auditDeniedModelAccess 记录 API Key 对模型服务的拒绝访问,不保存请求内容。
func (d Deps) auditDeniedModelAccess(r *http.Request, keyCtx apiKeyContext, serviceID uuid.UUID, operation string) {
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_type, action, resource_type, resource_id, result, after_state)
		VALUES ($1,'api_key','model_service.access_denied','model_service',$2,'failure',$3)
	`, keyCtx.OrganizationID, serviceID.String(), auditDataJSON(map[string]any{
		"api_key_id": keyCtx.KeyID.String(), "project_id": keyCtx.ProjectID.String(),
		"application_id": keyCtx.ApplicationID.String(), "operation": operation,
	}))
}
