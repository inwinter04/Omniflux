package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/inwinter04/omniflux/internal/asset"
	"github.com/inwinter04/omniflux/internal/auth"
)

type assetResponse struct {
	ID               string    `json:"id"`
	AssetType        string    `json:"asset_type"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	OwnerEmail       string    `json:"owner_email"`
	Status           string    `json:"status"`
	LatestVersion    int       `json:"latest_version"`
	CurrentVersionID *string   `json:"current_version_id"`
	CurrentVersion   *int      `json:"current_version"`
	HasDraft         bool      `json:"has_draft"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type assetVersionResponse struct {
	ID               string          `json:"id"`
	Version          int             `json:"version"`
	Spec             json.RawMessage `json:"spec,omitempty"`
	SourceVersionID  *string         `json:"source_version_id"`
	CreatedByEmail   string          `json:"created_by_email"`
	CreatedAt        time.Time       `json:"created_at"`
	PublishedByEmail *string         `json:"published_by_email"`
	PublishedAt      *time.Time      `json:"published_at"`
	Current          bool            `json:"current"`
}

type assetDetailResponse struct {
	assetResponse
	Spec     json.RawMessage        `json:"spec"`
	Versions []assetVersionResponse `json:"versions"`
}

type createAssetRequest struct {
	AssetType   string          `json:"asset_type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Spec        json.RawMessage `json:"spec"`
}

type updateAssetRequest struct {
	createAssetRequest
	ExpectedVersion int `json:"expected_version"`
}

type assetVersionRequest struct {
	VersionID string `json:"version_id"`
}

type rollbackAssetRequest struct {
	assetVersionRequest
	ExpectedVersion int `json:"expected_version"`
}

type updateAssetStatusRequest struct {
	Status string `json:"status"`
}

type assetAPIError struct {
	status  int
	code    string
	message string
}

func (e *assetAPIError) Error() string { return e.message }

func assetError(status int, code, message string) error {
	return &assetAPIError{status: status, code: code, message: message}
}

func writeAssetError(w http.ResponseWriter, err error, fallbackCode, fallbackMessage string) {
	var apiErr *assetAPIError
	if errors.As(err, &apiErr) {
		adminError(w, apiErr.status, apiErr.code, apiErr.message, nil)
		return
	}
	adminError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage, nil)
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func normalizeAssetSpec(raw json.RawMessage) (asset.PromptSpec, []byte, error) {
	spec, err := asset.ParsePromptSpec(raw)
	if err != nil {
		return spec, nil, assetError(http.StatusBadRequest, "PROMPT_SPEC_INVALID", err.Error())
	}
	canonical, err := json.Marshal(spec)
	if err != nil {
		return spec, nil, err
	}
	return spec, canonical, nil
}

func normalizeTypedAssetSpec(assetType string, raw json.RawMessage) ([]byte, string, error) {
	switch assetType {
	case "prompt_template":
		spec, canonical, err := normalizeAssetSpec(raw)
		return canonical, spec.ModelServiceID, err
	case "agent":
		spec, err := asset.ParseAgentSpec(raw)
		if err != nil {
			return nil, "", assetError(http.StatusBadRequest, "AGENT_SPEC_INVALID", err.Error())
		}
		canonical, err := json.Marshal(spec)
		return canonical, spec.ModelServiceID, err
	default:
		return nil, "", assetError(http.StatusBadRequest, "ASSET_TYPE_INVALID", "资产类型仅支持 prompt_template、agent")
	}
}

func validateAssetModelService(ctx context.Context, q rowQuerier, organizationID uuid.UUID, modelServiceID string) error {
	if modelServiceID == "" {
		return nil
	}
	serviceID, _ := uuid.Parse(modelServiceID)
	var exists bool
	if err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM model_services WHERE id = $1 AND organization_id = $2
		)
	`, serviceID, organizationID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return assetError(http.StatusBadRequest, "ASSET_MODEL_SERVICE_NOT_FOUND", "绑定的模型服务不存在或不属于当前组织")
	}
	return nil
}

func validateTypedAssetReferences(ctx context.Context, q rowQuerier, organizationID uuid.UUID, assetType string, raw json.RawMessage) error {
	if assetType != "agent" {
		return nil
	}
	spec, err := asset.ParseAgentSpec(raw)
	if err != nil {
		return assetError(http.StatusBadRequest, "AGENT_SPEC_INVALID", err.Error())
	}
	var exists bool
	promptID, _ := uuid.Parse(spec.PromptTemplateID)
	versionID, _ := uuid.Parse(spec.PromptVersionID)
	if err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM asset_versions v
			JOIN assets a ON a.id = v.asset_id
			WHERE v.id = $1 AND a.id = $2 AND a.organization_id = $3
			  AND a.asset_type = 'prompt_template' AND a.status <> 'archived'
		)
	`, versionID, promptID, organizationID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return assetError(http.StatusBadRequest, "AGENT_PROMPT_VERSION_NOT_FOUND", "引用的提示词版本不存在、已归档或不属于当前组织")
	}
	return nil
}

func validateAssetMetadata(name, description string) (string, string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return "", "", assetError(http.StatusBadRequest, "ASSET_NAME_REQUIRED", "请填写资产名称")
	}
	if len([]rune(name)) > 120 {
		return "", "", assetError(http.StatusBadRequest, "ASSET_NAME_TOO_LONG", "资产名称不能超过 120 字符")
	}
	if len([]rune(description)) > 2000 {
		return "", "", assetError(http.StatusBadRequest, "ASSET_DESCRIPTION_TOO_LONG", "资产描述不能超过 2000 字符")
	}
	return name, description, nil
}

func parseAssetID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, assetError(http.StatusBadRequest, "ASSET_ID_INVALID", "资产 ID 不合法")
	}
	return id, nil
}

func (d ServerDeps) Assets(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	assetType := r.URL.Query().Get("type")
	if assetType != "" && assetType != "prompt_template" && assetType != "agent" {
		adminError(w, http.StatusBadRequest, "ASSET_TYPE_INVALID", "资产类型仅支持 prompt_template、agent", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT a.id, a.asset_type, a.name, a.description, u.email, a.status,
		       a.latest_version, cv.id, cv.version, a.created_at, a.updated_at
		FROM assets a
		JOIN users u ON u.id = a.owner_user_id
		LEFT JOIN asset_versions cv ON cv.id = a.current_version_id
		WHERE a.organization_id = $1 AND ($2 = '' OR a.asset_type = $2)
		ORDER BY a.updated_at DESC
	`, principal.OrganizationID, assetType)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "ASSET_LIST_FAILED", "无法查询资产", nil)
		return
	}
	defer rows.Close()
	list := []assetResponse{}
	for rows.Next() {
		item, err := scanAssetResponse(rows.Scan)
		if err != nil {
			adminError(w, http.StatusInternalServerError, "ASSET_LIST_FAILED", "无法读取资产", nil)
			return
		}
		list = append(list, item)
	}
	adminOK(w, list)
}

func scanAssetResponse(scan func(dest ...any) error) (assetResponse, error) {
	var item assetResponse
	var id uuid.UUID
	var currentID *uuid.UUID
	err := scan(&id, &item.AssetType, &item.Name, &item.Description, &item.OwnerEmail, &item.Status,
		&item.LatestVersion, &currentID, &item.CurrentVersion, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	item.ID = id.String()
	if currentID != nil {
		value := currentID.String()
		item.CurrentVersionID = &value
	}
	item.HasDraft = item.CurrentVersion == nil || *item.CurrentVersion != item.LatestVersion
	return item, nil
}

func (d ServerDeps) AssetDetail(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_ID_INVALID", "资产 ID 不合法")
		return
	}
	var detail assetDetailResponse
	var currentID *uuid.UUID
	var baseID uuid.UUID
	err = d.Pool.QueryRow(r.Context(), `
		SELECT a.id, a.asset_type, a.name, a.description, u.email, a.status,
		       a.latest_version, cv.id, cv.version, a.created_at, a.updated_at, latest.spec
		FROM assets a
		JOIN users u ON u.id = a.owner_user_id
		JOIN asset_versions latest ON latest.asset_id = a.id AND latest.version = a.latest_version
		LEFT JOIN asset_versions cv ON cv.id = a.current_version_id
		WHERE a.id = $1 AND a.organization_id = $2
	`, assetID, principal.OrganizationID).Scan(&baseID, &detail.AssetType, &detail.Name, &detail.Description,
		&detail.OwnerEmail, &detail.Status, &detail.LatestVersion, &currentID, &detail.CurrentVersion,
		&detail.CreatedAt, &detail.UpdatedAt, &detail.Spec)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在或不属于当前组织", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "ASSET_DETAIL_FAILED", "无法查询资产详情", nil)
		return
	}
	detail.ID = baseID.String()
	if currentID != nil {
		value := currentID.String()
		detail.CurrentVersionID = &value
	}
	detail.HasDraft = detail.CurrentVersion == nil || *detail.CurrentVersion != detail.LatestVersion
	detail.Versions = []assetVersionResponse{}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT v.id, v.version, v.source_version_id, creator.email, v.created_at,
		       publisher.email, v.published_at, COALESCE(v.id = a.current_version_id, false)
		FROM asset_versions v
		JOIN assets a ON a.id = v.asset_id
		JOIN users creator ON creator.id = v.created_by
		LEFT JOIN users publisher ON publisher.id = v.published_by
		WHERE v.asset_id = $1 AND a.organization_id = $2
		ORDER BY v.version DESC
	`, assetID, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "ASSET_VERSION_LIST_FAILED", "无法查询资产版本", nil)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item assetVersionResponse
		var versionID, sourceID *uuid.UUID
		if err := rows.Scan(&versionID, &item.Version, &sourceID, &item.CreatedByEmail, &item.CreatedAt,
			&item.PublishedByEmail, &item.PublishedAt, &item.Current); err != nil {
			adminError(w, http.StatusInternalServerError, "ASSET_VERSION_LIST_FAILED", "无法读取资产版本", nil)
			return
		}
		if versionID != nil {
			item.ID = versionID.String()
		}
		if sourceID != nil {
			value := sourceID.String()
			item.SourceVersionID = &value
		}
		detail.Versions = append(detail.Versions, item)
	}
	adminOK(w, detail)
}

func (d ServerDeps) CreateAsset(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	var body createAssetRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	name, description, err := validateAssetMetadata(body.Name, body.Description)
	if err != nil {
		writeAssetError(w, err, "ASSET_CREATE_FAILED", "创建资产失败")
		return
	}
	assetType := body.AssetType
	if assetType == "" {
		assetType = "prompt_template"
	}
	canonical, modelServiceID, err := normalizeTypedAssetSpec(assetType, body.Spec)
	if err != nil {
		writeAssetError(w, err, "ASSET_CREATE_FAILED", "创建资产失败")
		return
	}
	if err := validateAssetModelService(r.Context(), d.Pool, principal.OrganizationID, modelServiceID); err != nil {
		writeAssetError(w, err, "ASSET_CREATE_FAILED", "创建资产失败")
		return
	}
	if err := validateTypedAssetReferences(r.Context(), d.Pool, principal.OrganizationID, assetType, canonical); err != nil {
		writeAssetError(w, err, "ASSET_CREATE_FAILED", "创建资产失败")
		return
	}
	assetID, versionID := uuid.New(), uuid.New()
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO assets(id, organization_id, asset_type, name, description, owner_user_id, latest_version)
			VALUES ($1,$2,$3,$4,$5,$6,1)
		`, assetID, principal.OrganizationID, assetType, name, description, principal.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO asset_versions(id, asset_id, version, spec, created_by)
			VALUES ($1,$2,1,$3,$4)
		`, versionID, assetID, canonical, principal.UserID); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
			VALUES ($1,$2,'user','asset.created','asset',$3,$4)
		`, principal.OrganizationID, principal.UserID, assetID.String(), auditJSON(map[string]any{
			"asset_type": assetType, "name": name, "description": description, "version": 1,
		}))
		return err
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "assets_organization_id_asset_type_name_key" {
			adminError(w, http.StatusConflict, "ASSET_NAME_EXISTS", "当前类型下已存在同名资产", nil)
			return
		}
		writeAssetError(w, err, "ASSET_CREATE_FAILED", "创建资产失败")
		return
	}
	adminOK(w, map[string]any{"id": assetID.String(), "version_id": versionID.String(), "version": 1})
}

func (d ServerDeps) UpdateAsset(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_UPDATE_FAILED", "更新资产失败")
		return
	}
	var body updateAssetRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	name, description, err := validateAssetMetadata(body.Name, body.Description)
	if err != nil {
		writeAssetError(w, err, "ASSET_UPDATE_FAILED", "更新资产失败")
		return
	}
	var assetType string
	err = d.Pool.QueryRow(r.Context(), `SELECT asset_type FROM assets WHERE id = $1 AND organization_id = $2`, assetID, principal.OrganizationID).Scan(&assetType)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在或不属于当前组织", nil)
		return
	}
	if err != nil {
		writeAssetError(w, err, "ASSET_UPDATE_FAILED", "更新资产失败")
		return
	}
	canonical, modelServiceID, err := normalizeTypedAssetSpec(assetType, body.Spec)
	if err != nil {
		writeAssetError(w, err, "ASSET_UPDATE_FAILED", "更新资产失败")
		return
	}
	if err := validateAssetModelService(r.Context(), d.Pool, principal.OrganizationID, modelServiceID); err != nil {
		writeAssetError(w, err, "ASSET_UPDATE_FAILED", "更新资产失败")
		return
	}
	if err := validateTypedAssetReferences(r.Context(), d.Pool, principal.OrganizationID, assetType, canonical); err != nil {
		writeAssetError(w, err, "ASSET_UPDATE_FAILED", "更新资产失败")
		return
	}
	versionID := uuid.New()
	var newVersion int
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var status, oldName string
		var latestVersion int
		var latestVersionID uuid.UUID
		err := tx.QueryRow(r.Context(), `
			SELECT a.status, a.name, a.latest_version, latest.id
			FROM assets a
			JOIN asset_versions latest ON latest.asset_id = a.id AND latest.version = a.latest_version
			WHERE a.id = $1 AND a.organization_id = $2
			FOR UPDATE OF a
		`, assetID, principal.OrganizationID).Scan(&status, &oldName, &latestVersion, &latestVersionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return assetError(http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在或不属于当前组织")
		}
		if err != nil {
			return err
		}
		if status == "archived" {
			return assetError(http.StatusConflict, "ASSET_ARCHIVED", "已归档资产不能编辑")
		}
		if body.ExpectedVersion != latestVersion {
			return assetError(http.StatusConflict, "ASSET_VERSION_CONFLICT", fmt.Sprintf("资产已更新到 v%d,请刷新后再保存", latestVersion))
		}
		newVersion = latestVersion + 1
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO asset_versions(id, asset_id, version, spec, source_version_id, created_by)
			VALUES ($1,$2,$3,$4,$5,$6)
		`, versionID, assetID, newVersion, canonical, latestVersionID, principal.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE assets SET name = $3, description = $4, latest_version = $5, updated_at = now()
			WHERE id = $1 AND organization_id = $2
		`, assetID, principal.OrganizationID, name, description, newVersion); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
			VALUES ($1,$2,'user','asset.draft_created','asset',$3,$4,$5)
		`, principal.OrganizationID, principal.UserID, assetID.String(),
			auditJSON(map[string]any{"name": oldName, "version": latestVersion}),
			auditJSON(map[string]any{"name": name, "description": description, "version": newVersion}))
		return err
	})
	if err != nil {
		writeAssetError(w, err, "ASSET_UPDATE_FAILED", "更新资产失败")
		return
	}
	adminOK(w, map[string]any{"version_id": versionID.String(), "version": newVersion})
}

func (d ServerDeps) PublishAsset(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_PUBLISH_FAILED", "发布资产失败")
		return
	}
	var body assetVersionRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	versionID, parseErr := uuid.Parse(body.VersionID)
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "ASSET_VERSION_ID_INVALID", "版本 ID 不合法", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var status string
		var latestVersion, oldCurrentVersion *int
		var currentID *uuid.UUID
		err := tx.QueryRow(r.Context(), `
			SELECT a.status, a.latest_version, a.current_version_id, cv.version
			FROM assets a
			LEFT JOIN asset_versions cv ON cv.id = a.current_version_id
			WHERE a.id = $1 AND a.organization_id = $2
			FOR UPDATE OF a
		`, assetID, principal.OrganizationID).Scan(&status, &latestVersion, &currentID, &oldCurrentVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return assetError(http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在或不属于当前组织")
		}
		if err != nil {
			return err
		}
		if status == "archived" {
			return assetError(http.StatusConflict, "ASSET_ARCHIVED", "已归档资产不能发布")
		}
		var targetVersion int
		var publishedAt *time.Time
		if err := tx.QueryRow(r.Context(), `
			SELECT version, published_at FROM asset_versions
			WHERE id = $1 AND asset_id = $2 FOR UPDATE
		`, versionID, assetID).Scan(&targetVersion, &publishedAt); errors.Is(err, pgx.ErrNoRows) {
			return assetError(http.StatusNotFound, "ASSET_VERSION_NOT_FOUND", "版本不存在")
		} else if err != nil {
			return err
		}
		if latestVersion == nil || targetVersion != *latestVersion {
			return assetError(http.StatusConflict, "ASSET_NOT_LATEST_VERSION", "只能发布最新草稿版本")
		}
		if publishedAt != nil {
			return assetError(http.StatusConflict, "ASSET_VERSION_ALREADY_PUBLISHED", "该版本已发布;历史版本请使用回滚生成新版本")
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE asset_versions SET published_by = $3, published_at = now()
			WHERE id = $1 AND asset_id = $2
		`, versionID, assetID, principal.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE assets SET current_version_id = $3, status = 'published', updated_at = now()
			WHERE id = $1 AND organization_id = $2
		`, assetID, principal.OrganizationID, versionID); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
			VALUES ($1,$2,'user','asset.published','asset',$3,$4,$5)
		`, principal.OrganizationID, principal.UserID, assetID.String(),
			auditJSON(map[string]any{"current_version_id": currentID}),
			auditJSON(map[string]any{"version_id": versionID.String(), "version": targetVersion, "status": "published"}))
		return err
	})
	if err != nil {
		writeAssetError(w, err, "ASSET_PUBLISH_FAILED", "发布资产失败")
		return
	}
	adminOK(w, map[string]any{"version_id": versionID.String()})
}

func (d ServerDeps) RollbackAsset(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_ROLLBACK_FAILED", "回滚资产失败")
		return
	}
	var body rollbackAssetRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	sourceID, parseErr := uuid.Parse(body.VersionID)
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "ASSET_VERSION_ID_INVALID", "版本 ID 不合法", nil)
		return
	}
	newVersionID := uuid.New()
	var newVersion int
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var status string
		var latestVersion int
		err := tx.QueryRow(r.Context(), `
			SELECT status, latest_version FROM assets
			WHERE id = $1 AND organization_id = $2 FOR UPDATE
		`, assetID, principal.OrganizationID).Scan(&status, &latestVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return assetError(http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在或不属于当前组织")
		}
		if err != nil {
			return err
		}
		if status == "archived" {
			return assetError(http.StatusConflict, "ASSET_ARCHIVED", "已归档资产不能回滚")
		}
		if body.ExpectedVersion != latestVersion {
			return assetError(http.StatusConflict, "ASSET_VERSION_CONFLICT", fmt.Sprintf("资产已更新到 v%d,请刷新后再回滚", latestVersion))
		}
		var sourceVersion int
		var spec json.RawMessage
		var publishedAt *time.Time
		if err := tx.QueryRow(r.Context(), `
			SELECT version, spec, published_at FROM asset_versions WHERE id = $1 AND asset_id = $2
		`, sourceID, assetID).Scan(&sourceVersion, &spec, &publishedAt); errors.Is(err, pgx.ErrNoRows) {
			return assetError(http.StatusNotFound, "ASSET_VERSION_NOT_FOUND", "版本不存在")
		} else if err != nil {
			return err
		}
		if publishedAt == nil {
			return assetError(http.StatusConflict, "ASSET_VERSION_NOT_PUBLISHED", "只能回滚到已发布的历史版本")
		}
		newVersion = latestVersion + 1
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO asset_versions(id, asset_id, version, spec, source_version_id, created_by, published_by, published_at)
			VALUES ($1,$2,$3,$4,$5,$6,$6,now())
		`, newVersionID, assetID, newVersion, spec, sourceID, principal.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE assets SET latest_version = $3, current_version_id = $4, status = 'published', updated_at = now()
			WHERE id = $1 AND organization_id = $2
		`, assetID, principal.OrganizationID, newVersion, newVersionID); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
			VALUES ($1,$2,'user','asset.rolled_back','asset',$3,$4,$5)
		`, principal.OrganizationID, principal.UserID, assetID.String(),
			auditJSON(map[string]any{"version": latestVersion}),
			auditJSON(map[string]any{"source_version": sourceVersion, "version": newVersion, "version_id": newVersionID.String()}))
		return err
	})
	if err != nil {
		writeAssetError(w, err, "ASSET_ROLLBACK_FAILED", "回滚资产失败")
		return
	}
	adminOK(w, map[string]any{"version_id": newVersionID.String(), "version": newVersion})
}

func (d ServerDeps) UpdateAssetStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_STATUS_FAILED", "更新资产状态失败")
		return
	}
	var body updateAssetStatusRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	switch body.Status {
	case "published", "disabled", "archived":
	default:
		adminError(w, http.StatusBadRequest, "ASSET_STATUS_INVALID", "状态仅支持 published、disabled、archived", nil)
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var oldStatus string
		var currentVersionID *uuid.UUID
		err := tx.QueryRow(r.Context(), `
			SELECT status, current_version_id FROM assets
			WHERE id = $1 AND organization_id = $2 FOR UPDATE
		`, assetID, principal.OrganizationID).Scan(&oldStatus, &currentVersionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return assetError(http.StatusNotFound, "ASSET_NOT_FOUND", "资产不存在或不属于当前组织")
		}
		if err != nil {
			return err
		}
		if oldStatus == "archived" {
			return assetError(http.StatusConflict, "ASSET_ARCHIVED", "已归档资产不能变更状态")
		}
		if body.Status == "published" && currentVersionID == nil {
			return assetError(http.StatusConflict, "ASSET_NOT_PUBLISHED", "没有可启用的已发布版本")
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE assets SET status = $3, updated_at = now()
			WHERE id = $1 AND organization_id = $2
		`, assetID, principal.OrganizationID, body.Status); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
			VALUES ($1,$2,'user','asset.status_changed','asset',$3,$4,$5)
		`, principal.OrganizationID, principal.UserID, assetID.String(),
			auditJSON(map[string]any{"status": oldStatus}), auditJSON(map[string]any{"status": body.Status}))
		return err
	})
	if err != nil {
		writeAssetError(w, err, "ASSET_STATUS_FAILED", "更新资产状态失败")
		return
	}
	adminOK(w, nil)
}
