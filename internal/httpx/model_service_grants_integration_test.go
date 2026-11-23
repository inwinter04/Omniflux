package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIntegrationModelServiceGrants(t *testing.T) {
	setupIntegration(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-grant", "object": "chat.completion", "created": 1, "model": "agent-mock",
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "allowed"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer upstream.Close()
	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")
	departmentID := createAssetTestDepartment(t, admin)
	allowedProjectID := createAssetTestProject(t, admin, departmentID, "Model Allowed Project")
	deniedProjectID := createAssetTestProject(t, admin, departmentID, "Model Denied Project")
	allowedKey := createAssetTestKey(t, admin, allowedProjectID, "Model Allowed App", "Model Allowed Key")
	deniedKey := createAssetTestKey(t, admin, deniedProjectID, "Model Denied App", "Model Denied Key")
	serviceID := createAgentTestService(t, admin, upstream.URL, "governed-model")

	code, body := doJSON(t, admin, http.MethodGet, "/api/v1/model-services/"+serviceID+"/grants", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	grants := body["data"].([]any)
	if len(grants) != 1 || grants[0].(map[string]any)["subject_type"] != "organization" {
		t.Fatalf("default grants = %#v", grants)
	}
	organizationGrantID := grants[0].(map[string]any)["id"].(string)
	code, body = doJSON(t, admin, http.MethodDelete, "/api/v1/model-services/"+serviceID+"/grants/"+organizationGrantID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	for _, key := range []string{allowedKey, deniedKey} {
		code, raw := doDataJSON(t, http.MethodGet, "/v1/models", nil, key)
		if code != http.StatusOK || len(raw.(map[string]any)["data"].([]any)) != 0 {
			t.Fatalf("models without grants status = %d body = %#v", code, raw)
		}
		code, raw = doDataJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model": "governed-model", "messages": []any{map[string]any{"role": "user", "content": "deny"}},
		}, key)
		if code != http.StatusForbidden {
			t.Fatalf("chat without grants status = %d body = %#v, want 403", code, raw)
		}
	}
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/model-services/"+serviceID+"/grants", map[string]any{
		"subject_type": "project", "subject_id": allowedProjectID, "actions": []string{"read", "invoke"},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	projectGrantID := assetDataMap(t, body)["id"].(string)
	code, raw := doDataJSON(t, http.MethodGet, "/v1/models", nil, allowedKey)
	if code != http.StatusOK || len(raw.(map[string]any)["data"].([]any)) != 1 {
		t.Fatalf("allowed models status = %d body = %#v", code, raw)
	}
	code, raw = doDataJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model": "governed-model", "messages": []any{map[string]any{"role": "user", "content": "allow"}},
	}, allowedKey)
	if code != http.StatusOK {
		t.Fatalf("allowed chat status = %d body = %#v", code, raw)
	}
	code, raw = doDataJSON(t, http.MethodGet, "/v1/models", nil, deniedKey)
	if code != http.StatusOK || len(raw.(map[string]any)["data"].([]any)) != 0 {
		t.Fatalf("denied models status = %d body = %#v", code, raw)
	}
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/model-services/"+serviceID+"/grants", map[string]any{
		"subject_type": "project", "subject_id": allowedProjectID, "actions": []string{"read"},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	if assetDataMap(t, body)["id"] != projectGrantID {
		t.Fatalf("grant upsert changed id: %#v", body)
	}
	code, raw = doDataJSON(t, http.MethodGet, "/v1/models", nil, allowedKey)
	if code != http.StatusOK || len(raw.(map[string]any)["data"].([]any)) != 1 {
		t.Fatalf("read-only models status = %d body = %#v", code, raw)
	}
	code, raw = doDataJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model": "governed-model", "messages": []any{map[string]any{"role": "user", "content": "deny read only"}},
	}, allowedKey)
	if code != http.StatusForbidden {
		t.Fatalf("read-only chat status = %d body = %#v, want 403", code, raw)
	}
}
