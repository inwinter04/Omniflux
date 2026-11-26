package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/governance"
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
	// 综合主动可用性探测与近 24 小时被动健康统计。
	ID                    string                       `json:"id"`
	Provider              string                       `json:"provider"`
	Name                  string                       `json:"name"`
	Region                *string                      `json:"region"`
	BaseURL               *string                      `json:"base_url"`
	ProxyID               *string                      `json:"proxy_id"`
	ProxyName             *string                      `json:"proxy_name"`
	ProxyType             string                       `json:"proxy_type"`
	ProxyStatus           string                       `json:"proxy_status"`
	ProxyPoolSize         int                          `json:"proxy_pool_size"`
	BillingCurrency       string                       `json:"billing_currency"`
	Status                string                       `json:"status"`
	Requests24h           int64                        `json:"requests_24h"`
	SuccessRate24h        *float64                     `json:"success_rate_24h"`
	AvgLatencyMS24h       *float64                     `json:"avg_latency_ms_24h"`
	LastUsedAt            *time.Time                   `json:"last_used_at"`
	ActiveCandidates      int                          `json:"active_candidates"`
	HealthStatus          string                       `json:"health_status"`
	ActiveCheckStatus     string                       `json:"active_check_status"`
	ActiveCheckReason     string                       `json:"active_check_reason,omitempty"`
	ActiveCheckStatusCode *int                         `json:"active_check_status_code,omitempty"`
	ActiveCheckLatencyMS  *int                         `json:"active_check_latency_ms,omitempty"`
	ActiveCheckError      string                       `json:"active_check_error,omitempty"`
	LastCheckedAt         *time.Time                   `json:"last_checked_at"`
	LastHealthyAt         *time.Time                   `json:"last_healthy_at"`
	Availability24h       *float64                     `json:"availability_24h"`
	Heartbeat24h          []governance.HeartbeatBucket `json:"heartbeat_24h"`
	CreatedAt             time.Time                    `json:"created_at"`
	UpdatedAt             time.Time                    `json:"updated_at"`
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
	if !ok || d.requirePermission(w, principal, auth.PermProviderRead) != nil {
		return
	}
	proxySummaries, err := loadPlatformActiveProxySummaries(r.Context(), d.Pool)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-accounts", nil)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT a.id, a.provider, a.name, a.region, a.base_url, a.billing_currency, a.status, a.created_at, a.updated_at,
		       COALESCE(hs.status, 'unknown'), COALESCE(hs.reason, ''), hs.http_status, hs.latency_ms,
		       COALESCE(hs.error_detail, ''), hs.last_checked_at, hs.last_healthy_at
		FROM provider_accounts a
		LEFT JOIN provider_account_health_states hs ON hs.provider_account_id = a.id
		WHERE a.organization_id = $1
		ORDER BY a.created_at DESC
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
		if err := rows.Scan(
			&id, &item.Provider, &item.Name, &item.Region, &item.BaseURL, &item.BillingCurrency, &item.Status, &item.CreatedAt, &item.UpdatedAt,
			&item.ActiveCheckStatus, &item.ActiveCheckReason, &item.ActiveCheckStatusCode, &item.ActiveCheckLatencyMS,
			&item.ActiveCheckError, &item.LastCheckedAt, &item.LastHealthyAt,
		); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-accounts", nil)
			return
		}
		item.ID = id.String()
		providerKey, err := bifrostx.MapProvider(item.Provider)
		if err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-accounts", nil)
			return
		}
		if sum, exists := proxySummaries[string(providerKey)]; exists && sum.ProxyType != "" {
			pid := sum.ProxyID
			pname := sum.ProxyName
			item.ProxyID = &pid
			item.ProxyName = &pname
			item.ProxyType = sum.ProxyType
			item.ProxyStatus = sum.ProxyStatus
			item.ProxyPoolSize = sum.PoolSize
		} else {
			item.ProxyType = bifrostx.ProxyTypeNone
			item.ProxyStatus = "none"
		}
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
		       avg(CASE WHEN u.state = 'success' THEN 1.0 ELSE 0.0 END) FILTER (WHERE u.id IS NOT NULL),
		       avg(u.total_ms) FILTER (WHERE u.state = 'success'),
		       max(u.created_at),
		       count(DISTINCT mc.id) FILTER (WHERE mc.status = 'active' AND mc.retired_at IS NULL)
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

	// 读取近 24h 主动探测明细构建心跳条与可用率。
	checkRows, err := d.Pool.Query(r.Context(), `
		SELECT provider_account_id, checked_at, ok, status, latency_ms, reason
		FROM provider_account_health_checks
		WHERE organization_id = $1 AND checked_at >= now() - interval '24 hours'
		ORDER BY checked_at ASC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-health-statistics", nil)
		return
	}
	defer checkRows.Close()
	checksByAccount := make(map[string][]probeCheckSample)
	for checkRows.Next() {
		var accID uuid.UUID
		var sample probeCheckSample
		if err := checkRows.Scan(&accID, &sample.CheckedAt, &sample.OK, &sample.Status, &sample.LatencyMS, &sample.Reason); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-health-statistics", nil)
			return
		}
		checksByAccount[accID.String()] = append(checksByAccount[accID.String()], sample)
	}

	now := time.Now()
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
			list[j].HealthStatus = deriveAccountHealthStatus(
				list[j].Status, list[j].ActiveCheckStatus,
				h.Requests, h.SuccessRate, h.AverageLatencyMS, h.ActiveCandidates,
			)
		}
		hb, avail := governance.BuildHeartbeat24h(toGovernanceSamples(checksByAccount[list[j].ID]), now)
		list[j].Heartbeat24h = hb
		list[j].Availability24h = avail
	}
	adminOK(w, list)
}

type providerAvailabilityItem struct {
	Provider            string                       `json:"provider"`
	Label               string                       `json:"label"`
	TotalAccounts       int                          `json:"total_accounts"`
	ActiveAccounts      int                          `json:"active_accounts"`
	HealthyAccounts     int                          `json:"healthy_accounts"`
	DegradedAccounts    int                          `json:"degraded_accounts"`
	UnhealthyAccounts   int                          `json:"unhealthy_accounts"`
	UnknownAccounts     int                          `json:"unknown_accounts"`
	UnsupportedAccounts int                          `json:"unsupported_accounts"`
	HealthStatus        string                       `json:"health_status"`
	Availability24h     *float64                     `json:"availability_24h"`
	AvgLatencyMS24h     *float64                     `json:"avg_latency_ms_24h"`
	Requests24h         int64                        `json:"requests_24h"`
	SuccessRate24h      *float64                     `json:"success_rate_24h"`
	ProxyPoolSize       int                          `json:"proxy_pool_size"`
	ActiveProxyID       *string                      `json:"active_proxy_id"`
	ActiveProxyName     *string                      `json:"active_proxy_name"`
	ActiveProxyType     string                       `json:"active_proxy_type"`
	ActiveProxyStatus   string                       `json:"active_proxy_status"`
	LastCheckedAt       *time.Time                   `json:"last_checked_at"`
	Heartbeat24h        []governance.HeartbeatBucket `json:"heartbeat_24h"`
}

type providerAvailabilitySummary struct {
	TotalProviders         int      `json:"total_providers"`
	TotalAccounts          int      `json:"total_accounts"`
	HealthyAccounts        int      `json:"healthy_accounts"`
	DegradedAccounts       int      `json:"degraded_accounts"`
	UnhealthyAccounts      int      `json:"unhealthy_accounts"`
	OverallAvailability24h *float64 `json:"overall_availability_24h"`
}

func (d ServerDeps) ProviderAccountsAvailability(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderRead) != nil {
		return
	}

	proxySummaries, err := loadPlatformActiveProxySummaries(r.Context(), d.Pool)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-health-statistics", nil)
		return
	}
	settings, err := d.loadProviderHealthSettings(r.Context())
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-health-statistics", nil)
		return
	}

	rows, err := d.Pool.Query(r.Context(), `
		SELECT a.id, a.provider, a.status,
		       COALESCE(hs.status, 'unknown'), hs.last_checked_at,
		       count(u.id),
		       count(u.id) FILTER (WHERE u.state = 'success'),
		       avg(u.total_ms) FILTER (WHERE u.state = 'success'),
		       count(DISTINCT mc.id) FILTER (WHERE mc.status = 'active' AND mc.retired_at IS NULL)
		FROM provider_accounts a
		LEFT JOIN provider_account_health_states hs ON hs.provider_account_id = a.id
		LEFT JOIN model_candidates mc ON mc.provider_account_id = a.id
		LEFT JOIN usage_records u ON u.model_candidate_id = mc.id
		     AND u.created_at >= now() - interval '24 hours'
		     AND u.state <> 'pending'
		WHERE a.organization_id = $1
		GROUP BY a.id, a.provider, a.status, hs.status, hs.last_checked_at
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-health-statistics", nil)
		return
	}
	defer rows.Close()

	type accStat struct {
		provider      string
		status        string
		activeCheck   string
		lastChecked   *time.Time
		reqs          int64
		succ          int64
		avgLatencyMS  *float64
		activeCands   int
	}
	type providerAgg struct {
		item        providerAvailabilityItem
		totalReqs   int64
		totalSucc   int64
		passiveSums float64
		passiveCnt  int64
	}
	byProvider := make(map[string]*providerAgg)
	summary := providerAvailabilitySummary{}

	for rows.Next() {
		var id uuid.UUID
		var st accStat
		if err := rows.Scan(
			&id, &st.provider, &st.status, &st.activeCheck, &st.lastChecked,
			&st.reqs, &st.succ, &st.avgLatencyMS, &st.activeCands,
		); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-health-statistics", nil)
			return
		}
		agg, exists := byProvider[st.provider]
		if !exists {
			agg = &providerAgg{
				item: providerAvailabilityItem{
					Provider:          st.provider,
					Label:             providerKeyLabel(st.provider),
					ActiveProxyType:   bifrostx.ProxyTypeNone,
					ActiveProxyStatus: "none",
				},
			}
			byProvider[st.provider] = agg
		}
		agg.item.TotalAccounts++
		summary.TotalAccounts++
		if st.status == "active" {
			agg.item.ActiveAccounts++
		}
		var passiveRate *float64
		if st.reqs > 0 {
			r := float64(st.succ) / float64(st.reqs)
			passiveRate = &r
			agg.totalReqs += st.reqs
			agg.totalSucc += st.succ
			if st.avgLatencyMS != nil {
				agg.passiveSums += *st.avgLatencyMS
				agg.passiveCnt++
			}
		}
		derived := deriveAccountHealthStatus(st.status, st.activeCheck, st.reqs, passiveRate, st.avgLatencyMS, st.activeCands)
		switch derived {
		case "healthy":
			agg.item.HealthyAccounts++
			summary.HealthyAccounts++
		case "degraded":
			agg.item.DegradedAccounts++
			summary.DegradedAccounts++
		case "unhealthy":
			agg.item.UnhealthyAccounts++
			summary.UnhealthyAccounts++
		default:
			if st.activeCheck == "unsupported" {
				agg.item.UnsupportedAccounts++
			} else {
				agg.item.UnknownAccounts++
			}
		}
		if st.lastChecked != nil && (agg.item.LastCheckedAt == nil || st.lastChecked.After(*agg.item.LastCheckedAt)) {
			t := *st.lastChecked
			agg.item.LastCheckedAt = &t
		}
	}

	checkRows, err := d.Pool.Query(r.Context(), `
		SELECT provider, checked_at, ok, status, latency_ms, reason
		FROM provider_account_health_checks
		WHERE organization_id = $1 AND checked_at >= now() - interval '24 hours'
		ORDER BY checked_at ASC
	`, principal.OrganizationID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-health-statistics", nil)
		return
	}
	defer checkRows.Close()
	checksByProvider := make(map[string][]probeCheckSample)
	var allChecksOK, allChecksTotal int
	for checkRows.Next() {
		var provider string
		var sample probeCheckSample
		if err := checkRows.Scan(&provider, &sample.CheckedAt, &sample.OK, &sample.Status, &sample.LatencyMS, &sample.Reason); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_LIST_FAILED", "failed-to-query-provider-health-statistics", nil)
			return
		}
		allChecksTotal++
		if sample.OK {
			allChecksOK++
		}
		checksByProvider[provider] = append(checksByProvider[provider], sample)
	}

	now := time.Now()
	keys := make([]string, 0, len(byProvider))
	for k := range byProvider {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return providerKeyLabel(keys[i]) < providerKeyLabel(keys[j]) })

	items := make([]providerAvailabilityItem, 0, len(keys))
	for _, k := range keys {
		agg := byProvider[k]
		if pKey, err := bifrostx.MapProvider(k); err == nil {
			if sum, ok := proxySummaries[string(pKey)]; ok && sum.ProxyType != "" {
				pid := sum.ProxyID
				pname := sum.ProxyName
				agg.item.ActiveProxyID = &pid
				agg.item.ActiveProxyName = &pname
				agg.item.ActiveProxyType = sum.ProxyType
				agg.item.ActiveProxyStatus = sum.ProxyStatus
				agg.item.ProxyPoolSize = sum.PoolSize
			}
		}
		agg.item.Requests24h = agg.totalReqs
		if agg.totalReqs > 0 {
			rate := float64(agg.totalSucc) / float64(agg.totalReqs)
			agg.item.SuccessRate24h = &rate
		}
		samples := checksByProvider[k]
		hb, avail := governance.BuildHeartbeat24h(toGovernanceSamples(samples), now)
		agg.item.Heartbeat24h = hb
		agg.item.Availability24h = avail
		if lat := avgSampleLatency(samples); lat != nil {
			agg.item.AvgLatencyMS24h = lat
		} else if agg.passiveCnt > 0 {
			lat := agg.passiveSums / float64(agg.passiveCnt)
			agg.item.AvgLatencyMS24h = &lat
		}

		switch {
		case agg.item.ActiveAccounts == 0:
			agg.item.HealthStatus = "disabled"
		case agg.item.HealthyAccounts == 0 && agg.item.UnhealthyAccounts > 0:
			agg.item.HealthStatus = "unhealthy"
		case agg.item.UnhealthyAccounts > 0 || agg.item.DegradedAccounts > 0:
			agg.item.HealthStatus = "degraded"
		case agg.item.HealthyAccounts > 0:
			agg.item.HealthStatus = "healthy"
		default:
			agg.item.HealthStatus = "no_data"
		}
		items = append(items, agg.item)
	}

	summary.TotalProviders = len(items)
	if allChecksTotal > 0 {
		overall := float64(allChecksOK) * 100 / float64(allChecksTotal)
		summary.OverallAvailability24h = &overall
	}
	adminOK(w, map[string]any{
		"providers": items,
		"settings":  settings,
		"summary":   summary,
	})
}

func (d ServerDeps) TestAllProviderAccounts(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderManage) != nil {
		return
	}
	if d.Governance == nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_TEST_FAILED", "connectivity-test-failed", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	batch, err := d.Governance.ProbeOrganizationAccounts(ctx, principal.OrganizationID, "manual")
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_TEST_FAILED", "connectivity-test-failed", nil)
		return
	}
	adminOK(w, batch)
}

func (d ServerDeps) ProviderAccountHealthHistory(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderRead) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_ID_INVALID", "invalid-provider-account-id", nil)
		return
	}
	limit := 50
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, parseErr := strconv.Atoi(rawLimit); parseErr == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	var provider, name string
	var currentStatus, currentReason, currentError string
	var currentHTTPStatus, currentLatencyMS *int
	var currentProxyName *string
	var consecutiveFailures int
	var lastCheckedAt, lastHealthyAt *time.Time
	err = d.Pool.QueryRow(r.Context(), `
		SELECT a.provider, a.name,
		       COALESCE(hs.status, 'unknown'), COALESCE(hs.reason, ''), hs.http_status, hs.latency_ms,
		       hs.proxy_name, COALESCE(hs.error_detail, ''), COALESCE(hs.consecutive_failures, 0),
		       hs.last_checked_at, hs.last_healthy_at
		FROM provider_accounts a
		LEFT JOIN provider_account_health_states hs ON hs.provider_account_id = a.id
		WHERE a.id = $1 AND a.organization_id = $2
	`, accountID, principal.OrganizationID).Scan(
		&provider, &name, &currentStatus, &currentReason, &currentHTTPStatus, &currentLatencyMS,
		&currentProxyName, &currentError, &consecutiveFailures, &lastCheckedAt, &lastHealthyAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider-account-does-not-exist-or", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_HEALTH_FAILED", "failed-to-query-provider-health-statistics", nil)
		return
	}

	var avail7d, avgLatency24h, p95Latency24h *float64
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT
			avg(CASE WHEN ok THEN 100.0 ELSE 0.0 END),
			avg(latency_ms) FILTER (WHERE checked_at >= now() - interval '24 hours' AND latency_ms IS NOT NULL),
			percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) FILTER (WHERE checked_at >= now() - interval '24 hours' AND latency_ms IS NOT NULL)
		FROM provider_account_health_checks
		WHERE provider_account_id = $1 AND checked_at >= now() - interval '7 days'
	`, accountID).Scan(&avail7d, &avgLatency24h, &p95Latency24h); err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_HEALTH_FAILED", "failed-to-query-provider-health-statistics", nil)
		return
	}

	hbRows, err := d.Pool.Query(r.Context(), `
		SELECT checked_at, ok, status, latency_ms, reason
		FROM provider_account_health_checks
		WHERE provider_account_id = $1 AND checked_at >= now() - interval '24 hours'
		ORDER BY checked_at ASC
	`, accountID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_HEALTH_FAILED", "failed-to-query-provider-health-statistics", nil)
		return
	}
	defer hbRows.Close()
	hbSamples := []probeCheckSample{}
	for hbRows.Next() {
		var s probeCheckSample
		if err := hbRows.Scan(&s.CheckedAt, &s.OK, &s.Status, &s.LatencyMS, &s.Reason); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_HEALTH_FAILED", "failed-to-query-provider-health-statistics", nil)
			return
		}
		hbSamples = append(hbSamples, s)
	}
	heartbeat, avail24h := governance.BuildHeartbeat24h(toGovernanceSamples(hbSamples), time.Now())

	type healthCheckRecord struct {
		ID          int64     `json:"id"`
		Trigger     string    `json:"trigger"`
		OK          bool      `json:"ok"`
		Status      string    `json:"status"`
		Reason      string    `json:"reason"`
		HTTPStatus  *int      `json:"http_status"`
		LatencyMS   *int      `json:"latency_ms"`
		ProxyName   *string   `json:"proxy_name"`
		ErrorDetail string    `json:"error_detail,omitempty"`
		CheckedAt   time.Time `json:"checked_at"`
	}
	rows, err := d.Pool.Query(r.Context(), `
		SELECT id, trigger, ok, status, reason, http_status, latency_ms, proxy_name, COALESCE(error_detail, ''), checked_at
		FROM provider_account_health_checks
		WHERE provider_account_id = $1
		ORDER BY checked_at DESC
		LIMIT $2
	`, accountID, limit)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_HEALTH_FAILED", "failed-to-query-provider-health-statistics", nil)
		return
	}
	defer rows.Close()
	checks := []healthCheckRecord{}
	for rows.Next() {
		var rec healthCheckRecord
		if err := rows.Scan(&rec.ID, &rec.Trigger, &rec.OK, &rec.Status, &rec.Reason, &rec.HTTPStatus, &rec.LatencyMS, &rec.ProxyName, &rec.ErrorDetail, &rec.CheckedAt); err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_HEALTH_FAILED", "failed-to-query-provider-health-statistics", nil)
			return
		}
		checks = append(checks, rec)
	}

	adminOK(w, map[string]any{
		"account_id":           accountID.String(),
		"provider":             provider,
		"name":                 name,
		"current_status":       currentStatus,
		"current_reason":       currentReason,
		"current_http_status":  currentHTTPStatus,
		"current_latency_ms":   currentLatencyMS,
		"current_proxy_name":   currentProxyName,
		"current_error_detail": currentError,
		"consecutive_failures": consecutiveFailures,
		"last_checked_at":      lastCheckedAt,
		"last_healthy_at":      lastHealthyAt,
		"availability_24h":     avail24h,
		"availability_7d":      avail7d,
		"avg_latency_ms_24h":   avgLatency24h,
		"p95_latency_ms_24h":   p95Latency24h,
		"heartbeat_24h":        heartbeat,
		"checks":               checks,
	})
}

func (d ServerDeps) ProviderHealthSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderRead) != nil {
		return
	}
	settings, err := d.loadProviderHealthSettings(r.Context())
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_HEALTH_SETTINGS_FAILED", "failed-to-query-provider-health-settings", nil)
		return
	}
	adminOK(w, settings)
}

func (d ServerDeps) UpdateProviderHealthSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return
	}
	if !isPlatformAdmin(principal) {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	var body governance.ProviderHealthSettings
	if !d.decodeJSON(w, r, &body) {
		return
	}
	if body.IntervalSeconds < 60 || body.IntervalSeconds > 86400 ||
		body.TimeoutSeconds < 3 || body.TimeoutSeconds > 60 ||
		body.DegradedLatencyMS < 500 || body.DegradedLatencyMS > 60000 ||
		body.FailureThreshold < 1 || body.FailureThreshold > 10 {
		adminError(w, http.StatusBadRequest, "PROVIDER_HEALTH_SETTINGS_INVALID", "provider-health-settings-invalid", nil)
		return
	}
	if d.Governance == nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_HEALTH_SETTINGS_FAILED", "failed-to-update-provider-health-settings", nil)
		return
	}
	before, _ := d.Governance.LoadProviderHealthSettings(r.Context())
	saved, err := d.Governance.SaveProviderHealthSettings(r.Context(), body, principal.UserID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_HEALTH_SETTINGS_FAILED", "failed-to-update-provider-health-settings", nil)
		return
	}
	if err := writeAuditEvent(r.Context(), d.Pool, principal.OrganizationID, principal.UserID,
		"provider_health.settings_updated", "provider_account", "health_settings",
		before, saved); err != nil {
		d.Logger.Warn("write provider health settings audit event failed", "error", err)
	}
	adminOK(w, saved)
}

func (d ServerDeps) loadProviderHealthSettings(ctx context.Context) (governance.ProviderHealthSettings, error) {
	if d.Governance != nil {
		return d.Governance.LoadProviderHealthSettings(ctx)
	}
	return governance.NewManager(d.Pool, d.MasterKey, d.Logger, nil).LoadProviderHealthSettings(ctx)
}

// CreateProviderAccount 创建 供应商账号并把 API Key 用主密钥加密入库。
func (d ServerDeps) CreateProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderManage) != nil {
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
		adminError(w, http.StatusBadRequest, "PROVIDER_CURRENCY_INVALID", "provider-currency-invalid", nil)
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

// DeleteProviderAccount 删除供应商账号;仍有在用候选时拒绝删除。
// 已退役候选会随账号删除,历史 usage 保留 provider/upstream_model,仅失去 candidate_id 关联。
func (d ServerDeps) DeleteProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderManage) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_INVALID_ID", "invalid-account-id", nil)
		return
	}
	var exists bool
	if err := d.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM provider_accounts WHERE id = $1 AND organization_id = $2)`, accountID, principal.OrganizationID).Scan(&exists); err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_DELETE_FAILED", "failed-to-delete-the-provider-account", nil)
		return
	}
	if !exists {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider-account-does-not-exist", nil)
		return
	}
	var count int
	if err := d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM model_candidates WHERE provider_account_id = $1 AND retired_at IS NULL
	`, accountID).Scan(&count); err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_DELETE_FAILED", "failed-to-delete-the-provider-account", nil)
		return
	}
	if count > 0 {
		adminError(w, http.StatusConflict, "PROVIDER_IN_USE", "this-account-is-still-referenced-by", map[string]any{"candidate_count": count})
		return
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `DELETE FROM model_candidates WHERE provider_account_id = $1 AND retired_at IS NOT NULL`, accountID); err != nil {
			return err
		}
		// 资源删除后已查不到名称,审计里记下它,审计列表才能显示“删的是什么”。
		var name string
		if err := tx.QueryRow(r.Context(), `DELETE FROM provider_accounts WHERE id = $1 AND organization_id = $2 RETURNING name`,
			accountID, principal.OrganizationID).Scan(&name); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"provider_account.deleted", "provider_account", accountID.String(), map[string]any{"name": name}, nil)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider-account-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_DELETE_FAILED", "failed-to-delete-the-provider-account", nil)
		return
	}
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
	APIKey          string  `json:"api_key"`
}

// UpdateProviderAccount 更新账号名称、区域、Base URL 与结算币种;API Key 留空表示不更换。
func (d ServerDeps) UpdateProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderManage) != nil {
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
	var oldName, provider, oldBillingCurrency string
	var oldRegion, oldBaseURL *string
	err = d.Pool.QueryRow(r.Context(), `
		SELECT name, provider, billing_currency, region, base_url
		FROM provider_accounts
		WHERE id = $1 AND organization_id = $2
	`, accountID, principal.OrganizationID).Scan(&oldName, &provider, &oldBillingCurrency, &oldRegion, &oldBaseURL)
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
		adminError(w, http.StatusBadRequest, "PROVIDER_CURRENCY_INVALID", "provider-currency-invalid", nil)
		return
	}
	newAPIKey := strings.TrimSpace(body.APIKey)
	rotateCredential := newAPIKey != ""
	var ciphertext []byte
	if rotateCredential {
		ciphertext, err = vault.Encrypt(d.MasterKey, []byte(newAPIKey))
		if err != nil {
			adminError(w, http.StatusInternalServerError, "PROVIDER_UPDATE_FAILED", "failed-to-update-the-provider-account", nil)
			return
		}
	}
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			UPDATE provider_accounts SET name = $3, region = $4, base_url = COALESCE($5, base_url), billing_currency = $6, updated_at = now()
			WHERE id = $1 AND organization_id = $2
		`, accountID, principal.OrganizationID, body.Name, body.Region, body.BaseURL, billingCurrency); err != nil {
			return err
		}
		if rotateCredential {
			if _, err := tx.Exec(r.Context(), `
				UPDATE provider_credentials SET ciphertext = $2, key_id = $3, updated_at = now()
				WHERE provider_account_id = $1
			`, accountID, ciphertext, vault.KeyID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE model_candidates SET cost_currency = $2, updated_at = now()
			WHERE provider_account_id = $1 AND retired_at IS NULL AND cost_currency <> $2
		`, accountID, billingCurrency); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO audit_events(organization_id, actor_user_id, actor_type, action, resource_type, resource_id, before_state, after_state)
			VALUES ($1,$2,'user','provider_account.updated','provider_account',$3,$4,$5)
		`, principal.OrganizationID, principal.UserID, accountID.String(),
			auditJSON(map[string]any{"name": oldName, "region": oldRegion, "base_url": oldBaseURL,
				"billing_currency": oldBillingCurrency, "credentials_rotated": false}),
			auditJSON(map[string]any{"name": body.Name, "region": body.Region, "base_url": body.BaseURL,
				"billing_currency":    billingCurrency,
				"credentials_rotated": rotateCredential}))
		return err
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "provider_accounts_organization_id_name_key" {
			adminError(w, http.StatusConflict, "PROVIDER_NAME_EXISTS", "a-provider-account-with-the-same", nil)
			return
		}
		adminError(w, http.StatusInternalServerError, "PROVIDER_UPDATE_FAILED", "failed-to-update-the-provider-account", nil)
		return
	}
	// base_url/凭据相关配置变化需要重建上游引擎。
	if !d.reloadEngine(w, r) {
		return
	}
	adminOK(w, nil)
}

type providerTestResult struct {
	OK         bool       `json:"ok"`
	Status     string     `json:"status,omitempty"`
	Reason     string     `json:"reason"`
	StatusCode int        `json:"status_code,omitempty"`
	LatencyMS  int        `json:"latency_ms,omitempty"`
	ProxyName  *string    `json:"proxy_name,omitempty"`
	Detail     string     `json:"detail,omitempty"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
}

type providerAccess struct {
	Provider  string
	BaseURL   *string
	APIKey    string
	ProxyType string
	ProxyURL  string
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
	providerKey, err := bifrostx.MapProvider(access.Provider)
	if err != nil {
		return access, err
	}
	proxy, err := loadPlatformProxyForProvider(ctx, d.Pool, d.MasterKey, string(providerKey))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			proxy = platformProxy{Type: bifrostx.ProxyTypeNone}
		} else {
			return access, err
		}
	}
	access.ProxyType = proxy.Type
	access.ProxyURL = proxy.URL
	return access, nil
}

// TestProviderAccount 用账号凭据向上游发起轻量列表请求,验证连通性与凭据有效性,并将结果落库。
func (d ServerDeps) TestProviderAccount(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || d.requirePermission(w, principal, auth.PermProviderManage) != nil {
		return
	}
	accountID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PROVIDER_ID_INVALID", "invalid-provider-account-id", nil)
		return
	}
	gov := d.Governance
	if gov == nil {
		gov = governance.NewManager(d.Pool, d.MasterKey, d.Logger, d.Engine)
	}
	res, err := gov.ProbeAccount(r.Context(), principal.OrganizationID, accountID, "manual")
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider-account-does-not-exist-is", nil)
		return
	}
	if errors.Is(err, governance.ErrProviderTestUnsupported) {
		adminError(w, http.StatusBadRequest, "PROVIDER_TEST_UNSUPPORTED", "provider-test-unsupported", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PROVIDER_TEST_FAILED", "connectivity-test-failed", nil)
		return
	}
	checkedAt := res.CheckedAt
	adminOK(w, providerTestResult{
		OK:         res.OK,
		Status:     res.Status,
		Reason:     res.Reason,
		StatusCode: res.StatusCode,
		LatencyMS:  res.LatencyMS,
		ProxyName:  res.ProxyName,
		Detail:     res.Detail,
		CheckedAt:  &checkedAt,
	})
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
