package httpx

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
		adminError(w, http.StatusBadRequest, "RESOURCE_STATUS_INVALID", "status-supports-only-enabled-or-disabled", nil)
		return "", false
	}
	return body.Status, true
}

func (d ServerDeps) updateStatusWithAudit(ctx context.Context, sql string, args []any, organizationID, userID uuid.UUID, action, resourceType, resourceID, status string) error {
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return writeAuditEvent(ctx, tx, organizationID, userID, action, resourceType, resourceID, nil, map[string]any{"status": status})
	})
}

func (d ServerDeps) UpdateProviderAccountStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_ID_INVALID", "invalid-account-id", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	err = d.updateStatusWithAudit(r.Context(),
		`UPDATE provider_accounts SET status = $3, updated_at = now() WHERE id = $1 AND organization_id = $2`,
		[]any{accountID, principal.OrganizationID, status},
		principal.OrganizationID, principal.UserID, "provider_account.status_changed", "provider_account", accountID.String(), status)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "account-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_STATUS_FAILED", "failed-to-update-the-account-status", nil)
		return
	}
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
		adminError(w, http.StatusBadRequest, "SERVICE_ID_INVALID", "invalid-model-service-id", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	err = d.updateStatusWithAudit(r.Context(),
		`UPDATE model_services SET status = $3, updated_at = now() WHERE id = $1 AND organization_id = $2`,
		[]any{serviceID, principal.OrganizationID, status},
		principal.OrganizationID, principal.UserID, "model_service.status_changed", "model_service", serviceID.String(), status)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "SERVICE_NOT_FOUND", "model-service-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "SERVICE_STATUS_FAILED", "failed-to-update-the-model-service-gd9ec9", nil)
		return
	}
	adminOK(w, nil)
}

func (d ServerDeps) UpdateModelCandidateStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	candidateID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "CANDIDATE_ID_INVALID", "invalid-candidate-model-id", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	err = d.updateStatusWithAudit(r.Context(), `
		UPDATE model_candidates SET status = $3 WHERE id = $1 AND retired_at IS NULL AND model_service_id IN (
			SELECT id FROM model_services WHERE organization_id = $2
		)
	`, []any{candidateID, principal.OrganizationID, status},
		principal.OrganizationID, principal.UserID, "model_candidate.status_changed", "model_candidate", candidateID.String(), status)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "CANDIDATE_NOT_FOUND", "candidate-model-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "CANDIDATE_STATUS_FAILED", "failed-to-update-the-candidate-model", nil)
		return
	}
	adminOK(w, nil)
}

func (d ServerDeps) UpdateProjectStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROJECT_ID_INVALID", "invalid-project-id", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	err = d.updateStatusWithAudit(r.Context(),
		`UPDATE projects SET status = $3, updated_at = now() WHERE id = $1 AND organization_id = $2`,
		[]any{id, principal.OrganizationID, status},
		principal.OrganizationID, principal.UserID, "project.status_changed", "project", id.String(), status)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROJECT_STATUS_FAILED", "failed-to-update-the-project-status", nil)
		return
	}
	adminOK(w, nil)
}

func (d ServerDeps) UpdateApplicationStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "APPLICATION_ID_INVALID", "invalid-application-id", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	err = d.updateStatusWithAudit(r.Context(),
		`UPDATE applications SET status = $3, updated_at = now() WHERE id = $1 AND project_id IN (SELECT id FROM projects WHERE organization_id = $2)`,
		[]any{id, principal.OrganizationID, status},
		principal.OrganizationID, principal.UserID, "application.status_changed", "application", id.String(), status)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "APPLICATION_NOT_FOUND", "application-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "APPLICATION_STATUS_FAILED", "failed-to-update-the-application-status", nil)
		return
	}
	adminOK(w, nil)
}

func (d ServerDeps) UpdateRoutePolicyStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "POLICY_ID_INVALID", "invalid-route-policy-id", nil)
		return
	}
	status, ok := parseResourceStatus(w, r, d)
	if !ok {
		return
	}
	err = d.updateStatusWithAudit(r.Context(),
		`UPDATE route_policies SET status = $3, updated_at = now() WHERE id = $1 AND organization_id = $2`,
		[]any{id, principal.OrganizationID, status},
		principal.OrganizationID, principal.UserID, "route_policy.status_changed", "route_policy", id.String(), status)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "POLICY_NOT_FOUND", "route-policy-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "POLICY_STATUS_FAILED", "failed-to-update-the-route-policy-10tstjg", nil)
		return
	}
	adminOK(w, nil)
}
