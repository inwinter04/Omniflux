package governance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSanitizeErrorDetailRedactsCredentials(t *testing.T) {
	raw := `Get "https://api.openai.com": proxyconnect tcp: dial tcp 1.2.3.4:8080: http://alice:s3cr3tPass@proxy.example.com:8080 failed`
	got := SanitizeErrorDetail(raw)
	if strings.Contains(got, "alice") || strings.Contains(got, "s3cr3tPass") {
		t.Fatalf("credentials leaked in detail: %s", got)
	}
	if !strings.Contains(got, "http://***@proxy.example.com:8080") {
		t.Fatalf("expected redacted proxy url in detail, got: %s", got)
	}
}

func TestClassifyTransportError(t *testing.T) {
	reason, _ := ClassifyTransportError(errors.New(`proxyconnect tcp: dial tcp 127.0.0.1:9999: connectex: No connection`), true)
	if reason != "proxy_error" {
		t.Fatalf("proxyconnect reason = %s, want proxy_error", reason)
	}
	reason, _ = ClassifyTransportError(context.DeadlineExceeded, true)
	if reason != "proxy_error" {
		t.Fatalf("proxy timeout reason = %s, want proxy_error", reason)
	}
	reason, _ = ClassifyTransportError(errors.New("dial tcp: connection refused"), false)
	if reason != "network_error" {
		t.Fatalf("direct error reason = %s, want network_error", reason)
	}
}

func TestProbeProxyTargetStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/unauth":
			w.WriteHeader(http.StatusUnauthorized)
		case "/authreq":
			w.WriteHeader(http.StatusProxyAuthRequired)
		case "/badgw":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	// Provider default URL accepts 401 (upstream reachable through proxy).
	out := ProbeProxyTarget(context.Background(), "none", "", srv.URL+"/unauth", false, 3*time.Second)
	if !out.OK || out.Status != "healthy" || out.Reason != "ok" {
		t.Fatalf("provider 401 probe = %+v", out)
	}

	// Custom URL requires 2xx/3xx.
	out = ProbeProxyTarget(context.Background(), "none", "", srv.URL+"/unauth", true, 3*time.Second)
	if out.OK || out.Reason != "probe_http_error" {
		t.Fatalf("custom 401 probe = %+v", out)
	}

	// 407 is proxy auth failure.
	out = ProbeProxyTarget(context.Background(), "none", "", srv.URL+"/authreq", false, 3*time.Second)
	if out.OK || out.Reason != "proxy_auth_failed" {
		t.Fatalf("407 probe = %+v", out)
	}

	// 502 is proxy bad gateway.
	out = ProbeProxyTarget(context.Background(), "none", "", srv.URL+"/badgw", false, 3*time.Second)
	if out.OK || out.Reason != "proxy_bad_gateway" {
		t.Fatalf("502 probe = %+v", out)
	}

	// Unreachable proxy port returns proxy_error.
	out = ProbeProxyTarget(context.Background(), "http", "http://127.0.0.1:1", "http://example.com", false, time.Second)
	if out.OK || out.Reason != "proxy_error" {
		t.Fatalf("unreachable proxy probe = %+v", out)
	}
}
