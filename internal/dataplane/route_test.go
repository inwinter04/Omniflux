package dataplane

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// idFor 生成与候选名绑定的确定性 UUID,模拟生产中数据库生成的候选 ID。
func idFor(name string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()
}

func testCandidate(name string, priority int, weight, inputCost, outputCost, quality float64) candidate {
	return candidate{
		ID:             idFor(name),
		Provider:       "openai_compatible",
		AccountID:      "acc-" + name,
		UpstreamModel:  "model-" + name,
		Priority:       priority,
		Weight:         weight,
		InputCostPerM:  inputCost,
		OutputCostPerM: outputCost,
		Quality:        quality,
	}
}

func noHealthScores() *policyScores {
	return &policyScores{Health: map[uuid.UUID]candidateHealth{}}
}

func TestSelectChainOrdersByPriorityAndCost(t *testing.T) {
	cheap := testCandidate("cheap", 1, 1, 10, 10, 0.5)
	expensive := testCandidate("expensive", 1, 1, 20, 20, 0.5)
	backup := testCandidate("backup", 2, 1, 5, 5, 0.5)
	// defaultPolicy 的 fallbackLimit=2 会截断链,这里不限制以便验证完整排序。
	policy := defaultPolicy
	policy.FallbackLimit = 0
	chain := selectChain(routeChain{expensive, backup, cheap}, policy, noHealthScores())
	if chain[0].ID != idFor("cheap") {
		t.Fatalf("cheapest candidate should rank first, got %s", chain[0].ID)
	}
	if chain[1].ID != idFor("expensive") {
		t.Fatalf("expensive candidate should rank second in its priority group, got %s", chain[1].ID)
	}
	if chain[2].ID != idFor("backup") {
		t.Fatalf("lower priority candidate should stay last, got %s", chain[2].ID)
	}
}

func TestSelectChainQualityBreaksCostTie(t *testing.T) {
	weak := testCandidate("weak", 1, 1, 10, 10, 0.2)
	strong := testCandidate("strong", 1, 1, 10, 10, 0.9)
	chain := selectChain(routeChain{weak, strong}, defaultPolicy, noHealthScores())
	if chain[0].ID != idFor("strong") {
		t.Fatalf("higher quality candidate should rank first on cost tie, got %s", chain[0].ID)
	}
}

func TestSelectChainAppliesFallbackLimit(t *testing.T) {
	first := testCandidate("first", 1, 1, 10, 10, 0.5)
	second := testCandidate("second", 2, 1, 10, 10, 0.5)
	third := testCandidate("third", 3, 1, 10, 10, 0.5)
	policy := defaultPolicy
	policy.FallbackLimit = 2
	chain := selectChain(routeChain{third, second, first}, policy, noHealthScores())
	if len(chain) != 2 {
		t.Fatalf("fallback limit 2 should truncate chain to 2, got %d", len(chain))
	}
	if chain[0].ID != idFor("first") || chain[1].ID != idFor("second") {
		t.Fatalf("unexpected chain order: %s, %s", chain[0].ID, chain[1].ID)
	}
}

func TestSelectChainHealthImprovesRanking(t *testing.T) {
	slow := testCandidate("slow", 1, 1, 10, 10, 0.5)
	fast := testCandidate("fast", 1, 1, 10, 10, 0.5)
	scores := &policyScores{Health: map[uuid.UUID]candidateHealth{
		uuid.MustParse(idFor("slow")): {AvgMS: 400, Successes: 90, Total: 100},
		uuid.MustParse(idFor("fast")): {AvgMS: 100, Successes: 99, Total: 100},
	}}
	chain := selectChain(routeChain{slow, fast}, defaultPolicy, scores)
	if chain[0].ID != idFor("fast") {
		t.Fatalf("faster and more reliable candidate should rank first, got %s", chain[0].ID)
	}
}

func TestWeightedOrderKeepsAllCandidates(t *testing.T) {
	group := routeChain{
		testCandidate("a", 1, 10, 10, 10, 0.5),
		testCandidate("b", 1, 1, 10, 10, 0.5),
		testCandidate("c", 1, 1, 10, 10, 0.5),
	}
	ordered := weightedOrder(group)
	if len(ordered) != len(group) {
		t.Fatalf("weighted order must not drop candidates, got %d of %d", len(ordered), len(group))
	}
	seen := map[string]bool{}
	for _, c := range ordered {
		if seen[c.ID] {
			t.Fatalf("candidate %s appeared twice", c.ID)
		}
		seen[c.ID] = true
	}
	for _, c := range group {
		if !seen[c.ID] {
			t.Fatalf("candidate %s missing from weighted order", c.ID)
		}
	}
}

func TestRelativeScore(t *testing.T) {
	if got := relativeScore(10, 10); got != 1 {
		t.Fatalf("relativeScore(10, 10) = %v, want 1", got)
	}
	// 候选指标等于组内最优时得满分;value 越差(相对 best 越大)分数越低。
	if got := relativeScore(20, 10); got != 0.5 {
		t.Fatalf("relativeScore(20, 10) = %v, want 0.5", got)
	}
	if got := relativeScore(10, 20); got != 1 {
		t.Fatalf("relativeScore(10, 20) = %v, want 1", got)
	}
	if got := relativeScore(0, 10); got != 0.5 {
		t.Fatalf("relativeScore(0, 10) = %v, want neutral 0.5", got)
	}
}

func TestSuccessRateWithoutHistory(t *testing.T) {
	if got := (candidateHealth{}).successRate(); got != 0.5 {
		t.Fatalf("empty history successRate = %v, want 0.5", got)
	}
}

func TestFindCandidateMapsProvider(t *testing.T) {
	chain := routeChain{testCandidate("primary", 1, 1, 10, 10, 0.5)}
	got, err := chain.findCandidate("openai", "model-primary", "acc-primary")
	if err != nil {
		t.Fatalf("findCandidate error = %v", err)
	}
	if got.ID != idFor("primary") {
		t.Fatalf("findCandidate got %s, want %s", got.ID, idFor("primary"))
	}
	if _, err := chain.findCandidate("openai", "missing-model", "acc-primary"); err == nil {
		t.Fatal("findCandidate should fail for unknown upstream model")
	}
}

func TestFindCandidateUsesAccountForDuplicateModels(t *testing.T) {
	first := testCandidate("first", 1, 1, 10, 10, 0.5)
	second := testCandidate("second", 2, 1, 20, 20, 0.5)
	second.UpstreamModel = first.UpstreamModel
	chain := routeChain{first, second}
	got, err := chain.findCandidate("openai", first.UpstreamModel, second.AccountID)
	if err != nil {
		t.Fatalf("findCandidate error = %v", err)
	}
	if got.ID != second.ID {
		t.Fatalf("findCandidate got %s, want %s", got.ID, second.ID)
	}
}

func TestConstraintsFilter(t *testing.T) {
	openai := testCandidate("openai-m", 1, 1, 10, 10, 0.5)
	openai.Provider = "openai"
	anthropic := testCandidate("anthropic-m", 1, 1, 10, 10, 0.5)
	anthropic.Provider = "anthropic"
	expensive := testCandidate("expensive-m", 1, 1, 40, 40, 0.5)

	cases := []struct {
		name        string
		constraints string
		expect      []string
	}{
		{
			name:        "no constraints keeps all",
			constraints: `{}`,
			expect:      []string{"model-openai-m", "model-anthropic-m", "model-expensive-m"},
		},
		{
			name:        "allowed providers whitelist",
			constraints: `{"allowed_providers":["openai"]}`,
			expect:      []string{"model-openai-m"},
		},
		{
			name:        "blocked providers",
			constraints: `{"blocked_providers":["anthropic"]}`,
			expect:      []string{"model-openai-m", "model-expensive-m"},
		},
		{
			name:        "allowed upstream models",
			constraints: `{"allowed_upstream_models":["model-anthropic-m"]}`,
			expect:      []string{"model-anthropic-m"},
		},
		{
			name:        "max cost per million",
			constraints: `{"max_cost_per_million":20}`,
			expect:      []string{"model-openai-m", "model-anthropic-m"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := parsePolicyConstraints([]byte(tc.constraints))
			got := c.filter(routeChain{openai, anthropic, expensive})
			if len(got) != len(tc.expect) {
				t.Fatalf("filter kept %d candidates, want %d", len(got), len(tc.expect))
			}
			for i, want := range tc.expect {
				if got[i].UpstreamModel != want {
					t.Fatalf("filtered[%d] = %s, want %s", i, got[i].UpstreamModel, want)
				}
			}
		})
	}
}

func TestValidateConstraintsJSON(t *testing.T) {
	if err := ValidateConstraintsJSON(nil); err != nil {
		t.Fatalf("nil constraints should pass, got %v", err)
	}
	if err := ValidateConstraintsJSON([]byte(`{}`)); err != nil {
		t.Fatalf("empty constraints should pass, got %v", err)
	}
	if err := ValidateConstraintsJSON([]byte(`{"allowed_providers":["openai"],"max_cost_per_million":10.5}`)); err != nil {
		t.Fatalf("valid constraints rejected: %v", err)
	}
	if err := ValidateConstraintsJSON([]byte(`{"unknown_key":true}`)); err == nil {
		t.Fatal("unknown key should be rejected")
	}
	if err := ValidateConstraintsJSON([]byte(`{"max_cost_per_million":-1}`)); err == nil {
		t.Fatal("negative cost should be rejected")
	}
	if err := ValidateConstraintsJSON([]byte(`{"allowed_providers":"openai"}`)); err == nil {
		t.Fatal("non-array providers should be rejected")
	}
}

func TestLoadServiceAppliesConstraints(t *testing.T) {
	// 约束过滤发生在 loadService 内:被过滤到 0 个候选时视为服务不可用。
	cheap := testCandidate("cheap", 1, 1, 10, 10, 0.5)
	expensive := testCandidate("expensive", 1, 1, 40, 40, 0.5)
	c := parsePolicyConstraints([]byte(`{"max_cost_per_million":20}`))
	filtered := c.filter(routeChain{cheap, expensive})
	if len(filtered) != 1 || filtered[0].UpstreamModel != "model-cheap" {
		t.Fatalf("constraint filter should keep only cheap candidate, got %d", len(filtered))
	}
}

func TestSelectChainSoftDeprioritizesUnhealthyAccounts(t *testing.T) {
	unhealthyPrimary := testCandidate("bad", 1, 1, 1, 1, 0.9)
	unhealthyPrimary.AccountID = idFor("acc-bad")
	healthyBackup := testCandidate("good", 1, 1, 50, 50, 0.3)
	healthyBackup.AccountID = idFor("acc-good")

	scores := &policyScores{
		Health: map[uuid.UUID]candidateHealth{},
		AccountHealth: map[uuid.UUID]string{
			uuid.MustParse(unhealthyPrimary.AccountID): "unhealthy",
			uuid.MustParse(healthyBackup.AccountID):    "healthy",
		},
	}

	chain := selectChain(routeChain{unhealthyPrimary, healthyBackup}, defaultPolicy, scores)
	if len(chain) != 2 {
		t.Fatalf("all candidates must remain in chain, got %d", len(chain))
	}
	if chain[0].ID != healthyBackup.ID || chain[1].ID != unhealthyPrimary.ID {
		t.Fatalf("healthy account should precede unhealthy account, got %v -> %v", chain[0].ID, chain[1].ID)
	}
}

func TestEvaluateRouteFXNormalizationAndCostCap(t *testing.T) {
	// 1 USD = 7.2 CNY: 7 CNY (< 1 USD) is much cheaper than 5 USD.
	cnyCheap := testCandidate("cny-cheap", 1, 1, 7, 7, 0.7)
	cnyCheap.CostCurrency = "CNY"
	usdExpensive := testCandidate("usd-expensive", 1, 1, 5, 5, 0.7)
	usdExpensive.CostCurrency = "USD"
	cnyTooExpensive := testCandidate("cny-overcap", 1, 1, 72, 72, 0.9)
	cnyTooExpensive.CostCurrency = "CNY" // 10 USD/1M > cap 6 USD

	policy := defaultPolicy
	policy.Strategy = StrategyCostFirst
	policy.CompareCurrency = "USD"
	policy.Weights = PresetWeights(StrategyCostFirst)
	capUSD := 6.0
	policy.Constraints = policyConstraints{MaxCostPerMillion: &capUSD}

	fxRates := map[string]float64{"USD": 1.0, "CNY": 7.2}
	decision := EvaluateRoute([]candidate{usdExpensive, cnyCheap, cnyTooExpensive}, policy, RouteContext{}, nil, fxRates, 0)
	if len(decision.Chain) != 2 {
		t.Fatalf("expected 2 candidates after FX cost cap filter, got %d", len(decision.Chain))
	}
	if decision.Chain[0].ID != cnyCheap.ID {
		t.Fatalf("expected normalized CNY candidate first, got %s", decision.Chain[0].ID)
	}
	var overCapEval *CandidateEvaluation
	for i := range decision.Evaluations {
		if decision.Evaluations[i].CandidateID == cnyTooExpensive.ID {
			overCapEval = &decision.Evaluations[i]
		}
	}
	if overCapEval == nil || overCapEval.ExcludedReason != "exceeds_max_cost" {
		t.Fatalf("expected exceeds_max_cost for over-cap candidate, got %+v", overCapEval)
	}
}

func TestEvaluateRouteStrategiesModes(t *testing.T) {
	a := testCandidate("alpha", 1, 1, 10, 10, 0.5)
	b := testCandidate("beta", 1, 1, 2, 2, 0.9)
	c := testCandidate("gamma", 1, 1, 20, 20, 0.3)

	// strict_failover preserves candidate order regardless of cost/quality within same priority
	strictPol := defaultPolicy
	strictPol.Strategy = StrategyStrictFailover
	strictDec := EvaluateRoute([]candidate{a, b, c}, strictPol, RouteContext{}, nil, nil, 0)
	if strictDec.Chain[0].ID != a.ID || strictDec.Chain[1].ID != b.ID || strictDec.Chain[2].ID != c.ID {
		t.Fatalf("strict_failover should preserve order [a,b,c], got [%s,%s,%s]", strictDec.Chain[0].ID, strictDec.Chain[1].ID, strictDec.Chain[2].ID)
	}

	// round_robin rotates first position deterministically by rrCounter
	rrPol := defaultPolicy
	rrPol.Strategy = StrategyRoundRobin
	rr0 := EvaluateRoute([]candidate{a, b, c}, rrPol, RouteContext{}, nil, nil, 0)
	rr1 := EvaluateRoute([]candidate{a, b, c}, rrPol, RouteContext{}, nil, nil, 1)
	rr2 := EvaluateRoute([]candidate{a, b, c}, rrPol, RouteContext{}, nil, nil, 2)
	if rr0.Chain[0].ID == rr1.Chain[0].ID || rr1.Chain[0].ID == rr2.Chain[0].ID || rr0.Chain[0].ID == rr2.Chain[0].ID {
		t.Fatalf("round_robin should rotate primaries across counters, got %s, %s, %s", rr0.Chain[0].ID, rr1.Chain[0].ID, rr2.Chain[0].ID)
	}

	// weighted computes weight probabilities and preserves all candidates
	wA := testCandidate("w-a", 1, 80, 10, 10, 0.5)
	wB := testCandidate("w-b", 1, 20, 10, 10, 0.5)
	wPol := defaultPolicy
	wPol.Strategy = StrategyWeighted
	wDec := EvaluateRoute([]candidate{wA, wB}, wPol, RouteContext{RandomSeed: 42}, nil, nil, 0)
	if len(wDec.Chain) != 2 || len(wDec.Evaluations) != 2 || wDec.Evaluations[0].WeightProbability == nil || *wDec.Evaluations[0].WeightProbability != 0.8 {
		t.Fatalf("weighted mode should set WeightProbability=0.8/0.2, got evals=%+v", wDec.Evaluations)
	}

	// latency_first uses AvgTTFTMS for streaming requests
	fastTTFT := testCandidate("fast-ttft", 1, 1, 10, 10, 0.5)
	slowTTFT := testCandidate("slow-ttft", 1, 1, 10, 10, 0.5)
	latScores := &policyScores{
		Health: map[uuid.UUID]candidateHealth{
			uuid.MustParse(fastTTFT.ID): {AvgMS: 5000, AvgTTFTMS: 120, TTFTCount: 5, Successes: 10, Total: 10},
			uuid.MustParse(slowTTFT.ID): {AvgMS: 900, AvgTTFTMS: 1800, TTFTCount: 5, Successes: 10, Total: 10},
		},
	}
	latPol := defaultPolicy
	latPol.Strategy = StrategyLatencyFirst
	latPol.Weights = PresetWeights(StrategyLatencyFirst)
	latDec := EvaluateRoute([]candidate{slowTTFT, fastTTFT}, latPol, RouteContext{Stream: true}, latScores, nil, 0)
	if latDec.Chain[0].ID != fastTTFT.ID {
		t.Fatalf("streaming latency_first should favor lower AvgTTFTMS, got %s", latDec.Chain[0].ID)
	}

	// custom weights are honored (100% quality puts quality=0.95 candidate first despite high cost)
	customPol := defaultPolicy
	customPol.Strategy = StrategyCustom
	customPol.Weights = policyWeights{Cost: 0, Quality: 1, Latency: 0, Reliability: 0}
	customDec := EvaluateRoute([]candidate{a, b, c}, customPol, RouteContext{}, nil, nil, 0)
	if customDec.Chain[0].ID != b.ID {
		t.Fatalf("custom quality-only weights should pick highest quality %s, got %s", b.ID, customDec.Chain[0].ID)
	}
}

func TestEvaluateRouteBayesianSmoothing(t *testing.T) {
	// 1/1 = (1+2)/(1+4) = 0.60 whereas 98/100 = (98+2)/(100+4) ≈ 0.9615
	newCand := candidateHealth{Successes: 1, Total: 1}
	provenCand := candidateHealth{Successes: 98, Total: 100}
	if !(provenCand.successRate() > newCand.successRate()) {
		t.Fatalf("proven candidate reliability (%v) must outrank single-hit candidate (%v)", provenCand.successRate(), newCand.successRate())
	}
}

func TestEvaluateRouteConditionalRulesAndCapabilities(t *testing.T) {
	visionCheap := testCandidate("vision-cheap", 1, 1, 5, 5, 0.7)
	visionCheap.Provider = "openai"
	visionCheap.Capabilities = []string{"vision", "streaming"}
	visionCheap.Tags = []string{"fast-vision"}
	ctx200k := 200000
	visionCheap.ContextWindow = &ctx200k

	textOnly := testCandidate("text-only", 1, 1, 1, 1, 0.9)
	textOnly.Provider = "deepseek"
	textOnly.Capabilities = []string{"reasoning"}
	ctx32k := 32000
	textOnly.ContextWindow = &ctx32k

	minTok := 50000
	policy := defaultPolicy
	policy.Rules = []RouteRule{
		{
			ID:       "rule-long-vision",
			Name:     "Long context & vision",
			Enabled:  true,
			Priority: 10,
			When: RouteRuleWhen{
				MinInputTokens: &minTok,
				Capabilities:   []string{"vision"},
				ProjectIDs:     []string{"proj-1"},
				Keywords:       []string{"screenshot"},
				Metadata:       map[string][]string{"tier": {"vip"}},
			},
			Target: RouteRuleTarget{
				Strategy:      StrategyQualityFirst,
				CandidateTags: []string{"fast-vision"},
			},
		},
	}

	reqCtx := RouteContext{
		EstimatedInputTokens: 64000,
		RequiredCapabilities: []string{"vision"},
		ProjectID:            "proj-1",
		KeywordText:          "Analyze this screenshot error please",
		Metadata:             map[string]string{"tier": "vip"},
	}
	dec := EvaluateRoute([]candidate{textOnly, visionCheap}, policy, reqCtx, nil, nil, 0)
	if dec.MatchedRuleID != "rule-long-vision" {
		t.Fatalf("expected rule-long-vision to match, got %q", dec.MatchedRuleID)
	}
	if dec.EffectiveStrategy != StrategyQualityFirst {
		t.Fatalf("expected effective strategy quality_first, got %q", dec.EffectiveStrategy)
	}
	if len(dec.Chain) != 1 || dec.Chain[0].ID != visionCheap.ID {
		t.Fatalf("expected only visionCheap in chain, got %d items", len(dec.Chain))
	}

	// Rule filter matching 0 candidates must safely fall back to full list with warning
	emptyRulePol := defaultPolicy
	emptyRulePol.Rules = []RouteRule{{
		ID: "r-empty", Name: "Missing tag", Enabled: true, Priority: 1,
		When:   RouteRuleWhen{},
		Target: RouteRuleTarget{CandidateTags: []string{"nonexistent-tag"}},
	}}
	fallbackDec := EvaluateRoute([]candidate{textOnly, visionCheap}, emptyRulePol, RouteContext{}, nil, nil, 0)
	if len(fallbackDec.Chain) != 2 || len(fallbackDec.Warnings) == 0 {
		t.Fatalf("expected empty rule target fallback with warning, got chain=%d warnings=%v", len(fallbackDec.Chain), fallbackDec.Warnings)
	}
}

func TestCandidateBreakerAndOutcomeClassification(t *testing.T) {
	rp := DefaultRetryPolicy()
	authAct := ClassifyAttemptOutcome(http.StatusUnauthorized, "", "", rp)
	if !authAct.SkipSameAccount || !authAct.ShouldFallback || authAct.ShouldRetry {
		t.Fatalf("unexpected auth error action: %+v", authAct)
	}
	ttftAct := ClassifyAttemptOutcome(http.StatusRequestTimeout, "ttft_timeout", "first_token_timeout", rp)
	if !ttftAct.ShouldFallback || !ttftAct.TripBreaker {
		t.Fatalf("unexpected ttft timeout action: %+v", ttftAct)
	}
	badReqAct := ClassifyAttemptOutcome(http.StatusBadRequest, "invalid_request", "", rp)
	if badReqAct.ShouldFallback || badReqAct.TripBreaker {
		t.Fatalf("400 should not fallback or trip breaker by default: %+v", badReqAct)
	}

	breaker := NewCandidateBreaker(nil)
	cbCfg := CircuitBreakerPolicy{Enabled: true, FailureThreshold: 2, CooldownSeconds: 30, HalfOpenMaxCalls: 1}
	c1 := testCandidate("c1", 1, 1, 10, 10, 0.8)
	c2 := testCandidate("c2", 1, 1, 20, 20, 0.8)
	ctx := context.Background()

	breaker.RecordFailure(ctx, c1.ID, http.StatusTooManyRequests, "rate_limit", "", rp, cbCfg)
	states := breaker.States(ctx, []uuid.UUID{uuid.MustParse(c1.ID), uuid.MustParse(c2.ID)}, cbCfg)
	if states[uuid.MustParse(c1.ID)].Open {
		t.Fatal("breaker must not open before threshold=2")
	}
	breaker.RecordFailure(ctx, c1.ID, http.StatusBadGateway, "upstream_error", "", rp, cbCfg)
	states = breaker.States(ctx, []uuid.UUID{uuid.MustParse(c1.ID), uuid.MustParse(c2.ID)}, cbCfg)
	if !states[uuid.MustParse(c1.ID)].Open {
		t.Fatal("breaker must open after threshold=2")
	}

	dec := EvaluateRoute([]candidate{c1, c2}, defaultPolicy, RouteContext{}, &policyScores{
		Health:   map[uuid.UUID]candidateHealth{},
		Circuits: states,
	}, nil, 0)
	if len(dec.Chain) != 1 || dec.Chain[0].ID != c2.ID {
		t.Fatalf("open circuit candidate should be skipped when healthy c2 exists, got %v", dec.ChainIDs)
	}

	// If ALL candidates are circuit-open, fallback still keeps chain non-empty
	allOpen := map[uuid.UUID]CircuitState{
		uuid.MustParse(c1.ID): {Open: true},
		uuid.MustParse(c2.ID): {Open: true},
	}
	allOpenDec := EvaluateRoute([]candidate{c1, c2}, defaultPolicy, RouteContext{}, &policyScores{
		Health:   map[uuid.UUID]candidateHealth{},
		Circuits: allOpen,
	}, nil, 0)
	if len(allOpenDec.Chain) != 2 {
		t.Fatalf("all-circuits-open safety fallback should preserve candidates, got %d", len(allOpenDec.Chain))
	}

	breaker.RecordSuccess(ctx, c1.ID)
	afterReset := breaker.States(ctx, []uuid.UUID{uuid.MustParse(c1.ID)}, cbCfg)
	if afterReset[uuid.MustParse(c1.ID)].Open {
		t.Fatal("RecordSuccess must close circuit")
	}
}