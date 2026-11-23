package httpx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIntegrationAgentStreamCancellation(t *testing.T) {
	env := setupIntegration(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		raw, _ := json.Marshal(map[string]any{
			"id": "chatcmpl-agent-slow", "object": "chat.completion.chunk", "created": 1, "model": "agent-mock",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "partial-answer"}}},
		})
		fmt.Fprintf(w, "data: %s\n\n", raw)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")
	serviceID := createAgentTestService(t, admin, upstream.URL, "agent-cancel")
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "Cancel Prompt", "spec": map[string]any{"template": "Answer slowly", "variables": []any{}, "model_service_id": serviceID},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	promptID := assetDataMap(t, body)["id"].(string)
	promptVersionID := assetDataMap(t, body)["version_id"].(string)
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/assets", map[string]any{
		"asset_type": "agent", "name": "Cancel Agent", "spec": map[string]any{
			"prompt_template_id": promptID, "prompt_version_id": promptVersionID,
			"model_service_id": serviceID, "max_history_messages": 20,
		},
	})
	assertAssetStatus(t, code, body, http.StatusOK)
	agentID := assetDataMap(t, body)["id"].(string)
	payload, _ := json.Marshal(map[string]any{"message": "start", "stream": true})
	req, _ := http.NewRequest(http.MethodPost, itEnv.base()+"/api/v1/assets/"+agentID+"/agent/chat", bytes.NewReader(payload))
	reqCtx, cancelRequest := context.WithCancel(t.Context())
	req = req.WithContext(reqCtx)
	req.Header.Set("Content-Type", "application/json")
	resp, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sessionID string
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, `"session_id"`) {
			var event map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
				t.Fatal(err)
			}
			sessionID, _ = event["session_id"].(string)
		}
		if strings.Contains(line, "partial-answer") {
			break
		}
	}
	cancelRequest()
	resp.Body.Close()
	if sessionID == "" {
		t.Fatal("missing session id in stream")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var status, response string
		if err := env.pool.QueryRow(t.Context(), `
			SELECT status, response_text FROM agent_runs WHERE session_id = $1 ORDER BY created_at DESC LIMIT 1
		`, uuid.MustParse(sessionID)).Scan(&status, &response); err == nil && status == "interrupted" {
			if response != "partial-answer" {
				t.Fatalf("interrupted response = %q", response)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent run was not finalized as interrupted")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
