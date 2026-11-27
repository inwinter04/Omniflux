package dataplane

import (
	"encoding/json"
	"fmt"
	mrand "math/rand/v2"
	"sort"
	"strings"
)

const (
	OrchestratorTierSimple    = "simple"
	OrchestratorTierMedium    = "medium"
	OrchestratorTierComplex   = "complex"
	OrchestratorTierReasoning = "reasoning"

	OrchestratorSessionAdaptive      = "adaptive"
	OrchestratorSessionPerTurn       = "per_turn"
	OrchestratorSessionStickyUpgrade = "sticky_upgrade"
)

// OrchestratorTiers explicitly maps upstream model names or candidate tags to complexity tiers.
type OrchestratorTiers struct {
	Simple    []string `json:"simple,omitempty"`
	Medium    []string `json:"medium,omitempty"`
	Complex   []string `json:"complex,omitempty"`
	Reasoning []string `json:"reasoning,omitempty"`
}

// OrchestratorThresholds configures complexity score boundaries in [0, 1].
type OrchestratorThresholds struct {
	SimpleMedium     float64 `json:"simple_medium"`
	MediumComplex    float64 `json:"medium_complex"`
	ComplexReasoning float64 `json:"complex_reasoning"`
}

// OrchestratorConfig controls how auto_orchestrator classifies turns and protects session caches.
type OrchestratorConfig struct {
	SessionMode          string                 `json:"session_mode"`
	StickyContextTokens  int                    `json:"sticky_context_tokens"`
	LongContextThreshold int                    `json:"long_context_threshold"`
	Thresholds           OrchestratorThresholds `json:"thresholds"`
	Tiers                OrchestratorTiers      `json:"tiers"`
}

// OrchestratorDecision records structured orchestrator telemetry inside RouteDecision.
type OrchestratorDecision struct {
	Tier             string             `json:"tier"`
	RawTier          string             `json:"raw_tier"`
	Score            float64            `json:"score"`
	SessionMode      string             `json:"session_mode"`
	CacheProtected   bool               `json:"cache_protected,omitempty"`
	UpgradedFromTier string             `json:"upgraded_from_tier,omitempty"`
	AutoBucketed     bool               `json:"auto_bucketed,omitempty"`
	Signals          []string           `json:"signals,omitempty"`
	Dimensions       map[string]float64 `json:"dimensions,omitempty"`
}

// DefaultOrchestratorConfig returns production defaults balancing general LLM QA cost savings
// with AI coding agent prompt-cache preservation.
func DefaultOrchestratorConfig() OrchestratorConfig {
	return OrchestratorConfig{
		SessionMode:          OrchestratorSessionAdaptive,
		StickyContextTokens:  8000,
		LongContextThreshold: 32000,
		Thresholds: OrchestratorThresholds{
			SimpleMedium:     0.22,
			MediumComplex:    0.48,
			ComplexReasoning: 0.72,
		},
	}
}

// ParseOrchestratorConfig decodes orchestrator_config JSON with safe defaults.
func ParseOrchestratorConfig(raw []byte) OrchestratorConfig {
	cfg := DefaultOrchestratorConfig()
	if len(raw) == 0 || string(raw) == "null" {
		return cfg
	}
	_ = json.Unmarshal(raw, &cfg)
	switch strings.TrimSpace(cfg.SessionMode) {
	case OrchestratorSessionAdaptive, OrchestratorSessionPerTurn, OrchestratorSessionStickyUpgrade:
		cfg.SessionMode = strings.TrimSpace(cfg.SessionMode)
	default:
		cfg.SessionMode = OrchestratorSessionAdaptive
	}
	if cfg.StickyContextTokens < 1000 {
		cfg.StickyContextTokens = 8000
	}
	if cfg.LongContextThreshold < 2000 {
		cfg.LongContextThreshold = 32000
	}
	th := cfg.Thresholds
	if !(th.SimpleMedium > 0 && th.SimpleMedium < th.MediumComplex && th.MediumComplex < th.ComplexReasoning && th.ComplexReasoning <= 1.0) {
		cfg.Thresholds = DefaultOrchestratorConfig().Thresholds
	}
	cfg.Tiers.Simple = NormalizeCandidateTags(cfg.Tiers.Simple)
	cfg.Tiers.Medium = NormalizeCandidateTags(cfg.Tiers.Medium)
	cfg.Tiers.Complex = NormalizeCandidateTags(cfg.Tiers.Complex)
	cfg.Tiers.Reasoning = NormalizeCandidateTags(cfg.Tiers.Reasoning)
	return cfg
}

// ValidateOrchestratorConfigJSON validates route_policies.orchestrator_config updates.
func ValidateOrchestratorConfigJSON(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var cfg OrchestratorConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("orchestrator_config must be a JSON object")
	}
	switch strings.TrimSpace(cfg.SessionMode) {
	case "", OrchestratorSessionAdaptive, OrchestratorSessionPerTurn, OrchestratorSessionStickyUpgrade:
	default:
		return fmt.Errorf("orchestrator_config.session_mode must be one of adaptive, per_turn, sticky_upgrade")
	}
	if cfg.StickyContextTokens < 0 || cfg.StickyContextTokens > 1000000 {
		return fmt.Errorf("orchestrator_config.sticky_context_tokens must be between 0 and 1000000")
	}
	if cfg.LongContextThreshold < 0 || cfg.LongContextThreshold > 2000000 {
		return fmt.Errorf("orchestrator_config.long_context_threshold must be between 0 and 2000000")
	}
	th := cfg.Thresholds
	if th.SimpleMedium != 0 || th.MediumComplex != 0 || th.ComplexReasoning != 0 {
		if !(th.SimpleMedium > 0 && th.SimpleMedium < th.MediumComplex && th.MediumComplex < th.ComplexReasoning && th.ComplexReasoning <= 1.0) {
			return fmt.Errorf("orchestrator_config.thresholds must satisfy 0 < simple_medium < medium_complex < complex_reasoning <= 1")
		}
	}
	return nil
}

func orchestratorTierToInt(tier string) int {
	switch tier {
	case OrchestratorTierSimple:
		return 0
	case OrchestratorTierMedium:
		return 1
	case OrchestratorTierComplex:
		return 2
	case OrchestratorTierReasoning:
		return 3
	default:
		return 1
	}
}

func orchestratorIntToTier(idx int) string {
	switch idx {
	case 0:
		return OrchestratorTierSimple
	case 1:
		return OrchestratorTierMedium
	case 2:
		return OrchestratorTierComplex
	case 3:
		return OrchestratorTierReasoning
	default:
		return OrchestratorTierMedium
	}
}

func orchestratorTierWeights(tierIdx int) policyWeights {
	switch tierIdx {
	case 0: // simple: maximize economy & speed
		return policyWeights{Cost: 0.55, Quality: 0.15, Latency: 0.20, Reliability: 0.10}
	case 2: // complex: prioritize capability & reliability
		return policyWeights{Cost: 0.15, Quality: 0.55, Latency: 0.10, Reliability: 0.20}
	case 3: // reasoning: prioritize strongest frontier quality & reliability
		return policyWeights{Cost: 0.10, Quality: 0.60, Latency: 0.10, Reliability: 0.20}
	default: // medium
		return PresetWeights(StrategyBalanced)
	}
}

func countMatchedMarkers(lower string, markers []string) int {
	hits := 0
	for _, m := range markers {
		if strings.Contains(lower, m) {
			hits++
		}
	}
	return hits
}

var (
	orchCodeMarkers = []string{
		"```", "diff --git", "stack trace", "traceback", "panic:", "func ", "function ",
		"class ", "interface ", "import ", "const ", "select ", ".go:", ".ts:", ".py:", ".rs:",
		"debug", "refactor", "compile", "bug", "sql",
		"重构", "报错", "堆栈", "代码", "接口", "编译", "单元测试",
	}
	orchReasoningMarkers = []string{
		"step by step", "step-by-step", "prove", "architecture", "trade-off", "tradeoff",
		"root cause", "first principles", "design doc", "system design",
		"一步步", "推理", "证明", "架构设计", "方案对比", "根因分析", "深度分析", "权衡", "排查原因", "重构方案",
	}
	orchTechnicalMarkers = []string{
		"concurrency", "deadlock", "race condition", "distributed", "consensus", "kubernetes",
		"postgres", "migration", "transaction", "lock-free", "oauth", "cryptography",
		"algorithm", "complexity", "benchmark", "throughput",
		"并发", "死锁", "分布式", "事务", "索引", "架构", "算法", "性能优化", "安全漏洞", "鉴权", "网关",
	}
	orchMultiStepMarkers = []string{
		"1.", "2.", "3.", "then ", "after that", "finally", "ensure that",
		"第一", "第二", "首先", "其次", "然后", "最后", "并且确保", "同时满足",
	}
	orchSimpleMarkers = []string{
		"hello", "hi ", "hey ", "thanks", "thank you", "translate", "summarize", "summary", "polish", "what is", "who is",
		"你好", "您好", "谢谢", "翻译", "总结", "概括", "润色", "什么是", "解释一下",
	}
)

// ClassifyOrchestratorTask scores a request across deterministic <1ms heuristics
// covering context size, code presence, reasoning markers, domain vocabulary, multi-step
// structure, agent tool depth, and simple QA indicators.
func ClassifyOrchestratorTask(reqCtx RouteContext, cfg OrchestratorConfig) OrchestratorDecision {
	text := strings.TrimSpace(reqCtx.LatestUserText)
	if text == "" {
		text = strings.TrimSpace(reqCtx.KeywordText)
	}
	lower := strings.ToLower(text)

	signals := make([]string, 0, 6)
	longCtx := cfg.LongContextThreshold
	if longCtx < 2000 {
		longCtx = 32000
	}

	// 1. token_count (0.16)
	var normLen float64
	switch {
	case reqCtx.EstimatedInputTokens <= 250:
		normLen = 0
	case reqCtx.EstimatedInputTokens < 4000:
		normLen = 0.50 * float64(reqCtx.EstimatedInputTokens-250) / float64(4000-250)
	case reqCtx.EstimatedInputTokens < longCtx:
		normLen = 0.50 + 0.35*float64(reqCtx.EstimatedInputTokens-4000)/float64(longCtx-4000)
	default:
		normLen = 1.0
	}
	if reqCtx.EstimatedInputTokens >= longCtx {
		signals = append(signals, "long_context")
	} else if reqCtx.EstimatedInputTokens >= 4000 {
		signals = append(signals, "medium_context")
	}

	// 2. code_presence (0.24)
	codeHits := countMatchedMarkers(lower, orchCodeMarkers)
	normCode := min(1.0, float64(codeHits)*0.35)
	if codeHits > 0 {
		signals = append(signals, "code_presence")
	}

	// 3. reasoning_markers (0.26)
	effort := strings.ToLower(strings.TrimSpace(reqCtx.ReasoningEffort))
	var normReason float64
	switch effort {
	case "high", "xhigh":
		normReason = 1.0
		signals = append(signals, "reasoning_effort_high")
	case "medium":
		normReason = 0.65
		signals = append(signals, "reasoning_effort_medium")
	case "low":
		normReason = 0.30
	}
	if containsString(reqCtx.RequiredCapabilities, "reasoning") && normReason < 0.55 {
		normReason = 0.55
	}
	reasonHits := countMatchedMarkers(lower, orchReasoningMarkers)
	if reasonHits > 0 {
		markerScore := min(1.0, float64(reasonHits)*0.40)
		if markerScore > normReason {
			normReason = markerScore
		}
		signals = append(signals, "reasoning_markers")
	}

	// 4. technical_terms (0.16)
	techHits := countMatchedMarkers(lower, orchTechnicalMarkers)
	normTech := min(1.0, float64(techHits)*0.35)
	if techHits > 0 {
		signals = append(signals, "technical_terms")
	}

	// 5. multi_step_patterns (0.10)
	multiHits := countMatchedMarkers(lower, orchMultiStepMarkers)
	normMulti := min(1.0, float64(multiHits)*0.35)
	if multiHits > 1 {
		signals = append(signals, "multi_step")
	}

	// 6. agentic_depth (0.14)
	hasTools := containsString(reqCtx.RequiredCapabilities, "tools")
	var normAgentic float64
	switch {
	case reqCtx.ToolTurnCount >= 4:
		normAgentic = 1.0
	case reqCtx.ToolTurnCount >= 2:
		normAgentic = 0.75
	case reqCtx.ToolTurnCount == 1 || reqCtx.ToolCount >= 5:
		normAgentic = 0.50
	case reqCtx.ToolCount > 0 || hasTools:
		normAgentic = 0.35
	}
	if normAgentic > 0 {
		signals = append(signals, "agentic_tools")
	}

	hasVision := containsString(reqCtx.RequiredCapabilities, "vision")
	hasPDF := containsString(reqCtx.RequiredCapabilities, "pdf_input")
	if hasVision {
		signals = append(signals, "vision")
	}
	if hasPDF {
		signals = append(signals, "pdf_input")
	}

	// 7. simple_indicators (-0.18 adjustment)
	var normSimple float64
	if reqCtx.EstimatedInputTokens < 500 && reqCtx.ToolTurnCount == 0 && normCode == 0 && effort == "" && !hasVision && !hasPDF {
		simpleHits := countMatchedMarkers(lower, orchSimpleMarkers)
		if simpleHits > 0 {
			normSimple = min(1.0, float64(simpleHits)*0.60)
		} else if reqCtx.EstimatedInputTokens <= 80 && multiHits == 0 && techHits == 0 && reasonHits == 0 {
			normSimple = 0.5
		}
		if normSimple > 0 {
			signals = append(signals, "simple_qa")
		}
	}

	dims := map[string]float64{
		"token_count":         0.16 * normLen,
		"code_presence":       0.24 * normCode,
		"reasoning_markers":   0.26 * normReason,
		"technical_terms":     0.16 * normTech,
		"multi_step_patterns": 0.10 * normMulti,
		"agentic_depth":       0.14 * normAgentic,
		"simple_adjustment":   -0.18 * normSimple,
	}

	score := dims["token_count"] + dims["code_presence"] + dims["reasoning_markers"] +
		dims["technical_terms"] + dims["multi_step_patterns"] + dims["agentic_depth"] + dims["simple_adjustment"]
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	th := cfg.Thresholds
	if !(th.SimpleMedium > 0 && th.SimpleMedium < th.MediumComplex && th.MediumComplex < th.ComplexReasoning && th.ComplexReasoning <= 1.0) {
		th = DefaultOrchestratorConfig().Thresholds
	}

	tierIdx := 0
	switch {
	case score >= th.ComplexReasoning:
		tierIdx = 3
	case score >= th.MediumComplex:
		tierIdx = 2
	case score >= th.SimpleMedium:
		tierIdx = 1
	default:
		tierIdx = 0
	}

	// Hard capability & workflow floors.
	if (effort == "high" || effort == "xhigh") && tierIdx < 3 {
		tierIdx = 3
	}
	if reqCtx.EstimatedInputTokens >= longCtx && tierIdx < 2 {
		tierIdx = 2
	}
	if (reqCtx.ToolTurnCount >= 2 || hasVision || hasPDF) && tierIdx < 1 {
		tierIdx = 1
	}

	rawTier := orchestratorIntToTier(tierIdx)
	mode := cfg.SessionMode
	if mode == "" {
		mode = OrchestratorSessionAdaptive
	}

	return OrchestratorDecision{
		Tier:        rawTier,
		RawTier:     rawTier,
		Score:       score,
		SessionMode: mode,
		Signals:     signals,
		Dimensions:  dims,
	}
}

func matchesOrchestratorList(c candidate, patterns []string) bool {
	for _, raw := range patterns {
		pat := strings.TrimSpace(raw)
		if pat == "" {
			continue
		}
		if strings.EqualFold(c.UpstreamModel, pat) {
			return true
		}
		for _, tag := range c.Tags {
			if strings.EqualFold(strings.TrimSpace(tag), pat) {
				return true
			}
		}
	}
	return false
}

func modelNameTierHint(modelName string) float64 {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	if lower == "" {
		return 0
	}
	tokens := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '-' || r == '_' || r == '/' || r == ':'
	})
	tokenSet := make(map[string]bool, len(tokens))
	for _, tok := range tokens {
		tokenSet[tok] = true
	}

	for _, r := range []string{"reasoner", "reasoning", "thinking", "r1", "o1", "o3", "o4", "opus", "max", "ultra"} {
		if tokenSet[r] {
			return 0.25
		}
	}
	if strings.Contains(lower, "5.4") || strings.Contains(lower, "pro") {
		return 0.14
	}
	for _, e := range []string{"air", "mini", "flash", "flashx", "lite", "haiku", "nano", "small", "turbo", "8b", "7b"} {
		if tokenSet[e] {
			return -0.20
		}
	}
	return 0
}

func assignOrchestratorTiers(working []candidate, cfg OrchestratorConfig, compareCurrency string, fxRates map[string]float64) (map[string]int, bool) {
	tierOf := make(map[string]int, len(working))
	unmapped := make([]candidate, 0, len(working))

	for _, c := range working {
		switch {
		case matchesOrchestratorList(c, cfg.Tiers.Reasoning):
			tierOf[c.ID] = 3
		case matchesOrchestratorList(c, cfg.Tiers.Complex):
			tierOf[c.ID] = 2
		case matchesOrchestratorList(c, cfg.Tiers.Medium):
			tierOf[c.ID] = 1
		case matchesOrchestratorList(c, cfg.Tiers.Simple):
			tierOf[c.ID] = 0
		default:
			unmapped = append(unmapped, c)
		}
	}

	if len(unmapped) == 0 {
		return tierOf, false
	}

	maxNormCost := 0.0
	candCost := make(map[string]float64, len(unmapped))
	for _, c := range unmapped {
		rawCost := (c.InputCostPerM + c.OutputCostPerM) / 2.0
		norm, comp := normalizeAmount(rawCost, currencyOf(c), compareCurrency, fxRates)
		if comp && norm > 0 {
			candCost[c.ID] = norm
			if norm > maxNormCost {
				maxNormCost = norm
			}
		}
	}

	strengthOf := make(map[string]float64, len(unmapped))
	for _, c := range unmapped {
		costFactor := 0.0
		if maxNormCost > 0 {
			costFactor = candCost[c.ID] / maxNormCost
		}
		capBonus := 0.0
		if containsString(c.Capabilities, "reasoning") {
			capBonus += 0.08
		}
		if containsString(c.Capabilities, "vision") || containsString(c.Capabilities, "tools") {
			capBonus += 0.04
		}
		strengthOf[c.ID] = c.Quality*0.55 + costFactor*0.25 + capBonus + modelNameTierHint(c.UpstreamModel)
	}

	sort.SliceStable(unmapped, func(i, j int) bool {
		si, sj := strengthOf[unmapped[i].ID], strengthOf[unmapped[j].ID]
		if si != sj {
			return si < sj
		}
		if candCost[unmapped[i].ID] != candCost[unmapped[j].ID] {
			return candCost[unmapped[i].ID] < candCost[unmapped[j].ID]
		}
		if unmapped[i].Quality != unmapped[j].Quality {
			return unmapped[i].Quality < unmapped[j].Quality
		}
		return unmapped[i].UpstreamModel < unmapped[j].UpstreamModel
	})

	n := len(unmapped)
	for i, c := range unmapped {
		var tIdx int
		switch {
		case n == 1:
			tIdx = 1
		case n == 2:
			if i == 0 {
				tIdx = 0
			} else {
				tIdx = 2
			}
		case n == 3:
			if i == 0 {
				tIdx = 0
			} else if i == 1 {
				tIdx = 1
			} else {
				tIdx = 3
			}
		default:
			pct := float64(i) / float64(n-1)
			switch {
			case pct < 0.25:
				tIdx = 0
			case pct < 0.55:
				tIdx = 1
			case pct < 0.85:
				tIdx = 2
			default:
				tIdx = 3
			}
		}
		tierOf[c.ID] = tIdx
	}

	return tierOf, true
}

func evaluateOrchestratorOrder(
	working []candidate,
	policy routePolicy,
	reqCtx RouteContext,
	scores *policyScores,
	evalByID map[string]*CandidateEvaluation,
	evalMetrics map[string]float64,
	tierOf map[string]int,
	targetTierIdx int,
	stickyCandidate *candidate,
	decision *RouteDecision,
	rrCounter uint64,
	prng *mrand.Rand,
) []candidate {
	orderTier := func(items []candidate) []candidate {
		if len(items) == 0 {
			return nil
		}
		copied := append([]candidate(nil), items...)
		sort.SliceStable(copied, func(i, j int) bool { return copied[i].Priority < copied[j].Priority })
		out := make([]candidate, 0, len(copied))
		for start := 0; start < len(copied); {
			end := start + 1
			for end < len(copied) && copied[end].Priority == copied[start].Priority {
				end++
			}
			out = append(out, orderPriorityGroup(copied[start:end], StrategyCustom, evalMetrics, evalByID, scores, rrCounter, prng)...)
			start = end
		}
		return out
	}

	buckets := [4][]candidate{}
	for _, c := range working {
		tIdx := tierOf[c.ID]
		if tIdx < 0 {
			tIdx = 0
		} else if tIdx > 3 {
			tIdx = 3
		}
		buckets[tIdx] = append(buckets[tIdx], c)
	}

	chain := make([]candidate, 0, len(working))
	for t := targetTierIdx; t <= 3; t++ {
		ordered := orderTier(buckets[t])
		if t == targetTierIdx && stickyCandidate != nil && decision.Orchestrator != nil && decision.Orchestrator.CacheProtected && len(ordered) > 0 {
			bestPriority := ordered[0].Priority
			for idx, c := range ordered {
				if c.ID == stickyCandidate.ID && c.Priority == bestPriority {
					if idx > 0 {
						copy(ordered[1:idx+1], ordered[0:idx])
						ordered[0] = c
					}
					decision.AffinityHit = true
					decision.StickyCandidateID = c.ID
					break
				}
			}
		}
		chain = append(chain, ordered...)
	}

	if len(chain) == 0 {
		for t := targetTierIdx - 1; t >= 0; t-- {
			ordered := orderTier(buckets[t])
			chain = append(chain, ordered...)
		}
		if len(chain) > 0 {
			decision.Warnings = append(decision.Warnings, "orchestrator_tier_downgrade_fallback")
		}
	}

	return chain
}
