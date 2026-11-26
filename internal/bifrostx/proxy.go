package bifrostx

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

const (
	ProxyTypeNone   = "none"
	ProxyTypeHTTP   = "http"
	ProxyTypeSocks5 = "socks5"
)

// normalizeProxy 统一管理台传入的代理配置,并完成保存前校验。
// 代理 URL 允许包含用户名和密码,但不允许携带 path/query/fragment。
func normalizeProxy(proxyType, proxyURL string) (string, string, error) {
	normalizedType := strings.ToLower(strings.TrimSpace(proxyType))
	if normalizedType == "" {
		normalizedType = ProxyTypeNone
	}
	normalizedURL := strings.TrimSpace(proxyURL)

	switch normalizedType {
	case ProxyTypeNone:
		if normalizedURL != "" {
			return "", "", fmt.Errorf("proxy URL must be empty when proxy type is none")
		}
		return normalizedType, "", nil
	case ProxyTypeHTTP, ProxyTypeSocks5:
	default:
		return "", "", fmt.Errorf("unsupported proxy type %q", proxyType)
	}

	parsed, err := url.Parse(normalizedURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid proxy URL: %w", err)
	}
	if parsed.Host == "" {
		return "", "", fmt.Errorf("proxy URL must contain a host")
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", fmt.Errorf("proxy URL must not contain path, query, or fragment")
	}
	if parsed.Port() == "" {
		return "", "", fmt.Errorf("proxy URL must contain an explicit port")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", "", fmt.Errorf("proxy URL port %q is invalid", parsed.Port())
	}

	expectedSchemes := map[string]bool{"http": true, "https": true}
	if normalizedType == ProxyTypeSocks5 {
		expectedSchemes = map[string]bool{"socks5": true, "socks5h": true}
	}
	if !expectedSchemes[parsed.Scheme] {
		return "", "", fmt.Errorf("proxy URL scheme %q is invalid for proxy type %q", parsed.Scheme, normalizedType)
	}
	return normalizedType, normalizedURL, nil
}

// NormalizeProxy 返回规范化后的代理类型和地址,供管理 API 持久化使用。
func NormalizeProxy(proxyType, proxyURL string) (string, string, error) {
	return normalizeProxy(proxyType, proxyURL)
}

// BuildProxyConfig 把数据库中的代理配置转换为 Bifrost 的 ProviderConfig.ProxyConfig。
func BuildProxyConfig(proxyType, proxyURL string) (*schemas.ProxyConfig, error) {
	normalizedType, normalizedURL, err := normalizeProxy(proxyType, proxyURL)
	if err != nil {
		return nil, err
	}
	if normalizedType == ProxyTypeNone {
		return nil, nil
	}
	return &schemas.ProxyConfig{
		Type: schemas.ProxyType(normalizedType),
		URL:  &schemas.SecretVar{Val: normalizedURL},
	}, nil
}

// ProxySignature 生成用于比较同一 provider key 下代理是否一致的签名。
func ProxySignature(proxyType, proxyURL string) (string, error) {
	normalizedType, normalizedURL, err := normalizeProxy(proxyType, proxyURL)
	if err != nil {
		return "", err
	}
	return normalizedType + "\x00" + normalizedURL, nil
}

func SupportedProviderKeys() []string {
	keys := []string{"openai", "azure", "anthropic", "gemini", "vertex", "bedrock"}
	for _, vendor := range VendorCatalog() {
		keys = append(keys, vendor.ID)
	}
	return keys
}

// IsSupportedProviderKey reports whether a provider key can carry a platform proxy.
func IsSupportedProviderKey(providerKey string) bool {
	switch providerKey {
	case "openai", "azure", "anthropic", "gemini", "vertex", "bedrock":
		return true
	}
	return VendorByID(providerKey) != nil
}

// ProviderDefaultProbeURL returns the default HTTP base URL used to probe proxy reachability to a provider.
func ProviderDefaultProbeURL(providerKey string) string {
	switch providerKey {
	case "openai":
		return "https://api.openai.com"
	case "anthropic":
		return "https://api.anthropic.com"
	case "gemini":
		return "https://generativelanguage.googleapis.com"
	case "vertex":
		return "https://aiplatform.googleapis.com"
	case "bedrock":
		return "https://bedrock-runtime.us-east-1.amazonaws.com"
	case "azure":
		return ""
	default:
		if vendor := VendorByID(providerKey); vendor != nil {
			return vendor.BaseURL
		}
		return ""
	}
}

// ValidateProbeURL normalizes an optional HTTP/HTTPS probe URL and rejects embedded credentials.
func ValidateProbeURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid probe URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("probe URL scheme must be http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("probe URL must contain a host")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("probe URL must not contain credentials")
	}
	return parsed.String(), nil
}
