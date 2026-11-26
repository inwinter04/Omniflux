package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

func TestIntegrationRoutePolicyGovernanceAndFX(t *testing.T) {
	env := setupIntegration(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-rp", "object": "chat.completion", "created": 1, "model": req["model"],
			"choices": []any{map[string]any{
				"index": 0, "message": map[string]any{"role": "assistant", "content": "ok-rp"}, "finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 120, "completion_tokens": 40, "total_tokens": 160},
		})
	}))
	defer upstream.Close()

	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")

	// 1) FX rate read, settings update & manual rate override
	code, body := doJSON(t, admin, http.MethodGet, "/api/v1/system/fx-rates", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/system/fx-rates/settings", map[string]any{
		"auto_sync_enabled": true, "sync_interval_hours": 6,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	code, body = doJSON(t, admin, http.MethodPut, "/api/v1/system/fx-rates/CNY", map[string]any{
		"rate_per_usd": 7.25, "manual_override": true,
	})
	assertAssetStatus(t, code, body, http.StatusOK)

	// 2) Create two different currency provider accounts & mixed-currency model service with metadata
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
		"provider": "openai_compatible", "name": "USD 账号", "base_url": upstream.URL,
		"billing_currency": "USD", "api_key": "sk-usd-rp",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	usdAcc := assetDataMap(t, body)["id"].(string)

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
		"provider": "openai_compatible", "name": "CNY 账号", "base_url": upstream.URL,
		"billing_currency": "CNY", "api_key": "sk-cny-rp",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	cnyAcc := assetDataMap(t, body)["id"].(string)

	if err := env.engine.Reload(t.Context(), []bifrostx.ProviderSpec{
		{Provider: "openai_compatible", AccountID: usdAcc, Name: "USD 账号", APIKey: "sk-usd-rp", BaseURL: upstream.URL},
		{Provider: "openai_compatible", AccountID: cnyAcc, Name: "CNY 账号", APIKey: "sk-cny-rp", BaseURL: upstream.URL},
	}); err != nil {
		t.Fatal(err)
	}

	// 3) Create custom route policy with Phase 2 rule, weights, retry policy & circuit breaker
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/route-policies", map[string]any{
		"name":             "混合调度策略",
		"strategy":         "custom",
		"compare_currency": "USD",
		"weights":          map[string]any{"cost": 0.6, "quality": 0.2, "latency": 0.1, "reliability": 0.1},
		"rules": []any{map[string]any{
			"id": "rule-vision", "name": "Vision Tier", "enabled": true, "priority": 1,
			"when":   map[string]any{"capabilities": []string{"vision"}},
			"target": map[string]any{"candidate_tags": []string{"vision-tier"}},
		}},
		"retry_policy": map[string]any{
			"retry_429": true, "retry_5xx": true, "retry_timeout": true,
			"fallback_on_auth_error": true, "fallback_on_not_found": true,
			"fallback_on_rate_limit": true, "fallback_on_server_error": true,
			"fallback_on_ttft_timeout": true, "fallback_on_client_error": false,
			"max_retries_per_candidate": 1,
		},
		"circuit_breaker": map[string]any{
			"enabled": true, "failure_threshold": 3, "cooldown_seconds": 30, "half_open_max_calls": 1,
		},
		"fallback_limit": 2,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	policyID := assetDataMap(t, body)["id"].(string)

	// Updating without resending weights preserves custom weights & increments version
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/route-policies/"+policyID, map[string]any{
		"name": "混合调度策略-已更新",
	})
	assertAssetStatus(t, code, body, http.StatusOK)

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/route-policies", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	var updatedPol map[string]any
	for _, raw := range body["data"].([]any) {
		p := raw.(map[string]any)
		if p["id"] == policyID {
			updatedPol = p
			break
		}
	}
	if updatedPol == nil || int(updatedPol["version"].(float64)) != 2 {
		t.Fatalf("expected version 2 after update, got %#v", updatedPol)
	}
	wMap := updatedPol["weights"].(map[string]any)
	if wMap["cost"].(float64) != 0.6 {
		t.Fatalf("expected custom cost weight 0.6 preserved on update, got %#v", wMap)
	}

	// 4) Create multi-currency model service bound to this policy with candidate metadata
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/model-services", map[string]any{
		"name":            "cross-currency-service",
		"route_policy_id": policyID,
		"candidates": []any{
			map[string]any{
				"provider_account_id": usdAcc, "upstream_model": "model-usd-vision",
				"priority": 1, "weight": 1, "input_cost_per_million": 10, "output_cost_per_million": 20,
				"quality_score": 0.9, "context_window": 128000,
				"capabilities": []string{"vision", "streaming"}, "tags": []string{"vision-tier"},
			},
			map[string]any{
				"provider_account_id": cnyAcc, "upstream_model": "model-cny-cheap",
				"priority": 1, "weight": 1, "input_cost_per_million": 7.25, "output_cost_per_million": 14.5,
				"quality_score": 0.7, "context_window": 64000,
				"capabilities": []string{"streaming"}, "tags": []string{"cheap"},
			},
		},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	serviceID := assetDataMap(t, body)["id"].(string)

	// 5) Disabling bound route policy returns 409 POLICY_IN_USE / policy-in-use-cannot-disable
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/route-policies/"+policyID+"/status", map[string]any{
		"status": "disabled",
	})
	if code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict when disabling bound policy, got %d body=%v", code, body)
	}
	if body["reason"] != "policy-in-use-cannot-disable" {
		t.Fatalf("expected reason policy-in-use-cannot-disable, got %v", body)
	}

	// 6) Clone route policy
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/route-policies/"+policyID+"/clone", map[string]any{
		"name": "混合调度策略-克隆",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	clonedID := assetDataMap(t, body)["id"].(string)
	if clonedID == "" || clonedID == policyID {
		t.Fatalf("expected new cloned policy id, got %q", clonedID)
	}

	// 7) Dry-Run Simulate: matching vision rule narrows to vision-tier candidate
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/route-policies/simulate", map[string]any{
		"model_service_id":      serviceID,
		"policy_id":             policyID,
		"required_capabilities": []string{"vision"},
		"estimated_input_tokens": 1000,
		"estimated_output_tokens": 500,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	simData := assetDataMap(t, body)
	simDecision := simData["decision"].(map[string]any)
	if simDecision["matched_rule_id"] != "rule-vision" {
		t.Fatalf("expected rule-vision to match in simulate, got %#v", simDecision)
	}
	if len(simDecision["chain_candidate_ids"].([]any)) != 1 {
		t.Fatalf("expected 1 filtered candidate from rule, got %#v", simDecision["chain_candidate_ids"])
	}

	// 8) Execute live Playground call & verify overview/metrics/decisions endpoints
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/route-policies/"+policyID+"/playground", map[string]any{
		"model_service_id": serviceID,
		"prompt":           "Hello router test",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	pgData := assetDataMap(t, body)
	if pgData["reply"] != "ok-rp" {
		t.Fatalf("expected playground reply ok-rp, got %#v", pgData)
	}

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/route-policies/"+policyID+"/overview?days=7", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	ov := assetDataMap(t, body)
	if int(ov["summary"].(map[string]any)["requests"].(float64)) < 1 {
		t.Fatalf("expected >= 1 request in overview, got %#v", ov["summary"])
	}

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/route-policies/"+policyID+"/metrics?days=7", nil)
	assertAssetStatus(t, code, body, http.StatusOK)

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/route-policies/"+policyID+"/decisions?page=1&page_size=20", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	decPage := assetDataMap(t, body)
	if int(decPage["total"].(float64)) < 1 {
		t.Fatalf("expected >= 1 policy decision record, got %#v", decPage)
	}
}
