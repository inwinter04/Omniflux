package httpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

func TestIntegrationPromptWorkbench(t *testing.T) {
	env := setupIntegration(t)
	var requests int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		content := `{"name":"Ada","age":36}`
		if request["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			parts := []string{`{"name":`, `"Ada",`, `"age":36}`}
			for index, part := range parts {
				chunk := map[string]any{
					"id": "chatcmpl-stream", "object": "chat.completion.chunk", "created": 1, "model": request["model"],
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": part}}},
				}
				if index == len(parts)-1 {
					chunk["usage"] = map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
				}
				raw, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", raw)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-mock", "object": "chat.completion", "created": 1, "model": request["model"],
			"choices": []any{map[string]any{
				"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	defer upstream.Close()

	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
		"provider": "openai_compatible", "name": "workbench-upstream", "base_url": upstream.URL, "api_key": "sk-workbench-test-0123456789",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	accountID := assetDataMap(t, body)["id"].(string)
	if err := env.engine.Reload(t.Context(), []bifrostx.ProviderSpec{{
		Provider: "openai_compatible", AccountID: accountID, Name: "workbench-upstream",
		APIKey: "sk-workbench-test-0123456789", BaseURL: upstream.URL,
	}}); err != nil {
		t.Fatal(err)
	}
	serviceIDs := []string{}
	for index, name := range []string{"workbench-a", "workbench-b"} {
		code, body = doJSON(t, admin, http.MethodPost, "/api/v1/model-services", map[string]any{
			"name": name,
			"candidates": []any{map[string]any{
				"provider_account_id": accountID, "upstream_model": fmt.Sprintf("mock-%d", index),
				"priority": 1, "weight": 1, "input_cost_per_million": 10, "output_cost_per_million": 20, "quality_score": 0.8,
			}},
		})
		assertAssetStatus(t, code, body, http.StatusOK)
		serviceIDs = append(serviceIDs, assetDataMap(t, body)["id"].(string))
	}
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "Workbench Prompt", "description": "real model test",
		"spec": map[string]any{
			"template": "Generate {{ .name }}", "model_service_id": serviceIDs[0],
			"variables": []any{map[string]any{"name": "name", "type": "string", "required": true}},
			"output_schema": map[string]any{
				"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}, "age": map[string]any{"type": "integer"}},
				"required": []string{"name", "age"}, "additionalProperties": false,
			},
		},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	assetID := assetDataMap(t, body)["id"].(string)
	versionID := assetDataMap(t, body)["version_id"].(string)

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+assetID+"/run", map[string]any{
		"version_id": versionID, "variables": map[string]any{"name": "Ada"}, "model_service_ids": serviceIDs[:1],
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	run := assetDataMap(t, body)["runs"].([]any)[0].(map[string]any)
	if run["status"] != "success" || run["schema_valid"] != true || run["input_tokens"].(float64) != 10 || run["cost"].(float64) <= 0 {
		t.Fatalf("single run = %#v", run)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+assetID+"/run", map[string]any{
		"version_id": versionID, "variables": map[string]any{"name": "Ada"}, "model_service_ids": serviceIDs,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	if got := len(assetDataMap(t, body)["runs"].([]any)); got != 2 {
		t.Fatalf("compare runs = %d, want 2", got)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+assetID+"/evaluation-sets", map[string]any{
		"name": "Basic quality", "description": "deterministic checks",
		"cases": []any{
			map[string]any{"input": map[string]any{"name": "Ada"}, "match_type": "contains", "expected": `"Ada"`},
			map[string]any{"input": map[string]any{"name": "Ada"}, "match_type": "regex", "expected": `"age":36`},
		},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	setID := assetDataMap(t, body)["id"].(string)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+assetID+"/evaluate", map[string]any{
		"version_id": versionID, "evaluation_set_id": setID, "model_service_ids": serviceIDs[:1],
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	summary := assetDataMap(t, body)["summary"].(map[string]any)
	if summary["matched"].(float64) != 2 || summary["accuracy"].(float64) != 1 {
		t.Fatalf("evaluation summary = %#v", summary)
	}

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/evaluation-sets", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	overviews := body["data"].([]any)
	if len(overviews) != 1 || overviews[0].(map[string]any)["case_count"].(float64) != 2 || overviews[0].(map[string]any)["accuracy"].(float64) != 1 {
		t.Fatalf("evaluation overviews = %#v", overviews)
	}
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/assets/"+assetID+"/evaluation-sets/"+setID, map[string]any{
		"name": "Basic quality updated", "description": "governance edit",
		"cases": []any{map[string]any{"input": map[string]any{"name": "Ada"}, "match_type": "contains", "expected": `"Ada"`}},
	})
	assertAssetStatus(t, code, body, http.StatusOK)

	requestBody := []byte(`{"version_id":"` + versionID + `","variables":{"name":"Ada"},"stream":true}`)
	req, _ := http.NewRequest(http.MethodPost, env.base()+"/api/v1/assets/"+assetID+"/run", bytes.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream status=%d headers=%v", resp.StatusCode, resp.Header)
	}
	var streamText strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		streamText.WriteString(scanner.Text() + "\n")
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(streamText.String(), "event: delta") || !strings.Contains(streamText.String(), `"schema_valid":true`) {
		t.Fatalf("stream response = %s", streamText.String())
	}

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/assets/"+assetID+"/runs", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	if got := len(body["data"].([]any)); got < 6 {
		t.Fatalf("run history count = %d, want at least 6", got)
	}
	if atomic.LoadInt32(&requests) < 6 {
		t.Fatalf("upstream requests = %d, want at least 6", requests)
	}
}
