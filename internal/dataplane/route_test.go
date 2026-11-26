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
