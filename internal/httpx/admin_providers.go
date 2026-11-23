package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/vault"
)

var allowedProviders = map[string]bool{
	"openai": true, "azure_openai": true, "anthropic": true, "gemini": true,
	"vertex": true, "bedrock": true, "openai_compatible": true,
}

// v1PathProviders 标记 Bifrost 会在 base_url 后拼接 /v1/... 路径的 provider;
// 这类 provider 的 base_url 以 /v1 结尾时会拼出 /v1/v1/... 必然 404。
var v1PathProviders = map[string]bool{
	"openai": true, "openai_compatible": true, "anthropic": true,
}

type providerAccountResponse struct {
	// 近 24 小时被动健康统计,来自 usage_records;无流量时为 null。
	ID               string     `json:"id"`
	Provider         string     `json:"provider"`
	Name             string     `json:"name"`
	Region           *string    `json:"region"`
	BaseURL          *string    `json:"base_url"`
	BillingCurrency  string     `json:"billing_currency"`
	Status           string     `json:"status"`
	Requests24h      int64      `json:"requests_24h"`
	SuccessRate24h   *float64   `json:"success_rate_24h"`
	AvgLatencyMS24h  *float64   `json:"avg_latency_ms_24h"`
	LastUsedAt       *time.Time `json:"last_used_at"`
	ActiveCandidates int        `json:"active_candidates"`
	HealthStatus     string     `json:"health_status"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type createProviderAccountRequest struct {
	Provider        string  `json:"provider"`
	Name            string  `json:"name"`
	Region          *string `json:"region"`
	BaseURL         *string `json:"base_url"`
	BillingCurrency string  `json:"billing_currency"`
	APIKey          string  `json:"api_key"`
}

// ProviderAccounts 返回当前组织的 供应商账号列表(不含凭据)。
func (d ServerDeps) ProviderAccounts(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireViewer(w, principal) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, provider, name, region, base_url, billing_currency, status, created_at, updated_at
		FROM provider_accounts WHERE organization_id = $1 ORDER BY created_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-accounts", nil)
		return
	}
	defer rows.Close()
	list := []providerAccountResponse{}
	for rows.Next() {
		var item providerAccountResponse
		var id uuid.UUID
		if err := rows.Scan(&id, &item.Provider, &item.Name, &item.Region, &item.BaseURL, &item.BillingCurrency, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-accounts", nil)
			return
		}
		item.ID = id.String()
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-accounts", nil)
		return
	}
	// 近 24 小时被动健康统计:路由已按成功率自动降权,这里补足可见性。
	healthRows, err := d.Pool.Query(r.Context(), `
		SELECT a.id,
		       count(u.id),
		       avg(CASE WHEN u.state = 'success' THEN 1.0 ELSE 0.0 END),
		       avg(u.total_ms) FILTER (WHERE u.state = 'success'),
		       max(u.created_at),
		       count(DISTINCT mc.id) FILTER (WHERE mc.status = 'active')
		FROM provider_accounts a
		LEFT JOIN model_candidates mc ON mc.provider_account_id = a.id
		LEFT JOIN usage_records u ON u.model_candidate_id = mc.id
		     AND u.created_at >= now() - interval '24 hours'
		     AND u.state <> 'pending'
		WHERE a.organization_id = $1
		GROUP BY a.id
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-health-statistics", nil)
		return
	}
	defer healthRows.Close()
	health := map[uuid.UUID]struct {
		Requests         int64
		SuccessRate      *float64
		AverageLatencyMS *float64
		LastUsedAt       *time.Time
		ActiveCandidates int
	}{}
	for healthRows.Next() {
		var id uuid.UUID
		var h struct {
			Requests         int64
			SuccessRate      *float64
			AverageLatencyMS *float64
			LastUsedAt       *time.Time
			ActiveCandidates int
		}
		if err := healthRows.Scan(&id, &h.Requests, &h.SuccessRate, &h.AverageLatencyMS, &h.LastUsedAt, &h.ActiveCandidates); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-health-statistics", nil)
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
			list[j].AvgLatencyMS24h = h.AverageLatencyMS
			list[j].LastUsedAt = h.LastUsedAt
			list[j].ActiveCandidates = h.ActiveCandidates
			list[j].HealthStatus = deriveHealthStatus(list[j].Status, h.Requests, h.SuccessRate, h.AverageLatencyMS, h.ActiveCandidates)
		}
	}
	adminOK(w, list)
}

// CreateProviderAccount 创建 供应商账号并把 API Key 用主密钥加密入库。
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
	if !allowedProviders[body.Provider] && bifrostx.VendorByID(body.Provider) == nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_UNSUPPORTED", "unsupported-provider-type", nil)
		return
	}
	// 厂商账号的接口地址由服务端目录内置;仅本地/自部署类厂商允许自定义 base_url。
	if vendor := bifrostx.VendorByID(body.Provider); vendor != nil {
		if body.BaseURL != nil && strings.TrimSpace(*body.BaseURL) != "" && !vendor.EditableURL {
			adminError(w, http.StatusBadRequest, "PROVIDER_BASE_URL_NOT_ALLOWED",
				"provider-base-url-not-allowed", nil)
			return
		}
	}
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "PROVIDER_NAME_REQUIRED", "enter-an-account-name", nil)
		return
	}
	if strings.TrimSpace(body.APIKey) == "" {
		adminError(w, http.StatusBadRequest, "PROVIDER_KEY_REQUIRED", "enter-an-api-key", nil)
		return
	}
	// Bifrost 会在 base_url 后自动拼接 /v1/... 路径;以 /v1 结尾会拼出 /v1/v1/... 必然 404。
	if v1PathProviders[body.Provider] && baseURLHasV1Suffix(body.BaseURL) {
		adminError(w, http.StatusBadRequest, "PROVIDER_BASE_URL_INVALID",
			"provider-base-url-v1-suffix", nil)
		return
	}
	// 引擎要求同一 provider 的 base URL 不能冲突,创建前提前拦截并说明原因。
	if body.BaseURL != nil && *body.BaseURL != "" {
		var count int
		if err := d.Pool.QueryRow(r.Context(), `
			SELECT count(*) FROM provider_accounts
			WHERE organization_id = $1 AND provider = $2 AND base_url IS NOT NULL AND base_url <> $3
		`, principal.OrganizationID, body.Provider, *body.BaseURL).Scan(&count); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_CREATE_FAILED", "failed-to-create-the-provider-account", nil)
			return
		}
		if count > 0 {
			adminError(w, http.StatusConflict, "PROVIDER_BASE_URL_CONFLICT",
				"provider-base-url-conflict", nil)
			return
		}
	}
	billingCurrency, err := normalizeCurrency(body.BillingCurrency)
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_CURRENCY_INVALID", "providerCurrencyInvalid", nil)
		return
	}
	ciphertext, err := vault.Encrypt(d.MasterKey, []byte(body.APIKey))
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_CREATE_FAILED", "failed-to-encrypt-credentials", nil)
		return
	}
	accountID := uuid.New()
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO provider_accounts(id, organization_id, provider, name, region, base_url, billing_currency)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
		`, accountID, principal.OrganizationID, body.Provider, body.Name, body.Region, body.BaseURL, billingCurrency); err != nil {
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
			adminError(w, http.StatusConflict, "PROVIDER_NAME_EXISTS", "a-provider-account-with-the-same", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PROVIDER_CREATE_FAILED", "failed-to-create-the-provider-account", nil)
		return
	}
	if !d.reloadEngine(w, r) {
		return
	}
	adminOK(w, map[string]string{"id": accountID.String()})
}

// DeleteProviderAccount 删除 供应商账号;被候选引用时拒绝删除并说明。
func (d ServerDeps) DeleteProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_INVALID_ID", "invalid-account-id", nil)
		return
	}
	var count int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM model_candidates WHERE provider_account_id = $1
	`, accountID).Scan(&count); err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_DELETE_FAILED", "failed-to-delete-the-provider-account", nil)
		return
	}
	if count > 0 {
		adminError(w, http.StatusConflict, "PROVIDER_IN_USE", "this-account-is-still-referenced-by", map[string]any{"candidate_count": count})
		return
	}
	tag, err := d.Pool.Exec(r.Context(), `
		DELETE FROM provider_accounts WHERE id = $1 AND organization_id = $2
	`, accountID, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_DELETE_FAILED", "failed-to-delete-the-provider-account", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider-account-does-not-exist", nil)
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

// reloadEngine 在 供应商配置变化后重建上游引擎;返回 false 表示失败且已写出错误响应。
func (d ServerDeps) reloadEngine(w http.ResponseWriter, r *http.Request) bool {
	if d.ReloadEngine == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	defer cancel()
	if err := d.ReloadEngine(ctx); err != nil {
		adminError(w, http.StatusInternalServerError, "ENGINE_RELOAD_FAILED",
			"provider-engine-reload-failed", nil)
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
	Name            string  `json:"name"`
	Region          *string `json:"region"`
	BaseURL         *string `json:"base_url"`
	BillingCurrency string  `json:"billing_currency"`
}

// UpdateProviderAccount 更新账号名称/区域/Base URL;provider 类型与凭据不可修改。
func (d ServerDeps) UpdateProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_ID_INVALID", "invalid-provider-account-id", nil)
		return
	}
	var body updateProviderAccountRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		adminError(w, http.StatusBadRequest, "PROVIDER_NAME_REQUIRED", "enter-an-account-name", nil)
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
			adminError(w, http.StatusInternalServerError, "PROVIDER_UPDATE_FAILED", "failed-to-update-the-provider-account", nil)
			return
		}
		if count > 0 {
			adminError(w, http.StatusConflict, "PROVIDER_BASE_URL_CONFLICT",
				"provider-base-url-conflict", nil)
			return
		}
	}
	var oldName, provider string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT name, provider FROM provider_accounts WHERE id = $1 AND organization_id = $2
	`, accountID, principal.OrganizationID).Scan(&oldName, &provider)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider-account-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_UPDATE_FAILED", "failed-to-update-the-provider-account", nil)
		return
	}
	// 厂商账号的接口地址由服务端目录内置,不可修改。
	if vendor := bifrostx.VendorByID(provider); vendor != nil && !vendor.EditableURL &&
		body.BaseURL != nil && strings.TrimSpace(*body.BaseURL) != "" {
		adminError(w, http.StatusBadRequest, "PROVIDER_BASE_URL_NOT_ALLOWED",
			"provider-base-url-not-editable", nil)
		return
	}
	// base_url 更新沿用创建时的约束:openai/anthropic 类 provider 不允许 /v1 结尾。
	if v1PathProviders[provider] && baseURLHasV1Suffix(body.BaseURL) {
		adminError(w, http.StatusBadRequest, "PROVIDER_BASE_URL_INVALID",
			"provider-base-url-v1-suffix", nil)
		return
	}
	billingCurrency, err := normalizeCurrency(body.BillingCurrency)
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_CURRENCY_INVALID", "providerCurrencyInvalid", nil)
		return
	}
	if _, err := d.Pool.Exec(r.Context(), `
		UPDATE provider_accounts SET name = $3, region = $4, base_url = $5, billing_currency = $6, updated_at = now()
		WHERE id = $1 AND organization_id = $2
	`, accountID, principal.OrganizationID, body.Name, body.Region, body.BaseURL, billingCurrency); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "provider_accounts_organization_id_name_key" {
			adminError(w, http.StatusConflict, "PROVIDER_NAME_EXISTS", "a-provider-account-with-the-same", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PROVIDER_UPDATE_FAILED", "failed-to-update-the-provider-account", nil)
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
	OK         bool   `json:"ok"`
	Reason     string `json:"reason"`
	Detail     string `json:"detail,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
}

type providerAccess struct {
	Provider string
	BaseURL  *string
	APIKey   string
}

func (d ServerDeps) loadProviderAccess(ctx context.Context, organizationID, accountID uuid.UUID) (providerAccess, error) {
	var access providerAccess
	var ciphertext []byte
	var keyID string
	err := d.Pool.QueryRow(ctx, `
		SELECT a.provider, a.base_url, c.ciphertext, c.key_id
		FROM provider_accounts a JOIN provider_credentials c ON c.provider_account_id = a.id
		WHERE a.id = $1 AND a.organization_id = $2
	`, accountID, organizationID).Scan(&access.Provider, &access.BaseURL, &ciphertext, &keyID)
	if err != nil {
		return access, err
	}
	plaintext, err := vault.Decrypt(d.MasterKey, ciphertext)
	if err != nil {
		return access, err
	}
	access.APIKey = string(plaintext)
	return access, nil
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
		adminError(w, http.StatusBadRequest, "PROVIDER_ID_INVALID", "invalid-provider-account-id", nil)
		return
	}
	access, err := d.loadProviderAccess(r.Context(), principal.OrganizationID, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider-account-does-not-exist-is", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_TEST_FAILED", "failed-to-read-credentials-check-the", nil)
		return
	}

	testURL, headers, err := providerProbeRequest(access.Provider, access.BaseURL, access.APIKey)
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_TEST_UNSUPPORTED",
			"provider-test-unsupported", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_TEST_FAILED", "connectivity-test-failed", nil)
		return
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		adminOK(w, providerTestResult{OK: false, Reason: "network_error", Detail: err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		adminOK(w, providerTestResult{OK: true, Reason: "connected", StatusCode: resp.StatusCode})
		return
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		adminOK(w, providerTestResult{OK: false, Reason: "credentials_rejected", StatusCode: resp.StatusCode})
		return
	}
	if resp.StatusCode == http.StatusNotFound {
		adminOK(w, providerTestResult{OK: false, Reason: "v1_suffix_suspected", StatusCode: resp.StatusCode})
		return
	}
	adminOK(w, providerTestResult{OK: false, Reason: "upstream_http_error", StatusCode: resp.StatusCode})
}

// baseURLHasV1Suffix 判断 base_url 的路径是否以 /v1 结尾。
// Bifrost 会在 openai/anthropic 类 provider 的 base_url 后拼接 /v1/... 路径,
// 以 /v1 结尾的地址会拼出 /v1/v1/... 必然 404;URL 解析失败时不判定,由实际调用暴露问题。
func baseURLHasV1Suffix(baseURL *string) bool {
	if baseURL == nil {
		return false
	}
	parsed, err := url.Parse(strings.TrimRight(*baseURL, "/"))
	if err != nil {
		return false
	}
	return parsed.Path == "/v1" || strings.HasSuffix(parsed.Path, "/v1")
}

// providerProbeRequest 按 provider 构造轻量探测请求;不支持的 provider 返回错误。
// 探测路径必须与数据面真实调用同前缀(openai 为 base + /v1/chat/completions,
// anthropic 为 base + /v1/messages),否则会像旧版探测 /models 一样出现"测试通过但调用 404"。
func providerProbeRequest(provider string, baseURL *string, apiKey string) (string, map[string]string, error) {
	switch provider {
	case "openai", "openai_compatible":
		base := "https://api.openai.com"
		if baseURL != nil && *baseURL != "" {
			base = strings.TrimRight(*baseURL, "/")
		}
		return base + "/v1/models", map[string]string{"Authorization": "Bearer " + apiKey}, nil
	case "anthropic":
		base := "https://api.anthropic.com"
		if baseURL != nil && *baseURL != "" {
			base = strings.TrimRight(*baseURL, "/")
		}
		return base + "/v1/models", map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		}, nil
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/models?key=" + apiKey, nil, nil
	default:
		if vendor := bifrostx.VendorByID(provider); vendor != nil {
			base := vendor.BaseURL
			if baseURL != nil && *baseURL != "" {
				base = strings.TrimRight(*baseURL, "/")
			}
			return base + vendor.ModelsPath(), map[string]string{"Authorization": "Bearer " + apiKey}, nil
		}
		return "", nil, fmt.Errorf("provider %s does not support online probe", provider)
	}
}
