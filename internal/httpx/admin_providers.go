package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/vault"
)

var allowedProviders = map[string]bool{
	"openai": true, "azure_openai": true, "anthropic": true, "gemini": true,
	"vertex": true, "bedrock": true, "openai_compatible": true,
}

type providerAccountResponse struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Name      string    `json:"name"`
	Region    *string   `json:"region"`
	BaseURL   *string   `json:"base_url"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type createProviderAccountRequest struct {
	Provider string  `json:"provider"`
	Name     string  `json:"name"`
	Region   *string `json:"region"`
	BaseURL  *string `json:"base_url"`
	APIKey   string  `json:"api_key"`
}

// ProviderAccounts 返回当前组织的 Provider 账号列表(不含凭据)。
func (d ServerDeps) ProviderAccounts(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, provider, name, region, base_url, status, created_at, updated_at
		FROM provider_accounts WHERE organization_id = $1 ORDER BY created_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "无法查询 Provider 账号", nil)
		return
	}
	defer rows.Close()
	list := []providerAccountResponse{}
	for rows.Next() {
		var item providerAccountResponse
		var id uuid.UUID
		if err := rows.Scan(&id, &item.Provider, &item.Name, &item.Region, &item.BaseURL, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "无法查询 Provider 账号", nil)
			return
		}
		item.ID = id.String()
		list = append(list, item)
	}
	adminOK(w, list)
}

// CreateProviderAccount 创建 Provider 账号并把 API Key 用主密钥加密入库。
func (d ServerDeps) CreateProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	var body createProviderAccountRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Provider = strings.TrimSpace(body.Provider)
	body.Name = strings.TrimSpace(body.Name)
	if !allowedProviders[body.Provider] {
		adminError(w, http.StatusBadRequest, "PROVIDER_UNSUPPORTED", "不支持的 provider 类型", nil)
		return
	}
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "PROVIDER_NAME_REQUIRED", "请填写账号名称", nil)
		return
	}
	if strings.TrimSpace(body.APIKey) == "" {
		adminError(w, http.StatusBadRequest, "PROVIDER_KEY_REQUIRED", "请填写 API Key", nil)
		return
	}
	// 引擎要求同一 provider 的 base URL 不能冲突,创建前提前拦截并说明原因。
	if body.BaseURL != nil && *body.BaseURL != "" {
		var count int
		if err := d.Pool.QueryRow(r.Context(), `
			SELECT count(*) FROM provider_accounts
			WHERE organization_id = $1 AND provider = $2 AND base_url IS NOT NULL AND base_url <> $3
		`, principal.OrganizationID, body.Provider, *body.BaseURL).Scan(&count); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_CREATE_FAILED", "创建 Provider 账号失败", nil)
			return
		}
		if count > 0 {
			adminError(w, http.StatusConflict, "PROVIDER_BASE_URL_CONFLICT",
				"同一 provider 下已存在不同 base URL 的账号;当前引擎要求同一 provider 使用相同 base URL", nil)
			return
		}
	}
	ciphertext, err := vault.Encrypt(d.MasterKey, []byte(body.APIKey))
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_CREATE_FAILED", "凭据加密失败", nil)
		return
	}
	accountID := uuid.New()
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO provider_accounts(id, organization_id, provider, name, region, base_url)
			VALUES ($1,$2,$3,$4,$5,$6)
		`, accountID, principal.OrganizationID, body.Provider, body.Name, body.Region, body.BaseURL); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO provider_credentials(provider_account_id, key_id, ciphertext)
			VALUES ($1,$2,$3)
		`, accountID, vault.KeyID, ciphertext); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, after_state)
			VALUES ($1,$2,'user','provider_account.created','provider_account',$3,$4)
		`, principal.OrganizationID, principal.UserID, accountID.String(), auditJSON(map[string]any{"provider": body.Provider, "name": body.Name}))
		return err
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "provider_accounts_organization_id_name_key" {
			adminError(w, http.StatusConflict, "PROVIDER_NAME_EXISTS", "同名 Provider 账号已存在", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PROVIDER_CREATE_FAILED", "创建 Provider 账号失败", nil)
		return
	}
	if !d.reloadEngine(w, r) {
		return
	}
	adminOK(w, map[string]string{"id": accountID.String()})
}

// DeleteProviderAccount 删除 Provider 账号;被候选引用时拒绝删除并说明。
func (d ServerDeps) DeleteProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_INVALID_ID", "账号 ID 不合法", nil)
		return
	}
	var count int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM model_candidates WHERE provider_account_id = $1
	`, accountID).Scan(&count); err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_DELETE_FAILED", "删除 Provider 账号失败", nil)
		return
	}
	if count > 0 {
		adminError(w, http.StatusConflict, "PROVIDER_IN_USE", "该账号仍被模型候选引用,请先下线相关候选", map[string]int{"candidate_count": count})
		return
	}
	tag, err := d.Pool.Exec(r.Context(), `
		DELETE FROM provider_accounts WHERE id = $1 AND organization_id = $2
	`, accountID, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_DELETE_FAILED", "删除 Provider 账号失败", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "Provider 账号不存在", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id)
		VALUES ($1,$2,'user','provider_account.deleted','provider_account',$3)
	`, principal.OrganizationID, principal.UserID, accountID.String())
	if !d.reloadEngine(w, r) {
		return
	}
	adminOK(w, map[string]string{"status": "deleted"})
}

// reloadEngine 在 Provider 配置变化后重建上游引擎;返回 false 表示失败且已写出错误响应。
func (d ServerDeps) reloadEngine(w http.ResponseWriter, r *http.Request) bool {
	if d.ReloadEngine == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	defer cancel()
	if err := d.ReloadEngine(ctx); err != nil {
		adminError(w, http.StatusInternalServerError, "ENGINE_RELOAD_FAILED",
			"Provider 配置已保存,但上游引擎重载失败,请检查配置后重试或重启服务", nil)
		return false
	}
	return true
}

func auditJSON(value map[string]any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return data
}

type updateProviderAccountRequest struct {
	Name    string  `json:"name"`
	Region  *string `json:"region"`
	BaseURL *string `json:"base_url"`
}

// UpdateProviderAccount 更新账号名称/区域/Base URL;provider 类型与凭据不可修改。
func (d ServerDeps) UpdateProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireAdmin(w, principal) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_ID_INVALID", "Provider 账号 ID 不合法", nil)
		return
	}
	var body updateProviderAccountRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "PROVIDER_NAME_REQUIRED", "请填写账号名称", nil)
		return
	}
	// base_url 更新沿用创建时的引擎约束:同 provider 下不允许出现不同 base URL。
	if body.BaseURL != nil && *body.BaseURL != "" {
		var count int
		if err := d.Pool.QueryRow(r.Context(), `
			SELECT count(*) FROM provider_accounts
			WHERE organization_id = $1 AND provider = (SELECT provider FROM provider_accounts WHERE id = $2)
			  AND id <> $2 AND base_url IS NOT NULL AND base_url <> $3
		`, principal.OrganizationID, accountID, *body.BaseURL).Scan(&count); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_UPDATE_FAILED", "更新 Provider 账号失败", nil)
			return
		}
		if count > 0 {
			adminError(w, http.StatusConflict, "PROVIDER_BASE_URL_CONFLICT",
				"同一 provider 下已存在不同 base URL 的账号;当前引擎要求同一 provider 使用相同 base URL", nil)
			return
		}
	}
	var oldName string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT name FROM provider_accounts WHERE id = $1 AND organization_id = $2
	`, accountID, principal.OrganizationID).Scan(&oldName)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "Provider 账号不存在或不属于当前组织", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_UPDATE_FAILED", "更新 Provider 账号失败", nil)
		return
	}
	if _, err := d.Pool.Exec(r.Context(), `
		UPDATE provider_accounts SET name = $3, region = $4, base_url = $5, updated_at = now()
		WHERE id = $1 AND organization_id = $2
	`, accountID, principal.OrganizationID, body.Name, body.Region, body.BaseURL); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "provider_accounts_organization_id_name_key" {
			adminError(w, http.StatusConflict, "PROVIDER_NAME_EXISTS", "同名 Provider 账号已存在", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PROVIDER_UPDATE_FAILED", "更新 Provider 账号失败", nil)
		return
	}
	_, _ = d.Pool.Exec(r.Context(), `
		INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
		VALUES ($1,$2,'user','provider_account.updated','provider_account',$3,$4,$5)
	`, principal.OrganizationID, principal.UserID, accountID.String(),
		auditJSON(map[string]any{"name": oldName}), auditJSON(map[string]any{"name": body.Name, "region": body.Region, "base_url": body.BaseURL}))
	// base_url/凭据相关配置变化需要重建上游引擎。
	if !d.reloadEngine(w, r) {
		return
	}
	adminOK(w, nil)
}

