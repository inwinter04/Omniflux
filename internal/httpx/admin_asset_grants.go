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

type assetGrantResponse struct {
	ID          string    `json:"id"`
	AssetID     string    `json:"asset_id"`
	SubjectType string    `json:"subject_type"`
	SubjectID   string    `json:"subject_id"`
	SubjectName string    `json:"subject_name"`
	Actions     []string  `json:"actions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type upsertAssetGrantRequest struct {
	SubjectType string   `json:"subject_type"`
	SubjectID   string   `json:"subject_id"`
	Actions     []string `json:"actions"`
}

func (d ServerDeps) AssetGrants(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_GRANT_LIST_FAILED", "查询资产授权失败")
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM assets WHERE id = $1 AND organization_id = $2)
	`, assetID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "ASSET_GRANT_LIST_FAILED", "查询资产授权失败", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在或不属于当前组织", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT g.id, g.asset_id, g.subject_type, g.subject_id,
		       COALESCE(o.name, dept.name, p.name, '未知对象'), g.actions, g.created_at, g.updated_at
		FROM asset_grants g
		LEFT JOIN organizations o ON g.subject_type = 'organization' AND o.id = g.subject_id
		LEFT JOIN departments dept ON g.subject_type = 'department' AND dept.id = g.subject_id AND dept.organization_id = g.organization_id
		LEFT JOIN projects p ON g.subject_type = 'project' AND p.id = g.subject_id AND p.organization_id = g.organization_id
		WHERE g.asset_id = $1 AND g.organization_id = $2
		ORDER BY g.created_at ASC
	`, assetID, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "ASSET_GRANT_LIST_FAILED", "查询资产授权失败", nil)
		return
	}
	defer rows.Close()
	list := []assetGrantResponse{}
	for rows.Next() {
		var item assetGrantResponse
		var id, grantAssetID, subjectID uuid.UUID
		if err := rows.Scan(&id, &grantAssetID, &item.SubjectType, &subjectID, &item.SubjectName,
			&item.Actions, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "ASSET_GRANT_LIST_FAILED", "读取资产授权失败", nil)
			return
		}
		item.ID = id.String()
		item.AssetID = grantAssetID.String()
		item.SubjectID = subjectID.String()
		list = append(list, item)
	}
	adminOK(w, list)
}

func (d ServerDeps) UpsertAssetGrant(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_GRANT_SAVE_FAILED", "保存资产授权失败")
		return
	}
	var body upsertAssetGrantRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	subjectID, parseErr := uuid.Parse(body.SubjectID)
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "ASSET_GRANT_SUBJECT_INVALID", "授权对象 ID 不合法", nil)
		return
	}
	switch body.SubjectType {
	case "organization", "department", "project":
	default:
		adminError(w, http.StatusBadRequest, "ASSET_GRANT_SUBJECT_INVALID", "授权对象仅支持组织、部门、项目", nil)
		return
	}
	seen := map[string]bool{}
	for _, action := range body.Actions {
		if action != "read" && action != "invoke" {
			adminError(w, http.StatusBadRequest, "ASSET_GRANT_ACTION_INVALID", "授权动作仅支持 read、invoke", nil)
			return
		}
		if seen[action] {
			adminError(w, http.StatusBadRequest, "ASSET_GRANT_ACTION_INVALID", "授权动作不能重复", nil)
			return
		}
		seen[action] = true
	}
	if len(body.Actions) == 0 {
		adminError(w, http.StatusBadRequest, "ASSET_GRANT_ACTION_INVALID", "至少选择一项授权动作", nil)
		return
	}
	var grantID uuid.UUID
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(r.Context(), `
			SELECT status FROM assets WHERE id = $1 AND organization_id = $2 FOR UPDATE
		`, assetID, principal.OrganizationID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return assetError(http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在或不属于当前组织")
		}
		if err != nil {
			return err
		}
		if status == "archived" {
			return assetError(http.StatusConflict, "ASSET_ARCHIVED", "已归档资产不能新增授权")
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
			return assetError(http.StatusBadRequest, "ASSET_GRANT_SUBJECT_NOT_FOUND", "授权对象不存在或不属于当前组织")
		}
		var beforeActions []string
		if err := tx.QueryRow(r.Context(), `SELECT actions FROM asset_grants WHERE asset_id = $1 AND subject_type = $2 AND subject_id = $3`, assetID, body.SubjectType, subjectID).Scan(&beforeActions); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO asset_grants(asset_id, organization_id, subject_type, subject_id, actions, created_by)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (asset_id, subject_type, subject_id)
			DO UPDATE SET actions = EXCLUDED.actions, updated_at = now()
			RETURNING id
		`, assetID, principal.OrganizationID, body.SubjectType, subjectID, body.Actions, principal.UserID).Scan(&grantID); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
			VALUES ($1,$2,'user','asset_grant.upserted','asset_grant',$3,$4,$5)
		`, principal.OrganizationID, principal.UserID, grantID.String(),
			auditJSON(map[string]any{"asset_id": assetID.String(), "subject_type": body.SubjectType, "subject_id": subjectID.String(), "actions": beforeActions}),
			auditJSON(map[string]any{"asset_id": assetID.String(), "subject_type": body.SubjectType, "subject_id": subjectID.String(), "actions": body.Actions}))
		return err
	})
	if err != nil {
		writeAssetError(w, err, "ASSET_GRANT_SAVE_FAILED", "保存资产授权失败")
		return
	}
	adminOK(w, map[string]string{"id": grantID.String()})
}

func (d ServerDeps) RevokeAssetGrant(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_GRANT_REVOKE_FAILED", "撤销资产授权失败")
		return
	}
	grantID, parseErr := uuid.Parse(chi.URLParam(r, "grantID"))
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "ASSET_GRANT_ID_INVALID", "授权 ID 不合法", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var subjectType string
		var subjectID uuid.UUID
		var actions []string
		err := tx.QueryRow(r.Context(), `
			SELECT g.subject_type, g.subject_id, g.actions
			FROM asset_grants g
			JOIN assets a ON a.id = g.asset_id
			WHERE g.id = $1 AND g.asset_id = $2 AND g.organization_id = $3
			FOR UPDATE OF g
		`, grantID, assetID, principal.OrganizationID).Scan(&subjectType, &subjectID, &actions)
		if errors.Is(err, pgx.ErrNoRows) {
			return assetError(http.StatusNotFound, "ASSET_GRANT_NOT_FOUND", "授权不存在或不属于当前组织")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM asset_grants WHERE id = $1`, grantID); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state)
			VALUES ($1,$2,'user','asset_grant.revoked','asset_grant',$3,$4)
		`, principal.OrganizationID, principal.UserID, grantID.String(), auditJSON(map[string]any{
			"asset_id": assetID.String(), "subject_type": subjectType, "subject_id": subjectID.String(), "actions": actions,
		}))
		return err
	})
	if err != nil {
		writeAssetError(w, err, "ASSET_GRANT_REVOKE_FAILED", "撤销资产授权失败")
		return
	}
	adminOK(w, nil)
}
