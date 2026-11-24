package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

func TestIntegrationMultiCurrencyUsageReporting(t *testing.T) {
	env := setupIntegration(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-currency", "object": "chat.completion", "created": 1, "model": request["model"],
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	defer upstream.Close()
	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")
	departmentID := createAssetTestDepartment(t, admin)
	projectID := createAssetTestProject(t, admin, departmentID, "Currency Project")
	apiKey := createAssetTestKey(t, admin, projectID, "Currency App", "Currency Key")
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
		"provider": "openai_compatible", "name": "人民币账号", "base_url": upstream.URL,
		"billing_currency": "CNY", "api_key": "sk-currency-test",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	accountID := assetDataMap(t, body)["id"].(string)
	if err := env.engine.Reload(t.Context(), []bifrostx.ProviderSpec{{
		Provider: "openai_compatible", AccountID: accountID, Name: "人民币账号", APIKey: "sk-currency-test", BaseURL: upstream.URL,
	}}); err != nil {
		t.Fatal(err)
	}
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/model-services", map[string]any{
		"name": "cny-model", "candidates": []any{map[string]any{
			"provider_account_id": accountID, "upstream_model": "cny-upstream", "priority": 1, "weight": 1,
			"input_cost_per_million": 7, "output_cost_per_million": 14, "quality_score": 0.5,
		}},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	cnyServiceID := assetDataMap(t, body)["id"].(string)
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/organizations", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	organizationID := body["data"].([]any)[0].(map[string]any)["id"].(string)
	// 组织级预算的 scope_id 可省略或传任意值,服务端一律使用当前组织 ID,不能借它把预算挂到别的范围上。
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/budgets", map[string]any{
		"scope_type": "organization", "scope_id": "00000000-0000-0000-0000-000000000000",
		"window_start": time.Now().Add(-time.Hour).Format(time.RFC3339), "window_end": time.Now().Add(time.Hour).Format(time.RFC3339),
		"limit_amount": "100", "currency": "CNY",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	budgetID := assetDataMap(t, body)["id"].(string)
	var budgetScopeID string
	if err := env.pool.QueryRow(t.Context(), `SELECT scope_id FROM budget_windows WHERE id = $1`, uuid.MustParse(budgetID)).Scan(&budgetScopeID); err != nil {
		t.Fatal(err)
	}
	if budgetScopeID != organizationID {
		t.Fatalf("organization budget scope = %s, want %s", budgetScopeID, organizationID)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "Currency Prompt", "spec": map[string]any{"template": "Say ok", "variables": []any{}, "model_service_id": cnyServiceID},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	promptID := assetDataMap(t, body)["id"].(string)
	promptVersionID := assetDataMap(t, body)["version_id"].(string)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+promptID+"/run", map[string]any{
		"version_id": promptVersionID, "variables": map[string]any{}, "model_service_ids": []string{cnyServiceID},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"asset_type": "agent", "name": "Currency Agent", "spec": map[string]any{
			"prompt_template_id": promptID, "prompt_version_id": promptVersionID,
			"model_service_id": cnyServiceID, "max_history_messages": 20,
		},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	agentID := assetDataMap(t, body)["id"].(string)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+agentID+"/agent/chat", map[string]any{"message": "debug"})
	assertAssetStatus(t, code, body, http.StatusOK)
	code, raw := doDataJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model": "cny-model", "messages": []any{map[string]any{"role": "user", "content": "api"}},
	}, apiKey)
	if code != http.StatusOK {
		t.Fatalf("cny api call = %d %#v", code, raw)
	}
	var currency string
	if err := env.pool.QueryRow(t.Context(), `SELECT cost_currency FROM usage_records WHERE model_service_id = $1 ORDER BY created_at DESC LIMIT 1`, uuid.MustParse(cnyServiceID)).Scan(&currency); err != nil {
		t.Fatal(err)
	}
	if currency != "CNY" {
		t.Fatalf("usage currency = %s", currency)
	}

	_ = createAgentTestService(t, admin, upstream.URL, "usd-model")
	code, raw = doDataJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model": "usd-model", "messages": []any{map[string]any{"role": "user", "content": "api"}},
	}, apiKey)
	if code != http.StatusOK {
		t.Fatalf("usd api call = %d %#v", code, raw)
	}
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/usage/summary?days=7", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	summary := assetDataMap(t, body)
	costCurrencies := map[string]bool{}
	for _, rawCost := range summary["costs"].([]any) {
		costCurrencies[rawCost.(map[string]any)["currency"].(string)] = true
	}
	if !costCurrencies["CNY"] || !costCurrencies["USD"] || summary["cost_currency"] != "MIXED" {
		t.Fatalf("currency summary = %#v", summary)
	}
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/usage/records?page=1&page_size=100", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	records := assetDataMap(t, body)["records"].([]any)
	sources := map[string]bool{}
	for _, rawRecord := range records {
		record := rawRecord.(map[string]any)
		sources[record["source"].(string)] = true
	}
	if !sources["api"] || !sources["prompt_debug"] || !sources["agent_debug"] {
		t.Fatalf("record sources = %#v", sources)
	}
	var spent float64
	if err := env.pool.QueryRow(t.Context(), `SELECT spent_amount FROM budget_windows WHERE id = $1`, uuid.MustParse(budgetID)).Scan(&spent); err != nil {
		t.Fatal(err)
	}
	if spent <= 0 || spent >= 0.001 {
		t.Fatalf("cny budget spent = %f, should include only cny costs", spent)
	}
}
