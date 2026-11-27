package bifrostx

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func startTestConnectProxy(t *testing.T, counter *atomic.Int32) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen proxy: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			clientConn, err := ln.Accept()
			if err != nil {
				return
			}
			counter.Add(1)
			go func(conn net.Conn) {
				defer conn.Close()
				req, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil {
					return
				}
				if req.Method != http.MethodConnect {
					_, _ = conn.Write([]byte("HTTP/1.1 405 Method Not Allowed\r\nConnection: close\r\n\r\n"))
					return
				}
				targetConn, err := net.Dial("tcp", req.Host)
				if err != nil {
					_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n"))
					return
				}
				defer targetConn.Close()
				_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				done := make(chan struct{}, 2)
				go func() {
					_, _ = io.Copy(targetConn, conn)
					done <- struct{}{}
				}()
				go func() {
					_, _ = io.Copy(conn, targetConn)
					done <- struct{}{}
				}()
				<-done
			}(clientConn)
		}
	}()
	return "http://" + ln.Addr().String()
}

func TestEngineUpdateProviderProxyHotSwap(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1,
			"model":   "gpt-test",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer upstream.Close()

	var proxyAHits, proxyBHits atomic.Int32
	proxyAURL := startTestConnectProxy(t, &proxyAHits)
	proxyBURL := startTestConnectProxy(t, &proxyBHits)

	engine, err := NewEngine(context.Background(), []ProviderSpec{
		{
			Provider:  "openai",
			AccountID: "acc-1",
			Name:      "openai-primary",
			APIKey:    "sk-test",
			BaseURL:   upstream.URL,
			ProxyType: ProxyTypeHTTP,
			ProxyURL:  proxyAURL,
		},
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	engineKey, err := EngineProviderKey("acc-1", "openai", ProtocolOpenAIChat)
	if err != nil {
		t.Fatalf("EngineProviderKey failed: %v", err)
	}
	msg := "ping"
	req := &schemas.BifrostChatRequest{
		Provider: engineKey,
		Model:    "gpt-test",
		Input:    []schemas.ChatMessage{{Role: schemas.ChatMessageRoleUser, Content: &schemas.ChatMessageContent{ContentStr: &msg}}},
	}

	bctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	if _, bErr := engine.Client().ChatCompletionRequest(bctx, req); bErr != nil {
		t.Fatalf("initial request via proxyA failed: %v", bErr)
	}
	if proxyAHits.Load() == 0 || proxyBHits.Load() != 0 {
		t.Fatalf("unexpected proxy hits after initial request: A=%d B=%d", proxyAHits.Load(), proxyBHits.Load())
	}
	prevA := proxyAHits.Load()

	if err := engine.UpdateProviderProxy(schemas.OpenAI, ProxyTypeHTTP, proxyBURL); err != nil {
		t.Fatalf("UpdateProviderProxy failed: %v", err)
	}

	bctx2 := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
	if _, bErr := engine.Client().ChatCompletionRequest(bctx2, req); bErr != nil {
		t.Fatalf("request after hot swap via proxyB failed: %v", bErr)
	}
	if proxyAHits.Load() != prevA || proxyBHits.Load() == 0 {
		t.Fatalf("unexpected proxy hits after hot swap: A=%d (prev=%d) B=%d", proxyAHits.Load(), prevA, proxyBHits.Load())
	}
}

func TestEngineIsolatesAccountsByProtocolAndBaseURL(t *testing.T) {
	var respHits, chatHits, anthropicHits, geminiHits atomic.Int32

	responsesUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer sk-resp" {
			http.Error(w, "unexpected responses request: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		respHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "resp_1",
			"object": "response",
			"status": "completed",
			"model":  "gpt-5",
			"output": []any{map[string]any{
				"id":   "msg_1",
				"type": "message",
				"role": "assistant",
				"content": []any{map[string]any{
					"type": "output_text",
					"text": "from-responses",
				}},
			}},
		})
	}))
	defer responsesUpstream.Close()

	chatUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-chat" {
			http.Error(w, "unexpected chat request: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		chatHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_1",
			"object":  "chat.completion",
			"created": 1,
			"model":   "gpt-4o",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "from-chat-fallback"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer chatUpstream.Close()

	anthropicUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "sk-ant" {
			http.Error(w, "unexpected anthropic request: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		anthropicHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_ant_1",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-sonnet-4-5",
			"stop_reason": "end_turn",
			"content": []any{map[string]any{
				"type": "text",
				"text": "from-anthropic",
			}},
			"usage": map[string]any{
				"input_tokens":  5,
				"output_tokens": 3,
			},
		})
	}))
	defer anthropicUpstream.Close()

	geminiUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-2.5-pro:generateContent" || r.Header.Get("x-goog-api-key") != "sk-gem" {
			http.Error(w, "unexpected gemini request: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		geminiHits.Add(1)
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
				"promptTokenCount":     4,
				"candidatesTokenCount": 2,
				"totalTokenCount":      6,
			},
		})
	}))
	defer geminiUpstream.Close()

	engine, err := NewEngine(context.Background(), []ProviderSpec{
		{
			Provider:    "openai",
			APIProtocol: ProtocolOpenAIResponses,
			AccountID:   "acc-resp",
			Name:        "openai-resp",
			APIKey:      "sk-resp",
			BaseURL:     responsesUpstream.URL,
		},
		{
			Provider:    "openai",
			APIProtocol: ProtocolOpenAIChat,
			AccountID:   "acc-chat",
			Name:        "openai-chat",
			APIKey:      "sk-chat",
			BaseURL:     chatUpstream.URL,
		},
		{
			Provider:    "anthropic",
			APIProtocol: ProtocolAnthropicMessages,
			AccountID:   "acc-ant",
			Name:        "ant-1",
			APIKey:      "sk-ant",
			BaseURL:     anthropicUpstream.URL,
		},
		{
			Provider:    "gemini",
			APIProtocol: ProtocolGeminiNative,
			AccountID:   "acc-gem",
			Name:        "gem-1",
			APIKey:      "sk-gem",
			BaseURL:     geminiUpstream.URL + "/v1beta",
		},
	})
	if err != nil {
		t.Fatalf("NewEngine with isolated multi-protocol accounts failed: %v", err)
	}
	defer engine.Close()

	callResponses := func(accountID, provider, proto, model string) {
		t.Helper()
		key, err := EngineProviderKey(accountID, provider, proto)
		if err != nil {
			t.Fatalf("EngineProviderKey(%s): %v", accountID, err)
		}
		text := "hello"
		req := &schemas.BifrostResponsesRequest{
			Provider: key,
			Model:    model,
			Input: []schemas.ResponsesMessage{{
				Role: &[]schemas.ResponsesMessageRoleType{schemas.ResponsesInputMessageRoleUser}[0],
				Content: &schemas.ResponsesMessageContent{
					ContentStr: &text,
				},
			}},
		}
		bctx := schemas.NewBifrostContext(context.Background(), schemas.NoDeadline)
		resp, bErr := engine.Client().ResponsesRequest(bctx, req)
		if bErr != nil {
			t.Fatalf("ResponsesRequest(%s) failed: %+v", accountID, bErr)
		}
		if len(resp.Output) == 0 {
			t.Fatalf("ResponsesRequest(%s) returned empty output", accountID)
		}
	}

	callResponses("acc-resp", "openai", ProtocolOpenAIResponses, "gpt-5")
	callResponses("acc-chat", "openai", ProtocolOpenAIChat, "gpt-4o")
	callResponses("acc-ant", "anthropic", ProtocolAnthropicMessages, "claude-sonnet-4-5")
	callResponses("acc-gem", "gemini", ProtocolGeminiNative, "gemini-2.5-pro")

	if respHits.Load() != 1 || chatHits.Load() != 1 || anthropicHits.Load() != 1 || geminiHits.Load() != 1 {
		t.Fatalf("unexpected hit counts: resp=%d chat=%d ant=%d gem=%d",
			respHits.Load(), chatHits.Load(), anthropicHits.Load(), geminiHits.Load())
	}
}
