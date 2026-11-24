package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

func TestIntegrationAgentDebugRuntime(t *testing.T) {
	setupIntegration(t)
	var calls int32
	messageLog := make(chan []any, 10)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		messages, _ := request["messages"].([]any)
		messageLog <- messages
		content := fmt.Sprintf("answer-%d", call)
		if request["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, part := range []string{"stream-", content} {
				raw, _ := json.Marshal(map[string]any{
					"id": "chatcmpl-agent-stream", "object": "chat.completion.chunk", "created": 1, "model": request["model"],
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": part}}},
					"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 6, "total_tokens": 18},
				})
				fmt.Fprintf(w, "data: %s\n\n", raw)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-agent", "object": "chat.completion", "created": 1, "model": request["model"],
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 6, "total_tokens": 18},
		})
	}))
	defer upstream.Close()

	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")
	serviceID := createAgentTestService(t, admin, upstream.URL, "agent-debug")
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "Agent Prompt", "spec": map[string]any{
			"template": "You are helping {{ .name }}", "model_service_id": serviceID,
			"variables": []any{map[string]any{"name": "name", "type": "string", "required": true}},
		},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	promptID := assetDataMap(t, body)["id"].(string)
	promptVersionID := assetDataMap(t, body)["version_id"].(string)
	agentSpec := map[string]any{
		"prompt_template_id": promptID, "prompt_version_id": promptVersionID,
		"model_service_id": serviceID, "temperature": 0.4, "max_tokens": 1000, "max_history_messages": 20,
	}
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"asset_type": "agent", "name": "Broken Agent", "spec": map[string]any{
			"prompt_template_id": promptID, "prompt_version_id": uuid.NewString(), "model_service_id": serviceID, "max_history_messages": 20,
		},
	})
	assertAssetStatus(t, code, body, http.StatusBadRequest)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"asset_type": "agent", "name": "Debug Agent", "description": "multi-turn test", "spec": agentSpec,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	agentID := assetDataMap(t, body)["id"].(string)

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+agentID+"/agent/chat", map[string]any{
		"message": "first question", "variables": map[string]any{"name": "Ada"},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	firstRun := assetDataMap(t, body)
	sessionID := firstRun["session_id"].(string)
	if firstRun["response"] != "answer-1" || firstRun["status"] != "success" || firstRun["input_tokens"].(float64) != 12 {
		t.Fatalf("first run = %#v", firstRun)
	}
	firstMessages := <-messageLog
	if len(firstMessages) != 2 || firstMessages[0].(map[string]any)["content"] != "You are helping Ada" {
		t.Fatalf("first upstream messages = %#v", firstMessages)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+agentID+"/agent/chat", map[string]any{
		"session_id": sessionID, "message": "second question",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	if assetDataMap(t, body)["response"] != "answer-2" {
		t.Fatalf("second run = %#v", body)
	}
	secondMessages := <-messageLog
	if len(secondMessages) != 4 || secondMessages[2].(map[string]any)["content"] != "answer-1" || secondMessages[3].(map[string]any)["content"] != "second question" {
		t.Fatalf("second upstream messages = %#v", secondMessages)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+agentID+"/agent/chat", map[string]any{
		"session_id": sessionID, "message": "changed vars", "variables": map[string]any{"name": "Grace"},
	})
	assertAssetStatus(t, code, body, http.StatusBadRequest)
	// 管理面错误只返回机器字段:code 为大写错误码,reason 为 kebab-case 标识,英文说明只放在 detail。
	if body["code"] != "AGENT_SESSION_VARIABLES_IMMUTABLE" || body["reason"] != "agent-session-variables-immutable" || body["detail"] != "Existing sessions cannot change initial variables. Create a new session." {
		t.Fatalf("input error = %#v", body)
	}

	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+agentID+"/agent/chat", map[string]any{
		"session_id": uuid.NewString(), "message": "missing session",
	})
	assertAssetStatus(t, code, body, http.StatusNotFound)
	if body["code"] != "AGENT_SESSION_NOT_FOUND" || body["reason"] != "agent-session-not-found" {
		t.Fatalf("missing session error = %#v", body)
	}

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/assets/"+agentID+"/agent/sessions/"+sessionID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	detail := assetDataMap(t, body)
	if len(detail["messages"].([]any)) != 4 || detail["session"].(map[string]any)["title"] != "first question" {
		t.Fatalf("session detail = %#v", detail)
	}

	payload, _ := json.Marshal(map[string]any{"session_id": sessionID, "message": "stream question", "stream": true})
	req, _ := http.NewRequest(http.MethodPost, itEnv.base()+"/api/v1/assets/"+agentID+"/agent/chat", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rawStream, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	streamText := string(rawStream)
	if resp.StatusCode != http.StatusOK || !strings.Contains(streamText, "event: delta") || !strings.Contains(streamText, "stream-answer-3") || !strings.Contains(streamText, `"status":"success"`) {
		t.Fatalf("stream status = %d body = %s", resp.StatusCode, streamText)
	}
	<-messageLog

	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/assets/"+agentID+"/agent/sessions/"+sessionID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	if got := len(assetDataMap(t, body)["messages"].([]any)); got != 6 {
		t.Fatalf("messages after stream = %d, want 6", got)
	}
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/assets/"+agentID+"/agent/runs", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	runs := body["data"].([]any)
	if len(runs) != 3 || runs[0].(map[string]any)["cost"].(float64) <= 0 {
		t.Fatalf("agent runs = %#v", runs)
	}
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/assets?type=agent", nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	if len(body["data"].([]any)) != 1 {
		t.Fatalf("filtered agents = %#v", body["data"])
	}
}

func createAgentTestService(t *testing.T, admin *http.Client, upstreamURL, name string) string {
	t.Helper()
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
		"provider": "openai_compatible", "name": name + "-upstream", "base_url": upstreamURL, "api_key": "sk-agent-test-0123456789",
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	accountID := assetDataMap(t, body)["id"].(string)
	if err := itEnv.engine.Reload(t.Context(), []bifrostx.ProviderSpec{{
		Provider: "openai_compatible", AccountID: accountID, Name: name + "-upstream",
		APIKey: "sk-agent-test-0123456789", BaseURL: upstreamURL,
	}}); err != nil {
		t.Fatal(err)
	}
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/model-services", map[string]any{
		"name": name,
		"candidates": []any{map[string]any{
			"provider_account_id": accountID, "upstream_model": "agent-mock", "priority": 1, "weight": 1,
			"input_cost_per_million": 10, "output_cost_per_million": 20, "quality_score": 0.8,
		}},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	return assetDataMap(t, body)["id"].(string)
}
