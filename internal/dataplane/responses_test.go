package dataplane

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"
)

func strPtr(s string) *string { return &s }

func TestExtractResponsesRouteContextCapabilitiesAndMetadata(t *testing.T) {
	userRole := schemas.ResponsesInputMessageRoleUser
	pdfType := "application/pdf"
	pdfName := "architecture.pdf"
	maxOut := 2048
	cacheKey := "codex-cache-1"
	instructions := "Be concise."

	req := &schemas.BifrostResponsesRequest{
		Model: "coding-pro",
		Input: []schemas.ResponsesMessage{
			{
				Role: &userRole,
				Content: &schemas.ResponsesMessageContent{
					ContentBlocks: []schemas.ResponsesMessageContentBlock{
						{
							Type: schemas.ResponsesInputMessageContentBlockTypeText,
							Text: strPtr("Please inspect this screenshot and architecture spec."),
						},
						{
							Type: schemas.ResponsesInputMessageContentBlockTypeImage,
							ResponsesInputMessageContentBlockImage: &schemas.ResponsesInputMessageContentBlockImage{
								ImageURL: strPtr("data:image/png;base64,iVBORw0KGgo="),
							},
						},
						{
							Type: schemas.ResponsesInputMessageContentBlockTypeFile,
							ResponsesInputMessageContentBlockFile: &schemas.ResponsesInputMessageContentBlockFile{
								FileType: &pdfType,
								Filename: &pdfName,
							},
						},
					},
				},
			},
		},
		Params: &schemas.ResponsesParameters{
			Instructions:    &instructions,
			MaxOutputTokens: &maxOut,
			PromptCacheKey:  &cacheKey,
			Reasoning:       &schemas.ResponsesParametersReasoning{},
			Tools:           []schemas.ResponsesTool{{Type: "function"}},
			Text: &schemas.ResponsesTextConfig{
				Format: &schemas.ResponsesTextConfigFormat{Type: "json_schema"},
			},
			Metadata: &map[string]any{
				"tier": "pro",
			},
		},
	}

	headers := http.Header{}
	headers.Set("X-Omniflux-Meta-Env", "staging")
	keyCtx := apiKeyContext{
		KeyID:         uuid.New(),
		ProjectID:     uuid.New(),
		ApplicationID: uuid.New(),
	}

	got := ExtractResponsesRouteContext(req, true, ClientProtocolOpenAIResponses, keyCtx, headers, "sess-1")
	for _, wantCap := range []string{"vision", "pdf_input", "tools", "reasoning", "json_mode", "prompt_caching"} {
		if !slices.Contains(got.RequiredCapabilities, wantCap) {
			t.Fatalf("missing capability %q in %v", wantCap, got.RequiredCapabilities)
		}
	}
	if !got.Stream {
		t.Fatal("expected Stream=true")
	}
	if got.EstimatedOutputTokens != maxOut {
		t.Fatalf("EstimatedOutputTokens = %d, want %d", got.EstimatedOutputTokens, maxOut)
	}
	if got.EstimatedInputTokens < 300 {
		t.Fatalf("EstimatedInputTokens = %d, expected multimodal overhead >= 300", got.EstimatedInputTokens)
	}
	if got.Metadata["tier"] != "pro" || got.Metadata["env"] != "staging" {
		t.Fatalf("unexpected Metadata = %+v", got.Metadata)
	}
	if got.Protocol != ClientProtocolOpenAIResponses || got.SessionKey != "sess-1" {
		t.Fatalf("unexpected protocol/session: %q / %q", got.Protocol, got.SessionKey)
	}
}

func TestCandidateCostForUsageTieredCacheBilling(t *testing.T) {
	readRate := 1.0
	writeRate := 12.5
	cand := candidate{
		InputCostPerM:      10.0,
		OutputCostPerM:     20.0,
		CacheReadCostPerM:  &readRate,
		CacheWriteCostPerM: &writeRate,
	}

	// InputTokens (1000) includes CachedTokens (400) + CacheCreationTokens (200),
	// so uncached input is 400 tokens.
	cost := cand.costForUsage(usageSnapshot{
		InputTokens:         1000,
		CachedTokens:        400,
		CacheCreationTokens: 200,
		OutputTokens:        300,
	})
	want := (400*10.0 + 400*1.0 + 200*12.5 + 300*20.0) / 1_000_000
	if math.Abs(cost-want) > 1e-12 {
		t.Fatalf("tiered cache cost = %v, want %v", cost, want)
	}

	// When cache rates are nil, cache read/write tokens fall back cleanly to InputCostPerM once (no double billing).
	fallbackCand := candidate{InputCostPerM: 10.0, OutputCostPerM: 20.0}
	fallbackCost := fallbackCand.costForUsage(usageSnapshot{
		InputTokens:         1000,
		CachedTokens:        400,
		CacheCreationTokens: 200,
		OutputTokens:        300,
	})
	wantFallback := (1000*10.0 + 300*20.0) / 1_000_000
	if math.Abs(fallbackCost-wantFallback) > 1e-12 {
		t.Fatalf("fallback cache cost = %v, want %v", fallbackCost, wantFallback)
	}

	// Clamp guard if an upstream reports cached tokens exceeding input tokens.
	clampedCost := cand.costForUsage(usageSnapshot{
		InputTokens:         500,
		CachedTokens:        700,
		CacheCreationTokens: 200,
	})
	wantClamped := (500 * 1.0) / 1_000_000
	if math.Abs(clampedCost-wantClamped) > 1e-12 {
		t.Fatalf("clamped cost = %v, want %v", clampedCost, wantClamped)
	}
}

func TestSanitizeOpenAIResponsesPayload(t *testing.T) {
	payload, err := sanitizeOpenAIResponsesPayload(map[string]any{
		"type":         "response.completed",
		"extra_fields": map[string]any{"internal": true},
		"response": map[string]any{
			"id":           "resp_123",
			"model":        "codex-mini",
			"extra_fields": map[string]any{"provider": "hidden"},
		},
	})
	if err != nil {
		t.Fatalf("sanitizeOpenAIResponsesPayload failed: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("unmarshal sanitized payload: %v", err)
	}
	if _, exists := out["extra_fields"]; exists {
		t.Fatal("expected top-level extra_fields to be stripped")
	}
	resp := out["response"].(map[string]any)
	if _, exists := resp["extra_fields"]; exists {
		t.Fatal("expected nested response.extra_fields to be stripped")
	}
	if resp["model"] != "codex-mini" {
		t.Fatalf("response model = %v, want codex-mini", resp["model"])
	}
}

func TestWriteProtocolErrorEnvelopes(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		status    int
		code      string
		reason    string
		checkBody func(t *testing.T, body map[string]any)
	}{
		{
			name:   "openai responses envelope",
			path:   "/v1/responses",
			status: http.StatusTooManyRequests,
			code:   "budget_exceeded",
			reason: "budget_exceeded",
			checkBody: func(t *testing.T, body map[string]any) {
				errObj := body["error"].(map[string]any)
				if errObj["code"] != "budget_exceeded" || errObj["reason"] != "budget_exceeded" {
					t.Fatalf("unexpected openai error: %#v", errObj)
				}
			},
		},
		{
			name:   "anthropic envelope",
			path:   "/v1/messages",
			status: http.StatusTooManyRequests,
			code:   "rate_limit_exceeded",
			reason: "rate_limit_exceeded",
			checkBody: func(t *testing.T, body map[string]any) {
				if body["type"] != "error" {
					t.Fatalf("anthropic top-level type = %v, want error", body["type"])
				}
				errObj := body["error"].(map[string]any)
				if errObj["type"] != "rate_limit_error" || errObj["reason"] != "rate_limit_exceeded" {
					t.Fatalf("unexpected anthropic error: %#v", errObj)
				}
			},
		},
		{
			name:   "gemini envelope",
			path:   "/v1beta/models/gemini-2.5-pro:generateContent",
			status: http.StatusUnauthorized,
			code:   "invalid_api_key",
			reason: "invalid_api_key",
			checkBody: func(t *testing.T, body map[string]any) {
				errObj := body["error"].(map[string]any)
				if errObj["status"] != "UNAUTHENTICATED" || int(errObj["code"].(float64)) != http.StatusUnauthorized {
					t.Fatalf("unexpected gemini error: %#v", errObj)
				}
				if errObj["reason"] != "invalid_api_key" {
					t.Fatalf("gemini reason = %v, want invalid_api_key", errObj["reason"])
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			rec := httptest.NewRecorder()
			writeProtocolError(rec, req, tc.status, tc.code, "test error", apiErrorOptions{
				Reason:    tc.reason,
				RequestID: "req-fixed-123",
			})
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if rec.Header().Get("X-Request-ID") != "req-fixed-123" {
				t.Fatalf("X-Request-ID = %q, want req-fixed-123", rec.Header().Get("X-Request-ID"))
			}
			var decoded map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			tc.checkBody(t, decoded)
		})
	}
}
