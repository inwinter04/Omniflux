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

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/vault"
)

var allowedProviders = map[string]bool{
	"openai": true, "azure_openai": true, "anthropic": true, "gemini": true,
	"vertex": true, "bedrock": true, "openai_compatible": true,
}

type providerAccountResponse struct {
	// 近 24 小时被动健康统计,来自 usage_records;无流量时为 null。
	ID             string    `json:"id"`
	Provider       string    `json:"provider"`
	Name           string    `json:"name"`
	Region         *string   `json:"region"`
	BaseURL        *string   `json:"base_url"`
	Status         string    `json:"status"`
	Requests24h    int64     `json:"requests_24h"`
	SuccessRate24h *float64  `json:"success_rate_24h"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
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
	if !ok || d.requireViewer(w, principal) != nil {
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
	if err := rows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "无法查询 Provider 账号", nil)
		return
	}
	// 近 24 小时被动健康统计:路由已按成功率自动降权,这里补足可见性。
	healthRows, err := d.Pool.Query(r.Context(), `
		SELECT a.id,
		       count(u.id),
		       avg(CASE WHEN u.state = 'success' THEN 1.0 ELSE 0.0 END)
		FROM provider_accounts a
		LEFT JOIN model_candidates mc ON mc.provider_account_id = a.id
		LEFT JOIN usage_records u ON u.model_candidate_id = mc.id
		     AND u.created_at >= now() - interval '24 hours'
		     AND u.state <> 'pending'
		WHERE a.organization_id = $1
		GROUP BY a.id
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "无法查询 Provider 健康统计", nil)
		return
	}
	defer healthRows.Close()
	health := map[uuid.UUID]struct {
		Requests    int64
		SuccessRate *float64
	}{}
	for healthRows.Next() {
		var id uuid.UUID
		var h struct {
			Requests    int64
			SuccessRate *float64
		}
		if err := healthRows.Scan(&id, &h.Requests, &h.SuccessRate); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "无法查询 Provider 健康统计", nil)
			return
		}
		health[id] = h
	}
	for j := range list {
		var id uuid.UUID
		if err := id.UnmarshalText([]byte(list[j].ID)); err != nil {
			continue
		}
		if h, ok := health[id]; ok {
			list[j].Requests24h = h.Requests
			list[j].SuccessRate24h = h.SuccessRate
		}
	}
	adminOK(w, list)
}

// CreateProviderAccount 创建 Provider 账号并把 API Key 用主密钥加密入库。
func (d ServerDeps) CreateProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
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
	if !ok || d.requireProjectAdmin(w, principal) != nil {
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
	if !ok || d.requireProjectAdmin(w, principal) != nil {
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

type providerTestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// TestProviderAccount 用账号凭据向上游发起轻量列表请求,验证连通性与凭据有效性。
// azure_openai/vertex/bedrock 的认证模型特殊,暂不支持在线测试。
func (d ServerDeps) TestProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_ID_INVALID", "Provider 账号 ID 不合法", nil)
		return
	}
	var account struct {
		Provider string
		BaseURL  *string
	}
	err = d.Pool.QueryRow(r.Context(), `
		SELECT provider, base_url FROM provider_accounts WHERE id = $1 AND organization_id = $2
	`, accountID, principal.OrganizationID).Scan(&account.Provider, &account.BaseURL)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "Provider 账号不存在或不属于当前组织", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_TEST_FAILED", "连通性测试失败", nil)
		return
	}
	var ciphertext []byte
	var keyID string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT ciphertext, key_id FROM provider_credentials WHERE provider_account_id = $1
	`, accountID).Scan(&ciphertext, &keyID)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_CREDENTIAL_NOT_FOUND", "账号没有已保存的凭据", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_TEST_FAILED", "连通性测试失败", nil)
		return
	}
	plaintext, err := vault.Decrypt(d.MasterKey, ciphertext)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_TEST_FAILED", "凭据解密失败,请检查主密钥配置", nil)
		return
	}
	apiKey := string(plaintext)

	testURL, headers, err := providerProbeRequest(account.Provider, account.BaseURL, apiKey)
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_TEST_UNSUPPORTED",
			"该 provider 暂不支持在线连通性测试,请通过实际调用验证", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_TEST_FAILED", "连通性测试失败", nil)
		return
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		adminOK(w, providerTestResult{OK: false, Message: "无法连接上游: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		adminOK(w, providerTestResult{OK: true, Message: fmt.Sprintf("连通正常(HTTP %d)", resp.StatusCode)})
		return
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		adminOK(w, providerTestResult{OK: false, Message: fmt.Sprintf("凭据被拒绝(HTTP %d),请检查 API Key", resp.StatusCode)})
		return
	}
	adminOK(w, providerTestResult{OK: false, Message: fmt.Sprintf("上游返回 HTTP %d", resp.StatusCode)})
}

// providerProbeRequest 按 provider 构造轻量探测请求;不支持的 provider 返回错误。
func providerProbeRequest(provider string, baseURL *string, apiKey string) (string, map[string]string, error) {
	switch provider {
	case "openai", "openai_compatible":
		base := "https://api.openai.com/v1"
		if baseURL != nil && *baseURL != "" {
			base = strings.TrimRight(*baseURL, "/")
		}
		return base + "/models", map[string]string{"Authorization": "Bearer " + apiKey}, nil
	case "anthropic":
		return "https://api.anthropic.com/v1/models", map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		}, nil
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/models?key=" + apiKey, nil, nil
	default:
		return "", nil, fmt.Errorf("provider %s does not support online probe", provider)
	}
}
