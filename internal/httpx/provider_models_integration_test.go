package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	if groups[0].(map[string]any)["label"] != "模型厂商" {
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
	if kimiGlobal == nil || kimiGlobal["family_id"] != "kimi" || kimiGlobal["version_label"] != "标准 API · 国际站" {
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
