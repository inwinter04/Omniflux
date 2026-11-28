package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/dataplane"
)

// balanceEntry 是一条余额信息;字段可空,取决于厂商接口返回口径。
type balanceEntry struct {
	LabelKey          string   `json:"label_key"`
	Kind              string   `json:"kind"`
	Remaining         *float64 `json:"remaining"`
	Total             *float64 `json:"total"`
	Used              *float64 `json:"used"`
	Unit              string   `json:"unit"`
	NextResetTime     *int64   `json:"next_reset_time,omitempty"`
	NextResetTimeText *string  `json:"next_reset_time_text,omitempty"`
}

// planQuotaEntry 是归一化后的套餐额度窗口。百分比统一使用 0-100。
type planQuotaEntry struct {
	ID               string   `json:"id"`
	LabelKey         string   `json:"label_key"`
	WindowKind       string   `json:"window_kind"`
	WindowSeconds    *int64   `json:"window_seconds,omitempty"`
	UsedPercent      *float64 `json:"used_percent"`
	RemainingPercent *float64 `json:"remaining_percent"`
	Used             *float64 `json:"used,omitempty"`
	Remaining        *float64 `json:"remaining,omitempty"`
	Total            *float64 `json:"total,omitempty"`
	Unit             string   `json:"unit"`
	ResetAt          *string  `json:"reset_at,omitempty"`
}

// providerUsageSummary 是账号列表直显用的轻量余额/套餐摘要。
type providerUsageSummary struct {
	AccountID    string           `json:"account_id"`
	AccountName  string           `json:"account_name"`
	Provider     string           `json:"provider"`
	PricingKind  string           `json:"pricing_kind"`
	PlanKind     string           `json:"plan_kind"`
	QuotaKind    string           `json:"quota_kind"`
	QuotaSupport string           `json:"quota_support"`
	QuotaStatus  string           `json:"quota_status"`
	Balances     []balanceEntry   `json:"balances"`
	PlanQuotas   []planQuotaEntry `json:"plan_quotas"`
	QueriedAt    string           `json:"queried_at"`
}

// providerBalanceResponse 区分钱包余额与套餐额度,并给前端稳定的展示语义。
type providerBalanceResponse struct {
	PricingKind  string           `json:"pricing_kind"`
	PlanKind     string           `json:"plan_kind"`
	QuotaKind    string           `json:"quota_kind"`
	QuotaSupport string           `json:"quota_support"`
	QuotaStatus  string           `json:"quota_status"`
	Balances     []balanceEntry   `json:"balances"`
	PlanQuotas   []planQuotaEntry `json:"plan_quotas"`
	QueriedAt    string           `json:"queried_at"`
}

// ProviderAccountBalance 查询厂商余额与套餐额度。
// 未接入自动查询的厂商返回 HTTP 200 和 quota_support=unsupported,由前端展示不可用状态。
func (d ServerDeps) ProviderAccountBalance(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderManage) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_INVALID_ID", "invalid-account-id", nil)
		return
	}
	access, err := d.loadProviderAccess(r.Context(), principal.OrganizationID, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider-account-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_BALANCE_FAILED", "failed-to-read-credentials", nil)
		return
	}
	vendor := bifrostx.VendorByID(access.Provider)
	metadata, hasVendor := bifrostx.VendorCatalogEntryByID(access.Provider)
	if !hasVendor {
		metadata = bifrostx.VendorCatalogEntry{
			PricingKind:  "unknown",
			QuotaKind:    "none",
			QuotaSupport: "unsupported",
		}
	}
	response := providerBalanceResponse{
		PricingKind:  metadata.PricingKind,
		PlanKind:     metadata.PlanKind,
		QuotaKind:    metadata.QuotaKind,
		QuotaSupport: metadata.QuotaSupport,
		QuotaStatus:  "unsupported",
		Balances:     []balanceEntry{},
		PlanQuotas:   []planQuotaEntry{},
		QueriedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if metadata.QuotaSupport != "supported" || vendor == nil {
		adminOK(w, response)
		return
	}
	client, err := balanceHTTPClientFor(access.ProxyType, access.ProxyURL)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_BALANCE_FAILED", "failed-to-build-provider-proxy", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	entries, qerr := queryVendorBalance(ctx, vendor.ID, vendor.BaseURL, access.APIKey, client)
	if qerr != nil {
		adminError(w, http.StatusBadGateway, "PROVIDER_BALANCE_QUERY_FAILED", "provider-balance-query-failed", nil)
		return
	}
	response.QuotaStatus = "ok"
	response.Balances = make([]balanceEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind != "quota" {
			response.Balances = append(response.Balances, entry)
		}
	}
	response.PlanQuotas = planQuotasFromBalanceEntries(entries)
	adminOK(w, response)
}

// ProviderUsageSummaries 批量查询当前组织内支持自动查询的账号摘要。
// 供供应商列表直显余额/套餐用量;不支持自动查询的账号由厂商目录元数据标记。
func (d ServerDeps) ProviderUsageSummaries(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderRead) != nil {
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, name, provider
		FROM provider_accounts
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_USAGE_QUERY_FAILED", "failed-to-query-provider-accounts", nil)
		return
	}
	type accountRow struct {
		id       uuid.UUID
		name     string
		provider string
	}
	accounts := make([]accountRow, 0, 16)
	for rows.Next() {
		var item accountRow
		if err := rows.Scan(&item.id, &item.name, &item.provider); err != nil {
			rows.Close()
			adminError(w, http.StatusInternalServerError, "PROVIDER_USAGE_QUERY_FAILED", "failed-to-query-provider-accounts", nil)
			return
		}
		metadata, exists := bifrostx.VendorCatalogEntryByID(item.provider)
		if !exists || metadata.QuotaSupport != "supported" {
			continue
		}
		accounts = append(accounts, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_USAGE_QUERY_FAILED", "failed-to-query-provider-accounts", nil)
		return
	}

	type result struct {
		summary providerUsageSummary
		err     error
	}
	results := make(map[uuid.UUID]result, len(accounts))
	var resultMu sync.Mutex
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	queriedAt := time.Now().UTC().Format(time.RFC3339)
	for _, account := range accounts {
		wg.Add(1)
		go func(account accountRow) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			metadata, _ := bifrostx.VendorCatalogEntryByID(account.provider)
			summary := providerUsageSummary{
				AccountID:    account.id.String(),
				AccountName:  account.name,
				Provider:     account.provider,
				PricingKind:  metadata.PricingKind,
				PlanKind:     metadata.PlanKind,
				QuotaKind:    metadata.QuotaKind,
				QuotaSupport: metadata.QuotaSupport,
				QuotaStatus:  "ok",
				Balances:     []balanceEntry{},
				PlanQuotas:   []planQuotaEntry{},
				QueriedAt:    queriedAt,
			}
			access, err := d.loadProviderAccess(r.Context(), principal.OrganizationID, account.id)
			if err != nil {
				summary.QuotaStatus = "query_failed"
				resultMu.Lock()
				results[account.id] = result{summary: summary, err: err}
				resultMu.Unlock()
				return
			}
			client, err := balanceHTTPClientFor(access.ProxyType, access.ProxyURL)
			if err != nil {
				summary.QuotaStatus = "query_failed"
				resultMu.Lock()
				results[account.id] = result{summary: summary, err: err}
				resultMu.Unlock()
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			vendor := bifrostx.VendorByID(account.provider)
			entries, err := queryVendorBalance(ctx, account.provider, vendor.BaseURL, access.APIKey, client)
			if err != nil {
				summary.QuotaStatus = "query_failed"
				resultMu.Lock()
				results[account.id] = result{summary: summary, err: err}
				resultMu.Unlock()
				return
			}
			for _, entry := range entries {
				if entry.Kind != "quota" {
					summary.Balances = append(summary.Balances, entry)
				}
			}
			summary.PlanQuotas = planQuotasFromBalanceEntries(entries)
			resultMu.Lock()
			results[account.id] = result{summary: summary}
			resultMu.Unlock()
		}(account)
	}
	wg.Wait()

	items := make([]providerUsageSummary, 0, len(accounts))
	for _, account := range accounts {
		if item, exists := results[account.id]; exists {
			items = append(items, item.summary)
		}
	}
	adminOK(w, map[string]any{"items": items, "queried_at": queriedAt})
}

// queryVendorBalance 按 BalanceKind 调用厂商余额接口并归一化结果。
func queryVendorBalance(ctx context.Context, vendorID, baseURL, apiKey string, client *http.Client) ([]balanceEntry, error) {
	vendor := bifrostx.VendorByID(vendorID)
	if vendor == nil {
		return nil, errors.New("this-provider-does-not-support-balance")
	}
	switch vendor.BalanceKind {
	case "deepseek":
		return queryDeepSeekBalance(ctx, apiKey, client)
	case "zhipu":
		return queryZhipuBalance(ctx, vendorID, baseURL, apiKey, client)
	case "zhipu_plan":
		return queryZhipuPlanBalance(ctx, vendorID, baseURL, apiKey, client)
	case "kimi_plan":
		return queryKimiPlanBalance(ctx, baseURL, apiKey, client)
	case "stepfun":
		return queryStepFunBalance(ctx, vendorID, baseURL, apiKey, client)
	case "siliconflow":
		return querySiliconFlowBalance(ctx, vendorID, baseURL, apiKey, client)
	case "openrouter":
		return queryOpenRouterBalance(ctx, apiKey, client)
	case "novita":
		return queryNovitaBalance(ctx, apiKey, client)
	default:
		return nil, errors.New("this-provider-does-not-support-balance")
	}
}

// balanceGet 请求厂商余额接口并解析 JSON;401/403 转成明确的鉴权错误。
func balanceGet(ctx context.Context, url string, headers map[string]string, client *http.Client) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("failed to build the balance API request")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("balance API network request failed; check server network or provider endpoint")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, errors.New("failed to read the balance API response")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errors.New("authentication failed: API key is invalid or lacks balance query permission")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("balance API returned HTTP " + resp.Status)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("balance API response is not valid JSON")
	}
	return payload, nil
}

// balanceF64 从嵌套 JSON 里取数值字段,兼容数字与字符串两种格式。
func balanceF64(v map[string]any, keys ...string) *float64 {
	var cur any = v
	for i, key := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		node, exists := m[key]
		if !exists {
			return nil
		}
		if i == len(keys)-1 {
			switch n := node.(type) {
			case float64:
				return &n
			case string:
				if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
					return &f
				}
			}
			return nil
		}
		cur = node
	}
	return nil
}

// ── DeepSeek ────────────────────────────────────────────────
// GET https://api.deepseek.com/user/balance
// 响应: { balance_infos: [{ currency, total_balance, ... }], is_available }
// is_available 只表示余额是否够调用,不代表 Key 失效,不参与展示判断。
func queryDeepSeekBalance(ctx context.Context, apiKey string, client *http.Client) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, "https://api.deepseek.com/user/balance", map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, client)
	if err != nil {
		return nil, err
	}
	infos, _ := payload["balance_infos"].([]any)
	entries := make([]balanceEntry, 0, len(infos))
	for _, item := range infos {
		info, ok := item.(map[string]any)
		if !ok {
			continue
		}
		unit, _ := info["currency"].(string)
		if unit == "" {
			unit = "CNY"
		}
		remaining := balanceF64(info, "total_balance")
		labelKey := "wallet_usd"
		if unit == "CNY" {
			labelKey = "wallet_cny"
		}
		entries = append(entries, balanceEntry{LabelKey: labelKey, Kind: "balance", Remaining: remaining, Unit: unit})
	}
	if len(entries) == 0 {
		return nil, errors.New("balance API response is missing balance_infos")
	}
	// 多币种钱包只保留有余额的行,避免空钱包造成误导。
	hasPositive := false
	for _, e := range entries {
		if e.Remaining != nil && *e.Remaining > 0 {
			hasPositive = true
			break
		}
	}
	if hasPositive {
		kept := make([]balanceEntry, 0, len(entries))
		for _, e := range entries {
			if e.Remaining != nil && *e.Remaining > 0 {
				kept = append(kept, e)
			}
		}
		entries = kept
	}
	return entries, nil
}

// ── 智谱 GLM ────────────────────────────────────────────────
// GET {open.bigmodel.cn | api.z.ai}/api/biz/account/query-customer-account-report
// 无公开文档;鉴权头直接放 API Key,不带 Bearer 前缀。
// 响应: { code, msg, success, data: { balance, availableBalance, ... } },单位为账户币种。
func queryZhipuBalance(ctx context.Context, vendorID, baseURL, apiKey string, client *http.Client) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, baseURL+"/api/biz/account/query-customer-account-report", map[string]string{
		"Authorization":   apiKey,
		"Accept-Language": "en-US,en",
	}, client)
	if err != nil {
		return nil, err
	}
	if success, ok := payload["success"].(bool); ok && !success {
		msg, _ := payload["msg"].(string)
		if msg == "" {
			msg = "unknown error"
		}
		return nil, errors.New("provider API error: " + msg)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return nil, errors.New("balance API response is missing data")
	}
	// availableBalance 是扣掉冻结后的可用余额;没有时退回 balance。
	remaining := balanceF64(data, "availableBalance")
	if remaining == nil {
		remaining = balanceF64(data, "balance")
	}
	if remaining == nil {
		return nil, errors.New("balance API response is missing balance fields")
	}
	unit := "CNY"
	labelKey := "provider_zhipu"
	if vendorID == "zhipu-glm-en" {
		unit = "USD"
		labelKey = "provider_zai"
	}
	return []balanceEntry{{LabelKey: labelKey, Remaining: remaining, Unit: unit}}, nil
}

// planQuotasFromBalanceEntries 把内部限额记录转换为套餐窗口展示模型。
func planQuotasFromBalanceEntries(entries []balanceEntry) []planQuotaEntry {
	quotas := make([]planQuotaEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind != "quota" {
			continue
		}
		quota := planQuotaEntry{
			ID:               fmt.Sprintf("%s-%d", entry.LabelKey, len(quotas)+1),
			LabelKey:         entry.LabelKey,
			WindowKind:       "unknown",
			UsedPercent:      entry.Used,
			RemainingPercent: entry.Remaining,
			Unit:             "",
		}
		switch entry.LabelKey {
		case "quota_5h":
			quota.WindowKind = "rolling"
			seconds := int64(18000)
			quota.WindowSeconds = &seconds
		case "quota_weekly", "quota_7d":
			quota.WindowKind = "weekly"
			seconds := int64(604800)
			quota.WindowSeconds = &seconds
		default:
			quota.WindowKind = "subscription_month"
		}
		if entry.NextResetTimeText != nil {
			quota.ResetAt = entry.NextResetTimeText
		} else if entry.NextResetTime != nil {
			seconds := *entry.NextResetTime
			// 部分厂商返回毫秒时间戳;阈值足以区分 Unix 秒。
			if seconds > 1e12 {
				seconds /= 1000
			}
			value := time.Unix(seconds, 0).UTC().Format(time.RFC3339)
			quota.ResetAt = &value
		}
		quotas = append(quotas, quota)
	}
	// 当前智谱上游可能把 5 小时与每周窗口都标成同一 Credit 类型;按重置时间先后区分。
	if len(quotas) == 2 && quotas[0].LabelKey == "quota_weekly" && quotas[1].LabelKey == "quota_weekly" {
		sort.Slice(quotas, func(i, j int) bool {
			left := quotas[i].ResetAt
			right := quotas[j].ResetAt
			if left == nil || right == nil {
				return false
			}
			return *left < *right
		})
		fiveHours := int64(18000)
		quotas[0].ID = "quota_5h-1"
		quotas[0].LabelKey = "quota_5h"
		quotas[0].WindowKind = "rolling"
		quotas[0].WindowSeconds = &fiveHours
		quotas[1].ID = "quota_weekly-2"
		quotas[1].LabelKey = "quota_weekly"
		quotas[1].WindowKind = "weekly"
		seconds := int64(604800)
		quotas[1].WindowSeconds = &seconds
	}
	return quotas
}

// quotaTimeString 归一化上游秒、毫秒和字符串时间。
func quotaTimeString(value any) *string {
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil
		}
		return &trimmed
	case float64:
		if v <= 0 {
			return nil
		}
		// JavaScript 和部分管理 API 使用毫秒;阈值足以覆盖秒级 Unix 时间。
		if v > 1e12 {
			v /= 1000
		}
		formatted := time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
		return &formatted
	default:
		return nil
	}
}

// ── Kimi For Coding ─────────────────────────────────────────
// GET {api.kimi.com | api.kimi.ai}/coding/v1/usages
// 鉴权: Authorization: Bearer <Coding API Key 或 OAuth token>。
// 新版返回 usages.limit_*: used_ratio 为 0-1;旧版返回 usage/limits 绝对值。
func queryKimiPlanBalance(ctx context.Context, baseURL, apiKey string, client *http.Client) ([]balanceEntry, error) {
	url := strings.TrimRight(baseURL, "/") + "/coding/v1/usages"
	payload, err := balanceGet(ctx, url, map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Accept":        "application/json",
	}, client)
	if err != nil {
		return nil, err
	}

	usages, ok := payload["usages"].(map[string]any)
	if !ok {
		if data, exists := payload["data"].(map[string]any); exists {
			usages, ok = data["usages"].(map[string]any)
		}
	}
	if ok {
		type kimiWindow struct {
			label      string
			windowKind string
			seconds    *int64
		}
		fiveHours := int64(18000)
		sevenDays := int64(604800)
		windows := []struct {
			key    string
			window kimiWindow
		}{
			{"limit_5h", kimiWindow{label: "quota_5h", windowKind: "rolling", seconds: &fiveHours}},
			{"limit_7d", kimiWindow{label: "quota_7d", windowKind: "weekly", seconds: &sevenDays}},
			{"limit_month_total", kimiWindow{label: "quota_month_total", windowKind: "subscription_month"}},
			{"limit_month_code", kimiWindow{label: "quota_month_code", windowKind: "subscription_month"}},
		}
		entries := make([]balanceEntry, 0, len(windows))
		for _, item := range windows {
			limit, exists := usages[item.key].(map[string]any)
			if !exists {
				continue
			}
			ratio := balanceF64(limit, "used_ratio", "ratio")
			if ratio == nil {
				continue
			}
			// Kimi 新版官方契约使用 0-1 ratio。
			usedPercent := *ratio * 100
			if strings.Contains(strings.ToLower(item.key), "percent") {
				usedPercent = *ratio
			}
			remainingPercent := 100 - usedPercent
			entry := balanceEntry{
				LabelKey:  item.window.label,
				Kind:      "quota",
				Used:      &usedPercent,
				Remaining: &remainingPercent,
				Unit:      "%",
			}
			if total := balanceF64(limit, "total", "limit"); total != nil {
				entry.Total = total
			}
			if used := balanceF64(limit, "used"); used != nil {
				entry.Used = used
			}
			if remaining := balanceF64(limit, "remaining"); remaining != nil {
				entry.Remaining = remaining
			}
			if entry.Total != nil && entry.Used != nil {
				remaining := *entry.Total - *entry.Used
				entry.Remaining = &remaining
			}
			entry.NextResetTimeText = quotaTimeString(limit["reset_time"])
			entries = append(entries, entry)
			_ = item.window.seconds
		}
		if len(entries) > 0 {
			return entries, nil
		}
	}

	// 旧版契约: usage/limits 携带绝对 used/remaining/resetTime。
	limits, _ := payload["limits"].([]any)
	if usage, exists := payload["usage"].(map[string]any); exists {
		if nested, nestedOK := usage["limits"].([]any); nestedOK {
			limits = nested
		}
	}
	entries := make([]balanceEntry, 0, len(limits))
	for _, item := range limits {
		limit, ok := item.(map[string]any)
		if !ok {
			continue
		}
		used := balanceF64(limit, "used")
		total := balanceF64(limit, "limit", "total")
		remaining := balanceF64(limit, "remaining")
		if used == nil || remaining == nil {
			continue
		}
		if total == nil {
			value := *used + *remaining
			total = &value
		}
		window := ""
		if value, ok := limit["window"].(string); ok {
			window = value
		} else if value, ok := limit["detail"].(string); ok {
			window = value
		}
		label := strings.ToLower(strings.TrimSpace(window))
		labelKey := "quota_" + label
		switch {
		case strings.Contains(label, "5"):
			labelKey = "quota_5h"
		case strings.Contains(label, "7"):
			labelKey = "quota_7d"
		case strings.Contains(label, "month"):
			labelKey = "quota_month_total"
		}
		entry := balanceEntry{
			LabelKey:  labelKey,
			Kind:      "quota",
			Total:     total,
			Used:      used,
			Remaining: remaining,
			Unit:      "credits",
		}
		if reset := balanceF64(limit, "resetTime", "reset_time"); reset != nil {
			value := int64(*reset)
			entry.NextResetTime = &value
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, errors.New("plan API response contains no usable quota window")
	}
	return entries, nil
}

// ── 智谱 GLM Coding Plan ────────────────────────────────────
// GET {open.bigmodel.cn | api.z.ai}/api/monitor/usage/quota/limit
// 响应: { success, data: { limits: [{ type, percentage, nextResetTime }] } };
// type 为 TOKENS_LIMIT/CREDIT_LIMIT,percentage 为已用百分比,通常含 5 小时与每周两个窗口。
// 按量账户(无套餐)查询时返回 HTTP 200 + 业务错误 {"code":500,"msg":"当前用户不存在coding plan"},
// 此时回落查询账户余额。鉴权同样不加 Bearer 前缀。
func queryZhipuPlanBalance(ctx context.Context, vendorID, baseURL, apiKey string, client *http.Client) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, baseURL+"/api/monitor/usage/quota/limit", map[string]string{
		"Authorization":   apiKey,
		"Content-Type":    "application/json",
		"Accept-Language": "en-US,en",
	}, client)
	if err != nil {
		// 按量账户的套餐查询是业务错误而非 HTTP 错误;HTTP 层错误直接透出。
		return nil, err
	}
	if success, ok := payload["success"].(bool); ok && !success {
		msg, _ := payload["msg"].(string)
		// 按量付费账户没有 Coding Plan:接口返回 200 + 业务错误
		// (如"当前用户不存在coding plan")。只认明确提示无套餐的消息才回落查
		// 账户余额;其余业务错误(Key 失效等)直接透出,避免掩盖真实故障。
		if zhipuMsgSaysNoPlan(msg) {
			return queryZhipuBalance(ctx, vendorID, baseURL, apiKey, client)
		}
		if msg == "" {
			msg = "unknown error"
		}
		return nil, errors.New("provider API error: " + msg)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return nil, errors.New("plan API response is missing data")
	}
	limits, ok := data["limits"].([]any)
	if !ok {
		return nil, errors.New("plan API response is missing data.limits")
	}
	windowByType := map[string]string{
		"TOKENS_LIMIT": "quota_5h",
		"CREDIT_LIMIT": "quota_weekly",
	}
	entries := make([]balanceEntry, 0, len(limits))
	for _, item := range limits {
		limit, ok := item.(map[string]any)
		if !ok {
			continue
		}
		limitType, _ := limit["type"].(string)
		window, known := windowByType[limitType]
		if !known {
			// 上游可能新增限额类型;小写/驼峰归一后仍不认识则跳过。
			continue
		}
		percentage := balanceF64(limit, "percentage")
		if percentage == nil {
			continue
		}
		remaining := 100 - *percentage
		windowKey := window
		entry := balanceEntry{LabelKey: windowKey, Kind: "quota", Used: percentage, Remaining: &remaining, Unit: "%"}
		if reset := balanceF64(limit, "nextResetTime"); reset != nil {
			value := int64(*reset)
			entry.NextResetTime = &value
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, errors.New("plan API response contains no usable quota window")
	}
	return entries, nil
}

// zhipuMsgSaysNoPlan 判断智谱业务错误消息是否为「账户没有 Coding Plan」。
// 消息去除空白后需同时包含 coding plan
// 与一个"不存在/没有"类关键词,避免把 Key 失效等其他错误误判为无套餐。
func zhipuMsgSaysNoPlan(msg string) bool {
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '_' || r == '-' {
			return -1
		}
		return r
	}, msg)
	lower := strings.ToLower(msg)
	if !strings.Contains(lower, "coding plan") && !strings.Contains(strings.ToLower(compact), "codingplan") {
		return false
	}
	for _, kw := range []string{"不存在", "没有", "未开通", "not exist", "not found", "not have", "no coding plan", "not subscribed"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// ── StepFun ─────────────────────────────────────────────────
// GET https://api.stepfun.com/v1/accounts
// 响应: { object, type, balance, total_cash_balance, total_voucher_balance }
func queryStepFunBalance(ctx context.Context, vendorID, baseURL, apiKey string, client *http.Client) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, baseURL+"/v1/accounts", map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, client)
	if err != nil {
		return nil, err
	}
	remaining := balanceF64(payload, "balance")
	if remaining == nil {
		return nil, errors.New("balance API response is missing balance")
	}
	labelKey := "provider_stepfun"
	if vendorID == "stepfun-api-en" {
		labelKey = "provider_stepfun_global"
	}
	return []balanceEntry{{LabelKey: labelKey, Remaining: remaining, Unit: "CNY"}}, nil
}

// ── SiliconFlow ─────────────────────────────────────────────
// GET https://api.siliconflow.cn|com/v1/user/info
// 响应: { code, data: { balance, chargeBalance, totalBalance, status } }
func querySiliconFlowBalance(ctx context.Context, vendorID, baseURL, apiKey string, client *http.Client) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, baseURL+"/v1/user/info", map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, client)
	if err != nil {
		return nil, err
	}
	remaining := balanceF64(payload, "data", "totalBalance")
	if remaining == nil {
		return nil, errors.New("balance API response is missing data.totalBalance")
	}
	unit := "CNY"
	labelKey := "provider_siliconflow"
	if vendorID == "siliconflow-en" {
		unit = "USD"
		labelKey = "provider_siliconflow_global"
	}
	return []balanceEntry{{LabelKey: labelKey, Remaining: remaining, Unit: unit}}, nil
}

// ── OpenRouter ──────────────────────────────────────────────
// GET https://openrouter.ai/api/v1/credits
// 响应: { data: { total_credits, total_usage } };remaining = credits - usage。
func queryOpenRouterBalance(ctx context.Context, apiKey string, client *http.Client) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, "https://openrouter.ai/api/v1/credits", map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, client)
	if err != nil {
		return nil, err
	}
	total := balanceF64(payload, "data", "total_credits")
	used := balanceF64(payload, "data", "total_usage")
	if total == nil || used == nil {
		return nil, errors.New("balance API response is missing quota fields")
	}
	remaining := *total - *used
	return []balanceEntry{{LabelKey: "provider_openrouter", Total: total, Used: used, Remaining: &remaining, Unit: "USD"}}, nil
}

// ── Novita AI ───────────────────────────────────────────────
// GET https://api.novita.ai/v3/user/balance
// 响应: { availableBalance, cashBalance, creditLimit, outstandingInvoices };金额单位 0.0001 USD。
func queryNovitaBalance(ctx context.Context, apiKey string, client *http.Client) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, "https://api.novita.ai/v3/user/balance", map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, client)
	if err != nil {
		return nil, err
	}
	available := balanceF64(payload, "availableBalance")
	if available == nil {
		return nil, errors.New("balance API response is missing availableBalance")
	}
	v := *available / 10000
	return []balanceEntry{{LabelKey: "provider_novita", Remaining: &v, Unit: "USD"}}, nil
}

const upstreamBalanceCacheTTL = 60 * time.Second

type cachedUpstreamBalance struct {
	items     []dataplane.UpstreamQuotaItem
	expiresAt time.Time
}

var upstreamBalanceCache = struct {
	mu      sync.RWMutex
	entries map[string]cachedUpstreamBalance
}{
	entries: make(map[string]cachedUpstreamBalance),
}

// queryAuthorizedUpstreamBalances 查询项目已授权模型关联的上游供应商账户余量(带 60s 缓存)。
func (d ServerDeps) queryAuthorizedUpstreamBalances(ctx context.Context, orgID, projectID uuid.UUID) ([]dataplane.UpstreamQuotaItem, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT DISTINCT ON (a.id) a.id, a.name, a.provider
		FROM provider_accounts a
		JOIN model_candidates c
		  ON c.provider_account_id = a.id AND c.status = 'active' AND c.retired_at IS NULL
		JOIN model_services s
		  ON s.id = c.model_service_id AND s.organization_id = $1 AND s.status = 'active'
		WHERE a.organization_id = $1 AND a.status = 'active'
		  AND EXISTS (
		    SELECT 1 FROM model_service_grants g
		    WHERE g.model_service_id = s.id AND g.organization_id = $1
		      AND g.actions && ARRAY['read','invoke']::text[]
		      AND ((g.subject_type = 'organization' AND g.subject_id = $1)
		        OR (g.subject_type = 'project' AND g.subject_id = $2)
		        OR (g.subject_type = 'department' AND g.subject_id = (
		          SELECT p.department_id FROM projects p WHERE p.id = $2 AND p.organization_id = $1
		        )))
		  )
		ORDER BY a.id ASC, c.priority ASC
		LIMIT 6
	`, orgID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type accountCandidate struct {
		id       uuid.UUID
		name     string
		provider string
	}
	accounts := make([]accountCandidate, 0, 3)
	for rows.Next() {
		var acc accountCandidate
		if err := rows.Scan(&acc.id, &acc.name, &acc.provider); err != nil {
			return nil, err
		}
		vendor := bifrostx.VendorByID(acc.provider)
		if vendor == nil || vendor.BalanceKind == "" {
			continue
		}
		accounts = append(accounts, acc)
		if len(accounts) >= 3 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	results := make([]dataplane.UpstreamQuotaItem, 0, len(accounts)*2)
	now := time.Now()
	for _, acc := range accounts {
		cacheKey := acc.id.String()
		upstreamBalanceCache.mu.RLock()
		cached, hit := upstreamBalanceCache.entries[cacheKey]
		upstreamBalanceCache.mu.RUnlock()
		if hit && now.Before(cached.expiresAt) {
			results = append(results, cached.items...)
			continue
		}

		access, loadErr := d.loadProviderAccess(ctx, orgID, acc.id)
		if loadErr != nil {
			continue
		}
		vendor := bifrostx.VendorByID(access.Provider)
		if vendor == nil || vendor.BalanceKind == "" {
			continue
		}
		client, cErr := balanceHTTPClientFor(access.ProxyType, access.ProxyURL)
		if cErr != nil {
			continue
		}
		qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		balances, qErr := queryVendorBalance(qCtx, vendor.ID, vendor.BaseURL, access.APIKey, client)
		cancel()
		if qErr != nil {
			if d.Logger != nil {
				d.Logger.Debug("client quota upstream balance query skipped", "account_id", acc.id, "provider", acc.provider, "error", qErr)
			}
			continue
		}

		mapped := make([]dataplane.UpstreamQuotaItem, 0, len(balances))
		for _, entry := range balances {
			total := entry.Total
			if entry.Unit == "%" && total == nil {
				hundred := 100.0
				total = &hundred
			}
			planName := entry.LabelKey
			if planName == "" {
				planName = acc.provider
			}
			mapped = append(mapped, dataplane.UpstreamQuotaItem{
				AccountID:     acc.id.String(),
				AccountName:   acc.name,
				Provider:      acc.provider,
				LabelKey:      entry.LabelKey,
				Kind:          entry.Kind,
				PlanName:      planName,
				Remaining:     entry.Remaining,
				Total:         total,
				Used:          entry.Used,
				Unit:          entry.Unit,
				NextResetTime: entry.NextResetTime,
			})
		}
		upstreamBalanceCache.mu.Lock()
		if len(upstreamBalanceCache.entries) > 256 {
			cutoff := time.Now()
			for k, v := range upstreamBalanceCache.entries {
				if cutoff.After(v.expiresAt) {
					delete(upstreamBalanceCache.entries, k)
				}
			}
		}
		upstreamBalanceCache.entries[cacheKey] = cachedUpstreamBalance{
			items:     mapped,
			expiresAt: time.Now().Add(upstreamBalanceCacheTTL),
		}
		upstreamBalanceCache.mu.Unlock()
		results = append(results, mapped...)
	}
	return results, nil
}
