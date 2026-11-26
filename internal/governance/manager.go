package governance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/vault"
)

var (
	ErrProxyBindingUnavailable = errors.New("proxy binding not found or disabled")
	ErrProxyProbeTargetMissing = errors.New("proxy has no probe target")
	ErrProviderTestUnsupported = errors.New("provider does not support online probe")
)

var (
	metricProxyFailovers = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "omniflux_proxy_failovers_total",
		Help: "Total proxy failover/switch events by provider and trigger",
	}, []string{"provider_key", "trigger"})

	metricProxyHealth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "omniflux_proxy_health_status",
		Help: "Proxy binding health state (1=healthy, 0.5=suspect, 0=unhealthy)",
	}, []string{"provider_key", "proxy_id"})

	metricAccountProbe = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "omniflux_provider_account_probe_status",
		Help: "Provider account active probe health (1=healthy, 0.5=degraded, 0=unhealthy)",
	}, []string{"account_id", "provider"})
)

type ProxyGovernanceSettings struct {
	Enabled                  bool       `json:"enabled"`
	ProbeIntervalSeconds     int        `json:"probe_interval_seconds"`
	FastProbeIntervalSeconds int        `json:"fast_probe_interval_seconds"`
	ProbeTimeoutSeconds      int        `json:"probe_timeout_seconds"`
	FailThreshold            int        `json:"fail_threshold"`
	RecoverThreshold         int        `json:"recover_threshold"`
	SwitchCooldownSeconds    int        `json:"switch_cooldown_seconds"`
	FailbackMode             string     `json:"failback_mode"`
	FailbackSoakSeconds      int        `json:"failback_soak_seconds"`
	UpdatedBy                *string    `json:"updated_by,omitempty"`
	UpdatedAt                *time.Time `json:"updated_at,omitempty"`
}

func (s ProxyGovernanceSettings) Policy() ProxyPolicy {
	mode := FailbackSticky
	if s.FailbackMode == string(FailbackAutoAfterSoak) {
		mode = FailbackAutoAfterSoak
	}
	return ProxyPolicy{
		Enabled:           s.Enabled,
		ProbeInterval:     time.Duration(s.ProbeIntervalSeconds) * time.Second,
		FastProbeInterval: time.Duration(s.FastProbeIntervalSeconds) * time.Second,
		ProbeTimeout:      time.Duration(s.ProbeTimeoutSeconds) * time.Second,
		FallThreshold:     s.FailThreshold,
		RiseThreshold:     s.RecoverThreshold,
		SwitchCooldown:    time.Duration(s.SwitchCooldownSeconds) * time.Second,
		FailbackMode:      mode,
		FailbackSoak:      time.Duration(s.FailbackSoakSeconds) * time.Second,
	}
}

type ActiveProxy struct {
	ProxyID  uuid.UUID
	Name     string
	Type     string
	URL      string
	ProbeURL string
	Priority int
}

type FailoverEvent struct {
	ID            string    `json:"id"`
	ProviderKey   string    `json:"provider_key"`
	FromProxyID   *string   `json:"from_proxy_id"`
	FromProxyName *string   `json:"from_proxy_name"`
	ToProxyID     *string   `json:"to_proxy_id"`
	ToProxyName   *string   `json:"to_proxy_name"`
	Trigger       string    `json:"trigger"`
	Reason        string    `json:"reason"`
	Detail        *string   `json:"detail,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type HeartbeatBucket struct {
	BucketStart  time.Time `json:"bucket_start"`
	Status       string    `json:"status"`
	Total        int       `json:"total"`
	OKCount      int       `json:"ok_count"`
	AvgLatencyMS *int      `json:"avg_latency_ms"`
	LastReason   string    `json:"last_reason,omitempty"`
}

type ProxyTargetProbeResult struct {
	ProviderKey string `json:"provider_key"`
	TargetURL   string `json:"target_url"`
	OK          bool   `json:"ok"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	StatusCode  int    `json:"status_code,omitempty"`
	LatencyMS   int    `json:"latency_ms"`
	ErrorDetail string `json:"error_detail,omitempty"`
}

type PoolProbeSummary struct {
	ProviderKey   string                   `json:"provider_key"`
	Results       []ProxyTargetProbeResult `json:"results"`
	Switched      bool                     `json:"switched"`
	ActiveProxyID *string                  `json:"active_proxy_id,omitempty"`
	Event         *FailoverEvent           `json:"event,omitempty"`
}

// Manager coordinates proxy failover state machines, periodic active probing, and account health checks.
type Manager struct {
	pool      *pgxpool.Pool
	masterKey []byte
	logger    *slog.Logger

	engineMu sync.RWMutex
	engine   *bifrostx.Engine

	schedMu          sync.Mutex
	started          bool
	bgCtx            context.Context
	fastConfirmUntil map[string]time.Time
	probingProviders map[string]bool
	probingAccounts  map[uuid.UUID]bool
}

func NewManager(pool *pgxpool.Pool, masterKey []byte, logger *slog.Logger, engine *bifrostx.Engine) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		pool:             pool,
		masterKey:        masterKey,
		logger:           logger,
		engine:           engine,
		fastConfirmUntil: make(map[string]time.Time),
		probingProviders: make(map[string]bool),
		probingAccounts:  make(map[uuid.UUID]bool),
	}
}

func (m *Manager) SetEngine(engine *bifrostx.Engine) {
	m.engineMu.Lock()
	m.engine = engine
	m.engineMu.Unlock()
}

func (m *Manager) currentEngine() *bifrostx.Engine {
	m.engineMu.RLock()
	defer m.engineMu.RUnlock()
	return m.engine
}

// Start launches background schedulers for proxy failover probing and provider account availability checks.
func (m *Manager) Start(ctx context.Context) {
	m.schedMu.Lock()
	if m.started {
		m.schedMu.Unlock()
		return
	}
	m.started = true
	m.bgCtx = ctx
	m.schedMu.Unlock()

	go m.runProxyScheduler(ctx)
	go m.runAccountScheduler(ctx)
}

func (m *Manager) runProxyScheduler(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	m.runProxyTick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runProxyTick(ctx)
		}
	}
}

func (m *Manager) runProxyTick(ctx context.Context) {
	settings, err := m.LoadProxyGovernanceSettings(ctx)
	if err != nil {
		m.logger.Warn("load proxy governance settings failed", "error", err)
		return
	}
	if !settings.Enabled {
		return
	}
	policy := settings.Policy()
	now := time.Now()

	rows, err := m.pool.Query(ctx, `
		SELECT DISTINCT pp.provider_key,
		       COALESCE(hs.status, 'unknown') AS status,
		       hs.last_checked_at
		FROM platform_proxy_providers pp
		JOIN platform_proxies p ON p.id = pp.proxy_id
		LEFT JOIN proxy_health_states hs ON hs.provider_key = pp.provider_key AND hs.proxy_id = pp.proxy_id
		WHERE p.enabled = true AND pp.enabled = true
	`)
	if err != nil {
		m.logger.Warn("query due proxy bindings failed", "error", err)
		return
	}
	defer rows.Close()

	dueProviders := make(map[string]bool)
	for rows.Next() {
		var providerKey, status string
		var lastChecked *time.Time
		if err := rows.Scan(&providerKey, &status, &lastChecked); err != nil {
			continue
		}
		interval := policy.ProbeInterval
		if status == string(ProxyStatusSuspect) || m.isFastConfirmActive(providerKey, now) {
			interval = policy.FastProbeInterval
		}
		if lastChecked == nil || now.Sub(*lastChecked) >= interval {
			dueProviders[providerKey] = true
		}
	}
	if err := rows.Err(); err != nil {
		return
	}
	for providerKey := range dueProviders {
		trigger := "auto"
		if m.isFastConfirmActive(providerKey, now) {
			trigger = "fast_confirm"
		}
		m.launchProviderProbe(ctx, providerKey, trigger)
	}
}

func (m *Manager) isFastConfirmActive(providerKey string, now time.Time) bool {
	m.schedMu.Lock()
	defer m.schedMu.Unlock()
	until, ok := m.fastConfirmUntil[providerKey]
	if !ok {
		return false
	}
	if now.After(until) {
		delete(m.fastConfirmUntil, providerKey)
		return false
	}
	return true
}

// ReportTransportError notifies proxy governance of a data-plane or probe transport failure,
// initiating fast active confirmation without blocking the caller or immediately switching IPs.
func (m *Manager) ReportTransportError(providerKey string) {
	if providerKey == "" {
		return
	}
	m.schedMu.Lock()
	m.fastConfirmUntil[providerKey] = time.Now().Add(2 * time.Minute)
	started := m.started
	bgCtx := m.bgCtx
	m.schedMu.Unlock()

	if started && bgCtx != nil {
		m.launchProviderProbe(bgCtx, providerKey, "fast_confirm")
	}
}

func (m *Manager) launchProviderProbe(ctx context.Context, providerKey, trigger string) {
	m.schedMu.Lock()
	if m.probingProviders[providerKey] {
		m.schedMu.Unlock()
		return
	}
	m.probingProviders[providerKey] = true
	m.schedMu.Unlock()

	go func() {
		defer func() {
			m.schedMu.Lock()
			delete(m.probingProviders, providerKey)
			m.schedMu.Unlock()
		}()
		probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if _, err := m.ProbeProviderPool(probeCtx, providerKey, trigger); err != nil && !errors.Is(err, context.Canceled) {
			m.logger.Warn("background proxy pool probe failed", "provider_key", providerKey, "error", err)
		}
	}()
}

func (m *Manager) LoadProxyGovernanceSettings(ctx context.Context) (ProxyGovernanceSettings, error) {
	var s ProxyGovernanceSettings
	var updatedBy uuid.NullUUID
	var updatedAt time.Time
	err := m.pool.QueryRow(ctx, `
		SELECT enabled, probe_interval_seconds, fast_probe_interval_seconds, probe_timeout_seconds,
		       fail_threshold, recover_threshold, switch_cooldown_seconds, failback_mode, failback_soak_seconds,
		       updated_by, updated_at
		FROM proxy_governance_settings
		WHERE singleton = true
	`).Scan(
		&s.Enabled, &s.ProbeIntervalSeconds, &s.FastProbeIntervalSeconds, &s.ProbeTimeoutSeconds,
		&s.FailThreshold, &s.RecoverThreshold, &s.SwitchCooldownSeconds, &s.FailbackMode, &s.FailbackSoakSeconds,
		&updatedBy, &updatedAt,
	)
	if err != nil {
		return s, err
	}
	if updatedBy.Valid {
		v := updatedBy.UUID.String()
		s.UpdatedBy = &v
	}
	s.UpdatedAt = &updatedAt
	return s, nil
}

func (m *Manager) SaveProxyGovernanceSettings(ctx context.Context, s ProxyGovernanceSettings, actorUserID uuid.UUID) (ProxyGovernanceSettings, error) {
	_, err := m.pool.Exec(ctx, `
		UPDATE proxy_governance_settings
		SET enabled = $1,
		    probe_interval_seconds = $2,
		    fast_probe_interval_seconds = $3,
		    probe_timeout_seconds = $4,
		    fail_threshold = $5,
		    recover_threshold = $6,
		    switch_cooldown_seconds = $7,
		    failback_mode = $8,
		    failback_soak_seconds = $9,
		    updated_by = $10,
		    updated_at = now()
		WHERE singleton = true
	`, s.Enabled, s.ProbeIntervalSeconds, s.FastProbeIntervalSeconds, s.ProbeTimeoutSeconds,
		s.FailThreshold, s.RecoverThreshold, s.SwitchCooldownSeconds, s.FailbackMode, s.FailbackSoakSeconds, actorUserID)
	if err != nil {
		return ProxyGovernanceSettings{}, err
	}
	return m.LoadProxyGovernanceSettings(ctx)
}

type loadedProxyRecord struct {
	ProxyID        uuid.UUID
	Name           string
	Type           string
	URL            string
	ProbeURL       string
	ProxyEnabled   bool
	BindingEnabled bool
	Priority       int
	CreatedAt      time.Time
	Health         BindingState
}

func (m *Manager) loadProviderPool(ctx context.Context, providerKey string) (*uuid.UUID, *time.Time, []loadedProxyRecord, error) {
	var stateProxyID uuid.NullUUID
	var lastSwitchedAt *time.Time
	err := m.pool.QueryRow(ctx, `
		SELECT active_proxy_id, last_switched_at
		FROM provider_proxy_states
		WHERE provider_key = $1
	`, providerKey).Scan(&stateProxyID, &lastSwitchedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, err
	}
	var activeID *uuid.UUID
	if stateProxyID.Valid {
		v := stateProxyID.UUID
		activeID = &v
	}

	rows, err := m.pool.Query(ctx, `
		SELECT p.id, p.name, p.proxy_type, p.proxy_ciphertext, COALESCE(p.probe_url, ''),
		       p.enabled, pp.enabled, pp.priority, pp.created_at,
		       COALESCE(hs.status, 'unknown'),
		       COALESCE(hs.consecutive_failures, 0),
		       COALESCE(hs.consecutive_successes, 0),
		       COALESCE(hs.ejection_count, 0),
		       hs.cooldown_until, hs.healthy_since, hs.last_latency_ms,
		       COALESCE(hs.last_reason, ''), COALESCE(hs.last_error_detail, ''), hs.last_checked_at
		FROM platform_proxy_providers pp
		JOIN platform_proxies p ON p.id = pp.proxy_id
		LEFT JOIN proxy_health_states hs ON hs.provider_key = pp.provider_key AND hs.proxy_id = pp.proxy_id
		WHERE pp.provider_key = $1
		ORDER BY pp.priority ASC, pp.created_at ASC
	`, providerKey)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	records := []loadedProxyRecord{}
	for rows.Next() {
		var rec loadedProxyRecord
		var ciphertext []byte
		var status string
		if err := rows.Scan(
			&rec.ProxyID, &rec.Name, &rec.Type, &ciphertext, &rec.ProbeURL,
			&rec.ProxyEnabled, &rec.BindingEnabled, &rec.Priority, &rec.CreatedAt,
			&status, &rec.Health.ConsecutiveFailures, &rec.Health.ConsecutiveSuccesses,
			&rec.Health.EjectionCount, &rec.Health.CooldownUntil, &rec.Health.HealthySince,
			&rec.Health.LastLatencyMS, &rec.Health.LastReason, &rec.Health.LastErrorDetail, &rec.Health.LastCheckedAt,
		); err != nil {
			return nil, nil, nil, err
		}
		plaintext, err := vault.Decrypt(m.masterKey, ciphertext)
		if err != nil {
			return nil, nil, nil, err
		}
		rec.URL = string(plaintext)
		rec.Health.ProxyID = rec.ProxyID
		rec.Health.ProxyName = rec.Name
		rec.Health.ProxyEnabled = rec.ProxyEnabled
		rec.Health.BindingEnabled = rec.BindingEnabled
		rec.Health.Priority = rec.Priority
		rec.Health.CreatedAt = rec.CreatedAt
		rec.Health.Status = ProxyStatus(status)
		records = append(records, rec)
	}
	return activeID, lastSwitchedAt, records, rows.Err()
}

// ResolveAllActiveProxies returns the currently active sticky proxy per provider key.
func (m *Manager) ResolveAllActiveProxies(ctx context.Context) (map[string]ActiveProxy, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT DISTINCT ON (pp.provider_key)
		       pp.provider_key, p.id, p.name, p.proxy_type, p.proxy_ciphertext, COALESCE(p.probe_url, ''), pp.priority
		FROM platform_proxy_providers pp
		JOIN platform_proxies p ON p.id = pp.proxy_id
		LEFT JOIN provider_proxy_states s ON s.provider_key = pp.provider_key
		WHERE p.enabled = true AND pp.enabled = true
		ORDER BY pp.provider_key,
		         CASE WHEN s.active_proxy_id = pp.proxy_id THEN 0 ELSE 1 END,
		         pp.priority ASC, pp.created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]ActiveProxy)
	for rows.Next() {
		var providerKey string
		var ap ActiveProxy
		var ciphertext []byte
		if err := rows.Scan(&providerKey, &ap.ProxyID, &ap.Name, &ap.Type, &ciphertext, &ap.ProbeURL, &ap.Priority); err != nil {
			return nil, err
		}
		plaintext, err := vault.Decrypt(m.masterKey, ciphertext)
		if err != nil {
			return nil, err
		}
		ap.URL = string(plaintext)
		result[providerKey] = ap
	}
	return result, rows.Err()
}

// ResolveActiveProxy returns the active sticky proxy for a single provider key.
func (m *Manager) ResolveActiveProxy(ctx context.Context, providerKey string) (ActiveProxy, bool, error) {
	var ap ActiveProxy
	var ciphertext []byte
	err := m.pool.QueryRow(ctx, `
		SELECT p.id, p.name, p.proxy_type, p.proxy_ciphertext, COALESCE(p.probe_url, ''), pp.priority
		FROM platform_proxy_providers pp
		JOIN platform_proxies p ON p.id = pp.proxy_id
		LEFT JOIN provider_proxy_states s ON s.provider_key = pp.provider_key
		WHERE pp.provider_key = $1 AND p.enabled = true AND pp.enabled = true
		ORDER BY CASE WHEN s.active_proxy_id = pp.proxy_id THEN 0 ELSE 1 END,
		         pp.priority ASC, pp.created_at ASC
		LIMIT 1
	`, providerKey).Scan(&ap.ProxyID, &ap.Name, &ap.Type, &ciphertext, &ap.ProbeURL, &ap.Priority)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActiveProxy{}, false, nil
	}
	if err != nil {
		return ActiveProxy{}, false, err
	}
	plaintext, err := vault.Decrypt(m.masterKey, ciphertext)
	if err != nil {
		return ActiveProxy{}, false, err
	}
	ap.URL = string(plaintext)
	return ap, true, nil
}

// SyncProviders reconciles active sticky proxies for the specified providers after config edits or deletions.
func (m *Manager) SyncProviders(ctx context.Context, providerKeys []string) error {
	seen := make(map[string]bool, len(providerKeys))
	for _, providerKey := range providerKeys {
		if providerKey == "" || seen[providerKey] {
			continue
		}
		seen[providerKey] = true
		if err := m.syncSingleProvider(ctx, providerKey); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) syncSingleProvider(ctx context.Context, providerKey string) error {
	activeID, _, records, err := m.loadProviderPool(ctx, providerKey)
	if err != nil {
		return err
	}
	usable := make([]loadedProxyRecord, 0, len(records))
	usableBindings := make([]BindingState, 0, len(records))
	byID := make(map[uuid.UUID]loadedProxyRecord, len(records))
	var currentValid *loadedProxyRecord
	for i := range records {
		rec := records[i]
		byID[rec.ProxyID] = rec
		if rec.Health.IsUsable() {
			usable = append(usable, rec)
			usableBindings = append(usableBindings, rec.Health)
			if activeID != nil && rec.ProxyID == *activeID {
				currentValid = &records[i]
			}
		}
	}

	if len(usable) == 0 {
		if activeID != nil {
			fromName := m.lookupProxyName(ctx, *activeID)
			if err := m.commitSwitch(ctx, providerKey, activeID, fromName, nil, nil, "config_change", "no_enabled_proxies", ""); err != nil {
				return err
			}
		} else {
			_, _ = m.pool.Exec(ctx, `DELETE FROM provider_proxy_states WHERE provider_key = $1`, providerKey)
		}
		return m.applyEngineProxy(providerKey, bifrostx.ProxyTypeNone, "")
	}

	if currentValid != nil {
		return m.applyEngineProxy(providerKey, currentValid.Type, currentValid.URL)
	}

	best := pickBestAvailable(usableBindings, time.Now())
	targetRec := byID[best.ProxyID]
	var fromName *string
	if activeID != nil {
		fromName = m.lookupProxyName(ctx, *activeID)
	}
	toID := targetRec.ProxyID
	toName := targetRec.Name
	if err := m.commitSwitch(ctx, providerKey, activeID, fromName, &toID, &toName, "config_change", "active_proxy_unavailable", ""); err != nil {
		return err
	}
	return m.applyEngineProxy(providerKey, targetRec.Type, targetRec.URL)
}

func (m *Manager) ManualSwitchProxy(ctx context.Context, providerKey string, toProxyID uuid.UUID, detail string) (FailoverEvent, error) {
	activeID, _, records, err := m.loadProviderPool(ctx, providerKey)
	if err != nil {
		return FailoverEvent{}, err
	}
	var target *loadedProxyRecord
	var current *loadedProxyRecord
	for i := range records {
		if records[i].ProxyID == toProxyID && records[i].Health.IsUsable() {
			target = &records[i]
		}
		if activeID != nil && records[i].ProxyID == *activeID {
			current = &records[i]
		}
	}
	if target == nil {
		return FailoverEvent{}, ErrProxyBindingUnavailable
	}
	var fromName *string
	if current != nil {
		name := current.Name
		fromName = &name
	} else if activeID != nil {
		fromName = m.lookupProxyName(ctx, *activeID)
	}
	toName := target.Name
	event, err := m.commitSwitchWithEvent(ctx, providerKey, activeID, fromName, &toProxyID, &toName, "manual", "manual_switch", detail)
	if err != nil {
		return FailoverEvent{}, err
	}
	if err := m.applyEngineProxy(providerKey, target.Type, target.URL); err != nil {
		return FailoverEvent{}, err
	}
	return event, nil
}

func (m *Manager) ProbeProxy(ctx context.Context, proxyID uuid.UUID, trigger string) ([]ProxyTargetProbeResult, error) {
	settings, err := m.LoadProxyGovernanceSettings(ctx)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(settings.ProbeTimeoutSeconds) * time.Second

	var name, proxyType string
	var ciphertext []byte
	var probeURL string
	err = m.pool.QueryRow(ctx, `
		SELECT name, proxy_type, proxy_ciphertext, COALESCE(probe_url, '')
		FROM platform_proxies
		WHERE id = $1
	`, proxyID).Scan(&name, &proxyType, &ciphertext, &probeURL)
	if err != nil {
		return nil, err
	}
	plaintext, err := vault.Decrypt(m.masterKey, ciphertext)
	if err != nil {
		return nil, err
	}
	proxyURL := string(plaintext)

	rows, err := m.pool.Query(ctx, `
		SELECT pp.provider_key
		FROM platform_proxy_providers pp
		WHERE pp.proxy_id = $1
		ORDER BY pp.provider_key ASC
	`, proxyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	providerKeys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		providerKeys = append(providerKeys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(providerKeys) == 0 {
		if probeURL == "" {
			return nil, ErrProxyProbeTargetMissing
		}
		outcome := ProbeProxyTarget(ctx, proxyType, proxyURL, probeURL, true, timeout)
		return []ProxyTargetProbeResult{{
			ProviderKey: "",
			TargetURL:   probeURL,
			OK:          outcome.OK,
			Status:      outcome.Status,
			Reason:      outcome.Reason,
			StatusCode:  outcome.HTTPStatus,
			LatencyMS:   outcome.LatencyMS,
			ErrorDetail: outcome.ErrorDetail,
		}}, nil
	}

	policy := settings.Policy()
	results := make([]ProxyTargetProbeResult, 0, len(providerKeys))
	for _, providerKey := range providerKeys {
		targetURL, isCustom := resolveProxyTargetURL(probeURL, providerKey)
		if targetURL == "" {
			continue
		}
		outcome := ProbeProxyTarget(ctx, proxyType, proxyURL, targetURL, isCustom, timeout)
		results = append(results, ProxyTargetProbeResult{
			ProviderKey: providerKey,
			TargetURL:   targetURL,
			OK:          outcome.OK,
			Status:      outcome.Status,
			Reason:      outcome.Reason,
			StatusCode:  outcome.HTTPStatus,
			LatencyMS:   outcome.LatencyMS,
			ErrorDetail: outcome.ErrorDetail,
		})
		if err := m.recordProxyProbeAndApplyState(ctx, providerKey, proxyID, trigger, outcome, policy); err != nil {
			return nil, err
		}
		if _, err := m.evaluateProviderSwitch(ctx, providerKey, policy); err != nil {
			return nil, err
		}
	}
	if len(results) == 0 {
		return nil, ErrProxyProbeTargetMissing
	}
	return results, nil
}

func (m *Manager) ProbeProviderPool(ctx context.Context, providerKey, trigger string) (PoolProbeSummary, error) {
	settings, err := m.LoadProxyGovernanceSettings(ctx)
	if err != nil {
		return PoolProbeSummary{}, err
	}
	policy := settings.Policy()
	_, _, records, err := m.loadProviderPool(ctx, providerKey)
	if err != nil {
		return PoolProbeSummary{}, err
	}
	summary := PoolProbeSummary{ProviderKey: providerKey, Results: []ProxyTargetProbeResult{}}

	type probeItem struct {
		rec     loadedProxyRecord
		outcome ProxyProbeOutcome
		target  string
	}
	usable := make([]loadedProxyRecord, 0, len(records))
	for _, rec := range records {
		if rec.Health.IsUsable() {
			usable = append(usable, rec)
		}
	}
	ch := make(chan probeItem, len(usable))
	var wg sync.WaitGroup
	for _, rec := range usable {
		targetURL, isCustom := resolveProxyTargetURL(rec.ProbeURL, providerKey)
		if targetURL == "" {
			continue
		}
		wg.Add(1)
		go func(r loadedProxyRecord, tURL string, custom bool) {
			defer wg.Done()
			outcome := ProbeProxyTarget(ctx, r.Type, r.URL, tURL, custom, policy.ProbeTimeout)
			ch <- probeItem{rec: r, outcome: outcome, target: tURL}
		}(rec, targetURL, isCustom)
	}
	wg.Wait()
	close(ch)

	for item := range ch {
		summary.Results = append(summary.Results, ProxyTargetProbeResult{
			ProviderKey: providerKey,
			TargetURL:   item.target,
			OK:          item.outcome.OK,
			Status:      item.outcome.Status,
			Reason:      item.outcome.Reason,
			StatusCode:  item.outcome.HTTPStatus,
			LatencyMS:   item.outcome.LatencyMS,
			ErrorDetail: item.outcome.ErrorDetail,
		})
		if err := m.recordProxyProbeAndApplyState(ctx, providerKey, item.rec.ProxyID, trigger, item.outcome, policy); err != nil {
			return PoolProbeSummary{}, err
		}
	}

	ev, err := m.evaluateProviderSwitch(ctx, providerKey, policy)
	if err != nil {
		return PoolProbeSummary{}, err
	}
	if ev != nil {
		summary.Switched = true
		summary.Event = ev
		summary.ActiveProxyID = ev.ToProxyID
	} else {
		activeID, _, _, err := m.loadProviderPool(ctx, providerKey)
		if err == nil && activeID != nil {
			s := activeID.String()
			summary.ActiveProxyID = &s
		}
	}
	return summary, nil
}

func (m *Manager) evaluateProviderSwitch(ctx context.Context, providerKey string, policy ProxyPolicy) (*FailoverEvent, error) {
	for attempt := 0; attempt < 4; attempt++ {
		activeID, lastSwitchedAt, records, err := m.loadProviderPool(ctx, providerKey)
		if err != nil {
			return nil, err
		}
		bindings := make([]BindingState, 0, len(records))
		byID := make(map[uuid.UUID]loadedProxyRecord, len(records))
		for _, rec := range records {
			bindings = append(bindings, rec.Health)
			byID[rec.ProxyID] = rec
		}
		now := time.Now()
		decision := EvaluateSwitch(activeID, lastSwitchedAt, bindings, now, policy)
		if !decision.ShouldSwitch {
			return nil, nil
		}

		// 切到备代理前执行即时验活，防止切换到同样不可用的备节点。
		if decision.Trigger == "auto_failover" && decision.ToProxyID != nil {
			candidate := byID[*decision.ToProxyID]
			targetURL, isCustom := resolveProxyTargetURL(candidate.ProbeURL, providerKey)
			if targetURL != "" {
				verify := ProbeProxyTarget(ctx, candidate.Type, candidate.URL, targetURL, isCustom, policy.ProbeTimeout)
				if !verify.OK {
					if err := m.recordProxyProbeAndApplyState(ctx, providerKey, candidate.ProxyID, "fast_confirm", verify, policy); err != nil {
						return nil, err
					}
					continue
				}
			}
		}

		var fromName *string
		if activeID != nil {
			if rec, exists := byID[*activeID]; exists {
				n := rec.Name
				fromName = &n
			} else {
				fromName = m.lookupProxyName(ctx, *activeID)
			}
		}
		var toName *string
		nextType, nextURL := bifrostx.ProxyTypeNone, ""
		if decision.ToProxyID != nil {
			rec := byID[*decision.ToProxyID]
			n := rec.Name
			toName = &n
			nextType = rec.Type
			nextURL = rec.URL
		}
		event, err := m.commitSwitchWithEvent(ctx, providerKey, activeID, fromName, decision.ToProxyID, toName, decision.Trigger, decision.Reason, decision.Detail)
		if err != nil {
			return nil, err
		}
		if err := m.applyEngineProxy(providerKey, nextType, nextURL); err != nil {
			m.logger.Warn("hot swap provider proxy failed", "provider_key", providerKey, "error", err)
		}
		return &event, nil
	}
	return nil, nil
}

func (m *Manager) recordProxyProbeAndApplyState(ctx context.Context, providerKey string, proxyID uuid.UUID, trigger string, outcome ProxyProbeOutcome, policy ProxyPolicy) error {
	now := time.Now()
	return pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		var current BindingState
		var status string
		err := tx.QueryRow(ctx, `
			SELECT status, consecutive_failures, consecutive_successes, ejection_count, cooldown_until, healthy_since
			FROM proxy_health_states
			WHERE provider_key = $1 AND proxy_id = $2
			FOR UPDATE
		`, providerKey, proxyID).Scan(
			&status, &current.ConsecutiveFailures, &current.ConsecutiveSuccesses,
			&current.EjectionCount, &current.CooldownUntil, &current.HealthySince,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			current.Status = ProxyStatusUnknown
		} else if err != nil {
			return err
		} else {
			current.Status = ProxyStatus(status)
		}

		next := ApplyProbeResult(current, outcome.OK, outcome.LatencyMS, outcome.Reason, outcome.ErrorDetail, now, policy)
		if _, err := tx.Exec(ctx, `
			INSERT INTO proxy_health_states(
				provider_key, proxy_id, status, consecutive_failures, consecutive_successes,
				ejection_count, cooldown_until, healthy_since, last_latency_ms, last_reason, last_error_detail, last_checked_at, updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,now())
			ON CONFLICT (provider_key, proxy_id) DO UPDATE SET
				status = EXCLUDED.status,
				consecutive_failures = EXCLUDED.consecutive_failures,
				consecutive_successes = EXCLUDED.consecutive_successes,
				ejection_count = EXCLUDED.ejection_count,
				cooldown_until = EXCLUDED.cooldown_until,
				healthy_since = EXCLUDED.healthy_since,
				last_latency_ms = EXCLUDED.last_latency_ms,
				last_reason = EXCLUDED.last_reason,
				last_error_detail = EXCLUDED.last_error_detail,
				last_checked_at = EXCLUDED.last_checked_at,
				updated_at = now()
		`, providerKey, proxyID, string(next.Status), next.ConsecutiveFailures, next.ConsecutiveSuccesses,
			next.EjectionCount, next.CooldownUntil, next.HealthySince, outcome.LatencyMS, outcome.Reason, nullIfEmpty(outcome.ErrorDetail), now); err != nil {
			return err
		}

		var httpStatus any
		if outcome.HTTPStatus > 0 {
			httpStatus = outcome.HTTPStatus
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO proxy_probe_checks(proxy_id, provider_key, trigger, ok, status, reason, http_status, latency_ms, error_detail, checked_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`, proxyID, providerKey, trigger, outcome.OK, outcome.Status, outcome.Reason, httpStatus, outcome.LatencyMS, nullIfEmpty(outcome.ErrorDetail), now); err != nil {
			return err
		}

		metricValue := 0.0
		switch next.Status {
		case ProxyStatusHealthy:
			metricValue = 1.0
		case ProxyStatusSuspect:
			metricValue = 0.5
		}
		metricProxyHealth.WithLabelValues(providerKey, proxyID.String()).Set(metricValue)
		return nil
	})
}

func (m *Manager) commitSwitch(ctx context.Context, providerKey string, fromID *uuid.UUID, fromName *string, toID *uuid.UUID, toName *string, trigger, reason, detail string) error {
	_, err := m.commitSwitchWithEvent(ctx, providerKey, fromID, fromName, toID, toName, trigger, reason, detail)
	return err
}

func (m *Manager) commitSwitchWithEvent(ctx context.Context, providerKey string, fromID *uuid.UUID, fromName *string, toID *uuid.UUID, toName *string, trigger, reason, detail string) (FailoverEvent, error) {
	eventID := uuid.New()
	var createdAt time.Time
	err := pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		if toID == nil {
			if _, err := tx.Exec(ctx, `
				INSERT INTO provider_proxy_states(provider_key, active_proxy_id, last_switched_at, last_switch_reason, updated_at)
				VALUES ($1, NULL, now(), $2, now())
				ON CONFLICT (provider_key) DO UPDATE SET
					active_proxy_id = NULL,
					last_switched_at = now(),
					last_switch_reason = EXCLUDED.last_switch_reason,
					updated_at = now()
			`, providerKey, reason); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `
				INSERT INTO provider_proxy_states(provider_key, active_proxy_id, last_switched_at, last_switch_reason, updated_at)
				VALUES ($1, $2, now(), $3, now())
				ON CONFLICT (provider_key) DO UPDATE SET
					active_proxy_id = EXCLUDED.active_proxy_id,
					last_switched_at = now(),
					last_switch_reason = EXCLUDED.last_switch_reason,
					updated_at = now()
			`, providerKey, *toID, reason); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `
			INSERT INTO proxy_failover_events(id, provider_key, from_proxy_id, from_proxy_name, to_proxy_id, to_proxy_name, trigger, reason, detail)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			RETURNING created_at
		`, eventID, providerKey, uuidPtrToAny(fromID), strPtrToAny(fromName), uuidPtrToAny(toID), strPtrToAny(toName), trigger, reason, nullIfEmpty(detail)).Scan(&createdAt)
	})
	if err != nil {
		return FailoverEvent{}, err
	}
	metricProxyFailovers.WithLabelValues(providerKey, trigger).Inc()
	m.logger.Info("provider proxy switched",
		"provider_key", providerKey,
		"trigger", trigger,
		"reason", reason,
		"from_proxy", strPtrVal(fromName),
		"to_proxy", strPtrVal(toName))

	var detailPtr *string
	if detail != "" {
		detailPtr = &detail
	}
	return FailoverEvent{
		ID:            eventID.String(),
		ProviderKey:   providerKey,
		FromProxyID:   uuidPtrString(fromID),
		FromProxyName: fromName,
		ToProxyID:     uuidPtrString(toID),
		ToProxyName:   toName,
		Trigger:       trigger,
		Reason:        reason,
		Detail:        detailPtr,
		CreatedAt:     createdAt,
	}, nil
}

func (m *Manager) applyEngineProxy(providerKey, proxyType, proxyURL string) error {
	engine := m.currentEngine()
	if engine == nil {
		return nil
	}
	return engine.UpdateProviderProxy(schemas.ModelProvider(providerKey), proxyType, proxyURL)
}

func (m *Manager) lookupProxyName(ctx context.Context, id uuid.UUID) *string {
	var name string
	if err := m.pool.QueryRow(ctx, `SELECT name FROM platform_proxies WHERE id = $1`, id).Scan(&name); err != nil {
		return nil
	}
	return &name
}

func resolveProxyTargetURL(customProbeURL, providerKey string) (string, bool) {
	if customProbeURL != "" {
		return customProbeURL, true
	}
	return bifrostx.ProviderDefaultProbeURL(providerKey), false
}

// BuildHeartbeat24h aggregates raw check timestamps/statuses into 24 consecutive hourly buckets.
func BuildHeartbeat24h(checks []struct {
	CheckedAt time.Time
	OK        bool
	Status    string
	LatencyMS *int
	Reason    string
}, now time.Time) ([]HeartbeatBucket, *float64) {
	endHour := now.UTC().Truncate(time.Hour)
	startHour := endHour.Add(-23 * time.Hour)
	buckets := make([]HeartbeatBucket, 24)
	latencySums := make([]int64, 24)
	latencyCounts := make([]int, 24)
	degradedSeen := make([]bool, 24)

	for i := 0; i < 24; i++ {
		buckets[i] = HeartbeatBucket{
			BucketStart: startHour.Add(time.Duration(i) * time.Hour),
			Status:      "no_data",
		}
	}

	var totalAll, okAll int
	for _, c := range checks {
		t := c.CheckedAt.UTC()
		if t.Before(startHour) || t.After(endHour.Add(time.Hour)) {
			continue
		}
		idx := int(t.Sub(startHour) / time.Hour)
		if idx < 0 || idx >= 24 {
			continue
		}
		totalAll++
		buckets[idx].Total++
		if c.OK {
			okAll++
			buckets[idx].OKCount++
		} else if c.Reason != "" {
			buckets[idx].LastReason = c.Reason
		}
		if c.Status == "degraded" {
			degradedSeen[idx] = true
			if buckets[idx].LastReason == "" && c.Reason != "" {
				buckets[idx].LastReason = c.Reason
			}
		}
		if c.LatencyMS != nil {
			latencySums[idx] += int64(*c.LatencyMS)
			latencyCounts[idx]++
		}
	}

	for i := 0; i < 24; i++ {
		if buckets[i].Total == 0 {
			continue
		}
		if latencyCounts[i] > 0 {
			avg := int(latencySums[i] / int64(latencyCounts[i]))
			buckets[i].AvgLatencyMS = &avg
		}
		switch {
		case buckets[i].OKCount == 0:
			buckets[i].Status = "unhealthy"
		case buckets[i].OKCount == buckets[i].Total && !degradedSeen[i]:
			buckets[i].Status = "healthy"
		default:
			buckets[i].Status = "degraded"
		}
	}

	if totalAll == 0 {
		return buckets, nil
	}
	avail := float64(okAll) * 100 / float64(totalAll)
	return buckets, &avail
}

func (m *Manager) ListFailoverEvents(ctx context.Context, providerKey string, limit int) ([]FailoverEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows pgx.Rows
	var err error
	if providerKey != "" {
		rows, err = m.pool.Query(ctx, `
			SELECT id, provider_key, from_proxy_id, from_proxy_name, to_proxy_id, to_proxy_name, trigger, reason, detail, created_at
			FROM proxy_failover_events
			WHERE provider_key = $1
			ORDER BY created_at DESC
			LIMIT $2
		`, providerKey, limit)
	} else {
		rows, err = m.pool.Query(ctx, `
			SELECT id, provider_key, from_proxy_id, from_proxy_name, to_proxy_id, to_proxy_name, trigger, reason, detail, created_at
			FROM proxy_failover_events
			ORDER BY created_at DESC
			LIMIT $1
		`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []FailoverEvent{}
	for rows.Next() {
		var id uuid.UUID
		var fromID, toID uuid.NullUUID
		var ev FailoverEvent
		if err := rows.Scan(&id, &ev.ProviderKey, &fromID, &ev.FromProxyName, &toID, &ev.ToProxyName, &ev.Trigger, &ev.Reason, &ev.Detail, &ev.CreatedAt); err != nil {
			return nil, err
		}
		ev.ID = id.String()
		if fromID.Valid {
			s := fromID.UUID.String()
			ev.FromProxyID = &s
		}
		if toID.Valid {
			s := toID.UUID.String()
			ev.ToProxyID = &s
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func uuidPtrToAny(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

func strPtrToAny(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func strPtrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func uuidPtrString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

var _ = fmt.Sprint
