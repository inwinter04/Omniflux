package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/vault"
)

// balanceEntry 是一条余额信息;字段可空,取决于厂商接口返回口径。
type balanceEntry struct {
	Label         string   `json:"label"`
	Kind          string   `json:"kind"`
	Window        string   `json:"window,omitempty"`
	Remaining     *float64 `json:"remaining"`
	Total         *float64 `json:"total"`
	Used          *float64 `json:"used"`
	Unit          string   `json:"unit"`
	NextResetTime *int64   `json:"next_reset_time,omitempty"`
}

// ProviderAccountBalance 查询厂商账户余额。
// 服务端解密凭据后调用厂商余额接口,结果做归一化;不支持余额查询的厂商返回明确错误。
func (d ServerDeps) ProviderAccountBalance(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requireProjectAdmin(w, principal) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_INVALID_ID", "账号 ID 不合法", nil)
		return
	}
	var provider string
	var ciphertext []byte
	err = d.Pool.QueryRow(r.Context(), `
		SELECT a.provider, c.ciphertext
		FROM provider_accounts a
		JOIN provider_credentials c ON c.provider_account_id = a.id
		WHERE a.id = $1 AND a.organization_id = $2
	`, accountID, principal.OrganizationID).Scan(&provider, &ciphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "供应商账号不存在或不属于当前组织", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_BALANCE_FAILED", "余额查询失败", nil)
		return
	}
	vendor := bifrostx.VendorByID(provider)
	if vendor == nil || vendor.BalanceKind == "" {
		adminError(w, http.StatusBadRequest, "PROVIDER_BALANCE_UNSUPPORTED", "该厂商暂不支持余额查询", nil)
		return
	}
	apiKey, err := vault.Decrypt(d.MasterKey, ciphertext)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_BALANCE_FAILED", "凭据解密失败", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	balances, qerr := queryVendorBalance(ctx, vendor.ID, vendor.BaseURL, string(apiKey))
	if qerr != nil {
		adminError(w, http.StatusBadGateway, "PROVIDER_BALANCE_QUERY_FAILED", qerr.Error(), nil)
		return
	}
	adminOK(w, map[string]any{"balances": balances})
}

// queryVendorBalance 按 BalanceKind 调用厂商余额接口并归一化结果。
func queryVendorBalance(ctx context.Context, vendorID, baseURL, apiKey string) ([]balanceEntry, error) {
	switch bifrostx.VendorByID(vendorID).BalanceKind {
	case "deepseek":
		return queryDeepSeekBalance(ctx, apiKey)
	case "zhipu":
		return queryZhipuBalance(ctx, vendorID, baseURL, apiKey)
	case "zhipu_plan":
		return queryZhipuPlanBalance(ctx, vendorID, baseURL, apiKey)
	case "stepfun":
		return queryStepFunBalance(ctx, vendorID, baseURL, apiKey)
	case "siliconflow":
		return querySiliconFlowBalance(ctx, vendorID, baseURL, apiKey)
	case "openrouter":
		return queryOpenRouterBalance(ctx, apiKey)
	case "novita":
		return queryNovitaBalance(ctx, apiKey)
	default:
		return nil, errors.New("该厂商暂不支持余额查询")
	}
}

// balanceGet 请求厂商余额接口并解析 JSON;401/403 转成明确的鉴权错误。
func balanceGet(ctx context.Context, url string, headers map[string]string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("余额接口请求构造失败")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := balanceHTTPClient.Do(req)
	if err != nil {
		return nil, errors.New("余额接口网络请求失败,请检查服务器网络或厂商地址")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, errors.New("读取余额接口响应失败")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errors.New("鉴权失败:API Key 无效或无余额查询权限")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("余额接口返回 HTTP " + resp.Status)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("余额接口响应不是有效 JSON")
	}
	return payload, nil
}

var balanceHTTPClient = &http.Client{Timeout: 15 * time.Second}

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
func queryDeepSeekBalance(ctx context.Context, apiKey string) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, "https://api.deepseek.com/user/balance", map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
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
		label := unit + " 钱包"
		if unit == "CNY" {
			label = "人民币钱包"
		} else if unit == "USD" {
			label = "美元钱包"
		}
		entries = append(entries, balanceEntry{Label: label, Kind: "balance", Remaining: remaining, Unit: unit})
	}
	if len(entries) == 0 {
		return nil, errors.New("余额接口响应缺少 balance_infos 字段")
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
func queryZhipuBalance(ctx context.Context, vendorID, baseURL, apiKey string) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, baseURL+"/api/biz/account/query-customer-account-report", map[string]string{
		"Authorization":   apiKey,
		"Accept-Language": "en-US,en",
	})
	if err != nil {
		return nil, err
	}
	if success, ok := payload["success"].(bool); ok && !success {
		msg, _ := payload["msg"].(string)
		if msg == "" {
			msg = "未知错误"
		}
		return nil, errors.New("厂商接口返回错误: " + msg)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return nil, errors.New("余额接口响应缺少 data 字段")
	}
	// availableBalance 是扣掉冻结后的可用余额;没有时退回 balance。
	remaining := balanceF64(data, "availableBalance")
	if remaining == nil {
		remaining = balanceF64(data, "balance")
	}
	if remaining == nil {
		return nil, errors.New("余额接口响应缺少余额字段")
	}
	unit := "CNY"
	label := "智谱 GLM"
	if vendorID == "zhipu-glm-en" {
		unit = "USD"
		label = "Z.ai"
	}
	return []balanceEntry{{Label: label, Remaining: remaining, Unit: unit}}, nil
}

// ── 智谱 GLM Coding Plan ────────────────────────────────────
// GET {open.bigmodel.cn | api.z.ai}/api/monitor/usage/quota/limit
// 响应: { success, data: { limits: [{ type, percentage, nextResetTime }] } };
// type 为 TOKENS_LIMIT/CREDIT_LIMIT,percentage 为已用百分比,通常含 5 小时与每周两个窗口。
// 按量账户(无套餐)查询时返回 HTTP 200 + 业务错误 {"code":500,"msg":"当前用户不存在coding plan"},
// 此时回落查询账户余额。鉴权同样不加 Bearer 前缀。
func queryZhipuPlanBalance(ctx context.Context, vendorID, baseURL, apiKey string) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, baseURL+"/api/monitor/usage/quota/limit", map[string]string{
		"Authorization":   apiKey,
		"Content-Type":    "application/json",
		"Accept-Language": "en-US,en",
	})
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
			return queryZhipuBalance(ctx, vendorID, baseURL, apiKey)
		}
		if msg == "" {
			msg = "未知错误"
		}
		return nil, errors.New("厂商接口返回错误: " + msg)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return nil, errors.New("套餐接口响应缺少 data 字段")
	}
	limits, ok := data["limits"].([]any)
	if !ok {
		return nil, errors.New("套餐接口响应缺少 data.limits 字段")
	}
	windowByType := map[string]string{
		"TOKENS_LIMIT": "5 小时",
		"CREDIT_LIMIT": "每周",
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
		entry := balanceEntry{Label: window + "限额", Kind: "quota", Window: window, Used: percentage, Remaining: &remaining, Unit: "%"}
		if reset := balanceF64(limit, "nextResetTime"); reset != nil {
			value := int64(*reset)
			entry.NextResetTime = &value
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, errors.New("套餐接口响应不含可用额度窗口")
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
func queryStepFunBalance(ctx context.Context, vendorID, baseURL, apiKey string) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, baseURL+"/v1/accounts", map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		return nil, err
	}
	remaining := balanceF64(payload, "balance")
	if remaining == nil {
		return nil, errors.New("余额接口响应缺少 balance 字段")
	}
	label := "StepFun"
	if vendorID == "stepfun-api-en" {
		label = "StepFun 国际站"
	}
	return []balanceEntry{{Label: label, Remaining: remaining, Unit: "CNY"}}, nil
}

// ── SiliconFlow ─────────────────────────────────────────────
// GET https://api.siliconflow.cn|com/v1/user/info
// 响应: { code, data: { balance, chargeBalance, totalBalance, status } }
func querySiliconFlowBalance(ctx context.Context, vendorID, baseURL, apiKey string) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, baseURL+"/v1/user/info", map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		return nil, err
	}
	remaining := balanceF64(payload, "data", "totalBalance")
	if remaining == nil {
		return nil, errors.New("余额接口响应缺少 data.totalBalance 字段")
	}
	unit := "CNY"
	label := "SiliconFlow"
	if vendorID == "siliconflow-en" {
		unit = "USD"
		label = "SiliconFlow (EN)"
	}
	return []balanceEntry{{Label: label, Remaining: remaining, Unit: unit}}, nil
}

// ── OpenRouter ──────────────────────────────────────────────
// GET https://openrouter.ai/api/v1/credits
// 响应: { data: { total_credits, total_usage } };remaining = credits - usage。
func queryOpenRouterBalance(ctx context.Context, apiKey string) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, "https://openrouter.ai/api/v1/credits", map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		return nil, err
	}
	total := balanceF64(payload, "data", "total_credits")
	used := balanceF64(payload, "data", "total_usage")
	if total == nil || used == nil {
		return nil, errors.New("余额接口响应缺少额度字段")
	}
	remaining := *total - *used
	return []balanceEntry{{Label: "OpenRouter", Total: total, Used: used, Remaining: &remaining, Unit: "USD"}}, nil
}

// ── Novita AI ───────────────────────────────────────────────
// GET https://api.novita.ai/v3/user/balance
// 响应: { availableBalance, cashBalance, creditLimit, outstandingInvoices };金额单位 0.0001 USD。
func queryNovitaBalance(ctx context.Context, apiKey string) ([]balanceEntry, error) {
	payload, err := balanceGet(ctx, "https://api.novita.ai/v3/user/balance", map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if err != nil {
		return nil, err
	}
	available := balanceF64(payload, "availableBalance")
	if available == nil {
		return nil, errors.New("余额接口响应缺少 availableBalance 字段")
	}
	v := *available / 10000
	return []balanceEntry{{Label: "Novita AI", Remaining: &v, Unit: "USD"}}, nil
}
