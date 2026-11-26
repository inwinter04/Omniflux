package httpx

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	providerUtils "github.com/maximhq/bifrost/core/providers/utils"

	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/vault"
)

type platformProxy struct {
	ID           uuid.UUID
	Name         string
	Type         string
	URL          string
	ProviderKeys []string
	UpdatedAt    time.Time
}

func scanPlatformProxy(scanner interface {
	Scan(dest ...any) error
}, masterKey []byte) (platformProxy, error) {
	var proxy platformProxy
	var ciphertext []byte
	if err := scanner.Scan(&proxy.ID, &proxy.Name, &proxy.Type, &ciphertext, &proxy.UpdatedAt); err != nil {
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
  SELECT id, name, proxy_type, proxy_ciphertext, updated_at
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
  SELECT id, name, proxy_type, proxy_ciphertext, updated_at
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
  SELECT p.id, p.name, p.proxy_type, p.proxy_ciphertext, p.updated_at
  FROM platform_proxies p
  JOIN platform_proxy_providers pp ON pp.proxy_id = p.id
  WHERE pp.provider_key = $1
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

func loadPlatformProxyTypes(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	rows, err := pool.Query(ctx, `
  SELECT pp.provider_key, p.proxy_type
  FROM platform_proxy_providers pp
  JOIN platform_proxies p ON p.id = pp.proxy_id
 `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types := make(map[string]string)
	for rows.Next() {
		var providerKey, proxyType string
		if err := rows.Scan(&providerKey, &proxyType); err != nil {
			return nil, err
		}
		types[providerKey] = proxyType
	}
	return types, rows.Err()
}

func attachPlatformProxyProviders(ctx context.Context, pool *pgxpool.Pool, proxies []platformProxy) error {
	if len(proxies) == 0 {
		return nil
	}
	rows, err := pool.Query(ctx, `
  SELECT proxy_id, provider_key
  FROM platform_proxy_providers
  ORDER BY provider_key ASC
 `)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := make(map[uuid.UUID]int, len(proxies))
	for index, proxy := range proxies {
		byID[proxy.ID] = index
		proxies[index].ProviderKeys = []string{}
	}
	for rows.Next() {
		var proxyID uuid.UUID
		var providerKey string
		if err := rows.Scan(&proxyID, &providerKey); err != nil {
			return err
		}
		if index, exists := byID[proxyID]; exists {
			proxies[index].ProviderKeys = append(proxies[index].ProviderKeys, providerKey)
		}
	}
	return rows.Err()
}

func insertPlatformProxy(ctx context.Context, tx pgx.Tx, proxy platformProxy, masterKey []byte) (time.Time, error) {
	ciphertext, err := vault.Encrypt(masterKey, []byte(proxy.URL))
	if err != nil {
		return time.Time{}, err
	}
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `
  INSERT INTO platform_proxies(id, name, proxy_type, proxy_ciphertext, proxy_key_id, created_at, updated_at)
  VALUES ($1, $2, $3, $4, $5, now(), now())
  RETURNING updated_at
 `, proxy.ID, proxy.Name, proxy.Type, ciphertext, vault.KeyID).Scan(&updatedAt)
	return updatedAt, err
}

func insertPlatformProxyProviders(ctx context.Context, tx pgx.Tx, proxy platformProxy) error {
	for _, providerKey := range proxy.ProviderKeys {
		if _, err := tx.Exec(ctx, `
    INSERT INTO platform_proxy_providers(provider_key, proxy_id)
    VALUES ($1, $2)
  `, providerKey, proxy.ID); err != nil {
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
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `
  UPDATE platform_proxies
  SET name = $2, proxy_type = $3, proxy_ciphertext = $4, proxy_key_id = $5, updated_at = now()
  WHERE id = $1
  RETURNING updated_at
 `, proxy.ID, proxy.Name, proxy.Type, ciphertext, vault.KeyID).Scan(&updatedAt)
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

func platformProxyTransport(proxyType, proxyURL string) (*http.Transport, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// 不配置代理时显式直连，避免进程环境代理造成测试和真实调用路径不一致。
	transport.Proxy = nil
	config, err := bifrostx.BuildProxyConfig(proxyType, proxyURL)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return transport, nil
	}
	proxy, tlsConfig, err := providerUtils.NetHTTPProxy(config)
	if err != nil {
		return nil, err
	}
	if proxy == nil {
		return nil, errors.New("platform proxy configuration did not produce an HTTP proxy")
	}
	transport.Proxy = proxy
	transport.TLSClientConfig = tlsConfig
	return transport, nil
}

func providerHTTPClientFor(proxyType, proxyURL string) (*http.Client, error) {
	transport, err := platformProxyTransport(proxyType, proxyURL)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func balanceHTTPClientFor(proxyType, proxyURL string) (*http.Client, error) {
	transport, err := platformProxyTransport(proxyType, proxyURL)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, nil
}
