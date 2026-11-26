package httpx

import (
	"context"
	"errors"
	"fmt"
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
)

type proxyBindingDetail struct {
	ProviderKey   string     `json:"provider_key"`
	ProviderLabel string     `json:"provider_label"`
	Priority      int        `json:"priority"`
	Enabled       bool       `json:"enabled"`
	Status        string     `json:"status"`
	LastLatencyMS *int       `json:"last_latency_ms"`
	LastReason    string     `json:"last_reason,omitempty"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
}

type platformProxyResponse struct {
	ID              string                       `json:"id"`
	Name            string                       `json:"name"`
	ProviderKeys    []string                     `json:"provider_keys"`
	ProviderLabels  []string                     `json:"provider_labels"`
	Bindings        []proxyBindingDetail         `json:"bindings"`
	ProxyType       string                       `json:"proxy_type"`
	ProxyURL        string                       `json:"proxy_url,omitempty"`
	Enabled         bool                         `json:"enabled"`
	ProbeURL        string                       `json:"probe_url,omitempty"`
	UpdatedAt       *time.Time                   `json:"updated_at"`
	ActiveAccounts  int                          `json:"active_accounts"`
	HealthStatus    string                       `json:"health_status"`
	Availability24h *float64                     `json:"availability_24h"`
	AvgLatencyMS24h *float64                     `json:"avg_latency_ms_24h"`
	Heartbeat24h    []governance.HeartbeatBucket `json:"heartbeat_24h"`
}

type proxyPoolMemberResponse struct {
	ProxyID              string                       `json:"proxy_id"`
	ProxyName            string                       `json:"proxy_name"`
	ProxyType            string                       `json:"proxy_type"`
	ProxyURL             string                       `json:"proxy_url,omitempty"`
	ProxyEnabled         bool                         `json:"proxy_enabled"`
	BindingEnabled       bool                         `json:"binding_enabled"`
	Priority             int                          `json:"priority"`
	IsActive             bool                         `json:"is_active"`
	Status               string                       `json:"status"`
	ConsecutiveFailures  int                          `json:"consecutive_failures"`
	ConsecutiveSuccesses int                          `json:"consecutive_successes"`
	EjectionCount        int                          `json:"ejection_count"`
	CooldownUntil        *time.Time                   `json:"cooldown_until"`
	HealthySince         *time.Time                   `json:"healthy_since"`
	LastLatencyMS        *int                         `json:"last_latency_ms"`
	LastReason           string                       `json:"last_reason,omitempty"`
	LastErrorDetail      string                       `json:"last_error_detail,omitempty"`
	LastCheckedAt        *time.Time                   `json:"last_checked_at"`
	Availability24h      *float64                     `json:"availability_24h"`
	Heartbeat24h         []governance.HeartbeatBucket `json:"heartbeat_24h"`
}

type providerProxyPoolResponse struct {
	ProviderKey       string                    `json:"provider_key"`
	ProviderLabel     string                    `json:"provider_label"`
	ActiveProxyID     *string                   `json:"active_proxy_id"`
	ActiveProxyName   *string                   `json:"active_proxy_name"`
	ActiveProxyType   *string                   `json:"active_proxy_type"`
	ActiveProxyStatus string                    `json:"active_proxy_status"`
	ActiveSince       *time.Time                `json:"active_since"`
	LastSwitchReason  *string                   `json:"last_switch_reason,omitempty"`
	HealthyCount      int                       `json:"healthy_count"`
	TotalCount        int                       `json:"total_count"`
	ActiveAccounts    int                       `json:"active_accounts"`
	Members           []proxyPoolMemberResponse `json:"members"`
}

type providerOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type platformProxyListResponse struct {
	Proxies         []platformProxyResponse            `json:"proxies"`
	Pools           []providerProxyPoolResponse        `json:"pools"`
	Settings        governance.ProxyGovernanceSettings `json:"settings"`
	ProviderOptions []providerOption                   `json:"provider_options"`
}

type platformProxyBindingInput struct {
	ProviderKey string `json:"provider_key"`
	Priority    int    `json:"priority"`
	Enabled     *bool  `json:"enabled"`
}

type platformProxyRequest struct {
	ProviderKeys []string                    `json:"provider_keys"`
	Bindings     []platformProxyBindingInput `json:"bindings"`
	Name         string                      `json:"name"`
	ProxyType    string                      `json:"proxy_type"`
	ProxyURL     string                      `json:"proxy_url"`
	Enabled      *bool                       `json:"enabled"`
	ProbeURL     *string                     `json:"probe_url"`
}

func redactProxyURL(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid proxy URL: %w", err)
	}
	parsed.User = nil
	return parsed.String(), nil
}

func providerKeyLabel(providerKey string) string {
	labels := map[string]string{
		"openai":    "OpenAI / OpenAI Compatible",
		"azure":     "Azure OpenAI",
		"anthropic": "Anthropic",
		"gemini":    "Gemini",
		"vertex":    "Vertex AI",
		"bedrock":   "Bedrock",
	}
	if label, exists := labels[providerKey]; exists {
		return label
	}
	if vendor := bifrostx.VendorByID(providerKey); vendor != nil {
		return vendor.Name
	}
	return providerKey
}

func providerLabels(providerKeys []string) []string {
	labels := make([]string, 0, len(providerKeys))
	for _, providerKey := range providerKeys {
		labels = append(labels, providerKeyLabel(providerKey))
	}
	return labels
}

func normalizeProxyBindings(rawBindings []platformProxyBindingInput, rawKeys []string) ([]platformProxyBinding, []string, bool) {
	seen := make(map[string]bool)
	bindings := make([]platformProxyBinding, 0, len(rawBindings)+len(rawKeys))
	if len(rawBindings) > 0 {
		for _, item := range rawBindings {
			key := strings.TrimSpace(item.ProviderKey)
			if key == "" || seen[key] || !bifrostx.IsSupportedProviderKey(key) {
				return nil, nil, false
			}
			seen[key] = true
			prio := item.Priority
			if prio == 0 {
				prio = 100
			}
			if prio < 1 {
				return nil, nil, false
			}
			enabled := true
			if item.Enabled != nil {
				enabled = *item.Enabled
			}
			bindings = append(bindings, platformProxyBinding{
				ProviderKey: key,
				Priority:    prio,
				Enabled:     enabled,
			})
		}
	} else {
		for _, rawKey := range rawKeys {
			key := strings.TrimSpace(rawKey)
			if key == "" || seen[key] || !bifrostx.IsSupportedProviderKey(key) {
				return nil, nil, false
			}
			seen[key] = true
			bindings = append(bindings, platformProxyBinding{
				ProviderKey: key,
				Priority:    100,
				Enabled:     true,
			})
		}
	}
	if len(bindings) == 0 {
		return nil, nil, false
	}
	sort.Slice(bindings, func(i, j int) bool {
		return providerKeyLabel(bindings[i].ProviderKey) < providerKeyLabel(bindings[j].ProviderKey)
	})
	keys := make([]string, len(bindings))
	for i, b := range bindings {
		keys[i] = b.ProviderKey
	}
	return bindings, keys, true
}

func platformProviderOptions() []providerOption {
	seen := make(map[string]bool)
	options := []providerOption{}
	for _, key := range bifrostx.SupportedProviderKeys() {
		if seen[key] {
			continue
		}
		seen[key] = true
		options = append(options, providerOption{Value: key, Label: providerKeyLabel(key)})
	}
	sort.Slice(options, func(i, j int) bool { return options[i].Label < options[j].Label })
	return options
}

func activeAccountsFor(proxy platformProxy, activeCounts map[string]int) int {
	total := 0
	for _, providerKey := range proxy.ProviderKeys {
		total += activeCounts[providerKey]
	}
	return total
}

type proxyHealthRow struct {
	ProviderKey          string
	ProxyID              uuid.UUID
	Status               string
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	EjectionCount        int
	CooldownUntil        *time.Time
	HealthySince         *time.Time
	LastLatencyMS        *int
	LastReason           string
	LastErrorDetail      string
	LastCheckedAt        *time.Time
}

type probeCheckSample struct {
	CheckedAt time.Time
	OK        bool
	Status    string
	LatencyMS *int
	Reason    string
}

func (d ServerDeps) PlatformProxies(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}

	proxies, err := loadPlatformProxies(r.Context(), d.Pool, d.MasterKey)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
		return
	}
	activeCounts, err := d.activeProviderAccountCounts(r.Context())
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
		return
	}

	settings, err := d.loadProxyGovSettings(r.Context())
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
		return
	}

	// 读取每个绑定当前健康态。
	healthRows, err := d.Pool.Query(r.Context(), `
		SELECT provider_key, proxy_id, status, consecutive_failures, consecutive_successes,
		       ejection_count, cooldown_until, healthy_since, last_latency_ms,
		       COALESCE(last_reason, ''), COALESCE(last_error_detail, ''), last_checked_at
		FROM proxy_health_states
	`)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
		return
	}
	defer healthRows.Close()
	healthByPair := make(map[string]proxyHealthRow)
	for healthRows.Next() {
		var h proxyHealthRow
		if err := healthRows.Scan(
			&h.ProviderKey, &h.ProxyID, &h.Status, &h.ConsecutiveFailures, &h.ConsecutiveSuccesses,
			&h.EjectionCount, &h.CooldownUntil, &h.HealthySince, &h.LastLatencyMS,
			&h.LastReason, &h.LastErrorDetail, &h.LastCheckedAt,
		); err != nil {
			adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
			return
		}
		healthByPair[h.ProviderKey+"|"+h.ProxyID.String()] = h
	}

	// 读取各厂商当前粘性生效代理。
	stateRows, err := d.Pool.Query(r.Context(), `
		SELECT provider_key, active_proxy_id, last_switched_at, last_switch_reason
		FROM provider_proxy_states
	`)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
		return
	}
	defer stateRows.Close()
	type poolStateRow struct {
		ActiveProxyID    *uuid.UUID
		LastSwitchedAt   *time.Time
		LastSwitchReason *string
	}
	poolStates := make(map[string]poolStateRow)
	for stateRows.Next() {
		var key string
		var activeID uuid.NullUUID
		var st poolStateRow
		if err := stateRows.Scan(&key, &activeID, &st.LastSwitchedAt, &st.LastSwitchReason); err != nil {
			adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
			return
		}
		if activeID.Valid {
			v := activeID.UUID
			st.ActiveProxyID = &v
		}
		poolStates[key] = st
	}

	// 一次性读取近 24 小时探针记录，用于节点和厂商池成员的心跳条与可用率统计。
	checkRows, err := d.Pool.Query(r.Context(), `
		SELECT proxy_id, provider_key, checked_at, ok, status, latency_ms, reason
		FROM proxy_probe_checks
		WHERE checked_at >= now() - interval '24 hours'
		ORDER BY checked_at ASC
	`)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
		return
	}
	defer checkRows.Close()
	checksByProxy := make(map[string][]probeCheckSample)
	checksByPair := make(map[string][]probeCheckSample)
	for checkRows.Next() {
		var pID uuid.UUID
		var pKey string
		var sample probeCheckSample
		if err := checkRows.Scan(&pID, &pKey, &sample.CheckedAt, &sample.OK, &sample.Status, &sample.LatencyMS, &sample.Reason); err != nil {
			adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
			return
		}
		pid := pID.String()
		checksByProxy[pid] = append(checksByProxy[pid], sample)
		checksByPair[pKey+"|"+pid] = append(checksByPair[pKey+"|"+pid], sample)
	}

	now := time.Now()
	proxyByID := make(map[uuid.UUID]platformProxy, len(proxies))
	redactedByID := make(map[uuid.UUID]string, len(proxies))
	list := make([]platformProxyResponse, 0, len(proxies))
	for _, proxy := range proxies {
		proxyByID[proxy.ID] = proxy
		redactedURL, redactErr := redactProxyURL(proxy.URL)
		if redactErr != nil {
			adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
			return
		}
		redactedByID[proxy.ID] = redactedURL
		updatedAt := proxy.UpdatedAt

		bindingDetails := make([]proxyBindingDetail, 0, len(proxy.Bindings))
		for _, b := range proxy.Bindings {
			h := healthByPair[b.ProviderKey+"|"+proxy.ID.String()]
			st := h.Status
			if st == "" {
				st = "unknown"
			}
			bindingDetails = append(bindingDetails, proxyBindingDetail{
				ProviderKey:   b.ProviderKey,
				ProviderLabel: providerKeyLabel(b.ProviderKey),
				Priority:      b.Priority,
				Enabled:       b.Enabled,
				Status:        st,
				LastLatencyMS: h.LastLatencyMS,
				LastReason:    h.LastReason,
				LastCheckedAt: h.LastCheckedAt,
			})
		}

		samples := checksByProxy[proxy.ID.String()]
		hb, avail := governance.BuildHeartbeat24h(toGovernanceSamples(samples), now)
		avgLat := avgSampleLatency(samples)
		list = append(list, platformProxyResponse{
			ID:              proxy.ID.String(),
			Name:            proxy.Name,
			ProviderKeys:    proxy.ProviderKeys,
			ProviderLabels:  providerLabels(proxy.ProviderKeys),
			Bindings:        bindingDetails,
			ProxyType:       proxy.Type,
			ProxyURL:        redactedURL,
			Enabled:         proxy.Enabled,
			ProbeURL:        proxy.ProbeURL,
			UpdatedAt:       &updatedAt,
			ActiveAccounts:  activeAccountsFor(proxy, activeCounts),
			HealthStatus:    deriveProxyNodeHealth(proxy.Enabled, bindingDetails),
			Availability24h: avail,
			AvgLatencyMS24h: avgLat,
			Heartbeat24h:    hb,
		})
	}

	// 按厂商聚合代理池与主备顺序。
	poolRows, err := d.Pool.Query(r.Context(), `
		SELECT pp.provider_key, pp.proxy_id, pp.priority, pp.enabled
		FROM platform_proxy_providers pp
		ORDER BY pp.provider_key ASC, pp.priority ASC, pp.created_at ASC
	`)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
		return
	}
	defer poolRows.Close()

	poolMap := make(map[string]*providerProxyPoolResponse)
	poolKeys := []string{}
	for poolRows.Next() {
		var providerKey string
		var proxyID uuid.UUID
		var priority int
		var bindingEnabled bool
		if err := poolRows.Scan(&providerKey, &proxyID, &priority, &bindingEnabled); err != nil {
			adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
			return
		}
		proxy, ok := proxyByID[proxyID]
		if !ok {
			continue
		}
		pool, exists := poolMap[providerKey]
		if !exists {
			st := poolStates[providerKey]
			pool = &providerProxyPoolResponse{
				ProviderKey:       providerKey,
				ProviderLabel:     providerKeyLabel(providerKey),
				ActiveProxyStatus: "unknown",
				ActiveSince:       st.LastSwitchedAt,
				LastSwitchReason:  st.LastSwitchReason,
				ActiveAccounts:    activeCounts[providerKey],
				Members:           []proxyPoolMemberResponse{},
			}
			poolMap[providerKey] = pool
			poolKeys = append(poolKeys, providerKey)
		}

		st := poolStates[providerKey]
		isActive := st.ActiveProxyID != nil && *st.ActiveProxyID == proxyID
		h := healthByPair[providerKey+"|"+proxyID.String()]
		status := h.Status
		if status == "" {
			status = "unknown"
		}
		samples := checksByPair[providerKey+"|"+proxyID.String()]
		hb, avail := governance.BuildHeartbeat24h(toGovernanceSamples(samples), now)

		member := proxyPoolMemberResponse{
			ProxyID:              proxyID.String(),
			ProxyName:            proxy.Name,
			ProxyType:            proxy.Type,
			ProxyURL:             redactedByID[proxyID],
			ProxyEnabled:         proxy.Enabled,
			BindingEnabled:       bindingEnabled,
			Priority:             priority,
			IsActive:             isActive,
			Status:               status,
			ConsecutiveFailures:  h.ConsecutiveFailures,
			ConsecutiveSuccesses: h.ConsecutiveSuccesses,
			EjectionCount:        h.EjectionCount,
			CooldownUntil:        h.CooldownUntil,
			HealthySince:         h.HealthySince,
			LastLatencyMS:        h.LastLatencyMS,
			LastReason:           h.LastReason,
			LastErrorDetail:      h.LastErrorDetail,
			LastCheckedAt:        h.LastCheckedAt,
			Availability24h:      avail,
			Heartbeat24h:         hb,
		}
		pool.Members = append(pool.Members, member)
		pool.TotalCount++
		if proxy.Enabled && bindingEnabled && status == "healthy" {
			pool.HealthyCount++
		}
		if isActive {
			idStr := proxyID.String()
			name := proxy.Name
			ptype := proxy.Type
			pool.ActiveProxyID = &idStr
			pool.ActiveProxyName = &name
			pool.ActiveProxyType = &ptype
			pool.ActiveProxyStatus = status
		}
	}

	sort.Slice(poolKeys, func(i, j int) bool { return providerKeyLabel(poolKeys[i]) < providerKeyLabel(poolKeys[j]) })
	pools := make([]providerProxyPoolResponse, 0, len(poolKeys))
	for _, k := range poolKeys {
		pool := poolMap[k]
		// 若状态表尚未落库，兜底标出最高优先级可用代理以便一致展示。
		if pool.ActiveProxyID == nil {
			for idx := range pool.Members {
				if pool.Members[idx].ProxyEnabled && pool.Members[idx].BindingEnabled {
					pool.Members[idx].IsActive = true
					idStr := pool.Members[idx].ProxyID
					name := pool.Members[idx].ProxyName
					ptype := pool.Members[idx].ProxyType
					pool.ActiveProxyID = &idStr
					pool.ActiveProxyName = &name
					pool.ActiveProxyType = &ptype
					pool.ActiveProxyStatus = pool.Members[idx].Status
					break
				}
			}
		}
		pools = append(pools, *pool)
	}

	adminOK(w, platformProxyListResponse{
		Proxies:         list,
		Pools:           pools,
		Settings:        settings,
		ProviderOptions: platformProviderOptions(),
	})
}

func (d ServerDeps) CreatePlatformProxy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}

	var body platformProxyRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	proxy, ok := d.parsePlatformProxyRequest(w, body, true, platformProxy{Enabled: true})
	if !ok {
		return
	}
	proxy.ID = uuid.New()
	redactedURL, redactErr := redactProxyURL(proxy.URL)
	if redactErr != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_CREATE_FAILED", "failed-to-create-platform-proxy", nil)
		return
	}

	var updatedAt time.Time
	err := pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var insertErr error
		updatedAt, insertErr = insertPlatformProxy(r.Context(), tx, proxy, d.MasterKey)
		if insertErr != nil {
			return insertErr
		}
		if err := insertPlatformProxyProviders(r.Context(), tx, proxy); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"platform_proxy.created", "platform_proxy", proxy.ID.String(), nil,
			map[string]any{"name": proxy.Name, "provider_keys": proxy.ProviderKeys, "proxy_type": proxy.Type, "enabled": proxy.Enabled})
	})
	if err != nil {
		d.writePlatformProxyConflict(w, err, "PLATFORM_PROXY_CREATE_FAILED", "failed-to-create-platform-proxy")
		return
	}
	if !d.syncProxiesAndReload(w, r, proxy.ProviderKeys) {
		return
	}
	adminOK(w, platformProxyResponse{
		ID:             proxy.ID.String(),
		Name:           proxy.Name,
		ProviderKeys:   proxy.ProviderKeys,
		ProviderLabels: providerLabels(proxy.ProviderKeys),
		ProxyType:      proxy.Type,
		ProxyURL:       redactedURL,
		Enabled:        proxy.Enabled,
		ProbeURL:       proxy.ProbeURL,
		UpdatedAt:      &updatedAt,
		HealthStatus:   "unknown",
		Heartbeat24h:   []governance.HeartbeatBucket{},
	})
}

func (d ServerDeps) UpdatePlatformProxy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}

	proxyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_ID_INVALID", "invalid-platform-proxy-id", nil)
		return
	}
	oldProxy, err := loadPlatformProxyByID(r.Context(), d.Pool, d.MasterKey, proxyID)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PLATFORM_PROXY_NOT_FOUND", "platform-proxy-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_UPDATE_FAILED", "failed-to-read-platform-proxy", nil)
		return
	}

	var body platformProxyRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	proxy, ok := d.parsePlatformProxyRequest(w, body, false, oldProxy)
	if !ok {
		return
	}
	proxy.ID = oldProxy.ID
	redactedURL, redactErr := redactProxyURL(proxy.URL)
	if redactErr != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_UPDATE_FAILED", "failed-to-update-platform-proxy", nil)
		return
	}

	var updatedAt time.Time
	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		var updateErr error
		updatedAt, updateErr = updatePlatformProxy(r.Context(), tx, proxy, d.MasterKey)
		if updateErr != nil {
			return updateErr
		}
		if err := replacePlatformProxyProviders(r.Context(), tx, proxy); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"platform_proxy.updated", "platform_proxy", proxy.ID.String(),
			map[string]any{"name": oldProxy.Name, "provider_keys": oldProxy.ProviderKeys, "proxy_type": oldProxy.Type, "enabled": oldProxy.Enabled},
			map[string]any{"name": proxy.Name, "provider_keys": proxy.ProviderKeys, "proxy_type": proxy.Type, "enabled": proxy.Enabled, "proxy_url_changed": oldProxy.URL != proxy.URL})
	})
	if err != nil {
		d.writePlatformProxyConflict(w, err, "PLATFORM_PROXY_UPDATE_FAILED", "failed-to-update-platform-proxy")
		return
	}
	affected := append([]string{}, oldProxy.ProviderKeys...)
	affected = append(affected, proxy.ProviderKeys...)
	if !d.syncProxiesAndReload(w, r, affected) {
		return
	}
	adminOK(w, platformProxyResponse{
		ID:             proxy.ID.String(),
		Name:           proxy.Name,
		ProviderKeys:   proxy.ProviderKeys,
		ProviderLabels: providerLabels(proxy.ProviderKeys),
		ProxyType:      proxy.Type,
		ProxyURL:       redactedURL,
		Enabled:        proxy.Enabled,
		ProbeURL:       proxy.ProbeURL,
		UpdatedAt:      &updatedAt,
		HealthStatus:   "unknown",
		Heartbeat24h:   []governance.HeartbeatBucket{},
	})
}

func (d ServerDeps) DeletePlatformProxy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}

	proxyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_ID_INVALID", "invalid-platform-proxy-id", nil)
		return
	}
	proxy, err := loadPlatformProxyByID(r.Context(), d.Pool, d.MasterKey, proxyID)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PLATFORM_PROXY_NOT_FOUND", "platform-proxy-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_DELETE_FAILED", "failed-to-read-platform-proxy", nil)
		return
	}

	err = pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		if err := deletePlatformProxy(r.Context(), tx, proxyID); err != nil {
			return err
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"platform_proxy.deleted", "platform_proxy", proxyID.String(),
			map[string]any{"name": proxy.Name, "provider_keys": proxy.ProviderKeys, "proxy_type": proxy.Type}, nil)
	})
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_DELETE_FAILED", "failed-to-delete-platform-proxy", nil)
		return
	}
	if !d.syncProxiesAndReload(w, r, proxy.ProviderKeys) {
		return
	}
	adminOK(w, map[string]string{"status": "deleted"})
}

func (d ServerDeps) TestPlatformProxy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	proxyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_ID_INVALID", "invalid-platform-proxy-id", nil)
		return
	}
	if d.Governance == nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_PROBE_FAILED", "failed-to-probe-platform-proxy", nil)
		return
	}
	results, err := d.Governance.ProbeProxy(r.Context(), proxyID, "manual")
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PLATFORM_PROXY_NOT_FOUND", "platform-proxy-does-not-exist", nil)
		return
	}
	if errors.Is(err, governance.ErrProxyProbeTargetMissing) {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_PROBE_TARGET_MISSING", "platform-proxy-probe-target-missing", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_PROBE_FAILED", "failed-to-probe-platform-proxy", nil)
		return
	}
	adminOK(w, map[string]any{"results": results})
}

type providerProxyOrderRequest struct {
	Items []struct {
		ProxyID  string `json:"proxy_id"`
		Priority int    `json:"priority"`
		Enabled  *bool  `json:"enabled"`
	} `json:"items"`
}

func (d ServerDeps) UpdateProviderProxyOrder(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	providerKey := strings.TrimSpace(chi.URLParam(r, "providerKey"))
	if !bifrostx.IsSupportedProviderKey(providerKey) {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_PROVIDER_INVALID", "platform-proxy-provider-invalid", nil)
		return
	}
	var body providerProxyOrderRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	if len(body.Items) == 0 {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_INVALID", "platform-proxy-invalid", nil)
		return
	}
	type parsedItem struct {
		proxyID  uuid.UUID
		priority int
		enabled  *bool
	}
	seen := make(map[uuid.UUID]bool, len(body.Items))
	items := make([]parsedItem, 0, len(body.Items))
	for _, item := range body.Items {
		pid, err := uuid.Parse(item.ProxyID)
		if err != nil || seen[pid] || item.Priority < 1 {
			adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_INVALID", "platform-proxy-invalid", nil)
			return
		}
		seen[pid] = true
		items = append(items, parsedItem{proxyID: pid, priority: item.Priority, enabled: item.Enabled})
	}

	err := pgx.BeginFunc(r.Context(), d.Pool, func(tx pgx.Tx) error {
		for _, item := range items {
			var tag int64
			if item.enabled != nil {
				res, err := tx.Exec(r.Context(), `
					UPDATE platform_proxy_providers
					SET priority = $3, enabled = $4
					WHERE provider_key = $1 AND proxy_id = $2
				`, providerKey, item.proxyID, item.priority, *item.enabled)
				if err != nil {
					return err
				}
				tag = res.RowsAffected()
			} else {
				res, err := tx.Exec(r.Context(), `
					UPDATE platform_proxy_providers
					SET priority = $3
					WHERE provider_key = $1 AND proxy_id = $2
				`, providerKey, item.proxyID, item.priority)
				if err != nil {
					return err
				}
				tag = res.RowsAffected()
			}
			if tag == 0 {
				return pgx.ErrNoRows
			}
		}
		return writeAuditEvent(r.Context(), tx, principal.OrganizationID, principal.UserID,
			"platform_proxy.order_updated", "platform_proxy", providerKey, nil,
			map[string]any{"provider_key": providerKey, "items_count": len(items)})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, http.StatusNotFound, "PLATFORM_PROXY_NOT_FOUND", "platform-proxy-does-not-exist", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_UPDATE_FAILED", "failed-to-update-platform-proxy", nil)
		return
	}
	if !d.syncProxiesAndReload(w, r, []string{providerKey}) {
		return
	}
	adminOK(w, map[string]string{"status": "updated"})
}

type switchProviderProxyRequest struct {
	ProxyID string `json:"proxy_id"`
}

func (d ServerDeps) SwitchProviderProxy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	providerKey := strings.TrimSpace(chi.URLParam(r, "providerKey"))
	if !bifrostx.IsSupportedProviderKey(providerKey) {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_PROVIDER_INVALID", "platform-proxy-provider-invalid", nil)
		return
	}
	var body switchProviderProxyRequest
	if !d.decodeJSON(w, r, &body) {
		return
	}
	proxyID, err := uuid.Parse(body.ProxyID)
	if err != nil {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_ID_INVALID", "invalid-platform-proxy-id", nil)
		return
	}
	if d.Governance == nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_SWITCH_FAILED", "failed-to-switch-platform-proxy", nil)
		return
	}
	event, err := d.Governance.ManualSwitchProxy(r.Context(), providerKey, proxyID, "admin_manual_switch")
	if errors.Is(err, governance.ErrProxyBindingUnavailable) {
		adminError(w, http.StatusConflict, "PLATFORM_PROXY_SWITCH_UNAVAILABLE", "platform-proxy-switch-unavailable", nil)
		return
	}
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_SWITCH_FAILED", "failed-to-switch-platform-proxy", nil)
		return
	}
	if err := writeAuditEvent(r.Context(), d.Pool, principal.OrganizationID, principal.UserID,
		"platform_proxy.switched", "platform_proxy", providerKey,
		map[string]any{"provider_key": providerKey, "from_proxy": event.FromProxyName},
		map[string]any{"provider_key": providerKey, "to_proxy": event.ToProxyName, "trigger": event.Trigger}); err != nil {
		d.Logger.Warn("write manual switch audit event failed", "error", err)
	}
	adminOK(w, event)
}

func (d ServerDeps) ProbeProviderProxies(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	providerKey := strings.TrimSpace(chi.URLParam(r, "providerKey"))
	if !bifrostx.IsSupportedProviderKey(providerKey) {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_PROVIDER_INVALID", "platform-proxy-provider-invalid", nil)
		return
	}
	if d.Governance == nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_PROBE_FAILED", "failed-to-probe-platform-proxy", nil)
		return
	}
	summary, err := d.Governance.ProbeProviderPool(r.Context(), providerKey, "manual")
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_PROBE_FAILED", "failed-to-probe-platform-proxy", nil)
		return
	}
	adminOK(w, summary)
}

func (d ServerDeps) ProxyFailoverEvents(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	if d.Governance == nil {
		adminOK(w, []governance.FailoverEvent{})
		return
	}
	providerKey := strings.TrimSpace(r.URL.Query().Get("provider_key"))
	limit := 50
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	events, err := d.Governance.ListFailoverEvents(r.Context(), providerKey, limit)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
		return
	}
	adminOK(w, events)
}

func (d ServerDeps) ProxyGovernanceSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	settings, err := d.loadProxyGovSettings(r.Context())
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_SETTINGS_FAILED", "failed-to-query-proxy-settings", nil)
		return
	}
	adminOK(w, settings)
}

func (d ServerDeps) UpdateProxyGovernanceSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		adminError(w, http.StatusUnauthorized, "PERMISSION_DENIED", "at-least-viewer-permission-is-required", nil)
		return
	}
	if err := auth.RequireRole(principal, "platform_admin"); err != nil {
		adminError(w, http.StatusForbidden, "PERMISSION_DENIED", "platform-admin-permission-is-required", nil)
		return
	}
	var body governance.ProxyGovernanceSettings
	if !d.decodeJSON(w, r, &body) {
		return
	}
	body.FailbackMode = strings.TrimSpace(body.FailbackMode)
	if body.ProbeIntervalSeconds < 15 || body.ProbeIntervalSeconds > 3600 ||
		body.FastProbeIntervalSeconds < 3 || body.FastProbeIntervalSeconds > 120 ||
		body.ProbeTimeoutSeconds < 2 || body.ProbeTimeoutSeconds > 60 ||
		body.FailThreshold < 1 || body.FailThreshold > 20 ||
		body.RecoverThreshold < 1 || body.RecoverThreshold > 20 ||
		body.SwitchCooldownSeconds < 0 || body.SwitchCooldownSeconds > 86400 ||
		(body.FailbackMode != string(governance.FailbackSticky) && body.FailbackMode != string(governance.FailbackAutoAfterSoak)) ||
		body.FailbackSoakSeconds < 60 || body.FailbackSoakSeconds > 86400 {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_SETTINGS_INVALID", "platform-proxy-settings-invalid", nil)
		return
	}
	if d.Governance == nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_SETTINGS_FAILED", "failed-to-update-proxy-settings", nil)
		return
	}
	before, _ := d.Governance.LoadProxyGovernanceSettings(r.Context())
	saved, err := d.Governance.SaveProxyGovernanceSettings(r.Context(), body, principal.UserID)
	if err != nil {
		adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_SETTINGS_FAILED", "failed-to-update-proxy-settings", nil)
		return
	}
	if err := writeAuditEvent(r.Context(), d.Pool, principal.OrganizationID, principal.UserID,
		"platform_proxy.settings_updated", "platform_proxy", "settings",
		before, saved); err != nil {
		d.Logger.Warn("write proxy settings audit event failed", "error", err)
	}
	adminOK(w, saved)
}

func (d ServerDeps) syncProxiesAndReload(w http.ResponseWriter, r *http.Request, providerKeys []string) bool {
	if d.Governance != nil {
		if err := d.Governance.SyncProviders(r.Context(), providerKeys); err != nil {
			if d.Logger != nil {
				d.Logger.Error("sync provider proxies failed", "provider_keys", providerKeys, "error", err)
			}
			adminError(w, http.StatusInternalServerError, "ENGINE_RELOAD_FAILED", "provider-engine-reload-failed", nil)
			return false
		}
		return true
	}
	return d.reloadEngine(w, r)
}

func (d ServerDeps) loadProxyGovSettings(ctx context.Context) (governance.ProxyGovernanceSettings, error) {
	if d.Governance != nil {
		return d.Governance.LoadProxyGovernanceSettings(ctx)
	}
	return governance.NewManager(d.Pool, d.MasterKey, d.Logger, nil).LoadProxyGovernanceSettings(ctx)
}

func (d ServerDeps) parsePlatformProxyRequest(w http.ResponseWriter, body platformProxyRequest, creating bool, oldProxy platformProxy) (platformProxy, bool) {
	bindings, providerKeys, valid := normalizeProxyBindings(body.Bindings, body.ProviderKeys)
	name := strings.TrimSpace(body.Name)
	proxyType := strings.TrimSpace(body.ProxyType)
	proxyURL := strings.TrimSpace(body.ProxyURL)

	if !valid {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_PROVIDER_INVALID", "platform-proxy-provider-invalid", nil)
		return platformProxy{}, false
	}
	if name == "" {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_NAME_REQUIRED", "platform-proxy-name-required", nil)
		return platformProxy{}, false
	}
	if proxyType == bifrostx.ProxyTypeNone {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_TYPE_INVALID", "platform-proxy-type-invalid", nil)
		return platformProxy{}, false
	}
	if !creating && proxyURL == "" && proxyType == oldProxy.Type {
		proxyURL = oldProxy.URL
	}
	normalizedType, normalizedURL, err := bifrostx.NormalizeProxy(proxyType, proxyURL)
	if err != nil || normalizedType == bifrostx.ProxyTypeNone {
		adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_INVALID", "platform-proxy-invalid", nil)
		return platformProxy{}, false
	}
	enabled := oldProxy.Enabled
	if creating {
		enabled = true
	}
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	probeURL := oldProxy.ProbeURL
	if body.ProbeURL != nil {
		validatedProbeURL, err := bifrostx.ValidateProbeURL(*body.ProbeURL)
		if err != nil {
			adminError(w, http.StatusBadRequest, "PLATFORM_PROXY_PROBE_URL_INVALID", "platform-proxy-probe-url-invalid", nil)
			return platformProxy{}, false
		}
		probeURL = validatedProbeURL
	}
	return platformProxy{
		ProviderKeys: providerKeys,
		Bindings:     bindings,
		Name:         name,
		Type:         normalizedType,
		URL:          normalizedURL,
		Enabled:      enabled,
		ProbeURL:     probeURL,
	}, true
}

func (d ServerDeps) writePlatformProxyConflict(w http.ResponseWriter, err error, code, reason string) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.ConstraintName == "platform_proxies_name_key" {
			adminError(w, http.StatusConflict, "PLATFORM_PROXY_NAME_EXISTS", "platform-proxy-name-exists", nil)
			return
		}
	}
	adminError(w, http.StatusInternalServerError, code, reason, nil)
}

func (d ServerDeps) activeProviderAccountCounts(ctx context.Context) (map[string]int, error) {
	rows, err := d.Pool.Query(ctx, `SELECT provider FROM provider_accounts WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var provider string
		if err := rows.Scan(&provider); err != nil {
			return nil, err
		}
		providerKey, err := bifrostx.MapProvider(provider)
		if err != nil {
			return nil, err
		}
		counts[string(providerKey)]++
	}
	return counts, rows.Err()
}

func deriveProxyNodeHealth(enabled bool, bindings []proxyBindingDetail) string {
	if !enabled {
		return "disabled"
	}
	if len(bindings) == 0 {
		return "unknown"
	}
	var healthy, unhealthy, suspect int
	for _, b := range bindings {
		if !b.Enabled {
			continue
		}
		switch b.Status {
		case "healthy":
			healthy++
		case "unhealthy":
			unhealthy++
		case "suspect":
			suspect++
		}
	}
	active := healthy + unhealthy + suspect
	if active == 0 {
		return "unknown"
	}
	if unhealthy > 0 && healthy == 0 {
		return "unhealthy"
	}
	if unhealthy > 0 || suspect > 0 {
		return "degraded"
	}
	return "healthy"
}

func toGovernanceSamples(samples []probeCheckSample) []struct {
	CheckedAt time.Time
	OK        bool
	Status    string
	LatencyMS *int
	Reason    string
} {
	out := make([]struct {
		CheckedAt time.Time
		OK        bool
		Status    string
		LatencyMS *int
		Reason    string
	}, len(samples))
	for i, s := range samples {
		out[i].CheckedAt = s.CheckedAt
		out[i].OK = s.OK
		out[i].Status = s.Status
		out[i].LatencyMS = s.LatencyMS
		out[i].Reason = s.Reason
	}
	return out
}

func avgSampleLatency(samples []probeCheckSample) *float64 {
	var sum int64
	var cnt int64
	for _, s := range samples {
		if s.LatencyMS != nil {
			sum += int64(*s.LatencyMS)
			cnt++
		}
	}
	if cnt == 0 {
		return nil
	}
	v := float64(sum) / float64(cnt)
	return &v
}
