package dataplane

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"

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
}

// routeChain 是按候选顺序排列的路由链,第一个是主候选,其余依次作为 fallback。
type routeChain []candidate

// selectChain 按 priority 升序排列候选;同优先级组内按 weight 做加权不放回抽样决定先后。
// fallbackLimit 是候选总数上限(含主候选),<= 0 表示不限制。
func selectChain(cands []candidate, fallbackLimit int) routeChain {
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
		chain = append(chain, weightedOrder(sorted[start:end])...)
		start = end
	}
	if fallbackLimit > 0 && fallbackLimit < len(chain) {
		chain = chain[:fallbackLimit]
	}
	return chain
}

// weightedOrder 对同一优先级的候选做加权不放回抽样:每次按 weight(<=0 按 1)随机抽出一个。
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
