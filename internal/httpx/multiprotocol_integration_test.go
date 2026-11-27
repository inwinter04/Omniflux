package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestMultiProtocolGateway(t *testing.T) {
	env := setupIntegration(t)
	admin := sessionClient(t, "it-admin@test.local", "it-admin-password-123456")

	var (
		respMu          sync.Mutex
		respSawImage    bool
		respHitsA       atomic.Int32
		respHitsB       atomic.Int32
		respCompactHits atomic.Int32
		chatHits        atomic.Int32
		antMu           sync.Mutex
		antSawImage     bool
		antHits         atomic.Int32
		antCountHits    atomic.Int32
		gemMu           sync.Mutex
		gemSawImage     bool
		gemHits         atomic.Int32
		gemStreamHits   atomic.Int32
		gemCountHits    atomic.Int32
	)

	newResponsesMock := func(hitCounter *atomic.Int32, modelName string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			switch r.URL.Path {
			case "/v1/responses/compact":
				respCompactHits.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":         "cmp_1",
					"object":     "response.compaction",
					"created_at": 1700000000,
					"output": []any{
						map[string]any{"id": "cmp_item_1", "type": "compaction", "encrypted_content": "enc-summary"},
					},
					"usage": map[string]any{"input_tokens": 50, "output_tokens": 10, "total_tokens": 60},
				})
			case "/v1/responses":
				hitCounter.Add(1)
				if strings.Contains(string(raw), "input_image") || strings.Contains(string(raw), "image_url") {
					respMu.Lock()
					respSawImage = true
					respMu.Unlock()
				}
				var reqBody map[string]any
				_ = json.Unmarshal(raw, &reqBody)
				if stream, _ := reqBody["stream"].(bool); stream {
					w.Header().Set("Content-Type", "text/event-stream")
					flusher, _ := w.(http.Flusher)
					events := []struct {
						name string
						data string
					}{
						{"response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_s1","object":"response","status":"in_progress","model":"` + modelName + `"}}`},
						{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"msg_s1","type":"message","role":"assistant","status":"in_progress","content":[]}}`},
						{"response.content_part.added", `{"type":"response.content_part.added","sequence_number":2,"item_id":"msg_s1","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`},
						{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":3,"item_id":"msg_s1","output_index":0,"content_index":0,"delta":"Hello stream"}`},
						{"response.completed", `{"type":"response.completed","sequence_number":4,"response":{"id":"resp_s1","object":"response","status":"completed","model":"` + modelName + `","output":[{"id":"msg_s1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello stream"}]}],"usage":{"input_tokens":20,"output_tokens":5,"total_tokens":25}}}`},
					}
					for _, ev := range events {
						_, _ = io.WriteString(w, "event: "+ev.name+"\ndata: "+ev.data+"\n\n")
						if flusher != nil {
							flusher.Flush()
						}
					}
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":     "resp_nonstream_1",
					"object": "response",
					"status": "completed",
					"model":  modelName,
					"output": []any{map[string]any{
						"id":   "msg_1",
						"type": "message",
						"role": "assistant",
						"content": []any{map[string]any{
							"type": "output_text",
							"text": "from-" + modelName,
						}},
					}},
					"usage": map[string]any{
						"input_tokens":  100,
						"output_tokens": 25,
						"total_tokens":  125,
						"input_tokens_details": map[string]any{
							"cached_tokens": 40,
						},
					},
				})
			default:
				http.Error(w, "unexpected responses path: "+r.URL.Path, http.StatusNotFound)
			}
		}))
	}

	respMockA := newResponsesMock(&respHitsA, "gpt-5-a")
	defer respMockA.Close()
	respMockB := newResponsesMock(&respHitsB, "gpt-5-b")
	defer respMockB.Close()

	chatMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected chat path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		chatHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_fallback_1",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   "gpt-4o-chat",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "bridged-from-chat"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 4, "total_tokens": 16},
		})
	}))
	defer chatMock.Close()

	antMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1/messages/count_tokens":
			antCountHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"input_tokens": 42})
		case "/v1/messages":
			antHits.Add(1)
			if strings.Contains(string(raw), `"type":"image"`) {
				antMu.Lock()
				antSawImage = true
				antMu.Unlock()
			}
			var reqBody map[string]any
			_ = json.Unmarshal(raw, &reqBody)
			if stream, _ := reqBody["stream"].(bool); stream {
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, _ := w.(http.Flusher)
				frames := []string{
					"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ant_s1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-5\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n",
					"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
					"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Claude stream ok\"}}\n\n",
					"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
					"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":6}}\n\n",
					"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
				}
				for _, frame := range frames {
					_, _ = io.WriteString(w, frame)
					if flusher != nil {
						flusher.Flush()
					}
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "msg_ant_1",
				"type":        "message",
				"role":        "assistant",
				"model":       "claude-sonnet-4-5",
				"stop_reason": "end_turn",
				"content":     []any{map[string]any{"type": "text", "text": "from-anthropic"}},
				"usage": map[string]any{
					"input_tokens":                500,
					"cache_read_input_tokens":     300,
					"cache_creation_input_tokens": 200,
					"output_tokens":               100,
				},
			})
		default:
			http.Error(w, "unexpected anthropic path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer antMock.Close()

	gemMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, ":countTokens"):
			gemCountHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalTokens":         12,
				"promptTokensDetails": []any{map[string]any{"modality": "TEXT", "tokenCount": 12}},
			})
		case strings.HasSuffix(r.URL.Path, ":streamGenerateContent"):
			gemStreamHits.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			_, _ = io.WriteString(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Gemini "}]}}]}`+"\n\n")
			_, _ = io.WriteString(w, `data: {"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"text":"stream"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":4,"totalTokenCount":12}}`+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		case strings.HasSuffix(r.URL.Path, ":generateContent"):
			gemHits.Add(1)
			if strings.Contains(string(raw), `"inlineData"`) {
				gemMu.Lock()
				gemSawImage = true
				gemMu.Unlock()
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []any{map[string]any{
					"finishReason": "STOP",
					"content": map[string]any{
						"role":  "model",
						"parts": []any{map[string]any{"text": "from-gemini"}},
					},
				}},
				"usageMetadata": map[string]any{
					"promptTokenCount":        20,
					"cachedContentTokenCount": 5,
					"candidatesTokenCount":    6,
					"totalTokenCount":         26,
				},
			})
		default:
			http.Error(w, "unexpected gemini path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer gemMock.Close()

	// Create Project, Application & API Key.
	_, body := doJSON(t, admin, http.MethodGet, "/api/v1/departments", nil)
	deptID := body["data"].([]any)[0].(map[string]any)["id"].(string)
	_, body = doJSON(t, admin, http.MethodPost, "/api/v1/projects", map[string]string{"name": "vc-proj", "department_id": deptID})
	projectID := body["data"].(map[string]any)["id"].(string)
	_, body = doJSON(t, admin, http.MethodPost, "/api/v1/applications", map[string]string{"name": "vc-app", "project_id": projectID})
	applicationID := body["data"].(map[string]any)["id"].(string)
	code, body := doJSON(t, admin, http.MethodPost, "/api/v1/api-keys", map[string]any{
		"name": "vc-key", "project_id": projectID, "application_id": applicationID, "rate_limit_per_min": 500,
	})
	if code != http.StatusOK {
		t.Fatalf("create api key status = %d body = %v", code, body)
	}
	plainKey := body["data"].(map[string]any)["key"].(string)

	var (
		accRespA string
		accRespB string
		accChat  string
		accAnt   string
		accGem   string
	)

	t.Run("admin provider catalog and multi-account protocol isolation", func(t *testing.T) {
		code, body := doJSON(t, admin, http.MethodGet, "/api/v1/provider-catalog", nil)
		if code != http.StatusOK {
			t.Fatalf("provider-catalog status = %d", code)
		}
		var foundOfficialOpenAI, foundOfficialAnt, foundOfficialGem bool
		for _, grpRaw := range body["data"].([]any) {
			grp := grpRaw.(map[string]any)
			for _, entRaw := range grp["entries"].([]any) {
				ent := entRaw.(map[string]any)
				switch ent["id"] {
				case "openai":
					foundOfficialOpenAI = ent["default_protocol"] == "openai_responses"
				case "anthropic":
					foundOfficialAnt = ent["default_protocol"] == "anthropic_messages"
				case "gemini":
					foundOfficialGem = ent["default_protocol"] == "gemini_native"
				}
			}
		}
		if !foundOfficialOpenAI || !foundOfficialAnt || !foundOfficialGem {
			t.Fatalf("official catalog defaults missing: openai=%v ant=%v gem=%v",
				foundOfficialOpenAI, foundOfficialAnt, foundOfficialGem)
		}

		// Invalid protocol for provider returns 400 provider-protocol-unsupported.
		badCode, badBody := doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
			"provider": "openai", "api_protocol": "anthropic_messages", "name": "invalid-proto", "api_key": "sk-bad",
		})
		if badCode != http.StatusBadRequest || badBody["reason"] != "provider-protocol-unsupported" {
			t.Fatalf("bad protocol status=%d body=%v", badCode, badBody)
		}

		createAccount := func(provider, proto, name, baseURL string) string {
			c, b := doJSON(t, admin, http.MethodPost, "/api/v1/provider-accounts", map[string]any{
				"provider": provider, "api_protocol": proto, "name": name, "base_url": baseURL, "api_key": "sk-test-key",
			})
			if c != http.StatusOK {
				t.Fatalf("create account %s status = %d body = %v", name, c, b)
			}
			return b["data"].(map[string]any)["id"].(string)
		}

		// Two openai_compatible accounts with distinct base_urls & protocols (zero conflict).
		accRespA = createAccount("openai_compatible", "openai_responses", "oai-resp-a", respMockA.URL)
		accRespB = createAccount("openai_compatible", "openai_responses", "oai-resp-b", respMockB.URL)
		accChat = createAccount("openai_compatible", "openai_chat", "oai-chat-only", chatMock.URL)
		accAnt = createAccount("anthropic", "anthropic_messages", "claude-native", antMock.URL)
		accGem = createAccount("gemini", "gemini_native", "gemini-native", gemMock.URL+"/v1beta")
	})

	// Create sticky policy + model services.
	code, body = doJSON(t, admin, http.MethodPost, "/api/v1/route-policies", map[string]any{
		"name":     "sticky-rr-policy",
		"strategy": "round_robin",
		"session_affinity": map[string]any{
			"enabled":      true,
			"ttl_seconds":  600,
			"key_strategy": "auto",
		},
	})
	if code != http.StatusOK {
		t.Fatalf("create sticky policy status = %d body = %v", code, body)
	}
	stickyPolicyID := body["data"].(map[string]any)["id"].(string)

	// Verify GET /api/v1/route-policies persists session_affinity.
	code, body = doJSON(t, admin, http.MethodGet, "/api/v1/route-policies", nil)
	if code != http.StatusOK {
		t.Fatalf("list route policies status = %d", code)
	}
	var gotAffinity bool
	for _, pRaw := range body["data"].([]any) {
		p := pRaw.(map[string]any)
		if p["id"] == stickyPolicyID {
			if aff, ok := p["session_affinity"].(map[string]any); ok {
				gotAffinity = aff["enabled"] == true && int(aff["ttl_seconds"].(float64)) == 600
			}
		}
	}
	if !gotAffinity {
		t.Fatal("expected GET /api/v1/route-policies to return saved session_affinity")
	}

	createService := func(name string, policyID any, candidates []any) {
		c, b := doJSON(t, admin, http.MethodPost, "/api/v1/model-services", map[string]any{
			"name":            name,
			"expose_router":   true,
			"router_model_id": name,
			"route_policy_id": policyID,
			"candidates":      candidates,
		})
		if c != http.StatusOK {
			t.Fatalf("create service %s status = %d body = %v", name, c, b)
		}
	}

	createService("codex-resp-svc", nil, []any{map[string]any{
		"provider_account_id": accRespA, "upstream_model": "gpt-5-a",
		"priority": 1, "weight": 1, "input_cost_per_million": 10.0, "output_cost_per_million": 20.0,
		"cache_read_cost_per_million": 1.0, "quality_score": 0.9,
		"capabilities": []string{"vision", "tools", "reasoning", "responses_api", "prompt_caching"},
	}})
	createService("codex-chat-svc", nil, []any{map[string]any{
		"provider_account_id": accChat, "upstream_model": "gpt-4o-chat",
		"priority": 1, "weight": 1, "input_cost_per_million": 5.0, "output_cost_per_million": 10.0, "quality_score": 0.7,
	}})
	createService("claude-svc", nil, []any{map[string]any{
		"provider_account_id": accAnt, "upstream_model": "claude-sonnet-4-5",
		"priority": 1, "weight": 1, "input_cost_per_million": 10.0, "output_cost_per_million": 20.0,
		"cache_read_cost_per_million": 1.0, "cache_write_cost_per_million": 12.5, "quality_score": 0.9,
		"capabilities": []string{"vision", "tools", "prompt_caching", "pdf_input"},
	}})
	createService("gemini-svc", nil, []any{map[string]any{
		"provider_account_id": accGem, "upstream_model": "gemini-2.5-pro",
		"priority": 1, "weight": 1, "input_cost_per_million": 2.0, "output_cost_per_million": 6.0, "quality_score": 0.85,
		"capabilities": []string{"vision", "pdf_input", "streaming"},
	}})
	createService("sticky-svc", stickyPolicyID, []any{
		map[string]any{
			"provider_account_id": accRespA, "upstream_model": "gpt-5-a",
			"priority": 1, "weight": 1, "input_cost_per_million": 10.0, "output_cost_per_million": 20.0, "quality_score": 0.8,
		},
		map[string]any{
			"provider_account_id": accRespB, "upstream_model": "gpt-5-b",
			"priority": 1, "weight": 1, "input_cost_per_million": 10.0, "output_cost_per_million": 20.0, "quality_score": 0.8,
		},
	})

	doRaw := func(method, path string, headers map[string]string, payload any) (int, http.Header, []byte) {
		var bodyReader io.Reader
		if payload != nil {
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			bodyReader = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, env.base()+path, bodyReader)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if payload != nil && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s failed: %v", method, path, err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, out
	}

	t.Run("codex responses multimodal stream compact and chat-bridge fallback", func(t *testing.T) {
		c, _, out := doRaw(http.MethodPost, "/v1/responses", map[string]string{"Authorization": "Bearer " + plainKey}, map[string]any{
			"model": "codex-resp-svc",
			"input": []any{map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "describe image"},
					map[string]any{"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgo="},
				},
			}},
		})
		if c != http.StatusOK {
			t.Fatalf("POST /v1/responses status = %d body = %s", c, out)
		}
		if strings.Contains(string(out), `"extra_fields"`) {
			t.Fatalf("responses payload leaked extra_fields: %s", out)
		}
		respMu.Lock()
		sawImg := respSawImage
		respMu.Unlock()
		if !sawImg {
			t.Fatal("expected respMock to receive input_image content")
		}

		// Streaming SSE.
		c, hdr, out := doRaw(http.MethodPost, "/v1/responses", map[string]string{"Authorization": "Bearer " + plainKey}, map[string]any{
			"model":  "codex-resp-svc",
			"stream": true,
			"input":  "stream test",
		})
		if c != http.StatusOK || !strings.Contains(hdr.Get("Content-Type"), "text/event-stream") || !strings.Contains(string(out), "Hello stream") {
			t.Fatalf("stream /v1/responses status=%d ct=%q body=%s", c, hdr.Get("Content-Type"), out)
		}

		// Auto-bridge Responses -> openai_chat account.
		c, _, out = doRaw(http.MethodPost, "/v1/responses", map[string]string{"Authorization": "Bearer " + plainKey}, map[string]any{
			"model": "codex-chat-svc",
			"input": "bridge hello",
		})
		if c != http.StatusOK || !strings.Contains(string(out), "bridged-from-chat") || chatHits.Load() == 0 {
			t.Fatalf("bridged /v1/responses status=%d chatHits=%d body=%s", c, chatHits.Load(), out)
		}

		// Compact endpoint.
		c, _, out = doRaw(http.MethodPost, "/v1/responses/compact", map[string]string{"Authorization": "Bearer " + plainKey}, map[string]any{
			"model": "codex-resp-svc",
			"input": "compact me",
		})
		if c != http.StatusOK || respCompactHits.Load() == 0 || !strings.Contains(string(out), "enc-summary") {
			t.Fatalf("compact status=%d hits=%d body=%s", c, respCompactHits.Load(), out)
		}
	})

	t.Run("claude code messages multimodal tiered billing stream and count_tokens", func(t *testing.T) {
		c, _, out := doRaw(http.MethodPost, "/v1/messages", map[string]string{"x-api-key": plainKey}, map[string]any{
			"model":      "claude-svc",
			"max_tokens": 256,
			"messages": []any{map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "inspect image"},
					map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": "image/png",
							"data":       "iVBORw0KGgo=",
						},
					},
				},
			}},
		})
		if c != http.StatusOK || !strings.Contains(string(out), "from-anthropic") {
			t.Fatalf("POST /v1/messages status=%d body=%s", c, out)
		}
		antMu.Lock()
		sawImg := antSawImage
		antMu.Unlock()
		if !sawImg {
			t.Fatal("expected anthropic mock to receive image block")
		}

		// Streaming SSE.
		c, hdr, out := doRaw(http.MethodPost, "/v1/messages", map[string]string{"x-api-key": plainKey}, map[string]any{
			"model":      "claude-svc",
			"max_tokens": 128,
			"stream":     true,
			"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		})
		if c != http.StatusOK || !strings.Contains(hdr.Get("Content-Type"), "text/event-stream") || !strings.Contains(string(out), "Claude stream ok") {
			t.Fatalf("stream /v1/messages status=%d ct=%q body=%s", c, hdr.Get("Content-Type"), out)
		}

		// Cross-protocol: Anthropic client -> OpenAI Responses backend.
		c, _, out = doRaw(http.MethodPost, "/v1/messages", map[string]string{"x-api-key": plainKey}, map[string]any{
			"model":      "codex-resp-svc",
			"max_tokens": 64,
			"messages":   []any{map[string]any{"role": "user", "content": "cross proto"}},
		})
		if c != http.StatusOK || !strings.Contains(string(out), "from-gpt-5-a") {
			t.Fatalf("cross-protocol /v1/messages status=%d body=%s", c, out)
		}

		// count_tokens.
		c, _, out = doRaw(http.MethodPost, "/v1/messages/count_tokens", map[string]string{"x-api-key": plainKey}, map[string]any{
			"model":    "claude-svc",
			"messages": []any{map[string]any{"role": "user", "content": "count me"}},
		})
		if c != http.StatusOK || antCountHits.Load() == 0 || !strings.Contains(string(out), `"input_tokens":42`) {
			t.Fatalf("count_tokens status=%d hits=%d body=%s", c, antCountHits.Load(), out)
		}
	})

	t.Run("gemini cli list generateContent stream and query key auth", func(t *testing.T) {
		c, _, out := doRaw(http.MethodGet, "/v1beta/models", map[string]string{"x-goog-api-key": plainKey}, nil)
		if c != http.StatusOK || !strings.Contains(string(out), `"models/gemini-svc"`) {
			t.Fatalf("GET /v1beta/models status=%d body=%s", c, out)
		}

		// Query-param ?key= auth + inlineData image.
		c, _, out = doRaw(http.MethodPost, "/v1beta/models/gemini-svc:generateContent?key="+plainKey, nil, map[string]any{
			"contents": []any{map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "read screenshot"},
					map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "iVBORw0KGgo="}},
				},
			}},
		})
		if c != http.StatusOK || !strings.Contains(string(out), "from-gemini") {
			t.Fatalf("POST :generateContent status=%d body=%s", c, out)
		}
		gemMu.Lock()
		sawImg := gemSawImage
		gemMu.Unlock()
		if !sawImg {
			t.Fatal("expected gemini mock to receive inlineData image")
		}

		// SSE streaming.
		c, hdr, out := doRaw(http.MethodPost, "/v1beta/models/gemini-svc:streamGenerateContent?alt=sse&key="+plainKey, nil, map[string]any{
			"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "stream"}}}},
		})
		if c != http.StatusOK || !strings.Contains(hdr.Get("Content-Type"), "text/event-stream") || !strings.Contains(string(out), "Gemini ") {
			t.Fatalf("stream :streamGenerateContent status=%d ct=%q body=%s", c, hdr.Get("Content-Type"), out)
		}

		// countTokens.
		c, _, out = doRaw(http.MethodPost, "/v1beta/models/gemini-svc:countTokens", map[string]string{"x-goog-api-key": plainKey}, map[string]any{
			"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "count tokens"}}}},
		})
		if c != http.StatusOK || gemCountHits.Load() == 0 || !strings.Contains(string(out), `"totalTokens":12`) {
			t.Fatalf(":countTokens status=%d hits=%d body=%s", c, gemCountHits.Load(), out)
		}
	})

	t.Run("protocol-native error envelopes", func(t *testing.T) {
		c, hdr, out := doRaw(http.MethodPost, "/v1/messages", map[string]string{"x-api-key": plainKey}, map[string]any{
			"messages": []any{map[string]any{"role": "user", "content": "missing model"}},
		})
		if c != http.StatusBadRequest || hdr.Get("X-Request-ID") == "" || !strings.Contains(string(out), `"type":"error"`) || !strings.Contains(string(out), `"reason":"missing_model"`) {
			t.Fatalf("anthropic error envelope status=%d rid=%q body=%s", c, hdr.Get("X-Request-ID"), out)
		}

		c, hdr, out = doRaw(http.MethodPost, "/v1beta/models/nonexistent-svc:generateContent", nil, map[string]any{
			"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "unauth"}}}},
		})
		if c != http.StatusUnauthorized || hdr.Get("X-Request-ID") == "" || !strings.Contains(string(out), `"UNAUTHENTICATED"`) {
			t.Fatalf("gemini error envelope status=%d rid=%q body=%s", c, hdr.Get("X-Request-ID"), out)
		}
	})

	t.Run("sticky session affinity pins session to single candidate", func(t *testing.T) {
		respHitsA.Store(0)
		respHitsB.Store(0)
		for i := 0; i < 4; i++ {
			c, _, out := doRaw(http.MethodPost, "/v1/responses", map[string]string{
				"Authorization": "Bearer " + plainKey,
				"Session-Id":    "sticky-codex-session-1",
			}, map[string]any{
				"model": "sticky-svc",
				"input": "turn message",
			})
			if c != http.StatusOK {
				t.Fatalf("sticky call %d failed status=%d body=%s", i, c, out)
			}
		}
		hitsA, hitsB := respHitsA.Load(), respHitsB.Load()
		if !((hitsA == 4 && hitsB == 0) || (hitsB == 4 && hitsA == 0)) {
			t.Fatalf("expected all 4 requests pinned to one candidate under round_robin+affinity, got A=%d B=%d", hitsA, hitsB)
		}
	})

	t.Run("usage persistence protocol filter and tiered cache cost", func(t *testing.T) {
		var cost float64
		var inputTokens, cachedTokens, cacheCreationTokens int64
		err := env.pool.QueryRow(context.Background(), `
			SELECT input_tokens, cached_tokens, cache_creation_tokens, cost::float8
			FROM usage_records
			WHERE protocol = 'anthropic_messages' AND state = 'success' AND cache_creation_tokens > 0
			ORDER BY created_at DESC LIMIT 1
		`).Scan(&inputTokens, &cachedTokens, &cacheCreationTokens, &cost)
		if err != nil {
			t.Fatalf("query anthropic usage_record: %v", err)
		}
		// 500 uncached*10 + 300 cached*1 + 200 cache_write*12.5 + 100 out*20 = (5000 + 300 + 2500 + 2000) / 1e6 = 0.0098
		if inputTokens != 1000 || cachedTokens != 300 || cacheCreationTokens != 200 || math.Abs(cost-0.0098) > 1e-9 {
			t.Fatalf("unexpected usage input=%d cached=%d cacheCreation=%d cost=%v, want 1000/300/200/0.0098",
				inputTokens, cachedTokens, cacheCreationTokens, cost)
		}

		code, summaryBody := doJSON(t, admin, http.MethodGet, "/api/v1/usage/summary?days=7", nil)
		if code != http.StatusOK {
			t.Fatalf("GET /api/v1/usage/summary status=%d", code)
		}
		sumData := summaryBody["data"].(map[string]any)
		if int64(sumData["total_cache_creation_tokens"].(float64)) != 200 {
			t.Fatalf("total_cache_creation_tokens = %v, want 200", sumData["total_cache_creation_tokens"])
		}
		byProto := sumData["by_protocol"].([]any)
		if len(byProto) < 3 {
			t.Fatalf("expected >= 3 protocols in by_protocol, got %d (%v)", len(byProto), byProto)
		}

		code, recBody := doJSON(t, admin, http.MethodGet, "/api/v1/usage/records?protocol=gemini_native", nil)
		if code != http.StatusOK {
			t.Fatalf("GET /api/v1/usage/records?protocol=gemini_native status=%d", code)
		}
		records := recBody["data"].(map[string]any)["records"].([]any)
		if len(records) < 2 {
			t.Fatalf("expected >= 2 gemini_native usage records, got %d", len(records))
		}
	})

	_ = uuid.Nil
}
