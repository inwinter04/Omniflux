package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/inwinter04/omniflux/internal/auth"
	"github.com/inwinter04/omniflux/internal/bifrostx"
)

type platformProxyResponse struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	ProviderKeys   []string   `json:"provider_keys"`
	ProviderLabels []string   `json:"provider_labels"`
	ProxyType      string     `json:"proxy_type"`
	ProxyURL       string     `json:"proxy_url,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at"`
	ActiveAccounts int        `json:"active_accounts"`
}

type providerOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type platformProxyListResponse struct {
	Proxies         []platformProxyResponse `json:"proxies"`
	ProviderOptions []providerOption        `json:"provider_options"`
}

type platformProxyRequest struct {
	ProviderKeys []string `json:"provider_keys"`
	Name         string   `json:"name"`
	ProxyType    string   `json:"proxy_type"`
	ProxyURL     string   `json:"proxy_url"`
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

func normalizeProviderKeys(rawKeys []string) ([]string, bool) {
	seen := make(map[string]bool)
	keys := make([]string, 0, len(rawKeys))
	for _, rawKey := range rawKeys {
		key := strings.TrimSpace(rawKey)
		if key == "" || seen[key] || !bifrostx.IsSupportedProviderKey(key) {
			return nil, false
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, false
	}
	sort.Slice(keys, func(i, j int) bool { return providerKeyLabel(keys[i]) < providerKeyLabel(keys[j]) })
	return keys, true
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

	list := make([]platformProxyResponse, 0, len(proxies))
	for _, proxy := range proxies {
		redactedURL, redactErr := redactProxyURL(proxy.URL)
		if redactErr != nil {
			adminError(w, http.StatusInternalServerError, "PLATFORM_PROXY_LIST_FAILED", "failed-to-query-platform-proxies", nil)
			return
		}
		updatedAt := proxy.UpdatedAt
		list = append(list, platformProxyResponse{
			ID:             proxy.ID.String(),
			Name:           proxy.Name,
			ProviderKeys:   proxy.ProviderKeys,
			ProviderLabels: providerLabels(proxy.ProviderKeys),
			ProxyType:      proxy.Type,
			ProxyURL:       redactedURL,
			UpdatedAt:      &updatedAt,
			ActiveAccounts: activeAccountsFor(proxy, activeCounts),
		})
	}
	adminOK(w, platformProxyListResponse{Proxies: list, ProviderOptions: platformProviderOptions()})
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
	proxy, ok := d.parsePlatformProxyRequest(w, body, true, platformProxy{})
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
			map[string]any{"name": proxy.Name, "provider_keys": proxy.ProviderKeys, "proxy_type": proxy.Type})
	})
	if err != nil {
		d.writePlatformProxyConflict(w, err, "PLATFORM_PROXY_CREATE_FAILED", "failed-to-create-platform-proxy")
		return
	}
	if !d.reloadEngine(w, r) {
		return
	}
	adminOK(w, platformProxyResponse{
		ID:             proxy.ID.String(),
		Name:           proxy.Name,
		ProviderKeys:   proxy.ProviderKeys,
		ProviderLabels: providerLabels(proxy.ProviderKeys),
		ProxyType:      proxy.Type,
		ProxyURL:       redactedURL,
		UpdatedAt:      &updatedAt,
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
			map[string]any{"name": oldProxy.Name, "provider_keys": oldProxy.ProviderKeys, "proxy_type": oldProxy.Type},
			map[string]any{"name": proxy.Name, "provider_keys": proxy.ProviderKeys, "proxy_type": proxy.Type, "proxy_url_changed": oldProxy.URL != proxy.URL})
	})
	if err != nil {
		d.writePlatformProxyConflict(w, err, "PLATFORM_PROXY_UPDATE_FAILED", "failed-to-update-platform-proxy")
		return
	}
	if !d.reloadEngine(w, r) {
		return
	}
	adminOK(w, platformProxyResponse{
		ID:             proxy.ID.String(),
		Name:           proxy.Name,
		ProviderKeys:   proxy.ProviderKeys,
		ProviderLabels: providerLabels(proxy.ProviderKeys),
		ProxyType:      proxy.Type,
		ProxyURL:       redactedURL,
		UpdatedAt:      &updatedAt,
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
	if !d.reloadEngine(w, r) {
		return
	}
	adminOK(w, map[string]string{"status": "deleted"})
}

func (d ServerDeps) parsePlatformProxyRequest(w http.ResponseWriter, body platformProxyRequest, creating bool, oldProxy platformProxy) (platformProxy, bool) {
	providerKeys, providerKeysValid := normalizeProviderKeys(body.ProviderKeys)
	name := strings.TrimSpace(body.Name)
	proxyType := strings.TrimSpace(body.ProxyType)
	proxyURL := strings.TrimSpace(body.ProxyURL)

	if !providerKeysValid {
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
	return platformProxy{ProviderKeys: providerKeys, Name: name, Type: normalizedType, URL: normalizedURL}, true
}

func (d ServerDeps) writePlatformProxyConflict(w http.ResponseWriter, err error, code, reason string) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.ConstraintName == "platform_proxy_providers_pkey" || pgErr.ConstraintName == "platform_proxy_providers_provider_key_key" {
			adminError(w, http.StatusConflict, "PLATFORM_PROXY_PROVIDER_CONFLICT", "platform-proxy-provider-conflict", nil)
			return
		}
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
