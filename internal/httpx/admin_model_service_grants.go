package httpx

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
)

type modelServiceGrantResponse struct {
	ID             string    `json:"id"`
	ModelServiceID string    `json:"model_service_id"`
	SubjectType    string    `json:"subject_type"`
	SubjectID      string    `json:"subject_id"`
	SubjectName    string    `json:"subject_name"`
	Actions        []string  `json:"actions"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (d ServerDeps) ModelServiceGrants(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermModelServiceRead) != nil {
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_ID_INVALID", "invalid-model-service-id", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT g.id, g.model_service_id, g.subject_type, g.subject_id,
		       COALESCE(o.name, dept.name, p.name, ''), g.actions, g.created_at, g.updated_at
		FROM model_service_grants g
		LEFT JOIN organizations o ON g.subject_type = 'organization' AND o.id = g.subject_id
		LEFT JOIN departments dept ON g.subject_type = 'department' AND dept.id = g.subject_id AND dept.organization_id = g.organization_id
		LEFT JOIN projects p ON g.subject_type = 'project' AND p.id = g.subject_id AND p.organization_id = g.organization_id
		WHERE g.model_service_id = $1 AND g.organization_id = $2
		ORDER BY g.created_at ASC
	`, serviceID, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_GRANT_LIST_FAILED", "failed-to-query-model-service-grants", nil)
		return
	}
	defer rows.Close()
	list := []modelServiceGrantResponse{}
	for rows.Next() {
		var item modelServiceGrantResponse
		var id, service, subject uuid.UUID
		if err := rows.Scan(&id, &service, &item.SubjectType, &subject, &item.SubjectName, &item.Actions, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "SERVICE_GRANT_LIST_FAILED", "failed-to-read-model-service-grants", nil)
			return
		}
		item.ID = id.String()
		item.ModelServiceID = service.String()
		item.SubjectID = subject.String()
		list = append(list, item)
	}
	adminOK(w, list)
}

func (d ServerDeps) UpsertModelServiceGrant(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermModelServiceManage) != nil {
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_ID_INVALID", "invalid-model-service-id", nil)
		return
	}
	var body upsertAssetGrantRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	subjectID, err := uuid.Parse(body.SubjectID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_GRANT_SUBJECT_INVALID", "invalid-grant-subject-id", nil)
		return
	}
	switch body.SubjectType {
	case "organization", "department", "project":
	default:
		adminError(w, http.StatusBadRequest, "SERVICE_GRANT_SUBJECT_INVALID", "grant-subjects-support-only-organizations-departments", nil)
		return
	}
	seen := map[string]bool{}
	for _, action := range body.Actions {
		if action != "read" && action != "invoke" {
			adminError(w, http.StatusBadRequest, "SERVICE_GRANT_ACTION_INVALID", "grant-actions-support-only-read-and", nil)
			return
		}
		if seen[action] {
			adminError(w, http.StatusBadRequest, "SERVICE_GRANT_ACTION_INVALID", "grant-actions-cannot-be-duplicated", nil)
			return
		}
		seen[action] = true
	}
	if len(body.Actions) == 0 {
		adminError(w, http.StatusBadRequest, "SERVICE_GRANT_ACTION_INVALID", "select-at-least-one-grant-action", nil)
		return
	}
	var grantID uuid.UUID
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM model_services WHERE id = $1 AND organization_id = $2) FOR UPDATE`, serviceID, principal.OrganizationID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return &assetAPIError{status: http.StatusNotFound, code: "SERVICE_NOT_FOUND", reason: "model-service-does-not-exist-or"}
		}
		var subjectExists bool
		switch body.SubjectType {
		case "organization":
			subjectExists = subjectID == principal.OrganizationID
		case "department":
			err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM departments WHERE id = $1 AND organization_id = $2)`, subjectID, principal.OrganizationID).Scan(&subjectExists)
		case "project":
			err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM projects WHERE id = $1 AND organization_id = $2)`, subjectID, principal.OrganizationID).Scan(&subjectExists)
		}
		if err != nil {
			return err
		}
		if !subjectExists {
			return &assetAPIError{status: http.StatusBadRequest, code: "SERVICE_GRANT_SUBJECT_NOT_FOUND", reason: "grant-subject-does-not-exist-or"}
		}
		var beforeActions []string
		if err := tx.QueryRow(r.Context(), `SELECT actions FROM model_service_grants WHERE model_service_id = $1 AND subject_type = $2 AND subject_id = $3`, serviceID, body.SubjectType, subjectID).Scan(&beforeActions); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO model_service_grants(model_service_id, organization_id, subject_type, subject_id, actions, created_by)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (model_service_id, subject_type, subject_id)
			DO UPDATE SET actions = EXCLUDED.actions, updated_at = now()
			RETURNING id
		`, serviceID, principal.OrganizationID, body.SubjectType, subjectID, body.Actions, principal.UserID).Scan(&grantID); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
			VALUES ($1,$2,'user','model_service_grant.upserted','model_service_grant',$3,$4,$5)
		`, principal.OrganizationID, principal.UserID, grantID.String(),
			auditJSON(map[string]any{"model_service_id": serviceID.String(), "subject_type": body.SubjectType, "subject_id": subjectID.String(), "actions": beforeActions}),
			auditJSON(map[string]any{"model_service_id": serviceID.String(), "subject_type": body.SubjectType, "subject_id": subjectID.String(), "actions": body.Actions}))
		return err
	})
	if err != nil {
		writeAssetError(w, err, "SERVICE_GRANT_SAVE_FAILED", "save-model-service-grant-failed")
		return
	}
	adminOK(w, map[string]string{"id": grantID.String()})
}

func (d ServerDeps) RevokeModelServiceGrant(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermModelServiceManage) != nil {
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_ID_INVALID", "invalid-model-service-id", nil)
		return
	}
	grantID, err := uuid.Parse(chi.URLParam(r, "grantID"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "SERVICE_GRANT_ID_INVALID", "invalid-grant-id", nil)
		return
	}
	// 先读出授权再删除:撤销后记录已不存在,审计快照是回溯所属模型服务的唯一来源。
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var subjectType string
		var subjectID uuid.UUID
		var actions []string
		err := tx.QueryRow(r.Context(), `
			SELECT subject_type, subject_id, actions
			FROM model_service_grants
			WHERE id = $1 AND model_service_id = $2 AND organization_id = $3
			FOR UPDATE
		`, grantID, serviceID, principal.OrganizationID).Scan(&subjectType, &subjectID, &actions)
		if errors.Is(err, pgx.ErrNoRows) {
			return &assetAPIError{status: http.StatusNotFound, code: "SERVICE_GRANT_NOT_FOUND", reason: "grant-does-not-exist-or-does"}
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM model_service_grants WHERE id = $1`, grantID); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state)
			VALUES ($1,$2,'user','model_service_grant.revoked','model_service_grant',$3,$4)
		`, principal.OrganizationID, principal.UserID, grantID.String(), auditJSON(map[string]any{
			"model_service_id": serviceID.String(), "subject_type": subjectType, "subject_id": subjectID.String(), "actions": actions,
		}))
		return err
	})
	if err != nil {
		writeAssetError(w, err, "SERVICE_GRANT_REVOKE_FAILED", "failed-to-revoke-the-model-service")
		return
	}
	adminOK(w, nil)
}
