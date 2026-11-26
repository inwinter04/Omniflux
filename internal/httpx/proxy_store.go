package httpx

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/governance"
	"github.com/inwinter04/omniflux/internal/vault"
)

type platformProxyBinding struct {
	ProviderKey string `json:"provider_key"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
}

type platformProxy struct {
	ID           uuid.UUID
	Name         string
	Type         string
	URL          string
	Enabled      bool
	ProbeURL     string
	ProviderKeys []string
	Bindings     []platformProxyBinding
	UpdatedAt    time.Time
}

type activeProxySummary struct {
	ProxyID     string
	ProxyName   string
	ProxyType   string
	ProxyStatus string
	PoolSize    int
}

func scanPlatformProxy(scanner interface {
	Scan(dest ...any) error
}, masterKey []byte) (platformProxy, error) {
	var proxy platformProxy
	var ciphertext []byte
	if err := scanner.Scan(&proxy.ID, &proxy.Name, &proxy.Type, &ciphertext, &proxy.Enabled, &proxy.ProbeURL, &proxy.UpdatedAt); err != nil {
		return platformProxy{}, err
	}
	plaintext, err := vault.Decrypt(masterKey, ciphertext)
	if err != nil {
		return platformProxy{}, err
	}
	proxy.URL = string(plaintext)
	return proxy, nil
}

func loadPlatformProxies(ctx context.Context, pool *pgxpool.Pool, masterKey []byte) ([]platformProxy, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name, proxy_type, proxy_ciphertext, enabled, COALESCE(probe_url, ''), updated_at
		FROM platform_proxies
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	proxies := []platformProxy{}
	for rows.Next() {
		proxy, err := scanPlatformProxy(rows, masterKey)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, proxy)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := attachPlatformProxyProviders(ctx, pool, proxies); err != nil {
		return nil, err
	}
	return proxies, nil
}

func loadPlatformProxyByID(ctx context.Context, pool *pgxpool.Pool, masterKey []byte, id uuid.UUID) (platformProxy, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, name, proxy_type, proxy_ciphertext, enabled, COALESCE(probe_url, ''), updated_at
		FROM platform_proxies
		WHERE id = $1
	`, id)
	proxy, err := scanPlatformProxy(row, masterKey)
	if err != nil {
		return platformProxy{}, err
	}
	if err := attachPlatformProxyProviders(ctx, pool, []platformProxy{proxy}); err != nil {
		return platformProxy{}, err
	}
	return proxy, nil
}

func loadPlatformProxyForProvider(ctx context.Context, pool *pgxpool.Pool, masterKey []byte, providerKey string) (platformProxy, error) {
	row := pool.QueryRow(ctx, `
		SELECT p.id, p.name, p.proxy_type, p.proxy_ciphertext, p.enabled, COALESCE(p.probe_url, ''), p.updated_at
		FROM platform_proxy_providers pp
		JOIN platform_proxies p ON p.id = pp.proxy_id
		LEFT JOIN provider_proxy_states s ON s.provider_key = pp.provider_key
		WHERE pp.provider_key = $1 AND p.enabled = true AND pp.enabled = true
		ORDER BY CASE WHEN s.active_proxy_id = pp.proxy_id THEN 0 ELSE 1 END,
		         pp.priority ASC, pp.created_at ASC
		LIMIT 1
	`, providerKey)
	proxy, err := scanPlatformProxy(row, masterKey)
	if err != nil {
		return platformProxy{}, err
	}
	if err := attachPlatformProxyProviders(ctx, pool, []platformProxy{proxy}); err != nil {
		return platformProxy{}, err
	}
	return proxy, nil
}

func loadPlatformActiveProxySummaries(ctx context.Context, pool *pgxpool.Pool) (map[string]activeProxySummary, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT ON (pp.provider_key)
		       pp.provider_key, p.id, p.name, p.proxy_type, COALESCE(hs.status, 'unknown')
		FROM platform_proxy_providers pp
		JOIN platform_proxies p ON p.id = pp.proxy_id
		LEFT JOIN provider_proxy_states s ON s.provider_key = pp.provider_key
		LEFT JOIN proxy_health_states hs ON hs.provider_key = pp.provider_key AND hs.proxy_id = pp.proxy_id
		WHERE p.enabled = true AND pp.enabled = true
		ORDER BY pp.provider_key,
		         CASE WHEN s.active_proxy_id = pp.proxy_id THEN 0 ELSE 1 END,
		         pp.priority ASC, pp.created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summaries := make(map[string]activeProxySummary)
	for rows.Next() {
		var providerKey string
		var id uuid.UUID
		var sum activeProxySummary
		if err := rows.Scan(&providerKey, &id, &sum.ProxyName, &sum.ProxyType, &sum.ProxyStatus); err != nil {
			return nil, err
		}
		sum.ProxyID = id.String()
		summaries[providerKey] = sum
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	countRows, err := pool.Query(ctx, `
		SELECT pp.provider_key, count(*)
		FROM platform_proxy_providers pp
		JOIN platform_proxies p ON p.id = pp.proxy_id
		WHERE p.enabled = true AND pp.enabled = true
		GROUP BY pp.provider_key
	`)
	if err != nil {
		return nil, err
	}
	defer countRows.Close()
	for countRows.Next() {
		var providerKey string
		var cnt int
		if err := countRows.Scan(&providerKey, &cnt); err != nil {
			return nil, err
		}
		sum := summaries[providerKey]
		sum.PoolSize = cnt
		summaries[providerKey] = sum
	}
	return summaries, countRows.Err()
}

func attachPlatformProxyProviders(ctx context.Context, pool *pgxpool.Pool, proxies []platformProxy) error {
	if len(proxies) == 0 {
		return nil
	}
	rows, err := pool.Query(ctx, `
		SELECT proxy_id, provider_key, priority, enabled
		FROM platform_proxy_providers
		ORDER BY priority ASC, provider_key ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := make(map[uuid.UUID]int, len(proxies))
	for index, proxy := range proxies {
		byID[proxy.ID] = index
		proxies[index].ProviderKeys = []string{}
		proxies[index].Bindings = []platformProxyBinding{}
	}
	for rows.Next() {
		var proxyID uuid.UUID
		var binding platformProxyBinding
		if err := rows.Scan(&proxyID, &binding.ProviderKey, &binding.Priority, &binding.Enabled); err != nil {
			return err
		}
		if index, exists := byID[proxyID]; exists {
			proxies[index].ProviderKeys = append(proxies[index].ProviderKeys, binding.ProviderKey)
			proxies[index].Bindings = append(proxies[index].Bindings, binding)
		}
	}
	return rows.Err()
}

func insertPlatformProxy(ctx context.Context, tx pgx.Tx, proxy platformProxy, masterKey []byte) (time.Time, error) {
	ciphertext, err := vault.Encrypt(masterKey, []byte(proxy.URL))
	if err != nil {
		return time.Time{}, err
	}
	var probeURL any
	if proxy.ProbeURL != "" {
		probeURL = proxy.ProbeURL
	}
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO platform_proxies(id, name, proxy_type, proxy_ciphertext, proxy_key_id, enabled, probe_url, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
		RETURNING updated_at
	`, proxy.ID, proxy.Name, proxy.Type, ciphertext, vault.KeyID, proxy.Enabled, probeURL).Scan(&updatedAt)
	return updatedAt, err
}

func insertPlatformProxyProviders(ctx context.Context, tx pgx.Tx, proxy platformProxy) error {
	bindings := proxy.Bindings
	if len(bindings) == 0 {
		for _, providerKey := range proxy.ProviderKeys {
			bindings = append(bindings, platformProxyBinding{
				ProviderKey: providerKey,
				Priority:    100,
				Enabled:     true,
			})
		}
	}
	for _, b := range bindings {
		prio := b.Priority
		if prio < 1 {
			prio = 100
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO platform_proxy_providers(provider_key, proxy_id, priority, enabled)
			VALUES ($1, $2, $3, $4)
		`, b.ProviderKey, proxy.ID, prio, b.Enabled); err != nil {
			return err
		}
	}
	return nil
}

func updatePlatformProxy(ctx context.Context, tx pgx.Tx, proxy platformProxy, masterKey []byte) (time.Time, error) {
	ciphertext, err := vault.Encrypt(masterKey, []byte(proxy.URL))
	if err != nil {
		return time.Time{}, err
	}
	var probeURL any
	if proxy.ProbeURL != "" {
		probeURL = proxy.ProbeURL
	}
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `
		UPDATE platform_proxies
		SET name = $2, proxy_type = $3, proxy_ciphertext = $4, proxy_key_id = $5,
		    enabled = $6, probe_url = $7, updated_at = now()
		WHERE id = $1
		RETURNING updated_at
	`, proxy.ID, proxy.Name, proxy.Type, ciphertext, vault.KeyID, proxy.Enabled, probeURL).Scan(&updatedAt)
	return updatedAt, err
}

func replacePlatformProxyProviders(ctx context.Context, tx pgx.Tx, proxy platformProxy) error {
	if _, err := tx.Exec(ctx, `DELETE FROM platform_proxy_providers WHERE proxy_id = $1`, proxy.ID); err != nil {
		return err
	}
	return insertPlatformProxyProviders(ctx, tx, proxy)
}

func deletePlatformProxy(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `DELETE FROM platform_proxies WHERE id = $1`, id)
	return err
}

func providerHTTPClientFor(proxyType, proxyURL string) (*http.Client, error) {
	return governance.HTTPClientForProxy(proxyType, proxyURL, 0, false)
}

func balanceHTTPClientFor(proxyType, proxyURL string) (*http.Client, error) {
	return governance.HTTPClientForProxy(proxyType, proxyURL, 15*time.Second, true)
}

var _ = bifrostx.ProxyTypeNone
