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
)

func TestIntegrationAgentDataPlane(t *testing.T) {
	env := setupIntegration(t)
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
		content := fmt.Sprintf("api-answer-%d", call)
		if request["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			raw, _ := json.Marshal(map[string]any{
				"id": "chatcmpl-agent-api", "object": "chat.completion.chunk", "created": 1, "model": request["model"],
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}}},
				"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 8, "total_tokens": 28},
			})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-agent-api", "object": "chat.completion", "created": 1, "model": request["model"],
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 8, "total_tokens": 28},
		})
	}))
	defer upstream.Close()

	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")
	departmentID := createAssetTestDepartment(t, admin)
	allowedProjectID := createAssetTestProject(t, admin, departmentID, "Agent Allowed Project")
	deniedProjectID := createAssetTestProject(t, admin, departmentID, "Agent Denied Project")
	allowedKey := createAssetTestKey(t, admin, allowedProjectID, "Agent Allowed App", "Agent Allowed Key")
	deniedKey := createAssetTestKey(t, admin, deniedProjectID, "Agent Denied App", "Agent Denied Key")
	serviceID := createAgentTestService(t, admin, upstream.URL, "agent-api")
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "API Agent Prompt", "spec": map[string]any{
			"template": "Serve {{ .customer }}", "model_service_id": serviceID,
			"variables": []any{map[string]any{"name": "customer", "type": "string", "required": true}},
		},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	promptID := assetDataMap(t, body)["id"].(string)
	promptVersionID := assetDataMap(t, body)["version_id"].(string)
	agentSpec := map[string]any{
		"prompt_template_id": promptID, "prompt_version_id": promptVersionID,
		"model_service_id": serviceID, "temperature": 0.2, "max_history_messages": 20,
	}
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"asset_type": "agent", "name": "客户 Agent", "spec": agentSpec,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	agentID := assetDataMap(t, body)["id"].(string)
	agentVersion1 := assetDataMap(t, body)["version_id"].(string)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+agentID+"/publish", map[string]any{"version_id": agentVersion1})
	assertAssetStatus(t, code, body, http.StatusOK)

	status, _, raw := doAgentAPIRequest(t, allowedKey, "客户 Agent", map[string]any{"message": "denied", "variables": map[string]any{"customer": "Ada"}}, false)
	if status != http.StatusForbidden {
		t.Fatalf("ungranted chat status = %d body = %s", status, raw)
	}
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+agentID+"/grants", map[string]any{
		"subject_type": "project", "subject_id": allowedProjectID, "actions": []string{"read", "invoke"},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	grantID := assetDataMap(t, body)["id"].(string)
	code, rawValue := doDataJSON(t, http.MethodGet, "/v1/agents", nil, allowedKey)
	if code != http.StatusOK || len(rawValue.([]any)) != 1 {
		t.Fatalf("agent list status = %d body = %#v", code, rawValue)
	}
	code, rawValue = doDataJSON(t, http.MethodGet, "/v1/agents", nil, deniedKey)
	if code != http.StatusOK || len(rawValue.([]any)) != 0 {
		t.Fatalf("denied agent list status = %d body = %#v", code, rawValue)
	}
	code, rawValue = doDataJSON(t, http.MethodGet, "/v1/agents/客户 Agent", nil, allowedKey)
	if code != http.StatusOK || rawValue.(map[string]any)["version"].(float64) != 1 {
		t.Fatalf("agent detail status = %d body = %#v", code, rawValue)
	}

	status, header, raw := doAgentAPIRequest(t, allowedKey, "客户 Agent", map[string]any{
		"message": "first api question", "variables": map[string]any{"customer": "Ada"},
	}, false)
	if status != http.StatusOK {
		t.Fatalf("first api chat status = %d body = %s", status, raw)
	}
	sessionID := header.Get("X-Agent-Session-Id")
	if sessionID == "" || header.Get("X-Agent-Version") != "1" || !strings.Contains(string(raw), "api-answer-1") {
		t.Fatalf("first api response headers = %#v body = %s", header, raw)
	}
	<-messageLog

	agentSpec["temperature"] = 0.8
	code, body = doJSON(t, admin, http.MethodPatch, "/api/v1/assets/"+agentID, map[string]any{
		"name": "客户 Agent", "spec": agentSpec, "expected_version": 1,
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	agentVersion2 := assetDataMap(t, body)["version_id"].(string)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets/"+agentID+"/publish", map[string]any{"version_id": agentVersion2})
	assertAssetStatus(t, code, body, http.StatusOK)

	status, header, raw = doAgentAPIRequest(t, allowedKey, "客户 Agent", map[string]any{
		"session_id": sessionID, "message": "second api question",
	}, false)
	if status != http.StatusOK || header.Get("X-Agent-Version") != "1" || !strings.Contains(string(raw), "api-answer-2") {
		t.Fatalf("second api response status = %d headers = %#v body = %s", status, header, raw)
	}
	secondMessages := <-messageLog
	if len(secondMessages) != 4 || secondMessages[2].(map[string]any)["content"] != "api-answer-1" {
		t.Fatalf("second api upstream messages = %#v", secondMessages)
	}

	status, header, raw = doAgentAPIRequest(t, allowedKey, "客户 Agent", map[string]any{
		"session_id": sessionID, "message": "stream api question",
	}, true)
	if status != http.StatusOK || header.Get("X-Agent-Version") != "1" || !strings.Contains(string(raw), "api-answer-3") || !strings.Contains(string(raw), "data: [DONE]") {
		t.Fatalf("stream api response status = %d headers = %#v body = %s", status, header, raw)
	}
	<-messageLog

	status, header, raw = doAgentAPIRequest(t, allowedKey, "客户 Agent", map[string]any{
		"message": "new session", "variables": map[string]any{"customer": "Grace"},
	}, false)
	if status != http.StatusOK || header.Get("X-Agent-Version") != "2" || header.Get("X-Agent-Session-Id") == sessionID {
		t.Fatalf("new session response status = %d headers = %#v body = %s", status, header, raw)
	}
	<-messageLog

	status, _, raw = doAgentAPIRequest(t, deniedKey, "客户 Agent", map[string]any{
		"session_id": sessionID, "message": "cross project access",
	}, false)
	if status != http.StatusForbidden {
		t.Fatalf("cross project chat status = %d body = %s", status, raw)
	}
	var runCount, usageCount, messageCount int
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM agent_runs WHERE asset_id = $1 AND status = 'success'`, uuid.MustParse(agentID)).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM usage_records WHERE request_id IN (SELECT id FROM agent_runs WHERE asset_id = $1) AND state = 'success'`, uuid.MustParse(agentID)).Scan(&usageCount); err != nil {
		t.Fatal(err)
	}
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM agent_messages WHERE session_id = $1`, uuid.MustParse(sessionID)).Scan(&messageCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 4 || usageCount != 4 || messageCount != 6 {
		t.Fatalf("accounting runs = %d usage = %d messages = %d, want 4/4/6", runCount, usageCount, messageCount)
	}
	code, body = doJSON(t, admin, http.MethodDelete, "/api/v1/assets/"+agentID+"/grants/"+grantID, nil)
	assertAssetStatus(t, code, body, http.StatusOK)
	status, _, raw = doAgentAPIRequest(t, allowedKey, "客户 Agent", map[string]any{"session_id": sessionID, "message": "revoked"}, false)
	if status != http.StatusForbidden {
		t.Fatalf("revoked chat status = %d body = %s", status, raw)
	}
}

func doAgentAPIRequest(t *testing.T, key, name string, payload map[string]any, stream bool) (int, http.Header, []byte) {
	t.Helper()
	payload["stream"] = stream
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, itEnv.base()+"/v1/agents/"+name+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, body
}
