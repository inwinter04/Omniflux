package dataplane

import (
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
	got, err := chain.findCandidate("openai", "model-primary")
	if err != nil {
		t.Fatalf("findCandidate error = %v", err)
	}
	if got.ID != idFor("primary") {
		t.Fatalf("findCandidate got %s, want %s", got.ID, idFor("primary"))
	}
	if _, err := chain.findCandidate("openai", "missing-model"); err == nil {
		t.Fatal("findCandidate should fail for unknown upstream model")
	}
}
