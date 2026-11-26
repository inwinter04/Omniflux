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

	msg := "ping"
	req := &schemas.BifrostChatRequest{
		Provider: schemas.OpenAI,
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
