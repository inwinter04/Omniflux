package dataplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/inwinter04/omniflux/internal/bifrostx"
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

func TestBuildAuthorizedModelListDeduplicatesConcreteAndServices(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	earlier := now.Add(-time.Hour)
	rows := []authorizedModelRow{
		// Ordinary grouping service (ExposeRouter=false) must NOT leak its management name.
		{ServiceName: "智谱 GLM Coding Plan 服务", ExposeRouter: false, ServiceCreated: now, Provider: "zhipu-glm-coding", UpstreamModel: "glm-5.4"},
		{ServiceName: "智谱 GLM Coding Plan 服务", ExposeRouter: false, ServiceCreated: now, Provider: "zhipu-glm-coding", UpstreamModel: "glm-5-turbo"},
		// Explicitly exposed smart router must appear as omniflux-router.
		{ServiceName: "智能编排路由", ExposeRouter: true, RouterModelID: "omniflux/auto", ServiceCreated: earlier, Provider: "openai", UpstreamModel: "glm-5.4"},
		// Router ID colliding with an active concrete upstream_model must not duplicate.
		{ServiceName: "同名覆盖组", ExposeRouter: true, RouterModelID: "glm-5.4", ServiceCreated: now, Provider: "openai", UpstreamModel: "glm-5.4"},
	}

	got := buildAuthorizedModelList(rows)
	if len(got) != 3 {
		t.Fatalf("buildAuthorizedModelList len = %d, want 3 (%+v)", len(got), got)
	}
	if got[0].ID != "glm-5-turbo" || got[0].OwnedBy != "zhipu-glm-coding" || got[0].IsRouter {
		t.Fatalf("unexpected item[0]: %+v", got[0])
	}
	if got[1].ID != "glm-5.4" || got[1].OwnedBy != "organization" || got[1].Created != earlier.Unix() || got[1].IsRouter {
		t.Fatalf("unexpected item[1]: %+v", got[1])
	}
	routers := map[string]authorizedModelItem{}
	for _, item := range got {
		if item.IsRouter {
			routers[item.ID] = item
		}
	}
	if len(routers) != 1 || routers["omniflux/auto"].OwnedBy != "omniflux-router" {
		t.Fatalf("expected only deduplicated router omniflux/auto, got %+v", routers)
	}
}

func TestValidateRouterModelID(t *testing.T) {
	for _, valid := range []string{"omniflux/auto", "auto", "coding-v2.1"} {
		got, err := ValidateRouterModelID("  " + valid + "  ")
		if err != nil || got != valid {
			t.Fatalf("ValidateRouterModelID(%q) = (%q, %v), want (%q, nil)", valid, got, err, valid)
		}
	}
	for _, invalid := range []string{"", "智谱 GLM Coding Plan 服务", "has space", "/leading-slash", "trailing/", "a..b"} {
		if _, err := ValidateRouterModelID(invalid); err == nil {
			t.Fatalf("ValidateRouterModelID(%q) succeeded, expected error", invalid)
		}
	}
}

func TestAutoOrchestratorComplexityAndCaching(t *testing.T) {
	eco := candidate{
		ID: idFor("eco"), Provider: "zhipu-glm-coding", AccountID: "acc-1",
		UpstreamModel: "glm-4.5-air", Priority: 1, Weight: 1,
		InputCostPerM: 0.5, OutputCostPerM: 1.5, Quality: 0.45,
		ContextWindow: intPtr(128000), Capabilities: []string{"streaming", "tools"},
	}
	mid := candidate{
		ID: idFor("mid"), Provider: "zhipu-glm-coding", AccountID: "acc-1",
		UpstreamModel: "glm-5.3", Priority: 1, Weight: 1,
		InputCostPerM: 2.0, OutputCostPerM: 6.0, Quality: 0.72,
		ContextWindow: intPtr(128000), Capabilities: []string{"streaming", "tools"},
	}
	flagship := candidate{
		ID: idFor("flagship"), Provider: "zhipu-glm-coding", AccountID: "acc-1",
		UpstreamModel: "glm-5.4", Priority: 1, Weight: 1,
		InputCostPerM: 8.0, OutputCostPerM: 24.0, Quality: 0.95,
		ContextWindow: intPtr(200000), Capabilities: []string{"streaming", "tools", "vision", "reasoning"},
	}
	reason := candidate{
		ID: idFor("reason"), Provider: "openai", AccountID: "acc-2",
		UpstreamModel: "o3-reasoner", Priority: 1, Weight: 1,
		InputCostPerM: 15.0, OutputCostPerM: 60.0, Quality: 0.99,
		ContextWindow: intPtr(200000), Capabilities: []string{"streaming", "tools", "reasoning"},
	}
	allCands := []candidate{eco, mid, flagship, reason}

	pol := defaultPolicy
	pol.Strategy = StrategyAutoOrchestrator
	pol.FallbackLimit = 0
	pol.SessionAffinity = SessionAffinityPolicy{Enabled: true, TTLSeconds: 300, KeyStrategy: "auto"}
	pol.Orchestrator = DefaultOrchestratorConfig()

	// 1. Short QA translation routes to simple tier & cheap model.
	simpleDec := EvaluateRoute(allCands, pol, RouteContext{
		EstimatedInputTokens: 80,
		LatestUserText:       "你好，把这段翻译成英文",
	}, noHealthScores(), nil, 0)
	if simpleDec.Orchestrator == nil || simpleDec.Orchestrator.Tier != OrchestratorTierSimple {
		t.Fatalf("expected simple tier, got %+v", simpleDec.Orchestrator)
	}
	if len(simpleDec.Chain) == 0 || simpleDec.Chain[0].UpstreamModel != "glm-4.5-air" {
		t.Fatalf("expected cheap model glm-4.5-air first, got %+v", simpleDec.Chain)
	}

	// 2. Complex coding + stack trace + 12k tokens routes to complex/reasoning and never downgrades to glm-4.5-air.
	complexDec := EvaluateRoute(allCands, pol, RouteContext{
		EstimatedInputTokens: 12000,
		LatestUserText:       "请做系统架构设计与根因分析，排查并发死锁 panic:\n```go\nfunc main() {}\n```\nstack trace: .go:42",
		ToolCount:            3,
		RequiredCapabilities: []string{"tools"},
	}, noHealthScores(), nil, 0)
	if complexDec.Orchestrator == nil ||
		(complexDec.Orchestrator.Tier != OrchestratorTierComplex && complexDec.Orchestrator.Tier != OrchestratorTierReasoning) {
		t.Fatalf("expected complex or reasoning tier, got %+v", complexDec.Orchestrator)
	}
	if len(complexDec.Chain) == 0 || (complexDec.Chain[0].UpstreamModel != "glm-5.4" && complexDec.Chain[0].UpstreamModel != "o3-reasoner") {
		t.Fatalf("expected frontier model first, got %+v", complexDec.Chain)
	}
	for _, c := range complexDec.Chain {
		if c.UpstreamModel == "glm-4.5-air" || c.UpstreamModel == "glm-5.3" {
			t.Fatalf("upgrade-only fallback chain must not downgrade to %s: %+v", c.UpstreamModel, complexDec.Chain)
		}
	}

	// 3. Explicit reasoning.effort = high routes to reasoning tier.
	highEffortDec := EvaluateRoute(allCands, pol, RouteContext{
		EstimatedInputTokens: 400,
		LatestUserText:       "prove correctness",
		ReasoningEffort:      "high",
	}, noHealthScores(), nil, 0)
	if highEffortDec.Orchestrator == nil || highEffortDec.Orchestrator.Tier != OrchestratorTierReasoning {
		t.Fatalf("expected reasoning tier for reasoning_effort=high, got %+v", highEffortDec.Orchestrator)
	}
	if len(highEffortDec.Chain) == 0 || highEffortDec.Chain[0].UpstreamModel != "o3-reasoner" {
		t.Fatalf("expected o3-reasoner for reasoning tier, got %+v", highEffortDec.Chain)
	}

	// 4. Adaptive session mode: short QA follow-up (< 8000 tokens, no tool loop) does NOT stay stuck on flagship.
	qaFollowUpDec := EvaluateRoute(allCands, pol, RouteContext{
		EstimatedInputTokens: 90,
		LatestUserText:       "你好，把这段翻译成英文",
		PreferredCandidateID: flagship.ID,
	}, noHealthScores(), nil, 0)
	if qaFollowUpDec.Orchestrator == nil || qaFollowUpDec.Orchestrator.CacheProtected {
		t.Fatalf("short QA turn should not lock warm cache, got %+v", qaFollowUpDec.Orchestrator)
	}
	if len(qaFollowUpDec.Chain) == 0 || qaFollowUpDec.Chain[0].UpstreamModel != "glm-4.5-air" {
		t.Fatalf("expected short QA follow-up to step down to glm-4.5-air, got %+v", qaFollowUpDec.Chain)
	}

	// 5. Adaptive session mode: long coding follow-up (>= 8000 tokens or ToolTurnCount >= 2) protects warm cache on flagship.
	codingFollowUpDec := EvaluateRoute(allCands, pol, RouteContext{
		EstimatedInputTokens: 15000,
		ToolTurnCount:        2,
		LatestUserText:       "你好，继续",
		PreferredCandidateID: flagship.ID,
	}, noHealthScores(), nil, 0)
	if codingFollowUpDec.Orchestrator == nil || !codingFollowUpDec.Orchestrator.CacheProtected || !codingFollowUpDec.AffinityHit {
		t.Fatalf("long coding follow-up must protect warm cache, got orch=%+v affinityHit=%v", codingFollowUpDec.Orchestrator, codingFollowUpDec.AffinityHit)
	}
	if len(codingFollowUpDec.Chain) == 0 || codingFollowUpDec.Chain[0].ID != flagship.ID {
		t.Fatalf("expected cache-protected flagship candidate first, got %+v", codingFollowUpDec.Chain)
	}

	// 6. Adaptive session mode: sticky economy candidate escalates when complex/vision request arrives.
	escalateDec := EvaluateRoute(allCands, pol, RouteContext{
		EstimatedInputTokens: 12000,
		LatestUserText:       "请基于截图重构并发架构并排查根因 ```go\nfunc fix() {}\n```",
		RequiredCapabilities: []string{"vision"},
		PreferredCandidateID: eco.ID,
	}, noHealthScores(), nil, 0)
	if escalateDec.Orchestrator == nil || escalateDec.Orchestrator.CacheProtected || escalateDec.Orchestrator.UpgradedFromTier != "" && escalateDec.Orchestrator.UpgradedFromTier != OrchestratorTierSimple {
		t.Fatalf("unexpected escalation decision: %+v", escalateDec.Orchestrator)
	}
	if len(escalateDec.Chain) == 0 || escalateDec.Chain[0].UpstreamModel != "glm-5.4" {
		t.Fatalf("expected escalation to vision-capable flagship glm-5.4, got %+v", escalateDec.Chain)
	}

	// 7. Hard capability gating: vision request never includes text-only candidates.
	for _, c := range escalateDec.Chain {
		if c.UpstreamModel == "glm-4.5-air" || c.UpstreamModel == "glm-5.3" || c.UpstreamModel == "o3-reasoner" {
			t.Fatalf("vision request must filter out non-vision model %s", c.UpstreamModel)
		}
	}
}

func TestClientQuotaResponseBuilders(t *testing.T) {
	usage := quotaUsage30d{Requests: 12, InputTokens: 3000, OutputTokens: 800, Cost: 1.25, Currency: "USD"}
	windows := []budgetScopeRow{
		{
			ID:          uuid.New(),
			ScopeType:   "organization",
			ScopeID:     uuid.New(),
			LimitAmount: "100.00",
			SpentAmount: "10.00",
			Currency:    "USD",
			WindowEnd:   time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:          uuid.New(),
			ScopeType:   "api_key",
			ScopeID:     uuid.New(),
			LimitAmount: "20.00",
			SpentAmount: "5.50",
			Currency:    "USD",
			WindowEnd:   time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC),
		},
	}

	budgetResp, ok := buildBudgetQuotaResponse(windows, usage)
	if !ok {
		t.Fatal("expected valid budget quota response")
	}
	if budgetResp.Source != "budget" || budgetResp.PlanName != "api_key_budget" || budgetResp.Unlimited {
		t.Fatalf("unexpected budget header: %+v", budgetResp)
	}
	if budgetResp.Remaining == nil || math.Abs(*budgetResp.Remaining-14.5) > 1e-6 {
		t.Fatalf("remaining = %v, want 14.5", budgetResp.Remaining)
	}
	if len(budgetResp.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(budgetResp.Items))
	}

	remPct := 82.0
	usedPct := 18.0
	totPct := 100.0
	resetMS := int64(1795800000000)
	upResp, ok := buildUpstreamQuotaResponse([]UpstreamQuotaItem{
		{
			AccountID:     "acc-1",
			AccountName:   "智谱",
			Provider:      "zhipu-glm-coding",
			LabelKey:      "quota_5h",
			Kind:          "quota",
			PlanName:      "quota_5h",
			Remaining:     &remPct,
			Used:          &usedPct,
			Total:         &totPct,
			Unit:          "%",
			NextResetTime: &resetMS,
		},
	}, usage)
	if !ok || upResp.Source != "upstream_balance" || upResp.PlanName != "quota_5h" || upResp.Unit != "%" {
		t.Fatalf("unexpected upstream quota response: %+v", upResp)
	}

	fallbackResp := buildUsageFallbackQuotaResponse(usage)
	if !fallbackResp.Unlimited || fallbackResp.Source != "usage" || fallbackResp.Remaining != nil || fallbackResp.Used == nil || *fallbackResp.Used != 1.25 {
		t.Fatalf("unexpected fallback quota response: %+v", fallbackResp)
	}
}

func TestStreamAttemptControlTimeoutAndCommitRace(t *testing.T) {
	timedCtrl := newStreamAttemptControl(context.Background(), 25*time.Millisecond)
	defer timedCtrl.cancel()
	select {
	case <-timedCtrl.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected pre-commit watchdog to cancel ctx")
	}
	if !timedCtrl.disarmTimedOut() {
		t.Fatal("expected disarmTimedOut() = true after timeout")
	}

	committedCtrl := newStreamAttemptControl(context.Background(), 50*time.Millisecond)
	defer committedCtrl.cancel()
	if !committedCtrl.commit() {
		t.Fatal("expected commit() = true before deadline")
	}
	time.Sleep(80 * time.Millisecond)
	if committedCtrl.ctx.Err() != nil {
		t.Fatalf("committed stream ctx should not be cancelled by disarmed timer: %v", committedCtrl.ctx.Err())
	}
}

func TestOpenResponsesStreamWinnerLongStreamAndTruncatedDetection(t *testing.T) {
	runScenario := func(sendCompleted bool) (bool, *schemas.BifrostError, []*schemas.BifrostResponsesStreamResponse) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "no flusher", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			emit := func(eventName, dataJSON string) {
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, dataJSON)
				flusher.Flush()
			}
			emit("response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_test","object":"response","status":"in_progress","model":"glm-5.4","output":[]}}`)
			time.Sleep(45 * time.Millisecond)
			emit("response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":1,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hello"}`)
			time.Sleep(70 * time.Millisecond)
			if sendCompleted {
				emit("response.completed", `{"type":"response.completed","sequence_number":2,"response":{"id":"resp_test","object":"response","status":"completed","model":"glm-5.4","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`)
			}
		}))
		defer upstream.Close()

		engine, err := bifrostx.NewEngine(context.Background(), []bifrostx.ProviderSpec{
			{
				Provider:    "openai",
				APIProtocol: bifrostx.ProtocolOpenAIResponses,
				AccountID:   "acc-stream-1",
				Name:        "stream-test",
				APIKey:      "sk-test",
				BaseURL:     upstream.URL,
			},
		})
		if err != nil {
			t.Fatalf("NewEngine failed: %v", err)
		}
		defer engine.Close()

		deps := Deps{
			Logger: slog.New(slog.DiscardHandler),
			Engine: engine,
		}
		svc := resolvedService{
			ID:   uuid.New(),
			Name: "glm-5.4",
			Policy: routePolicy{
				ProviderTimeoutMS:   80,
				FirstTokenTimeoutMS: 50,
				RetryPolicy:         DefaultRetryPolicy(),
				CircuitBreaker:      DefaultCircuitBreakerPolicy(),
			},
		}
		cand := candidate{
			ID:            uuid.NewString(),
			Provider:      "openai",
			APIProtocol:   bifrostx.ProtocolOpenAIResponses,
			AccountID:     "acc-stream-1",
			UpstreamModel: "glm-5.4",
		}
		decision := RouteDecision{Chain: routeChain{cand}}
		msg := "hi"
		role := schemas.ResponsesInputMessageRoleUser
		bifrostReq := &schemas.BifrostResponsesRequest{
			Model: "glm-5.4",
			Input: []schemas.ResponsesMessage{{
				Role:    &role,
				Content: &schemas.ResponsesMessageContent{ContentStr: &msg},
			}},
		}
		httpReq := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

		winner, _, lastErr, lastTTFT, clientAborted := deps.openResponsesStreamWinner(httpReq, svc, decision, bifrostReq, nil)
		if winner == nil || lastErr != nil || lastTTFT || clientAborted {
			t.Fatalf("openResponsesStreamWinner failed: winner=%v lastErr=%v ttft=%v aborted=%v", winner, lastErr, lastTTFT, clientAborted)
		}
		defer winner.Cancel()

		var all []*schemas.BifrostResponsesStreamResponse
		var gotStreamErr *schemas.BifrostError
		sawTerminal := false
		for _, ev := range winner.Buffered {
			all = append(all, ev)
			if isResponsesTerminalEvent(ev) {
				sawTerminal = true
			}
		}
		for chunk := range winner.Stream {
			if chunk.BifrostError != nil {
				gotStreamErr = chunk.BifrostError
				break
			}
			if chunk.BifrostResponsesStreamResponse != nil {
				all = append(all, chunk.BifrostResponsesStreamResponse)
				if isResponsesTerminalEvent(chunk.BifrostResponsesStreamResponse) {
					sawTerminal = true
				}
			}
		}
		return sawTerminal, gotStreamErr, all
	}

	// Stream lasts ~115ms > ProviderTimeoutMS (80ms) and must finish with response.completed.
	completedOK, errOK, eventsOK := runScenario(true)
	if errOK != nil || !completedOK || len(eventsOK) < 2 {
		t.Fatalf("expected completed long stream past ProviderTimeoutMS, err=%v sawTerminal=%v len=%d", errOK, completedOK, len(eventsOK))
	}

	// Truncated stream without response.completed must surface either a stream error or !sawTerminal.
	completedTrunc, errTrunc, eventsTrunc := runScenario(false)
	if (completedTrunc && errTrunc == nil) || len(eventsTrunc) == 0 {
		t.Fatalf("expected truncated stream detection, err=%v sawTerminal=%v len=%d", errTrunc, completedTrunc, len(eventsTrunc))
	}
}

func TestOpenResponsesStreamWinnerClassifiesTTFTBeforeFallback(t *testing.T) {
	slowUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
		}
	}))
	defer slowUpstream.Close()

	fastUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		emit := func(eventName, dataJSON string) {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, dataJSON)
			flusher.Flush()
		}
		emit("response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_fast","object":"response","status":"in_progress","model":"cand-fast","output":[]}}`)
		emit("response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":1,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"ok"}`)
		emit("response.completed", `{"type":"response.completed","sequence_number":2,"response":{"id":"resp_fast","object":"response","status":"completed","model":"cand-fast","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}`)
	}))
	defer fastUpstream.Close()

	engine, err := bifrostx.NewEngine(context.Background(), []bifrostx.ProviderSpec{
		{
			Provider:    "openai",
			APIProtocol: bifrostx.ProtocolOpenAIResponses,
			AccountID:   "acc-slow",
			Name:        "slow-upstream",
			APIKey:      "sk-slow",
			BaseURL:     slowUpstream.URL,
		},
		{
			Provider:    "openai",
			APIProtocol: bifrostx.ProtocolOpenAIResponses,
			AccountID:   "acc-fast",
			Name:        "fast-upstream",
			APIKey:      "sk-fast",
			BaseURL:     fastUpstream.URL,
		},
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	deps := Deps{
		Logger: slog.New(slog.DiscardHandler),
		Engine: engine,
	}
	svc := resolvedService{
		ID:   uuid.New(),
		Name: "smart-router",
		Policy: routePolicy{
			ProviderTimeoutMS:   500,
			FirstTokenTimeoutMS: 60,
			RetryPolicy:         DefaultRetryPolicy(),
			CircuitBreaker:      DefaultCircuitBreakerPolicy(),
		},
	}
	candSlow := candidate{
		ID:            uuid.NewString(),
		Provider:      "openai",
		APIProtocol:   bifrostx.ProtocolOpenAIResponses,
		AccountID:     "acc-slow",
		UpstreamModel: "cand-slow",
	}
	candFast := candidate{
		ID:            uuid.NewString(),
		Provider:      "openai",
		APIProtocol:   bifrostx.ProtocolOpenAIResponses,
		AccountID:     "acc-fast",
		UpstreamModel: "cand-fast",
	}
	decision := RouteDecision{Chain: routeChain{candSlow, candFast}}
	msg := "hi"
	role := schemas.ResponsesInputMessageRoleUser
	bifrostReq := &schemas.BifrostResponsesRequest{
		Model: "smart-router",
		Input: []schemas.ResponsesMessage{{
			Role:    &role,
			Content: &schemas.ResponsesMessageContent{ContentStr: &msg},
		}},
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	winner, attempts, lastErr, lastTTFT, clientAborted := deps.openResponsesStreamWinner(httpReq, svc, decision, bifrostReq, nil)
	if winner == nil || lastErr != nil || lastTTFT || clientAborted {
		t.Fatalf("expected fast fallback winner, got winner=%v lastErr=%v lastTTFT=%v aborted=%v attempts=%+v", winner, lastErr, lastTTFT, clientAborted, attempts)
	}
	defer winner.Cancel()
	for range winner.Stream {
	}

	if winner.Candidate.UpstreamModel != "cand-fast" {
		t.Fatalf("expected winning candidate cand-fast, got %q", winner.Candidate.UpstreamModel)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 prior attempt recorded before winner, got %d: %+v", len(attempts), attempts)
	}
	if attempts[0].UpstreamModel != "cand-slow" ||
		attempts[0].Outcome != "ttft_timeout" ||
		attempts[0].StatusCode != http.StatusGatewayTimeout ||
		attempts[0].ErrorType != "first_token_timeout" {
		t.Fatalf("expected first attempt classified as 504 ttft_timeout/first_token_timeout, got %+v", attempts[0])
	}
}

func TestResponsesStreamNormalizerSynthesizedAndNativeReasoning(t *testing.T) {
	roleAssistant := schemas.ResponsesInputMessageRoleAssistant
	reasoningType := schemas.ResponsesMessageTypeReasoning

	// Case 1: summaryMode == "none" -> emits response.reasoning_text.{delta,done} and empty summary: [], no role.
	normNone := newResponsesStreamNormalizer("none")
	itemAdded := &schemas.BifrostResponsesStreamResponse{
		Type:        schemas.ResponsesStreamResponseTypeOutputItemAdded,
		OutputIndex: intPtr(0),
		Item: &schemas.ResponsesMessage{
			ID:      strPtr("rs_1"),
			Type:    &reasoningType,
			Role:    &roleAssistant,
			Content: &schemas.ResponsesMessageContent{ContentBlocks: []schemas.ResponsesMessageContentBlock{}},
		},
	}
	outAdded := normNone.Normalize(itemAdded)
	if len(outAdded) != 1 || outAdded[0].Item == nil || outAdded[0].Item.Role != nil {
		t.Fatalf("expected normalized reasoning item without role, got %+v", outAdded)
	}
	if outAdded[0].Item.ResponsesReasoning == nil || outAdded[0].Item.ResponsesReasoning.Summary == nil {
		t.Fatalf("expected non-nil empty summary slice on reasoning item")
	}
	if rawAdded, err := sanitizeOpenAIResponsesPayload(outAdded[0]); err != nil || !bytes.Contains(rawAdded, []byte(`"summary":[]`)) || bytes.Contains(rawAdded, []byte(`"role"`)) {
		t.Fatalf("unexpected serialized output_item.added JSON: %s (err=%v)", rawAdded, err)
	}

	delta1 := normNone.Normalize(&schemas.BifrostResponsesStreamResponse{
		Type:        schemas.ResponsesStreamResponseTypeReasoningSummaryTextDelta,
		OutputIndex: intPtr(0),
		ItemID:      strPtr("rs_1"),
		Delta:       strPtr("step 1 "),
	})
	if len(delta1) != 1 || delta1[0].Type != schemas.ResponsesStreamResponseTypeReasoningTextDelta || delta1[0].ContentIndex == nil || *delta1[0].ContentIndex != 0 {
		t.Fatalf("expected single reasoning_text.delta with content_index=0, got %+v", delta1)
	}

	doneItem := normNone.Normalize(&schemas.BifrostResponsesStreamResponse{
		Type:        schemas.ResponsesStreamResponseTypeOutputItemDone,
		OutputIndex: intPtr(0),
		Item: &schemas.ResponsesMessage{
			ID:   strPtr("rs_1"),
			Type: &reasoningType,
			Role: &roleAssistant,
			Content: &schemas.ResponsesMessageContent{
				ContentBlocks: []schemas.ResponsesMessageContentBlock{{Type: schemas.ResponsesOutputMessageContentTypeReasoning, Text: strPtr("step 1 ")}},
			},
		},
	})
	if len(doneItem) != 2 || doneItem[0].Type != schemas.ResponsesStreamResponseTypeReasoningTextDone || doneItem[1].Type != schemas.ResponsesStreamResponseTypeOutputItemDone {
		t.Fatalf("expected reasoning_text.done followed by output_item.done, got %+v", doneItem)
	}
	if doneItem[1].Item.Role != nil || doneItem[1].Item.ResponsesReasoning == nil || len(doneItem[1].Item.ResponsesReasoning.Summary) != 0 {
		t.Fatalf("expected done reasoning item without role and empty summary, got %+v", doneItem[1].Item)
	}

	// Case 2: summaryMode == "auto" -> emits both raw reasoning_text.delta AND summary_part.added + summary_text.delta(summary_index:0)
	normAuto := newResponsesStreamNormalizer("auto")
	_ = normAuto.Normalize(itemAdded)
	outAuto := normAuto.Normalize(&schemas.BifrostResponsesStreamResponse{
		Type:        schemas.ResponsesStreamResponseTypeReasoningSummaryTextDelta,
		OutputIndex: intPtr(0),
		ItemID:      strPtr("rs_1"),
		Delta:       strPtr("thinking..."),
	})
	if len(outAuto) != 3 {
		t.Fatalf("expected 3 events (raw delta, summary_part.added, summary_text.delta), got %d", len(outAuto))
	}
	if outAuto[0].Type != schemas.ResponsesStreamResponseTypeReasoningTextDelta ||
		outAuto[1].Type != schemas.ResponsesStreamResponseTypeReasoningSummaryPartAdded ||
		outAuto[2].Type != schemas.ResponsesStreamResponseTypeReasoningSummaryTextDelta ||
		outAuto[2].SummaryIndex == nil || *outAuto[2].SummaryIndex != 0 {
		t.Fatalf("unexpected auto-mode events sequence: %+v", outAuto)
	}
	doneAuto := normAuto.Normalize(&schemas.BifrostResponsesStreamResponse{
		Type:        schemas.ResponsesStreamResponseTypeOutputItemDone,
		OutputIndex: intPtr(0),
		Item:        &schemas.ResponsesMessage{ID: strPtr("rs_1"), Type: &reasoningType},
	})
	if len(doneAuto) != 4 ||
		doneAuto[0].Type != schemas.ResponsesStreamResponseTypeReasoningTextDone ||
		doneAuto[1].Type != schemas.ResponsesStreamResponseTypeReasoningSummaryTextDone ||
		doneAuto[2].Type != schemas.ResponsesStreamResponseTypeReasoningSummaryPartDone ||
		doneAuto[3].Type != schemas.ResponsesStreamResponseTypeOutputItemDone {
		t.Fatalf("unexpected auto-mode done events: %+v", doneAuto)
	}
	if seq := doneAuto[3].SequenceNumber; seq <= outAuto[2].SequenceNumber {
		t.Fatalf("expected strictly monotonic sequence_number, got prev=%d done=%d", outAuto[2].SequenceNumber, seq)
	}

	// Case 3: Native Responses summary event (SummaryIndex already set) -> injects part.added once, never duplicates raw text.
	normNative := newResponsesStreamNormalizer("auto")
	sumIdx := 0
	outNative := normNative.Normalize(&schemas.BifrostResponsesStreamResponse{
		Type:         schemas.ResponsesStreamResponseTypeReasoningSummaryTextDelta,
		OutputIndex:  intPtr(0),
		SummaryIndex: &sumIdx,
		ItemID:       strPtr("rs_native"),
		Delta:        strPtr("brief"),
	})
	if len(outNative) != 2 ||
		outNative[0].Type != schemas.ResponsesStreamResponseTypeReasoningSummaryPartAdded ||
		outNative[1].Type != schemas.ResponsesStreamResponseTypeReasoningSummaryTextDelta {
		t.Fatalf("expected native stream to inject summary_part.added before summary delta, got %+v", outNative)
	}
}

func TestApplyProviderStreamingHintsForZhipu(t *testing.T) {
	tools := []schemas.ResponsesTool{{Type: schemas.ResponsesToolTypeFunction, Name: strPtr("exec_command")}}
	origParams := &schemas.ResponsesParameters{Tools: tools}
	req := &schemas.BifrostResponsesRequest{Params: origParams}

	candZhipu := candidate{Provider: "zhipu-glm-coding", APIProtocol: bifrostx.ProtocolOpenAIChat}
	if !applyProviderStreamingHintsForResponses(req, candZhipu) {
		t.Fatalf("expected tool_stream hint injection for zhipu-glm-coding openai_chat with tools")
	}
	if req.Params == origParams || origParams.ExtraParams != nil {
		t.Fatalf("expected shallow clone of Params so shared request is not mutated")
	}
	if req.Params.ExtraParams["tool_stream"] != true {
		t.Fatalf("expected tool_stream=true, got %+v", req.Params.ExtraParams)
	}

	// Non-Zhipu or native responses protocol must remain untouched.
	reqNonZhipu := &schemas.BifrostResponsesRequest{Params: &schemas.ResponsesParameters{Tools: tools}}
	if applyProviderStreamingHintsForResponses(reqNonZhipu, candidate{Provider: "deepseek", APIProtocol: bifrostx.ProtocolOpenAIChat}) {
		t.Fatalf("did not expect tool_stream hint on deepseek")
	}
	if applyProviderStreamingHintsForResponses(reqNonZhipu, candidate{Provider: "zhipu-glm-coding", APIProtocol: bifrostx.ProtocolOpenAIResponses}) {
		t.Fatalf("did not expect tool_stream hint on non-chat protocol")
	}

	// Chat streaming hint
	chatParams, ok := applyProviderStreamingHintsForChat(&schemas.ChatParameters{Tools: []schemas.ChatTool{{Type: schemas.ChatToolTypeFunction}}}, candZhipu)
	if !ok || chatParams.ExtraParams["tool_stream"] != true {
		t.Fatalf("expected ChatParameters tool_stream=true, got ok=%v extra=%+v", ok, chatParams.ExtraParams)
	}
}

func TestSanitizeOpenAIResponsesPayloadAndSSEHeaders(t *testing.T) {
	ev := &schemas.BifrostResponsesStreamResponse{
		Type:           schemas.ResponsesStreamResponseTypeCompleted,
		SequenceNumber: 5,
		Response: &schemas.BifrostResponsesResponse{
			Object: "response",
			ExtraFields: schemas.BifrostResponseExtraFields{
				RequestType: schemas.ResponsesStreamRequest,
			},
		},
		ExtraFields: schemas.BifrostResponseExtraFields{
			RequestType: schemas.ResponsesStreamRequest,
		},
	}
	cleaned, err := sanitizeOpenAIResponsesPayload(ev)
	if err != nil {
		t.Fatalf("sanitizeOpenAIResponsesPayload failed: %v", err)
	}
	if bytes.Contains(cleaned, []byte(`"extra_fields"`)) {
		t.Fatalf("expected all extra_fields removed, got %s", cleaned)
	}
	if !bytes.Contains(cleaned, []byte(`"type":"response.completed"`)) {
		t.Fatalf("expected type preserved, got %s", cleaned)
	}

	// Early commit check for output_item.added of reasoning / tool / message items.
	reasoningType := schemas.ResponsesMessageTypeReasoning
	if !responsesEventShouldCommit(&schemas.BifrostResponsesStreamResponse{
		Type: schemas.ResponsesStreamResponseTypeOutputItemAdded,
		Item: &schemas.ResponsesMessage{Type: &reasoningType},
	}) {
		t.Fatalf("expected reasoning output_item.added to commit SSE stream immediately")
	}
	if responsesEventShouldCommit(&schemas.BifrostResponsesStreamResponse{Type: schemas.ResponsesStreamResponseTypeCreated}) {
		t.Fatalf("expected response.created alone to remain pre-commit for multi-candidate fallback")
	}

	rec := httptest.NewRecorder()
	if _, ok := commitSSEHeaders(rec); !ok {
		t.Fatalf("expected httptest recorder to support http.Flusher")
	}
	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("expected X-Accel-Buffering=no header, got %q", got)
	}
}
