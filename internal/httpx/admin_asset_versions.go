package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pmezard/go-difflib/difflib"

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
		return item, assetError(http.StatusNotFound, "ASSET_VERSION_NOT_FOUND", "版本不存在或不属于当前组织")
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
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_VERSION_FAILED", "查询版本失败")
		return
	}
	versionID, parseErr := uuid.Parse(chi.URLParam(r, "versionID"))
	if parseErr != nil {
		adminError(w, http.StatusBadRequest, "ASSET_VERSION_ID_INVALID", "版本 ID 不合法", nil)
		return
	}
	item, err := queryAssetVersion(r.Context(), d.Pool, principal.OrganizationID, assetID, versionID)
	if err != nil {
		writeAssetError(w, err, "ASSET_VERSION_FAILED", "查询版本失败")
		return
	}
	adminOK(w, item)
}

func (d ServerDeps) AssetVersionDiff(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "ASSET_DIFF_FAILED", "生成版本差异失败")
		return
	}
	fromID, fromErr := uuid.Parse(r.URL.Query().Get("from"))
	toID, toErr := uuid.Parse(r.URL.Query().Get("to"))
	if fromErr != nil || toErr != nil {
		adminError(w, http.StatusBadRequest, "ASSET_VERSION_ID_INVALID", "from 与 to 必须是合法版本 ID", nil)
		return
	}
	from, err := queryAssetVersion(r.Context(), d.Pool, principal.OrganizationID, assetID, fromID)
	if err != nil {
		writeAssetError(w, err, "ASSET_DIFF_FAILED", "生成版本差异失败")
		return
	}
	to, err := queryAssetVersion(r.Context(), d.Pool, principal.OrganizationID, assetID, toID)
	if err != nil {
		writeAssetError(w, err, "ASSET_DIFF_FAILED", "生成版本差异失败")
		return
	}
	fromText, err := prettyAssetSpec(from.Spec)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "ASSET_DIFF_FAILED", "无法格式化来源版本", nil)
		return
	}
	toText, err := prettyAssetSpec(to.Spec)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "ASSET_DIFF_FAILED", "无法格式化目标版本", nil)
		return
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(fromText),
		B:        difflib.SplitLines(toText),
		FromFile: fmt.Sprintf("v%d", from.Version),
		ToFile:   fmt.Sprintf("v%d", to.Version),
		Context:  3,
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "ASSET_DIFF_FAILED", "生成版本差异失败", nil)
		return
	}
	if strings.TrimSpace(diff) == "" {
		diff = fmt.Sprintf("--- v%d\n+++ v%d\n@@ 内容一致 @@\n", from.Version, to.Version)
	}
	adminOK(w, map[string]any{"from_version": from.Version, "to_version": to.Version, "diff": diff})
}

func prettyAssetSpec(raw json.RawMessage) (string, error) {
	spec, err := asset.ParsePromptSpec(raw)
	if err != nil {
		agentSpec, agentErr := asset.ParseAgentSpec(raw)
		if agentErr != nil {
			return "", err
		}
		var output strings.Builder
		output.WriteString("智能体配置\n")
		fmt.Fprintf(&output, "提示词模板: %s\n提示词版本: %s\n模型服务: %s\n", agentSpec.PromptTemplateID, agentSpec.PromptVersionID, agentSpec.ModelServiceID)
		fmt.Fprintf(&output, "上下文消息数: %d\n最大输出 Token: %d\n", agentSpec.MaxHistoryMessages, agentSpec.MaxTokens)
		if agentSpec.Temperature != nil {
			fmt.Fprintf(&output, "温度: %.2f\n", *agentSpec.Temperature)
		}
		if agentSpec.TopP != nil {
			fmt.Fprintf(&output, "Top P: %.2f\n", *agentSpec.TopP)
		}
		return output.String(), nil
	}
	var output strings.Builder
	output.WriteString("模板内容\n")
	output.WriteString(spec.Template)
	if !strings.HasSuffix(spec.Template, "\n") {
		output.WriteByte('\n')
	}
	output.WriteString("\n变量设置\n")
	if len(spec.Variables) == 0 {
		output.WriteString("无\n")
	}
	for _, variable := range spec.Variables {
		required := "否"
		if variable.Required {
			required = "是"
		}
		fmt.Fprintf(&output, "- %s | 类型: %s | 必填: %s", variable.Name, promptVariableTypeText(variable.Type), required)
		if variable.Description != "" {
			description := strings.ReplaceAll(variable.Description, "\n", " ")
			fmt.Fprintf(&output, " | 说明: %s", description)
		}
		if variable.Default != nil {
			value, err := json.Marshal(variable.Default)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&output, " | 默认值: %s", value)
		}
		output.WriteByte('\n')
	}
	output.WriteString("\n模型服务\n")
	if spec.ModelServiceID == "" {
		output.WriteString("未绑定\n")
	} else {
		output.WriteString(spec.ModelServiceID + "\n")
	}
	return output.String(), nil
}

func promptVariableTypeText(value string) string {
	switch value {
	case "number":
		return "数字"
	case "integer":
		return "整数"
	case "boolean":
		return "布尔"
	default:
		return "文本"
	}
}

func (d ServerDeps) RenderAssetPreview(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	assetID, err := parseAssetID(r)
	if err != nil {
		writeAssetError(w, err, "PROMPT_PREVIEW_FAILED", "渲染预览失败")
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
			adminError(w, http.StatusBadRequest, "ASSET_VERSION_ID_INVALID", "版本 ID 不合法", nil)
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
		adminError(w, http.StatusNotFound, "ASSET_VERSION_NOT_FOUND", "版本不存在或不属于当前组织", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROMPT_PREVIEW_FAILED", "查询预览版本失败", nil)
		return
	}
	spec, _, err := normalizeAssetSpec(rawSpec)
	if err != nil {
		writeAssetError(w, err, "PROMPT_PREVIEW_FAILED", "渲染预览失败")
		return
	}
	rendered, err := asset.RenderPrompt(spec, body.Variables)
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROMPT_RENDER_INVALID", err.Error(), nil)
		return
	}
	if _, err := d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
		VALUES ($1,$2,'user','prompt_template.previewed','asset',$3,$4)
	`, principal.OrganizationID, principal.UserID, assetID.String(), auditJSON(map[string]any{
		"version_id": versionID.String(), "version": version,
	})); err != nil {
		adminError(w, http.StatusInternalServerError, "PROMPT_PREVIEW_AUDIT_FAILED", "渲染已完成但审计写入失败,请重试", nil)
		return
	}
	adminOK(w, map[string]any{
		"version_id": versionID.String(), "version": version, "rendered": rendered,
		"model_service_id": spec.ModelServiceID,
	})
}
