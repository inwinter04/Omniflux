package dataplane

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"

	"github.com/google/uuid"

	"github.com/inwinter04/omniflux/internal/bifrostx"
)

// candidate 是路由链中的一个上游候选,对应 model_candidates 加上其 provider account 的信息。
type candidate struct {
	ID             string
	Provider       string
	AccountID      string
	UpstreamModel  string
	Priority       int
	Weight         float64
	InputCostPerM  float64
	OutputCostPerM float64
	Quality        float64
}

// policyWeights 是路由策略的评分权重,来自 route_policies.weights JSON。
type policyWeights struct {
	Cost        float64
	Quality     float64
	Latency     float64
	Reliability float64
}

// routePolicy 是数据面路由真正读取的策略字段。
// strategy 字段只在管理端写入 weights 时作为预设模板,调度只认 weights。
type routePolicy struct {
	Strategy          string
	Weights           policyWeights
	FallbackLimit     int
	ProviderTimeoutMS int
}

var defaultPolicy = routePolicy{
	Strategy:          "balanced",
	Weights:           policyWeights{Cost: 0.4, Quality: 0.3, Latency: 0.15, Reliability: 0.15},
	FallbackLimit:     2,
	ProviderTimeoutMS: 60000,
}

// candidateHealth 是候选的历史表现(近 7 天用量统计),用于 latency/reliability 评分。
type candidateHealth struct {
	AvgMS     float64
	Successes int64
	Total     int64
}

func (h candidateHealth) successRate() float64 {
	if h.Total == 0 {
		return 0.5
	}
	return float64(h.Successes) / float64(h.Total)
}

// policyScores 汇总评分调度所需的历史统计;键是候选 ID。
type policyScores struct {
	Health map[uuid.UUID]candidateHealth
}

// routeChain 是按候选顺序排列的路由链,第一个是主候选,其余依次作为 fallback。
type routeChain []candidate

// scoreOf 计算单个候选的综合分:
//   - cost: 价格越低分越高,组内归一(全等价时 0.5);
//   - quality: 候选配置的 quality_score(管理员按模型能力评估维护,默认 0.5);
//   - latency: 组内平均耗时越低分越高(无历史 0.5);
//   - reliability: 成功率(无历史 0.5)。
func scoreOf(c candidate, all routeChain, scores *policyScores, w policyWeights) float64 {
	costScore := relativeScore(costRate(c), lowestCostRate(all))
	latencyScore := 0.5
	health, ok := scores.Health[parseUUID(c.ID)]
	if ok && health.Total > 0 {
		latencyScore = relativeScore(health.AvgMS, fastestAvgMS(all, scores))
	}
	reliabilityScore := 0.5
	if ok && health.Total > 0 {
		reliabilityScore = health.successRate()
	}
	return w.Cost*costScore + w.Quality*c.Quality + w.Latency*latencyScore + w.Reliability*reliabilityScore
}

// costRate 是每百万 token 的混合参考价(输入/输出等权)。
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

// relativeScore 把"数值越低越好"的指标转成 0~1 分:best/value(全等时 0.5)。
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

// selectChain 计算路由链:
//  1. priority 升序分组,决定主备层级;
//  2. 组内按策略评分降序;评分完全一致时退回 weight 加权随机;
//  3. fallbackLimit 截断总数(<=0 不限制)。
//
// scores 为 nil 时(无历史数据可查的调用路径)全部候选评分相同,行为等价于旧的加权随机。
func selectChain(cands []candidate, policy routePolicy, scores *policyScores) routeChain {
	sorted := make([]candidate, len(cands))
	copy(sorted, cands)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority < sorted[j].Priority })

	chain := make(routeChain, 0, len(sorted))
	start := 0
	for start < len(sorted) {
		end := start + 1
		for end < len(sorted) && sorted[end].Priority == sorted[start].Priority {
			end++
		}
		chain = append(chain, orderGroup(sorted[start:end], policy, scores)...)
		start = end
	}
	if policy.FallbackLimit > 0 && policy.FallbackLimit < len(chain) {
		chain = chain[:policy.FallbackLimit]
	}
	return chain
}

// orderGroup 对同优先级组排序:先按评分降序,再对并列者做加权随机补位。
func orderGroup(group []candidate, policy routePolicy, scores *policyScores) []candidate {
	if len(group) <= 1 {
		return group
	}
	scored := make([]struct {
		c     candidate
		score float64
	}, len(group))
	for i, c := range group {
		scored[i] = struct {
			c     candidate
			score float64
		}{c, scoreOf(c, group, scores, policy.Weights)}
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })

	result := make([]candidate, 0, len(group))
	for idx := 0; idx < len(scored); {
		tieStart := idx
		tieEnd := idx + 1
		for tieEnd < len(scored) && scored[tieEnd].score == scored[tieStart].score {
			tieEnd++
		}
		if tieEnd-tieStart == 1 {
			result = append(result, scored[tieStart].c)
			idx = tieEnd
			continue
		}
		tied := make([]candidate, 0, tieEnd-tieStart)
		for i := tieStart; i < tieEnd; i++ {
			tied = append(tied, scored[i].c)
		}
		// 评分并列的候选保留权重语义:加权随机决定先后。
		result = append(result, weightedOrder(tied)...)
		idx = tieEnd
	}
	return result
}

// weightedOrder 对候选做 weight 加权不放回抽样:每次按 weight(<=0 按 1)随机抽出一个。
func weightedOrder(group []candidate) []candidate {
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
		pick, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			// 加密随机源不可用属于致命环境问题,直接顺序回退。
			return remaining
		}
		point := float64(pick.Int64()) / 1_000_000 * total
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

// costUSD 按 input/output 每百万 token 单价计算成本(美元)。
func (c candidate) costUSD(inputTokens, outputTokens int64) float64 {
	return (float64(inputTokens)*c.InputCostPerM + float64(outputTokens)*c.OutputCostPerM) / 1_000_000
}

// findCandidate 按 provider + upstream model 在链中定位实际服务请求的候选。
// Bifrost 返回的是映射后的 provider 名(如 openai),候选存的是数据库标识(如 openai_compatible),需先转换再比较。
func (chain routeChain) findCandidate(provider, model string) (candidate, error) {
	for _, c := range chain {
		mapped, err := bifrostx.MapProvider(c.Provider)
		if err != nil {
			continue
		}
		if string(mapped) == provider && c.UpstreamModel == model {
			return c, nil
		}
	}
	return candidate{}, fmt.Errorf("served provider/model (%s, %s) not found in route chain", provider, model)
}
