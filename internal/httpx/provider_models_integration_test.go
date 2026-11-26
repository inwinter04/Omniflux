package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestIntegrationProviderModelsAndStatus(t *testing.T) {
	setupIntegration(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer sk-model-discovery-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{
			map[string]any{"id": "model-b", "object": "model"},
			map[string]any{"id": "model-a", "object": "model"},
		}})
	}))
	defer upstream.Close()
	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
		"provider": "openai_compatible", "name": "模型发现测试", "base_url": upstream.URL, "api_key": "sk-model-discovery-test",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	accountID := assetDataMap(t, body)["id"].(string)
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/provider-accounts/"+accountID+"/models", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	models := assetDataMap(t, body)["models"].([]any)
	if len(models) != 2 || models[0].(map[string]any)["id"] != "model-a" || models[1].(map[string]any)["id"] != "model-b" {
		t.Fatalf("discovered models = %#v", models)
	}
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts/"+accountID+"/test", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	testResult := assetDataMap(t, body)
	if testResult["ok"] != true || testResult["reason"] != "connected" || testResult["status_code"].(float64) != http.StatusOK {
		t.Fatalf("provider test result = %#v", testResult)
	}
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/provider-accounts/"+accountID+"/status", map[string]any{"status": "disabled"})
	assertAssetStatus(t, code, body, http.StatusOK)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/model-services", map[string]any{
		"name": "状态治理服务", "candidates": []any{map[string]any{
			"provider_account_id": accountID, "upstream_model": "model-a", "status": "disabled",
			"priority": 1, "weight": 1, "input_cost_per_million": 1, "output_cost_per_million": 2, "quality_score": 0.5,
		}},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	serviceID := assetDataMap(t, body)["id"].(string)
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/model-services", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	service := body["data"].([]any)[0].(map[string]any)
	candidate := service["candidates"].([]any)[0].(map[string]any)
	if service["status"] != "active" || candidate["status"] != "disabled" {
		t.Fatalf("initial statuses = %#v", service)
	}
	if service["health_status"] != "no_candidates" || service["active_candidates"].(float64) != 0 {
		t.Fatalf("initial service health = %#v", service)
	}
	candidateID := candidate["id"].(string)
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/model-candidates/"+candidateID+"/status", map[string]any{"status": "active"})
	assertAssetStatus(t, code, body, http.StatusOK)
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/model-services/"+serviceID+"/status", map[string]any{"status": "disabled"})
	assertAssetStatus(t, code, body, http.StatusOK)
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/provider-catalog", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	groups := body["data"].([]any)
	if groups[0].(map[string]any)["group"] != "cn" {
		t.Fatalf("catalog groups = %#v", groups)
	}
	var kimiGlobal map[string]any
	for _, rawGroup := range groups {
		for _, rawEntry := range rawGroup.(map[string]any)["entries"].([]any) {
			entry := rawEntry.(map[string]any)
			if entry["id"] == "kimi-global" {
				kimiGlobal = entry
			}
		}
	}
	if kimiGlobal == nil || kimiGlobal["family_id"] != "kimi" || kimiGlobal["version_kind"] != "standard" || kimiGlobal["region_kind"] != "global" {
		t.Fatalf("kimi global presentation = %#v", kimiGlobal)
	}
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/route-policies", map[string]any{"name": "状态策略"})
	assertAssetStatus(t, code, body, http.StatusOK)
	policyID := assetDataMap(t, body)["id"].(string)
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/route-policies/"+policyID+"/status", map[string]any{"status": "disabled"})
	assertAssetStatus(t, code, body, http.StatusOK)
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/projects", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	projectID := body["data"].([]any)[0].(map[string]any)["id"].(string)
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/projects/"+projectID+"/status", map[string]any{"status": "disabled"})
	assertAssetStatus(t, code, body, http.StatusOK)
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/applications", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	applicationID := body["data"].([]any)[0].(map[string]any)["id"].(string)
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/applications/"+applicationID+"/status", map[string]any{"status": "disabled"})
	assertAssetStatus(t, code, body, http.StatusOK)
}

func TestIntegrationModelServiceCandidateAssociation(t *testing.T) {
	setupIntegration(t)
	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
		"provider": "openai_compatible", "name": "候选关联测试", "base_url": "http://127.0.0.1:1", "api_key": "sk-association-test",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	accountID := assetDataMap(t, body)["id"].(string)
	for index := 0; index < 24; index++ {
		code, body = doJSON(t, admin, http.MethodPost, "/api/v1/model-services", map[string]any{
			"name": fmt.Sprintf("服务-%02d", index),
			"candidates": []any{map[string]any{
				"provider_account_id": accountID, "upstream_model": fmt.Sprintf("model-%02d", index),
				"priority": 1, "weight": 1, "quality_score": 0.5,
			}},
		})
		assertAssetStatus(t, code, body, http.StatusOK)
	}
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/model-services", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	services := body["data"].([]any)
	if len(services) != 24 {
		t.Fatalf("service count = %d", len(services))
	}
	for _, rawService := range services {
		service := rawService.(map[string]any)
		candidates := service["candidates"].([]any)
		if len(candidates) != 1 {
			t.Fatalf("service %s candidates = %#v", service["name"], candidates)
		}
		expected := "model-" + service["name"].(string)[len("服务-"):]
		if candidates[0].(map[string]any)["upstream_model"] != expected {
			t.Fatalf("service %s candidate = %#v, want %s", service["name"], candidates[0], expected)
		}
	}
}

func TestIntegrationProviderProxy(t *testing.T) {
	setupIntegration(t)
	var proxyRequests int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&proxyRequests, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{
			map[string]any{"id": "proxy-model", "object": "model"},
		}})
	}))
	defer proxy.Close()

	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
		"provider": "llama-cpp", "name": "代理测试", "api_key": "sk-proxy-test",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	accountID := assetDataMap(t, body)["id"].(string)

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/proxies", map[string]any{
		"provider_keys": []string{"llama-cpp", "openai"}, "name": "shared-proxy", "proxy_type": "http", "proxy_url": proxy.URL,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	proxyID := assetDataMap(t, body)["id"].(string)
	providerKeys, _ := assetDataMap(t, body)["provider_keys"].([]any)
	if len(providerKeys) != 2 {
		t.Fatalf("shared proxy provider keys = %#v", providerKeys)
	}

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/provider-accounts/"+accountID+"/models", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	models := assetDataMap(t, body)["models"].([]any)
	if len(models) != 1 || models[0].(map[string]any)["id"] != "proxy-model" {
		t.Fatalf("proxied discovered models = %#v", models)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts/"+accountID+"/test", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	testResult := assetDataMap(t, body)
	if testResult["ok"] != true || testResult["reason"] != "connected" {
		t.Fatalf("proxied provider test result = %#v", testResult)
	}
	if atomic.LoadInt32(&proxyRequests) < 2 {
		t.Fatalf("proxy requests = %d, want at least 2", proxyRequests)
	}

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/provider-accounts", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	var account map[string]any
	for _, raw := range body["data"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == accountID {
			account = item
			break
		}
	}
	if account == nil || account["proxy_type"] != "http" {
		t.Fatalf("provider account proxy type = %#v", account)
	}

	code, body = doJSON(t, admin, http.MethodDelete, "/api/v1/proxies/"+proxyID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/provider-accounts", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	for _, raw := range body["data"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == accountID && item["proxy_type"] != "none" {
			t.Fatalf("provider account after deleting proxy = %#v", item)
		}
	}
}

func TestIntegrationProviderProxyGovernance(t *testing.T) {
	setupIntegration(t)
	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")

	code, body := doJSON(t, admin, http.MethodGet, "/api/v1/proxies", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	result := assetDataMap(t, body)
	proxies := result["proxies"].([]any)
	options := result["provider_options"].([]any)
	if len(proxies) != 0 {
		t.Fatalf("initial platform proxies = %#v", proxies)
	}
	if len(options) == 0 {
		t.Fatal("provider options are empty")
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/proxies", map[string]any{
		"provider_keys": []string{"openai", "anthropic"}, "name": "shared-openai-proxy", "proxy_type": "http", "proxy_url": "http://127.0.0.1:3128",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	proxyID := assetDataMap(t, body)["id"].(string)

	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/proxies/"+proxyID, map[string]any{
		"provider_keys": []string{"openai"}, "name": "shared-openai-proxy", "proxy_type": "socks5", "proxy_url": "socks5://127.0.0.1:1080",
	})
	assertAssetStatus(t, code, body, http.StatusOK)

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/proxies", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	proxies = assetDataMap(t, body)["proxies"].([]any)
	if len(proxies) != 1 {
		t.Fatalf("platform proxies = %#v", proxies)
	}
	updated := proxies[0].(map[string]any)
	updatedProviderKeys, _ := updated["provider_keys"].([]any)
	if updated["proxy_type"] != "socks5" || updated["proxy_url"] != "socks5://127.0.0.1:1080" || len(updatedProviderKeys) != 1 {
		t.Fatalf("updated platform proxy = %#v", updated)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/proxies", map[string]any{
		"provider_keys": []string{"openai"}, "name": "duplicate-provider", "proxy_type": "http", "proxy_url": "http://127.0.0.1:3129",
	})
	assertAssetStatus(t, code, body, http.StatusConflict)
	if body["reason"] != "platform-proxy-provider-conflict" {
		t.Fatalf("provider conflict = %#v", body)
	}

	code, body = doJSON(t, admin, http.MethodDelete, "/api/v1/proxies/"+proxyID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
}
