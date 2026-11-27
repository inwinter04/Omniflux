package dataplane

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	mrand "math/rand/v2"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/inwinter04/omniflux/internal/bifrostx"
	"github.com/inwinter04/omniflux/internal/fx"
)

// candidate 是路由链中的一个上游候选,对应 model_candidates 加上其 provider account 的信息。
type candidate struct {
	ID               string
	Provider         string
	APIProtocol      string
	AccountID        string
	AccountName      string
	UpstreamModel    string
	Priority         int
	Weight           float64
	InputCostPerM    float64
	OutputCostPerM   float64
	CacheReadCostPerM  *float64
	CacheWriteCostPerM *float64
	CostCurrency     string
	Quality          float64
	ContextWindow    *int
	Capabilities     []string
	Tags             []string
}

// policyWeights 是路由策略的评分权重,来自 route_policies.weights JSON。
type policyWeights struct {
	Cost        float64 `json:"cost"`
	Quality     float64 `json:"quality"`
	Latency     float64 `json:"latency"`
	Reliability float64 `json:"reliability"`
}

// routePolicy 是数据面路由真正读取的策略字段。
type routePolicy struct {
	ID                  string
	Version             int
	Strategy            string
	CompareCurrency     string
	Weights             policyWeights
	FallbackLimit       int
	ProviderTimeoutMS   int
	FirstTokenTimeoutMS int
	Constraints         policyConstraints
	Rules               []RouteRule
	RetryPolicy         RetryPolicy
	CircuitBreaker      CircuitBreakerPolicy
	SessionAffinity     SessionAffinityPolicy
	Orchestrator        OrchestratorConfig
}

var defaultPolicy = routePolicy{
	Version:             1,
	Strategy:            StrategyBalanced,
	CompareCurrency:     "USD",
	Weights:             policyWeights{Cost: 0.4, Quality: 0.3, Latency: 0.15, Reliability: 0.15},
	FallbackLimit:       3,
	ProviderTimeoutMS:   60000,
	FirstTokenTimeoutMS: 45000,
	RetryPolicy:         DefaultRetryPolicy(),
	CircuitBreaker:      DefaultCircuitBreakerPolicy(),
	SessionAffinity:     DefaultSessionAffinityPolicy(),
	Orchestrator:        DefaultOrchestratorConfig(),
}

// candidateHealth 是候选的表现统计:兼顾流式 TTFT、总延迟与成功率平滑评分。
type candidateHealth struct {
	AvgMS     float64
	AvgTTFTMS float64
	TTFTCount int64
	Successes int64
	Total     int64
}

// successRate 返回 Bayesian/先验平滑后的可用率，避免新候选 1 次成功就锁死 100%。
func (h candidateHealth) successRate() float64 {
	if h.Total == 0 {
		return 0.5
	}
	return float64(h.Successes+2) / float64(h.Total+4)
}

// policyScores 汇总评分调度所需的统计、账号主动探测及短期熔断状态。
type policyScores struct {
	Health        map[uuid.UUID]candidateHealth
	AccountHealth map[uuid.UUID]string // provider_account_id -> 主动探测状态
	Circuits      map[uuid.UUID]CircuitState
}

// routeChain 是按候选顺序排列的路由链,第一个是主候选,其余依次作为 fallback。
type routeChain []candidate

// CandidateEvaluation 记录单个候选在一次路由决策中的完整过滤、归一化成本与评分明细。
type CandidateEvaluation struct {
	CandidateID         string   `json:"candidate_id"`
	Provider            string   `json:"provider"`
	AccountID           string   `json:"account_id"`
	AccountName         string   `json:"account_name,omitempty"`
	UpstreamModel       string   `json:"upstream_model"`
	Priority            int      `json:"priority"`
	Weight              float64  `json:"weight"`
	Selected            bool     `json:"selected"`
	Rank                int      `json:"rank,omitempty"`
	ExcludedReason      string   `json:"excluded_reason,omitempty"`
	CircuitOpen         bool     `json:"circuit_open"`
	AccountHealth       string   `json:"account_health,omitempty"`
	CostCurrency        string   `json:"cost_currency"`
	CompareCurrency     string   `json:"compare_currency"`
	EstimatedCost       float64  `json:"estimated_cost"`
	CostComparable      bool     `json:"cost_comparable"`
	CostScore           float64  `json:"cost_score"`
	QualityScore        float64  `json:"quality_score"`
	LatencyMS           *float64 `json:"latency_ms,omitempty"`
	LatencyMetric       string   `json:"latency_metric,omitempty"`
	LatencyScore        float64  `json:"latency_score"`
	ReliabilityRate     float64  `json:"reliability_rate"`
	ReliabilityScore    float64  `json:"reliability_score"`
	TotalScore          float64  `json:"total_score"`
	WeightProbability   *float64 `json:"weight_probability,omitempty"`
}

// RouteDecision 是单次路由决策的结构化结果，供真实调用留痕与 Dry-Run 模拟器共用。
type RouteDecision struct {
	Chain             routeChain            `json:"-"`
	ChainIDs          []string              `json:"chain_candidate_ids"`
	PolicyID          string                `json:"policy_id,omitempty"`
	PolicyVersion     int                   `json:"policy_version"`
	Strategy          string                `json:"strategy"`
	EffectiveStrategy string                `json:"effective_strategy"`
	CompareCurrency   string                `json:"compare_currency"`
	Weights           policyWeights         `json:"weights"`
	MatchedRuleID     string                `json:"matched_rule_id,omitempty"`
	MatchedRuleName   string                `json:"matched_rule_name,omitempty"`
	RandomSeed        int64                 `json:"random_seed,omitempty"`
	AffinityHit       bool                  `json:"affinity_hit,omitempty"`
	StickyCandidateID string                `json:"sticky_candidate_id,omitempty"`
	Orchestrator      *OrchestratorDecision `json:"orchestrator,omitempty"`
	SessionKey        string                `json:"-"`
	Protocol          string                `json:"-"`
	Warnings          []string              `json:"warnings,omitempty"`
	Evaluations       []CandidateEvaluation `json:"evaluations"`
}

// SimCandidate 与 EvaluateSimulation 为管理面模拟器与跨包调用提供无 DB 的纯路由接口。
type SimCandidate struct {
	ID                 string
	Provider           string
	APIProtocol        string
	AccountID          string
	AccountName        string
	UpstreamModel      string
	Priority           int
	Weight             float64
	InputCostPerM      float64
	OutputCostPerM     float64
	CacheReadCostPerM  *float64
	CacheWriteCostPerM *float64
	CostCurrency       string
	Quality            float64
	ContextWindow      *int
	Capabilities       []string
	Tags               []string
}

type SimPolicy struct {
	ID                  string
	Version             int
	Strategy            string
	CompareCurrency     string
	Weights             policyWeights
	FallbackLimit       int
	ProviderTimeoutMS   int
	FirstTokenTimeoutMS int
	AllowedProviders    []string
	BlockedProviders    []string
	AllowedModels       []string
	BlockedModels       []string
	MaxCostPerMillion   *float64
	Rules               []RouteRule
	RetryPolicy         RetryPolicy
	CircuitBreaker      CircuitBreakerPolicy
	SessionAffinity     SessionAffinityPolicy
	Orchestrator        OrchestratorConfig
}

func (p *SimPolicy) ApplyWeightsJSON(raw []byte) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	p.Weights = parsePolicyWeights(raw)
}

func (p *SimPolicy) ApplyConstraintsJSON(raw []byte) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	c := parsePolicyConstraints(raw)
	p.AllowedProviders = c.AllowedProviders
	p.BlockedProviders = c.BlockedProviders
	p.AllowedModels = c.AllowedUpstreamModel
	p.BlockedModels = c.BlockedUpstreamModel
	p.MaxCostPerMillion = c.MaxCostPerMillion
}

type SimHealth struct {
	AvgMS         float64
	AvgTTFTMS     float64
	TTFTCount     int64
	Successes     int64
	Total         int64
	AccountHealth string
	CircuitOpen   bool
}

func EvaluateSimulation(cands []SimCandidate, policy SimPolicy, reqCtx RouteContext, healthByCand map[string]SimHealth, fxRates map[string]float64, rrCounter uint64) RouteDecision {
	internalCands := make([]candidate, len(cands))
	scores := &policyScores{
		Health:        make(map[uuid.UUID]candidateHealth, len(cands)),
		AccountHealth: make(map[uuid.UUID]string, len(cands)),
		Circuits:      make(map[uuid.UUID]CircuitState, len(cands)),
	}
	for i, c := range cands {
		internalCands[i] = candidate{
			ID: c.ID, Provider: c.Provider, APIProtocol: c.APIProtocol, AccountID: c.AccountID, AccountName: c.AccountName,
			UpstreamModel: c.UpstreamModel, Priority: c.Priority, Weight: c.Weight,
			InputCostPerM: c.InputCostPerM, OutputCostPerM: c.OutputCostPerM,
			CacheReadCostPerM: c.CacheReadCostPerM, CacheWriteCostPerM: c.CacheWriteCostPerM,
			CostCurrency: c.CostCurrency,
			Quality: c.Quality, ContextWindow: c.ContextWindow, Capabilities: c.Capabilities, Tags: c.Tags,
		}
		if h, ok := healthByCand[c.ID]; ok {
			if cid := parseUUID(c.ID); cid != uuid.Nil {
				scores.Health[cid] = candidateHealth{
					AvgMS: h.AvgMS, AvgTTFTMS: h.AvgTTFTMS, TTFTCount: h.TTFTCount,
					Successes: h.Successes, Total: h.Total,
				}
				if h.CircuitOpen {
					scores.Circuits[cid] = CircuitState{Open: true}
				}
			}
			if aid := parseUUID(c.AccountID); aid != uuid.Nil && h.AccountHealth != "" {
				scores.AccountHealth[aid] = h.AccountHealth
			}
		}
	}
	rp := routePolicy{
		ID: policy.ID, Version: policy.Version, Strategy: policy.Strategy,
		CompareCurrency: policy.CompareCurrency, Weights: policy.Weights,
		FallbackLimit: policy.FallbackLimit, ProviderTimeoutMS: policy.ProviderTimeoutMS,
		FirstTokenTimeoutMS: policy.FirstTokenTimeoutMS,
		Constraints: policyConstraints{
			AllowedProviders:     policy.AllowedProviders,
			BlockedProviders:     policy.BlockedProviders,
			AllowedUpstreamModel: policy.AllowedModels,
			BlockedUpstreamModel: policy.BlockedModels,
			MaxCostPerMillion:    policy.MaxCostPerMillion,
		},
		Rules: policy.Rules, RetryPolicy: policy.RetryPolicy, CircuitBreaker: policy.CircuitBreaker,
		SessionAffinity: policy.SessionAffinity, Orchestrator: policy.Orchestrator,
	}
	return EvaluateRoute(internalCands, rp, reqCtx, scores, fxRates, rrCounter)
}

// selectChain 兼容原有调用签名，内部委托 EvaluateRoute 统一计算路由链。
func selectChain(cands []candidate, policy routePolicy, scores *policyScores) routeChain {
	decision := EvaluateRoute(cands, policy, RouteContext{}, scores, nil, 0)
	return decision.Chain
}

// EvaluateRoute 执行完整路由决策（条件规则 → 硬约束与能力 → 熔断过滤 → 归一化评分/调度排序）。
func EvaluateRoute(cands []candidate, policy routePolicy, reqCtx RouteContext, scores *policyScores, fxRates map[string]float64, rrCounter uint64) RouteDecision {
	if scores == nil {
		scores = &policyScores{
			Health:        map[uuid.UUID]candidateHealth{},
			AccountHealth: map[uuid.UUID]string{},
			Circuits:      map[uuid.UUID]CircuitState{},
		}
	}
	compareCurrency := strings.ToUpper(strings.TrimSpace(policy.CompareCurrency))
	if compareCurrency == "" {
		compareCurrency = "USD"
	}
	effStrategy := strings.TrimSpace(policy.Strategy)
	if effStrategy == "" {
		effStrategy = StrategyBalanced
	}
	effWeights := policy.Weights
	if effWeights.Cost+effWeights.Quality+effWeights.Latency+effWeights.Reliability <= 0 {
		effWeights = PresetWeights(effStrategy)
	}
	seed := reqCtx.RandomSeed
	if seed == 0 {
		seed = cryptoSeed()
	}

	decision := RouteDecision{
		PolicyID:          policy.ID,
		PolicyVersion:     policy.Version,
		Strategy:          effStrategy,
		EffectiveStrategy: effStrategy,
		CompareCurrency:   compareCurrency,
		Weights:           effWeights,
		RandomSeed:        seed,
	}
	if len(cands) == 0 {
		return decision
	}

	evalByID := make(map[string]*CandidateEvaluation, len(cands))
	for _, c := range cands {
		accHealth := "unknown"
		if aid := parseUUID(c.AccountID); aid != uuid.Nil {
			if st, ok := scores.AccountHealth[aid]; ok && st != "" {
				accHealth = st
			}
		}
		cbOpen := false
		if cid := parseUUID(c.ID); cid != uuid.Nil {
			cbOpen = scores.Circuits[cid].Open
		}
		evalByID[c.ID] = &CandidateEvaluation{
			CandidateID:     c.ID,
			Provider:        c.Provider,
			AccountID:       c.AccountID,
			AccountName:     c.AccountName,
			UpstreamModel:   c.UpstreamModel,
			Priority:        c.Priority,
			Weight:          c.Weight,
			CircuitOpen:     cbOpen,
			AccountHealth:   accHealth,
			CostCurrency:    currencyOf(c),
			CompareCurrency: compareCurrency,
			QualityScore:    c.Quality,
		}
	}

	// 1) Phase 2 条件规则匹配 (按 priority 升序 first-match-wins)。
	working := make([]candidate, len(cands))
	copy(working, cands)
	if matched := matchFirstRule(policy.Rules, reqCtx); matched != nil {
		decision.MatchedRuleID = matched.ID
		decision.MatchedRuleName = matched.Name
		if strings.TrimSpace(matched.Target.Strategy) != "" {
			decision.EffectiveStrategy = strings.TrimSpace(matched.Target.Strategy)
			if decision.EffectiveStrategy != StrategyCustom &&
				decision.EffectiveStrategy != StrategyWeighted &&
				decision.EffectiveStrategy != StrategyRoundRobin &&
				decision.EffectiveStrategy != StrategyStrictFailover {
				decision.Weights = PresetWeights(decision.EffectiveStrategy)
			}
		}
		if ruleFiltered, excluded := filterByRuleTarget(working, matched.Target); len(ruleFiltered) > 0 {
			for id, reason := range excluded {
				if ev := evalByID[id]; ev != nil && ev.ExcludedReason == "" {
					ev.ExcludedReason = reason
				}
			}
			working = ruleFiltered
		} else if len(working) > 0 && hasRuleFilters(matched.Target) {
			decision.Warnings = append(decision.Warnings, "rule_filter_empty_fallback")
		}
	}

	// 2) 硬约束过滤 (供应商/模型黑白名单、FX 归一化混合价上限)。
	hardFiltered, hardReasons := policy.Constraints.filterWithFX(working, compareCurrency, fxRates)
	for id, reason := range hardReasons {
		if ev := evalByID[id]; ev != nil && ev.ExcludedReason == "" {
			ev.ExcludedReason = reason
		}
	}
	working = hardFiltered

	// 3) 上下文窗口与能力匹配过滤。
	capFiltered := make([]candidate, 0, len(working))
	enforceCaps := anyCandidateSupportsAll(working, reqCtx.RequiredCapabilities)
	for _, c := range working {
		if reqCtx.EstimatedInputTokens > 0 && c.ContextWindow != nil && *c.ContextWindow > 0 && reqCtx.EstimatedInputTokens > *c.ContextWindow {
			if ev := evalByID[c.ID]; ev != nil && ev.ExcludedReason == "" {
				ev.ExcludedReason = "context_window_exceeded"
			}
			continue
		}
		if len(reqCtx.RequiredCapabilities) > 0 {
			if len(c.Capabilities) > 0 && !supportsAllCaps(c.Capabilities, reqCtx.RequiredCapabilities) {
				if ev := evalByID[c.ID]; ev != nil && ev.ExcludedReason == "" {
					ev.ExcludedReason = "missing_capability"
				}
				continue
			}
			if len(c.Capabilities) == 0 && enforceCaps {
				if ev := evalByID[c.ID]; ev != nil && ev.ExcludedReason == "" {
					ev.ExcludedReason = "missing_capability"
				}
				continue
			}
		}
		capFiltered = append(capFiltered, c)
	}
	working = capFiltered

	// 4) 短期熔断器过滤 (含全熔断兜底)。
	healthyCircuits := make([]candidate, 0, len(working))
	openCircuits := make([]candidate, 0)
	for _, c := range working {
		if st := scores.Circuits[parseUUID(c.ID)]; st.Open {
			openCircuits = append(openCircuits, c)
		} else {
			healthyCircuits = append(healthyCircuits, c)
		}
	}
	if len(healthyCircuits) > 0 {
		for _, c := range openCircuits {
			if ev := evalByID[c.ID]; ev != nil && ev.ExcludedReason == "" {
				ev.ExcludedReason = "circuit_open"
			}
		}
		working = healthyCircuits
	} else if len(openCircuits) > 0 {
		// 全候选都在冷却中时按冷却最早到期排序恢复兜底，避免流量死锁。
		sort.SliceStable(openCircuits, func(i, j int) bool {
			si := scores.Circuits[parseUUID(openCircuits[i].ID)].CooldownUntil
			sj := scores.Circuits[parseUUID(openCircuits[j].ID)].CooldownUntil
			if si == nil || sj == nil {
				return si == nil && sj != nil
			}
			return si.Before(*sj)
		})
		working = openCircuits
		decision.Warnings = append(decision.Warnings, "all_circuits_open_fallback")
	}

	// 5) 对最终参与调度的候选计算归一化成本与各维评分明细。
	inTok := float64(reqCtx.EstimatedInputTokens)
	outTok := float64(reqCtx.EstimatedOutputTokens)
	if inTok <= 0 && outTok <= 0 {
		inTok, outTok = 500_000, 500_000
	}
	type costCalc struct {
		est        float64
		comparable bool
	}
	costs := make(map[string]costCalc, len(working))
	bestCost := 0.0
	hasBestCost := false
	bestLat := 0.0
	hasBestLat := false
	hasNonComparableCost := false

	for _, c := range working {
		rawReqCost := (inTok*c.InputCostPerM + outTok*c.OutputCostPerM) / 1_000_000
		normCost, comp := normalizeAmount(rawReqCost, currencyOf(c), compareCurrency, fxRates)
		costs[c.ID] = costCalc{est: normCost, comparable: comp}
		if comp {
			if !hasBestCost || normCost < bestCost {
				bestCost = normCost
				hasBestCost = true
			}
		} else {
			hasNonComparableCost = true
		}

		latVal, _ := candidateLatencyVal(c, reqCtx.Stream, scores)
		if latVal > 0 && (!hasBestLat || latVal < bestLat) {
			bestLat = latVal
			hasBestLat = true
		}
	}
	if hasNonComparableCost {
		decision.Warnings = append(decision.Warnings, "missing_fx_rate_neutral_cost")
	}

	evalMetrics := make(map[string]float64, len(working))
	var (
		orchTierOf       map[string]int
		orchTargetTier   int
		orchStickyCand   *candidate
		isOrchestrator   = decision.EffectiveStrategy == StrategyAutoOrchestrator
	)
	if isOrchestrator {
		var autoBucketed bool
		orchTierOf, autoBucketed = assignOrchestratorTiers(working, policy.Orchestrator, compareCurrency, fxRates)
		orchDec := ClassifyOrchestratorTask(reqCtx, policy.Orchestrator)
		orchDec.AutoBucketed = autoBucketed
		orchTargetTier = orchestratorTierToInt(orchDec.RawTier)

		stickyThresh := policy.Orchestrator.StickyContextTokens
		if stickyThresh <= 0 {
			stickyThresh = 8000
		}
		protectCache := false
		switch orchDec.SessionMode {
		case OrchestratorSessionStickyUpgrade:
			protectCache = true
		case OrchestratorSessionPerTurn:
			protectCache = false
		default: // adaptive
			protectCache = reqCtx.EstimatedInputTokens >= stickyThresh || reqCtx.ToolTurnCount >= 2
		}

		if policy.SessionAffinity.Enabled && protectCache && strings.TrimSpace(reqCtx.PreferredCandidateID) != "" {
			for i := range working {
				if working[i].ID == reqCtx.PreferredCandidateID {
					orchStickyCand = &working[i]
					stickyTierIdx := orchTierOf[orchStickyCand.ID]
					if orchTargetTier <= stickyTierIdx {
						orchTargetTier = stickyTierIdx
						orchDec.CacheProtected = true
						orchDec.Tier = orchestratorIntToTier(stickyTierIdx)
					} else {
						orchDec.UpgradedFromTier = orchestratorIntToTier(stickyTierIdx)
					}
					break
				}
			}
		}

		decision.Weights = orchestratorTierWeights(orchTargetTier)
		decision.Orchestrator = &orchDec
	}

	for _, c := range working {
		ev := evalByID[c.ID]
		cc := costs[c.ID]
		ev.EstimatedCost = cc.est
		ev.CostComparable = cc.comparable
		if cc.comparable {
			ev.CostScore = relativeScore(cc.est, bestCost)
		} else {
			ev.CostScore = 0.5
		}
		latVal, latMetric := candidateLatencyVal(c, reqCtx.Stream, scores)
		ev.LatencyMetric = latMetric
		if latVal > 0 {
			v := latVal
			ev.LatencyMS = &v
			ev.LatencyScore = relativeScore(latVal, bestLat)
		} else {
			ev.LatencyScore = 0.5
		}
		rel := 0.5
		if h, ok := scores.Health[parseUUID(c.ID)]; ok {
			rel = h.successRate()
		}
		ev.ReliabilityRate = rel
		ev.ReliabilityScore = rel
		ev.TotalScore = decision.Weights.Cost*ev.CostScore +
			decision.Weights.Quality*ev.QualityScore +
			decision.Weights.Latency*ev.LatencyScore +
			decision.Weights.Reliability*ev.ReliabilityScore
		evalMetrics[c.ID] = ev.TotalScore
	}

	// 6) 按优先级分组 + 策略模式排序 (unhealthy 账号自动软降权到同优先级组末尾)。
	sort.SliceStable(working, func(i, j int) bool { return working[i].Priority < working[j].Priority })
	chain := make(routeChain, 0, len(working))
	prng := mrand.New(mrand.NewPCG(uint64(seed), uint64(seed)^0x9e3779b97f4a7c15))

	if isOrchestrator {
		chain = evaluateOrchestratorOrder(working, policy, reqCtx, scores, evalByID, evalMetrics, orchTierOf, orchTargetTier, orchStickyCand, &decision, rrCounter, prng)
	} else {
		for start := 0; start < len(working); {
			end := start + 1
			for end < len(working) && working[end].Priority == working[start].Priority {
				end++
			}
			orderedGroup := orderPriorityGroup(working[start:end], decision.EffectiveStrategy, evalMetrics, evalByID, scores, rrCounter, prng)
			chain = append(chain, orderedGroup...)
			start = end
		}

		// 6.5) 粘性会话亲和：仅当优选候选位于同最高优先级组且通过硬约束与熔断检查时置顶。
		if policy.SessionAffinity.Enabled && strings.TrimSpace(reqCtx.PreferredCandidateID) != "" && len(chain) > 0 {
			bestPriority := chain[0].Priority
			for idx, c := range chain {
				if c.ID == reqCtx.PreferredCandidateID && c.Priority == bestPriority {
					if idx > 0 {
						copy(chain[1:idx+1], chain[0:idx])
						chain[0] = c
					}
					decision.AffinityHit = true
					decision.StickyCandidateID = c.ID
					break
				}
			}
		}
	}

	// 7) 按 max_attempts (FallbackLimit) 截断链条。
	if policy.FallbackLimit > 0 && policy.FallbackLimit < len(chain) {
		for _, dropped := range chain[policy.FallbackLimit:] {
			if ev := evalByID[dropped.ID]; ev != nil && ev.ExcludedReason == "" {
				ev.ExcludedReason = "exceeds_max_attempts"
			}
		}
		chain = chain[:policy.FallbackLimit]
	}

	chainIDs := make([]string, len(chain))
	for i, c := range chain {
		chainIDs[i] = c.ID
		if ev := evalByID[c.ID]; ev != nil {
			ev.Selected = true
			ev.Rank = i + 1
		}
	}
	decision.Chain = chain
	decision.ChainIDs = chainIDs
	evals := make([]CandidateEvaluation, 0, len(cands))
	for _, c := range cands {
		evals = append(evals, *evalByID[c.ID])
	}
	decision.Evaluations = evals
	return decision
}

func orderPriorityGroup(group []candidate, strategy string, scoreMap map[string]float64, evalByID map[string]*CandidateEvaluation, scores *policyScores, rrCounter uint64, prng *mrand.Rand) []candidate {
	if len(group) <= 1 {
		if len(group) == 1 && strategy == StrategyWeighted {
			if ev := evalByID[group[0].ID]; ev != nil {
				p := 1.0
				ev.WeightProbability = &p
			}
		}
		return group
	}
	healthy := make([]candidate, 0, len(group))
	unhealthy := make([]candidate, 0)
	for _, c := range group {
		if isAccountUnhealthy(c, scores) {
			unhealthy = append(unhealthy, c)
		} else {
			healthy = append(healthy, c)
		}
	}
	orderSlice := func(items []candidate) []candidate {
		if len(items) <= 1 {
			if len(items) == 1 && strategy == StrategyWeighted {
				if ev := evalByID[items[0].ID]; ev != nil {
					p := 1.0
					ev.WeightProbability = &p
				}
			}
			return items
		}
		switch strategy {
		case StrategyStrictFailover:
			copied := append([]candidate(nil), items...)
			sort.SliceStable(copied, func(i, j int) bool {
				return copied[i].Weight > copied[j].Weight
			})
			return copied
		case StrategyRoundRobin:
			copied := append([]candidate(nil), items...)
			sort.SliceStable(copied, func(i, j int) bool { return copied[i].ID < copied[j].ID })
			offset := int(rrCounter % uint64(len(copied)))
			return append(copied[offset:], copied[:offset]...)
		case StrategyWeighted:
			totalW := 0.0
			for _, item := range items {
				w := item.Weight
				if w <= 0 {
					w = 1
				}
				totalW += w
			}
			if totalW > 0 {
				for _, item := range items {
					w := item.Weight
					if w <= 0 {
						w = 1
					}
					prob := w / totalW
					if ev := evalByID[item.ID]; ev != nil {
						p := prob
						ev.WeightProbability = &p
					}
				}
			}
			return weightedOrderWithPRNG(items, prng)
		default:
			scored := make([]candidate, len(items))
			copy(scored, items)
			sort.SliceStable(scored, func(i, j int) bool {
				si, sj := scoreMap[scored[i].ID], scoreMap[scored[j].ID]
				if math.Abs(si-sj) > 1e-12 {
					return si > sj
				}
				return false
			})
			result := make([]candidate, 0, len(scored))
			for idx := 0; idx < len(scored); {
				tieEnd := idx + 1
				for tieEnd < len(scored) && math.Abs(scoreMap[scored[tieEnd].ID]-scoreMap[scored[idx].ID]) <= 1e-12 {
					tieEnd++
				}
				if tieEnd-idx == 1 {
					result = append(result, scored[idx])
				} else {
					result = append(result, weightedOrderWithPRNG(scored[idx:tieEnd], prng)...)
				}
				idx = tieEnd
			}
			return result
		}
	}
	result := make([]candidate, 0, len(group))
	if len(healthy) > 0 {
		result = append(result, orderSlice(healthy)...)
	}
	if len(unhealthy) > 0 {
		result = append(result, orderSlice(unhealthy)...)
	}
	return result
}

func matchFirstRule(rules []RouteRule, reqCtx RouteContext) *RouteRule {
	if len(rules) == 0 {
		return nil
	}
	sorted := make([]RouteRule, 0, len(rules))
	for _, r := range rules {
		if r.Enabled {
			sorted = append(sorted, r)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].ID < sorted[j].ID
	})
	for i := range sorted {
		if ruleMatches(sorted[i].When, reqCtx) {
			return &sorted[i]
		}
	}
	return nil
}

func ruleMatches(w RouteRuleWhen, reqCtx RouteContext) bool {
	if w.MinInputTokens != nil && reqCtx.EstimatedInputTokens < *w.MinInputTokens {
		return false
	}
	if w.MaxInputTokens != nil && reqCtx.EstimatedInputTokens > *w.MaxInputTokens {
		return false
	}
	if w.Stream != nil && reqCtx.Stream != *w.Stream {
		return false
	}
	if len(w.Capabilities) > 0 && !supportsAllCaps(reqCtx.RequiredCapabilities, w.Capabilities) {
		return false
	}
	if len(w.ProjectIDs) > 0 && !containsString(w.ProjectIDs, reqCtx.ProjectID) {
		return false
	}
	if len(w.ApplicationIDs) > 0 && !containsString(w.ApplicationIDs, reqCtx.ApplicationID) {
		return false
	}
	if len(w.APIKeyIDs) > 0 && !containsString(w.APIKeyIDs, reqCtx.APIKeyID) {
		return false
	}
	if len(w.Keywords) > 0 {
		lowerText := strings.ToLower(reqCtx.KeywordText)
		matchedKW := false
		for _, kw := range w.Keywords {
			trimmed := strings.ToLower(strings.TrimSpace(kw))
			if trimmed != "" && strings.Contains(lowerText, trimmed) {
				matchedKW = true
				break
			}
		}
		if !matchedKW {
			return false
		}
	}
	for metaKey, allowedVals := range w.Metadata {
		if len(allowedVals) == 0 {
			continue
		}
		actual, ok := reqCtx.Metadata[metaKey]
		if !ok || !containsString(allowedVals, actual) {
			return false
		}
	}
	return true
}

func hasRuleFilters(t RouteRuleTarget) bool {
	return len(t.CandidateTags) > 0 || len(t.AllowedProviders) > 0 || len(t.BlockedProviders) > 0 || len(t.AllowedModels) > 0
}

func filterByRuleTarget(cands []candidate, target RouteRuleTarget) ([]candidate, map[string]string) {
	kept := make([]candidate, 0, len(cands))
	excluded := make(map[string]string)
	for _, c := range cands {
		if len(target.AllowedProviders) > 0 && !containsString(target.AllowedProviders, c.Provider) {
			excluded[c.ID] = "rule_excluded"
			continue
		}
		if containsString(target.BlockedProviders, c.Provider) {
			excluded[c.ID] = "rule_excluded"
			continue
		}
		if len(target.AllowedModels) > 0 && !containsString(target.AllowedModels, c.UpstreamModel) {
			excluded[c.ID] = "rule_excluded"
			continue
		}
		if len(target.CandidateTags) > 0 && !hasAnyTag(c.Tags, target.CandidateTags) {
			excluded[c.ID] = "rule_tag_mismatch"
			continue
		}
		kept = append(kept, c)
	}
	return kept, excluded
}

func hasAnyTag(candTags, wanted []string) bool {
	for _, w := range wanted {
		if containsString(candTags, w) {
			return true
		}
	}
	return false
}

func supportsAllCaps(candCaps, required []string) bool {
	for _, req := range required {
		if !containsString(candCaps, strings.ToLower(strings.TrimSpace(req))) {
			return false
		}
	}
	return true
}

func anyCandidateSupportsAll(cands []candidate, required []string) bool {
	if len(required) == 0 {
		return false
	}
	for _, c := range cands {
		if len(c.Capabilities) > 0 && supportsAllCaps(c.Capabilities, required) {
			return true
		}
	}
	return false
}

func normalizeAmount(raw float64, fromCurrency, compareCurrency string, fxRates map[string]float64) (float64, bool) {
	if fromCurrency == "" {
		fromCurrency = "USD"
	}
	if compareCurrency == "" {
		compareCurrency = "USD"
	}
	if fromCurrency == compareCurrency {
		return raw, true
	}
	if len(fxRates) == 0 {
		fxRates = map[string]float64{
			"USD": 1.0, "CNY": 7.2, "EUR": 0.92, "GBP": 0.79,
			"JPY": 154.0, "HKD": 7.8, "TWD": 32.5, "KRW": 1380.0,
		}
	}
	return fx.ConvertWithMap(raw, fromCurrency, compareCurrency, fxRates)
}

func candidateLatencyVal(c candidate, isStream bool, scores *policyScores) (float64, string) {
	if scores == nil {
		return 0, "none"
	}
	h, ok := scores.Health[parseUUID(c.ID)]
	if !ok || h.Total == 0 {
		return 0, "none"
	}
	if isStream && h.TTFTCount > 0 && h.AvgTTFTMS > 0 {
		return h.AvgTTFTMS, "ttft_ms"
	}
	if h.AvgMS > 0 {
		return h.AvgMS, "total_ms"
	}
	return 0, "none"
}

// scoreOf 保留给已有单元测试调用，内部直接对齐最新平滑成功率与归一化成本。
func scoreOf(c candidate, all routeChain, scores *policyScores, w policyWeights) float64 {
	costScore := relativeScore(costRate(c), lowestCostRate(all))
	latencyScore := 0.5
	reliabilityScore := 0.5
	if scores != nil {
		if health, ok := scores.Health[parseUUID(c.ID)]; ok && health.Total > 0 {
			latencyScore = relativeScore(health.AvgMS, fastestAvgMS(all, scores))
			reliabilityScore = health.successRate()
		}
	}
	return w.Cost*costScore + w.Quality*c.Quality + w.Latency*latencyScore + w.Reliability*reliabilityScore
}

func currencyOf(c candidate) string {
	if c.CostCurrency == "" {
		return "USD"
	}
	return c.CostCurrency
}

func costRate(c candidate) float64 {
	return (c.InputCostPerM + c.OutputCostPerM) / 2
}

func lowestCostRate(all routeChain) float64 {
	lowest := 0.0
	for i, c := range all {
		rate := costRate(c)
		if i == 0 || rate < lowest {
			lowest = rate
		}
	}
	return lowest
}

func fastestAvgMS(all routeChain, scores *policyScores) float64 {
	if scores == nil {
		return 0
	}
	fastest := 0.0
	found := false
	for _, c := range all {
		h, ok := scores.Health[parseUUID(c.ID)]
		if !ok || h.Total == 0 {
			continue
		}
		if !found || h.AvgMS < fastest {
			fastest = h.AvgMS
			found = true
		}
	}
	return fastest
}

// relativeScore 把"数值越低越好"的指标转成 0~1 分:best/value(全等或无数据时 0.5 或 1)。
func relativeScore(value, best float64) float64 {
	if value <= 0 || best <= 0 {
		return 0.5
	}
	if best >= value {
		return 1
	}
	return best / value
}

func parseUUID(id string) uuid.UUID {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

func isAccountUnhealthy(c candidate, scores *policyScores) bool {
	if scores == nil || len(scores.AccountHealth) == 0 {
		return false
	}
	return scores.AccountHealth[parseUUID(c.AccountID)] == "unhealthy"
}

// weightedOrder 使用独立加密随机源做加权不放回抽样。
func weightedOrder(group []candidate) []candidate {
	return weightedOrderWithPRNG(group, mrand.New(mrand.NewPCG(uint64(cryptoSeed()), uint64(cryptoSeed()^0x517cc1b727220a95))))
}

func weightedOrderWithPRNG(group []candidate, prng *mrand.Rand) []candidate {
	if len(group) <= 1 {
		return group
	}
	remaining := make([]candidate, len(group))
	copy(remaining, group)
	result := make([]candidate, 0, len(group))
	for len(remaining) > 0 {
		total := 0.0
		weights := make([]float64, len(remaining))
		for i, c := range remaining {
			w := c.Weight
			if w <= 0 {
				w = 1
			}
			weights[i] = w
			total += w
		}
		point := prng.Float64() * total
		chosen := len(remaining) - 1
		for i, w := range weights {
			point -= w
			if point < 0 {
				chosen = i
				break
			}
		}
		result = append(result, remaining[chosen])
		remaining = append(remaining[:chosen], remaining[chosen+1:]...)
	}
	return result
}

func cryptoSeed() int64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		val := int64(binary.LittleEndian.Uint64(buf[:]) & 0x7fffffffffffffff)
		if val > 0 {
			return val
		}
	}
	return 1
}

// costForUsage 按 input/cache_read/cache_write/output 分档计算成本(按候选原结算币种计价)。
// 遵循 Bifrost 约定：InputTokens 已包含 CachedReadTokens 与 CacheCreationTokens，未缓存输入需扣除这两部分避免重复计费。
func (c candidate) costForUsage(u usageSnapshot) float64 {
	input := u.InputTokens
	if input < 0 {
		input = 0
	}
	cachedRead := u.CachedTokens
	if cachedRead < 0 {
		cachedRead = 0
	}
	if cachedRead > input {
		cachedRead = input
	}
	rem := input - cachedRead
	cachedWrite := u.CacheCreationTokens
	if cachedWrite < 0 {
		cachedWrite = 0
	}
	if cachedWrite > rem {
		cachedWrite = rem
	}
	uncached := rem - cachedWrite

	readPrice := c.InputCostPerM
	if c.CacheReadCostPerM != nil {
		readPrice = *c.CacheReadCostPerM
	}
	writePrice := c.InputCostPerM
	if c.CacheWriteCostPerM != nil {
		writePrice = *c.CacheWriteCostPerM
	}
	output := u.OutputTokens
	if output < 0 {
		output = 0
	}
	return (float64(uncached)*c.InputCostPerM +
		float64(cachedRead)*readPrice +
		float64(cachedWrite)*writePrice +
		float64(output)*c.OutputCostPerM) / 1_000_000
}

// costUSD 按 input/output 每百万 token 单价计算成本(按候选原结算币种计价)。
func (c candidate) costUSD(inputTokens, outputTokens int64) float64 {
	return c.costForUsage(usageSnapshot{InputTokens: inputTokens, OutputTokens: outputTokens})
}

func logicalProviderName(provider string) string {
	if mapped, err := bifrostx.MapProvider(provider); err == nil {
		return string(mapped)
	}
	return provider
}

// findCandidate 按 provider + upstream model 在链中定位实际服务请求的候选。
func (chain routeChain) findCandidate(provider, model, accountID string) (candidate, error) {
	for _, c := range chain {
		engineKey, engineErr := bifrostx.EngineProviderKey(c.AccountID, c.Provider, c.APIProtocol)
		mapped, mapErr := bifrostx.MapProvider(c.Provider)
		if engineErr != nil && mapErr != nil {
			continue
		}
		providerMatched := (engineErr == nil && string(engineKey) == provider) || (mapErr == nil && string(mapped) == provider)
		if providerMatched && c.UpstreamModel == model && (accountID == "" || c.AccountID == accountID) {
			return c, nil
		}
	}
	return candidate{}, bifrostFindErr(provider, model)
}

func bifrostFindErr(provider, model string) error {
	return &AgentInputError{Code: "candidate_not_found", Message: "served provider/model (" + provider + ", " + model + ") not found in route chain"}
}

// fallbackSpecs 把主候选之后的链转成 Bifrost 的 fallback 列表(供 embeddings 等路径复用)。
func fallbackSpecs(chain routeChain) []schemas.Fallback {
	if len(chain) <= 1 {
		return nil
	}
	fallbacks := make([]schemas.Fallback, 0, len(chain)-1)
	for _, c := range chain[1:] {
		provider, err := bifrostx.EngineProviderKey(c.AccountID, c.Provider, c.APIProtocol)
		if err != nil {
			continue
		}
		fallbacks = append(fallbacks, schemas.Fallback{Provider: provider, Model: c.UpstreamModel, KeyID: c.AccountID})
	}
	if len(fallbacks) == 0 {
		return nil
	}
	return fallbacks
}
