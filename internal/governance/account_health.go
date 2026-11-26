package governance

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/vault"
)

type ProviderHealthSettings struct {
	Enabled           bool       `json:"enabled"`
	IntervalSeconds   int        `json:"interval_seconds"`
	TimeoutSeconds    int        `json:"timeout_seconds"`
	DegradedLatencyMS int        `json:"degraded_latency_ms"`
	FailureThreshold  int        `json:"failure_threshold"`
	UpdatedBy         *string    `json:"updated_by,omitempty"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
}

type AccountProbeResult struct {
	AccountID           string    `json:"account_id"`
	Provider            string    `json:"provider"`
	OK                  bool      `json:"ok"`
	Status              string    `json:"status"`
	Reason              string    `json:"reason"`
	StatusCode          int       `json:"status_code,omitempty"`
	LatencyMS           int       `json:"latency_ms"`
	ProxyID             *string   `json:"proxy_id,omitempty"`
	ProxyName           *string   `json:"proxy_name,omitempty"`
	Detail              string    `json:"detail,omitempty"`
	CheckedAt           time.Time `json:"checked_at"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
}

type BatchAccountProbeResult struct {
	Total       int                  `json:"total"`
	Checked     int                  `json:"checked"`
	Healthy     int                  `json:"healthy"`
	Degraded    int                  `json:"degraded"`
	Unhealthy   int                  `json:"unhealthy"`
	Unsupported int                  `json:"unsupported"`
	Results     []AccountProbeResult `json:"results"`
}

func (m *Manager) LoadProviderHealthSettings(ctx context.Context) (ProviderHealthSettings, error) {
	var s ProviderHealthSettings
	var updatedBy uuid.NullUUID
	var updatedAt time.Time
	err := m.pool.QueryRow(ctx, `
		SELECT enabled, interval_seconds, timeout_seconds, degraded_latency_ms, failure_threshold, updated_by, updated_at
		FROM provider_health_check_settings
		WHERE singleton = true
	`).Scan(&s.Enabled, &s.IntervalSeconds, &s.TimeoutSeconds, &s.DegradedLatencyMS, &s.FailureThreshold, &updatedBy, &updatedAt)
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

func (m *Manager) SaveProviderHealthSettings(ctx context.Context, s ProviderHealthSettings, actorUserID uuid.UUID) (ProviderHealthSettings, error) {
	_, err := m.pool.Exec(ctx, `
		UPDATE provider_health_check_settings
		SET enabled = $1,
		    interval_seconds = $2,
		    timeout_seconds = $3,
		    degraded_latency_ms = $4,
		    failure_threshold = $5,
		    updated_by = $6,
		    updated_at = now()
		WHERE singleton = true
	`, s.Enabled, s.IntervalSeconds, s.TimeoutSeconds, s.DegradedLatencyMS, s.FailureThreshold, actorUserID)
	if err != nil {
		return ProviderHealthSettings{}, err
	}
	return m.LoadProviderHealthSettings(ctx)
}

func (m *Manager) runAccountScheduler(ctx context.Context) {
	// 启动后稍作错峰再执行首次全量账号探测。
	initialTimer := time.NewTimer(10 * time.Second)
	select {
	case <-ctx.Done():
		initialTimer.Stop()
		return
	case <-initialTimer.C:
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	m.runAccountTick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runAccountTick(ctx)
		}
	}
}

func (m *Manager) runAccountTick(ctx context.Context) {
	settings, err := m.LoadProviderHealthSettings(ctx)
	if err != nil {
		m.logger.Warn("load provider health check settings failed", "error", err)
		return
	}
	if !settings.Enabled {
		return
	}
	interval := time.Duration(settings.IntervalSeconds) * time.Second
	now := time.Now()

	rows, err := m.pool.Query(ctx, `
		SELECT a.id, a.organization_id, s.last_checked_at
		FROM provider_accounts a
		LEFT JOIN provider_account_health_states s ON s.provider_account_id = a.id
		WHERE a.status = 'active'
		ORDER BY s.last_checked_at ASC NULLS FIRST
	`)
	if err != nil {
		m.logger.Warn("query due provider accounts failed", "error", err)
		return
	}
	defer rows.Close()

	type target struct {
		accountID uuid.UUID
		orgID     uuid.UUID
	}
	due := []target{}
	for rows.Next() {
		var item target
		var lastChecked *time.Time
		if err := rows.Scan(&item.accountID, &item.orgID, &lastChecked); err != nil {
			continue
		}
		if lastChecked == nil || now.Sub(*lastChecked) >= interval {
			due = append(due, item)
		}
	}
	if err := rows.Err(); err != nil || len(due) == 0 {
		return
	}

	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, item := range due {
		m.schedMu.Lock()
		if m.probingAccounts[item.accountID] {
			m.schedMu.Unlock()
			continue
		}
		m.probingAccounts[item.accountID] = true
		m.schedMu.Unlock()

		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			defer func() {
				m.schedMu.Lock()
				delete(m.probingAccounts, t.accountID)
				m.schedMu.Unlock()
			}()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			// 微小随机抖动避免多个同厂商账号同毫秒打向上游。
			jitter := time.Duration(rand.IntN(400)) * time.Millisecond
			select {
			case <-time.After(jitter):
			case <-ctx.Done():
				return
			}
			probeCtx, cancel := context.WithTimeout(ctx, time.Duration(settings.TimeoutSeconds+5)*time.Second)
			defer cancel()
			if _, err := m.probeAccountWithSettings(probeCtx, t.orgID, t.accountID, "auto", settings); err != nil && !errors.Is(err, context.Canceled) {
				m.logger.Warn("auto probe provider account failed", "account_id", t.accountID, "error", err)
			}
		}(item)
	}
	wg.Wait()
}

// ProbeAccount runs an immediate connectivity/credential probe for one provider account and persists the outcome.
func (m *Manager) ProbeAccount(ctx context.Context, organizationID, accountID uuid.UUID, trigger string) (AccountProbeResult, error) {
	settings, err := m.LoadProviderHealthSettings(ctx)
	if err != nil {
		return AccountProbeResult{}, err
	}
	return m.probeAccountWithSettings(ctx, organizationID, accountID, trigger, settings)
}

func (m *Manager) probeAccountWithSettings(ctx context.Context, organizationID, accountID uuid.UUID, trigger string, settings ProviderHealthSettings) (AccountProbeResult, error) {
	var provider string
	var baseURL *string
	var ciphertext []byte
	err := m.pool.QueryRow(ctx, `
		SELECT a.provider, a.base_url, c.ciphertext
		FROM provider_accounts a
		JOIN provider_credentials c ON c.provider_account_id = a.id
		WHERE a.id = $1 AND a.organization_id = $2
	`, accountID, organizationID).Scan(&provider, &baseURL, &ciphertext)
	if err != nil {
		return AccountProbeResult{}, err
	}
	plaintext, err := vault.Decrypt(m.masterKey, ciphertext)
	if err != nil {
		return AccountProbeResult{}, err
	}
	apiKey := string(plaintext)

	providerKey, err := bifrostx.MapProvider(provider)
	if err != nil {
		return AccountProbeResult{}, err
	}
	activeProxy, hasProxy, err := m.ResolveActiveProxy(ctx, string(providerKey))
	if err != nil {
		return AccountProbeResult{}, err
	}
	var proxyID *uuid.UUID
	var proxyName *string
	proxyType, proxyURL := bifrostx.ProxyTypeNone, ""
	if hasProxy {
		id := activeProxy.ProxyID
		name := activeProxy.Name
		proxyID = &id
		proxyName = &name
		proxyType = activeProxy.Type
		proxyURL = activeProxy.URL
	}

	now := time.Now()
	testURL, headers, probeReqErr := ProviderProbeRequest(provider, baseURL, apiKey)
	if probeReqErr != nil {
		if err := m.persistUnsupportedAccountState(ctx, accountID, now); err != nil {
			return AccountProbeResult{}, err
		}
		if trigger == "manual" {
			return AccountProbeResult{
				AccountID: accountID.String(),
				Provider:  provider,
				OK:        false,
				Status:    "unsupported",
				Reason:    "unsupported",
				CheckedAt: now,
			}, ErrProviderTestUnsupported
		}
		return AccountProbeResult{
			AccountID: accountID.String(),
			Provider:  provider,
			OK:        false,
			Status:    "unsupported",
			Reason:    "unsupported",
			CheckedAt: now,
		}, nil
	}

	timeout := time.Duration(settings.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client, err := HTTPClientForProxy(proxyType, proxyURL, timeout, false)
	if err != nil {
		return AccountProbeResult{}, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, testURL, nil)
	if err != nil {
		return AccountProbeResult{}, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, doErr := client.Do(req)
	latencyMS := int(time.Since(start).Milliseconds())

	var ok bool
	var checkStatus, reason, detail string
	var statusCode int
	if doErr != nil {
		ok = false
		checkStatus = "unhealthy"
		reason, detail = ClassifyTransportError(doErr, hasProxy)
		if reason == "proxy_error" {
			m.ReportTransportError(string(providerKey))
		}
	} else {
		defer resp.Body.Close()
		statusCode = resp.StatusCode
		switch {
		case statusCode >= 200 && statusCode < 300:
			ok = true
			if settings.DegradedLatencyMS > 0 && latencyMS > settings.DegradedLatencyMS {
				checkStatus = "degraded"
				reason = "slow_response"
			} else {
				checkStatus = "healthy"
				reason = "connected"
			}
		case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
			ok = false
			checkStatus = "unhealthy"
			reason = "credentials_rejected"
			detail = fmt.Sprintf("HTTP %d", statusCode)
		case statusCode == http.StatusTooManyRequests:
			ok = false
			checkStatus = "degraded"
			reason = "rate_limited"
			detail = fmt.Sprintf("HTTP %d", statusCode)
		case statusCode == http.StatusNotFound:
			ok = false
			checkStatus = "unhealthy"
			reason = "v1_suffix_suspected"
			detail = fmt.Sprintf("HTTP %d", statusCode)
		default:
			ok = false
			checkStatus = "unhealthy"
			reason = "upstream_http_error"
			detail = fmt.Sprintf("HTTP %d", statusCode)
		}
	}

	consecutiveFailures, err := m.persistAccountProbe(ctx, accountID, organizationID, provider, trigger, ok, checkStatus, reason, statusCode, latencyMS, proxyID, proxyName, detail, now, settings)
	if err != nil {
		return AccountProbeResult{}, err
	}

	return AccountProbeResult{
		AccountID:           accountID.String(),
		Provider:            provider,
		OK:                  ok,
		Status:              checkStatus,
		Reason:              reason,
		StatusCode:          statusCode,
		LatencyMS:           latencyMS,
		ProxyID:             uuidPtrString(proxyID),
		ProxyName:           proxyName,
		Detail:              detail,
		CheckedAt:           now,
		ConsecutiveFailures: consecutiveFailures,
	}, nil
}

func (m *Manager) persistUnsupportedAccountState(ctx context.Context, accountID uuid.UUID, now time.Time) error {
	_, err := m.pool.Exec(ctx, `
		INSERT INTO provider_account_health_states(provider_account_id, status, reason, last_checked_at, updated_at)
		VALUES ($1, 'unsupported', 'unsupported', $2, now())
		ON CONFLICT (provider_account_id) DO UPDATE SET
			status = 'unsupported',
			reason = 'unsupported',
			last_checked_at = EXCLUDED.last_checked_at,
			updated_at = now()
	`, accountID, now)
	return err
}

func (m *Manager) persistAccountProbe(
	ctx context.Context,
	accountID, organizationID uuid.UUID,
	provider, trigger string,
	ok bool,
	checkStatus, reason string,
	statusCode, latencyMS int,
	proxyID *uuid.UUID,
	proxyName *string,
	detail string,
	now time.Time,
	settings ProviderHealthSettings,
) (int, error) {
	var consecutiveFailures int
	err := pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		var prevFailures int
		var prevHealthyAt *time.Time
		err := tx.QueryRow(ctx, `
			SELECT consecutive_failures, last_healthy_at
			FROM provider_account_health_states
			WHERE provider_account_id = $1
			FOR UPDATE
		`, accountID).Scan(&prevFailures, &prevHealthyAt)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		stateStatus := checkStatus
		lastHealthyAt := prevHealthyAt
		if ok {
			consecutiveFailures = 0
			lastHealthyAt = &now
		} else {
			consecutiveFailures = prevFailures + 1
			threshold := settings.FailureThreshold
			if threshold < 1 {
				threshold = 1
			}
			switch reason {
			case "credentials_rejected", "v1_suffix_suspected":
				stateStatus = "unhealthy"
			case "rate_limited":
				stateStatus = "degraded"
			default:
				if consecutiveFailures < threshold {
					stateStatus = "degraded"
				} else {
					stateStatus = "unhealthy"
				}
			}
		}

		var httpStatus any
		if statusCode > 0 {
			httpStatus = statusCode
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO provider_account_health_states(
				provider_account_id, status, reason, http_status, latency_ms,
				proxy_id, proxy_name, error_detail, consecutive_failures, last_checked_at, last_healthy_at, updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now())
			ON CONFLICT (provider_account_id) DO UPDATE SET
				status = EXCLUDED.status,
				reason = EXCLUDED.reason,
				http_status = EXCLUDED.http_status,
				latency_ms = EXCLUDED.latency_ms,
				proxy_id = EXCLUDED.proxy_id,
				proxy_name = EXCLUDED.proxy_name,
				error_detail = EXCLUDED.error_detail,
				consecutive_failures = EXCLUDED.consecutive_failures,
				last_checked_at = EXCLUDED.last_checked_at,
				last_healthy_at = EXCLUDED.last_healthy_at,
				updated_at = now()
		`, accountID, stateStatus, reason, httpStatus, latencyMS, uuidPtrToAny(proxyID), strPtrToAny(proxyName), nullIfEmpty(detail), consecutiveFailures, now, lastHealthyAt); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO provider_account_health_checks(
				provider_account_id, organization_id, provider, trigger, ok, status, reason,
				http_status, latency_ms, proxy_id, proxy_name, error_detail, checked_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		`, accountID, organizationID, provider, trigger, ok, checkStatus, reason, httpStatus, latencyMS, uuidPtrToAny(proxyID), strPtrToAny(proxyName), nullIfEmpty(detail), now); err != nil {
			return err
		}

		val := 0.0
		switch stateStatus {
		case "healthy":
			val = 1.0
		case "degraded":
			val = 0.5
		}
		metricAccountProbe.WithLabelValues(accountID.String(), provider).Set(val)
		return nil
	})
	return consecutiveFailures, err
}

// ProbeOrganizationAccounts triggers synchronous bounded-concurrency probing for all active accounts in an org.
func (m *Manager) ProbeOrganizationAccounts(ctx context.Context, organizationID uuid.UUID, trigger string) (BatchAccountProbeResult, error) {
	settings, err := m.LoadProviderHealthSettings(ctx)
	if err != nil {
		return BatchAccountProbeResult{}, err
	}
	rows, err := m.pool.Query(ctx, `
		SELECT id
		FROM provider_accounts
		WHERE organization_id = $1 AND status = 'active'
		ORDER BY created_at DESC
	`, organizationID)
	if err != nil {
		return BatchAccountProbeResult{}, err
	}
	defer rows.Close()
	accountIDs := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return BatchAccountProbeResult{}, err
		}
		accountIDs = append(accountIDs, id)
	}
	if err := rows.Err(); err != nil {
		return BatchAccountProbeResult{}, err
	}

	batch := BatchAccountProbeResult{
		Total:   len(accountIDs),
		Results: make([]AccountProbeResult, len(accountIDs)),
	}
	if len(accountIDs) == 0 {
		return batch, nil
	}

	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for idx, id := range accountIDs {
		wg.Add(1)
		go func(i int, accID uuid.UUID) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := m.probeAccountWithSettings(ctx, organizationID, accID, trigger, settings)
			if err != nil && !errors.Is(err, ErrProviderTestUnsupported) {
				batch.Results[i] = AccountProbeResult{
					AccountID: accID.String(),
					OK:        false,
					Status:    "unhealthy",
					Reason:    "probe_internal_error",
					Detail:    SanitizeErrorDetail(err.Error()),
					CheckedAt: time.Now(),
				}
				return
			}
			batch.Results[i] = res
		}(idx, id)
	}
	wg.Wait()

	for _, res := range batch.Results {
		batch.Checked++
		switch res.Status {
		case "healthy":
			batch.Healthy++
		case "degraded":
			batch.Degraded++
		case "unsupported":
			batch.Unsupported++
		default:
			batch.Unhealthy++
		}
	}
	return batch, nil
}
