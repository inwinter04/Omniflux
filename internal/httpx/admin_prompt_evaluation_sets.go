package httpx

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
)

type evaluationSetOverview struct {
	ID          string     `json:"id"`
	AssetID     string     `json:"asset_id"`
	AssetName   string     `json:"asset_name"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	CaseCount   int        `json:"case_count"`
	RunCount    int        `json:"run_count"`
	Accuracy    *float64   `json:"accuracy"`
	LastRunAt   *time.Time `json:"last_run_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (d ServerDeps) AllPromptEvaluationSets(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermEvaluationRead) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT s.id, s.asset_id, a.name, s.name, s.description,
		       (SELECT count(*) FROM prompt_evaluation_cases c WHERE c.evaluation_set_id = s.id),
		       (SELECT count(*) FROM prompt_runs r WHERE r.evaluation_set_id = s.id),
		       (SELECT avg(CASE WHEN r.matched THEN 1.0 ELSE 0.0 END) FROM prompt_runs r WHERE r.evaluation_set_id = s.id AND r.matched IS NOT NULL),
		       (SELECT max(r.created_at) FROM prompt_runs r WHERE r.evaluation_set_id = s.id),
		       s.created_at, s.updated_at
		FROM prompt_evaluation_sets s
		JOIN assets a ON a.id = s.asset_id
		WHERE s.organization_id = $1
		ORDER BY s.updated_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "EVALUATION_SET_LIST_FAILED", "failed-to-query-evaluation-sets", nil)
		return
	}
	defer rows.Close()
	results := []evaluationSetOverview{}
	for rows.Next() {
		var item evaluationSetOverview
		var id, assetID uuid.UUID
		if err := rows.Scan(&id, &assetID, &item.AssetName, &item.Name, &item.Description, &item.CaseCount, &item.RunCount, &item.Accuracy, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "EVALUATION_SET_LIST_FAILED", "failed-to-read-evaluation-sets", nil)
			return
		}
		item.ID, item.AssetID = id.String(), assetID.String()
		results = append(results, item)
	}
	adminOK(w, results)
}

func (d ServerDeps) UpdatePromptEvaluationSet(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermEvaluationManage) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "EVALUATION_SET_UPDATE_FAILED", "update-evaluation-set-failed")
		return
	}
	setID, parseErr := uuid.Parse(chi.URLParam(r, "setID"))
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "EVALUATION_SET_ID_INVALID", "invalid-evaluation-set-id", nil)
		return
	}
	var body evaluationSetRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	if err := validateEvaluationSet(body); err != nil {
		writeAssetError(w, err, "EVALUATION_SET_UPDATE_FAILED", "update-evaluation-set-failed")
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM prompt_evaluation_sets WHERE id=$1 AND organization_id=$2 AND asset_id=$3)`, setID, principal.OrganizationID, assetID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return assetError(http.StatusNotFound, "EVALUATION_SET_NOT_FOUND", "evaluation-set-does-not-exist-or")
		}
		if _, err := tx.Exec(r.Context(), `UPDATE prompt_evaluation_sets SET name=$2, description=$3, updated_at=now() WHERE id=$1`, setID, strings.TrimSpace(body.Name), strings.TrimSpace(body.Description)); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM prompt_evaluation_cases WHERE evaluation_set_id=$1`, setID); err != nil {
			return err
		}
		for index, item := range body.Cases {
			input, _ := json.Marshal(item.Input)
			if _, err := tx.Exec(r.Context(), `INSERT INTO prompt_evaluation_cases(evaluation_set_id,input,match_type,expected,position) VALUES($1,$2,$3,$4,$5)`, setID, input, item.MatchType, item.Expected, index); err != nil {
				return err
			}
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id,actor_user_id,actor_type,action,resource_type,resource_id,after_state)
			VALUES($1,$2,'user','evaluation_set.updated','evaluation_set',$3,$4)
		`, principal.OrganizationID, principal.UserID, setID.String(), auditJSON(map[string]any{"asset_id": assetID.String(), "name": body.Name, "cases": len(body.Cases)}))
		return err
	})
	if err != nil {
		writeAssetError(w, err, "EVALUATION_SET_UPDATE_FAILED", "update-evaluation-set-failed")
		return
	}
	adminOK(w, nil)
}
