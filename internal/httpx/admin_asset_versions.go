package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/asset"
	"github.com/inwinter04/omniflux/internal/auth"
)

type renderAssetRequest struct {
	VersionID string         `json:"version_id"`
	Variables map[string]any `json:"variables"`
}

func queryAssetVersion(ctx context.Context, q rowQuerier, organizationID, assetID, versionID uuid.UUID) (assetVersionResponse, error) {
	var item assetVersionResponse
	var id, sourceID *uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT v.id, v.version, v.spec, v.source_version_id, creator.email, v.created_at,
		       publisher.email, v.published_at, COALESCE(v.id = a.current_version_id, false)
		FROM asset_versions v
		JOIN assets a ON a.id = v.asset_id
		JOIN users creator ON creator.id = v.created_by
		LEFT JOIN users publisher ON publisher.id = v.published_by
		WHERE v.id = $1 AND v.asset_id = $2 AND a.organization_id = $3
	`, versionID, assetID, organizationID).Scan(&id, &item.Version, &item.Spec, &sourceID, &item.CreatedByEmail,
		&item.CreatedAt, &item.PublishedByEmail, &item.PublishedAt, &item.Current)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, assetError(http.StatusNotFound, "ASSET_VERSION_NOT_FOUND", "version-does-not-exist-or-does")
	}
	if err != nil {
		return item, err
	}
	if id != nil {
		item.ID = id.String()
	}
	if sourceID != nil {
		value := sourceID.String()
		item.SourceVersionID = &value
	}
	return item, nil
}

func (d ServerDeps) AssetVersion(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_VERSION_FAILED", "query-asset-version-failed")
		return
	}
	if !d.requireAssetAccess(w, r, principal, assetID, false) {
		return
	}
	versionID, parseErr := uuid.Parse(chi.URLParam(r, "versionID"))
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "ASSET_VERSION_ID_INVALID", "invalid-version-id", nil)
		return
	}
	item, err := queryAssetVersion(r.Context(), d.Pool, principal.OrganizationID, assetID, versionID)
	if err != nil {
		writeAssetError(w, err, "ASSET_VERSION_FAILED", "query-asset-version-failed")
		return
	}
	adminOK(w, item)
}

func (d ServerDeps) AssetVersionDiff(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_DIFF_FAILED", "failed-to-generate-the-version-diff")
		return
	}
	if !d.requireAssetAccess(w, r, principal, assetID, false) {
		return
	}
	fromID, fromErr := uuid.Parse(r.URL.Query().Get("from"))
	toID, toErr := uuid.Parse(r.URL.Query().Get("to"))
	if fromErr != nil || toErr != nil {
		adminError(w, http.StatusBadRequest, "ASSET_VERSION_ID_INVALID", "from-and-to-must-be-valid", nil)
		return
	}
	from, err := queryAssetVersion(r.Context(), d.Pool, principal.OrganizationID, assetID, fromID)
	if err != nil {
		writeAssetError(w, err, "ASSET_DIFF_FAILED", "failed-to-generate-the-version-diff")
		return
	}
	to, err := queryAssetVersion(r.Context(), d.Pool, principal.OrganizationID, assetID, toID)
	if err != nil {
		writeAssetError(w, err, "ASSET_DIFF_FAILED", "failed-to-generate-the-version-diff")
		return
	}
	var assetType string
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT asset_type FROM assets WHERE id = $1 AND organization_id = $2
	`, assetID, principal.OrganizationID).Scan(&assetType); err != nil {
		writeAssetError(w, err, "ASSET_DIFF_FAILED", "failed-to-generate-the-version-diff")
		return
	}
	adminOK(w, map[string]any{
		"from_version": from.Version,
		"to_version":   to.Version,
		"asset_type":   assetType,
		"from_spec":    from.Spec,
		"to_spec":      to.Spec,
	})
}

func (d ServerDeps) RenderAssetPreview(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermPromptRead) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "PROMPT_PREVIEW_FAILED", "render-preview-failed")
		return
	}
	var body renderAssetRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	var versionID uuid.UUID
	var version int
	var rawSpec json.RawMessage
	if body.VersionID != "" {
		parsed, parseErr := uuid.Parse(body.VersionID)
		if parseErr != nil {
			adminError(w, http.StatusBadRequest, "ASSET_VERSION_ID_INVALID", "invalid-version-id", nil)
			return
		}
		versionID = parsed
		err = d.Pool.QueryRow(r.Context(), `
			SELECT v.id, v.version, v.spec FROM asset_versions v
			JOIN assets a ON a.id = v.asset_id
			WHERE v.id = $1 AND v.asset_id = $2 AND a.organization_id = $3
		`, versionID, assetID, principal.OrganizationID).Scan(&versionID, &version, &rawSpec)
	} else {
		err = d.Pool.QueryRow(r.Context(), `
			SELECT v.id, v.version, v.spec FROM asset_versions v
			JOIN assets a ON a.id = v.asset_id
			WHERE v.asset_id = $1 AND a.organization_id = $2 AND v.version = a.latest_version
		`, assetID, principal.OrganizationID).Scan(&versionID, &version, &rawSpec)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "ASSET_VERSION_NOT_FOUND", "version-does-not-exist-or-does", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROMPT_PREVIEW_FAILED", "failed-to-query-the-preview-version", nil)
		return
	}
	spec, _, err := normalizeAssetSpec(rawSpec)
	if err != nil {
		writeAssetError(w, err, "PROMPT_PREVIEW_FAILED", "render-preview-failed")
		return
	}
	rendered, err := asset.RenderPrompt(spec, body.Variables)
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROMPT_RENDER_INVALID", "prompt-render-invalid", nil)
		return
	}
	if _, err := d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
		VALUES ($1,$2,'user','prompt_template.previewed','asset',$3,$4)
	`, principal.OrganizationID, principal.UserID, assetID.String(), auditJSON(map[string]any{
		"version_id": versionID.String(), "version": version,
	})); err != nil {
		adminError(w, http.StatusInternalServerError, "PROMPT_PREVIEW_AUDIT_FAILED", "rendering-completed-but-writing-the-audit", nil)
		return
	}
	adminOK(w, map[string]any{
		"version_id": versionID.String(), "version": version, "rendered": rendered,
		"model_service_id": spec.ModelServiceID,
	})
}
