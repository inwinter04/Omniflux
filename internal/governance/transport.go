package governance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	providerUtils "github.com/maximhq/bifrost/core/providers/utils"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

var credentialURLPattern = regexp.MustCompile(`((?:https?|socks5h?)://)([^/\s@]+@)`)

// SanitizeErrorDetail strips embedded URL credentials and caps error detail length.
func SanitizeErrorDetail(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}
	cleaned = credentialURLPattern.ReplaceAllString(cleaned, "${1}***@")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	runes := []rune(cleaned)
	if len(runes) > 300 {
		return string(runes[:297]) + "..."
	}
	return cleaned
}

// TransportForProxy creates an HTTP transport using the specified proxy or explicit direct connection.
func TransportForProxy(proxyType, proxyURL string) (*http.Transport, error) {
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

// HTTPClientForProxy constructs an *http.Client bound to the given proxy configuration.
func HTTPClientForProxy(proxyType, proxyURL string, timeout time.Duration, followRedirects bool) (*http.Client, error) {
	transport, err := TransportForProxy(proxyType, proxyURL)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
	if !followRedirects {
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client, nil
}

// ClassifyTransportError distinguishes proxy transport failures from upstream/direct network failures.
func ClassifyTransportError(err error, usedProxy bool) (reason string, detail string) {
	if err == nil {
		return "", ""
	}
	detail = SanitizeErrorDetail(err.Error())
	if !usedProxy {
		return "network_error", detail
	}
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "proxyconnect") ||
		strings.Contains(lower, "socks") ||
		strings.Contains(lower, "proxy authentication") ||
		strings.Contains(lower, "407") ||
		strings.Contains(lower, "bad gateway") {
		return "proxy_error", detail
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return "proxy_error", detail
	}
	return "network_error", detail
}

// ProviderProbeRequest builds a lightweight zero-token probe URL and headers for a provider account.
func ProviderProbeRequest(provider string, baseURL *string, apiKey string) (string, map[string]string, error) {
	switch provider {
	case "openai", "openai_compatible":
		base := "https://api.openai.com"
		if baseURL != nil && *baseURL != "" {
			base = strings.TrimRight(*baseURL, "/")
		}
		return base + "/v1/models", map[string]string{"Authorization": "Bearer " + apiKey}, nil
	case "anthropic":
		base := "https://api.anthropic.com"
		if baseURL != nil && *baseURL != "" {
			base = strings.TrimRight(*baseURL, "/")
		}
		return base + "/v1/models", map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		}, nil
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/models", map[string]string{"x-goog-api-key": apiKey}, nil
	default:
		if vendor := bifrostx.VendorByID(provider); vendor != nil {
			base := vendor.BaseURL
			if baseURL != nil && *baseURL != "" {
				base = strings.TrimRight(*baseURL, "/")
			}
			return base + vendor.ModelsPath(), map[string]string{"Authorization": "Bearer " + apiKey}, nil
		}
		return "", nil, fmt.Errorf("provider %s does not support online probe", provider)
	}
}

// ProxyProbeOutcome represents the single-shot result of probing a target through a proxy.
type ProxyProbeOutcome struct {
	OK          bool
	Status      string // healthy, degraded, unhealthy
	Reason      string // ok, slow_response, proxy_timeout, proxy_error, proxy_auth_failed, proxy_bad_gateway, probe_http_error, upstream_http_error
	HTTPStatus  int
	LatencyMS   int
	ErrorDetail string
}

// ProbeProxyTarget checks whether the given proxy can successfully reach targetURL.
func ProbeProxyTarget(ctx context.Context, proxyType, proxyURL, targetURL string, isCustomURL bool, timeout time.Duration) ProxyProbeOutcome {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	client, err := HTTPClientForProxy(proxyType, proxyURL, timeout, false)
	if err != nil {
		return ProxyProbeOutcome{
			OK:          false,
			Status:      "unhealthy",
			Reason:      "proxy_error",
			ErrorDetail: SanitizeErrorDetail(err.Error()),
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return ProxyProbeOutcome{
			OK:          false,
			Status:      "unhealthy",
			Reason:      "proxy_error",
			ErrorDetail: SanitizeErrorDetail(err.Error()),
		}
	}
	req.Header.Set("User-Agent", "Omniflux-ProxyProbe/1.0")

	start := time.Now()
	resp, err := client.Do(req)
	latencyMS := int(time.Since(start).Milliseconds())
	if err != nil {
		reason := "proxy_error"
		var netErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || probeCtx.Err() == context.DeadlineExceeded || (errors.As(err, &netErr) && netErr.Timeout()) {
			reason = "proxy_timeout"
		}
		return ProxyProbeOutcome{
			OK:          false,
			Status:      "unhealthy",
			Reason:      reason,
			LatencyMS:   latencyMS,
			ErrorDetail: SanitizeErrorDetail(err.Error()),
		}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	code := resp.StatusCode
	if code == http.StatusProxyAuthRequired {
		return ProxyProbeOutcome{
			OK:          false,
			Status:      "unhealthy",
			Reason:      "proxy_auth_failed",
			HTTPStatus:  code,
			LatencyMS:   latencyMS,
			ErrorDetail: fmt.Sprintf("HTTP %d", code),
		}
	}
	if code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout {
		return ProxyProbeOutcome{
			OK:          false,
			Status:      "unhealthy",
			Reason:      "proxy_bad_gateway",
			HTTPStatus:  code,
			LatencyMS:   latencyMS,
			ErrorDetail: fmt.Sprintf("HTTP %d", code),
		}
	}

	if isCustomURL {
		if code >= 200 && code < 400 {
			status, reason := "healthy", "ok"
			if latencyMS > 5000 {
				status, reason = "degraded", "slow_response"
			}
			return ProxyProbeOutcome{OK: true, Status: status, Reason: reason, HTTPStatus: code, LatencyMS: latencyMS}
		}
		return ProxyProbeOutcome{
			OK:          false,
			Status:      "unhealthy",
			Reason:      "probe_http_error",
			HTTPStatus:  code,
			LatencyMS:   latencyMS,
			ErrorDetail: fmt.Sprintf("HTTP %d", code),
		}
	}

	// 探测厂商根路径时，4xx（407 除外）表示代理链路和 TLS 握手正常，仅业务鉴权拒绝；5xx 视作不可用。
	if code >= 100 && code < 500 {
		status, reason := "healthy", "ok"
		if latencyMS > 5000 {
			status, reason = "degraded", "slow_response"
		}
		return ProxyProbeOutcome{OK: true, Status: status, Reason: reason, HTTPStatus: code, LatencyMS: latencyMS}
	}
	return ProxyProbeOutcome{
		OK:          false,
		Status:      "unhealthy",
		Reason:      "upstream_http_error",
		HTTPStatus:  code,
		LatencyMS:   latencyMS,
		ErrorDetail: fmt.Sprintf("HTTP %d", code),
	}
}
