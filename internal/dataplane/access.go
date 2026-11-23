package dataplane

import (
	"context"

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
