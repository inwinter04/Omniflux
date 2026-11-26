package dataplane

import (
	"encoding/json"
	"fmt"

	"github.com/inwinter04/omniflux/internal/fx"
)

// policyConstraints 是路由策略的硬约束,来自 route_policies.constraints JSON。
// 空对象表示不限制;命中任一约束的候选会在评分前被过滤。
type policyConstraints struct {
	AllowedProviders     []string `json:"allowed_providers,omitempty"`
	BlockedProviders     []string `json:"blocked_providers,omitempty"`
	AllowedUpstreamModel []string `json:"allowed_upstream_models,omitempty"`
	BlockedUpstreamModel []string `json:"blocked_upstream_models,omitempty"`
	MaxCostPerMillion    *float64 `json:"max_cost_per_million,omitempty"`
}

// parsePolicyConstraints 解析策略约束;空 JSON 或解析失败都视为无约束。
// 管理面在写入时已校验结构,这里对历史脏数据保持宽容。
func parsePolicyConstraints(raw []byte) policyConstraints {
	if len(raw) == 0 {
		return policyConstraints{}
	}
	var c policyConstraints
	if err := json.Unmarshal(raw, &c); err != nil {
		return policyConstraints{}
	}
	return c
}

// containsString 判断列表是否包含目标(精确匹配)。
func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// (c policyConstraints) filter 按硬约束过滤候选(保留向后兼容单参数调用)。
func (c policyConstraints) filter(cands []candidate) []candidate {
	kept, _ := c.filterWithFX(cands, "USD", nil)
	return kept
}

func (c policyConstraints) filterWithFX(cands []candidate, compareCurrency string, fxRates map[string]float64) ([]candidate, map[string]string) {
	kept := make([]candidate, 0, len(cands))
	reasons := make(map[string]string)
	if compareCurrency == "" {
		compareCurrency = "USD"
	}
	for _, cand := range cands {
		if len(c.AllowedProviders) > 0 && !containsString(c.AllowedProviders, cand.Provider) {
			reasons[cand.ID] = "provider_not_allowed"
			continue
		}
		if containsString(c.BlockedProviders, cand.Provider) {
			reasons[cand.ID] = "provider_blocked"
			continue
		}
		if len(c.AllowedUpstreamModel) > 0 && !containsString(c.AllowedUpstreamModel, cand.UpstreamModel) {
			reasons[cand.ID] = "model_not_allowed"
			continue
		}
		if containsString(c.BlockedUpstreamModel, cand.UpstreamModel) {
			reasons[cand.ID] = "model_blocked"
			continue
		}
		if c.MaxCostPerMillion != nil {
			rate := costRateNormalized(cand, compareCurrency, fxRates)
			if rate.comparable && rate.amount > *c.MaxCostPerMillion {
				reasons[cand.ID] = "exceeds_max_cost"
				continue
			}
		}
		kept = append(kept, cand)
	}
	return kept, reasons
}

type normalizedCost struct {
	amount     float64
	comparable bool
}

func costRateNormalized(c candidate, compareCurrency string, fxRates map[string]float64) normalizedCost {
	raw := costRate(c)
	from := currencyOf(c)
	if from == compareCurrency || len(fxRates) == 0 && from == "USD" && compareCurrency == "USD" {
		return normalizedCost{amount: raw, comparable: true}
	}
	if len(fxRates) == 0 {
		fxRates = map[string]float64{
			"USD": 1.0, "CNY": 7.2, "EUR": 0.92, "GBP": 0.79,
			"JPY": 154.0, "HKD": 7.8, "TWD": 32.5, "KRW": 1380.0,
		}
	}
	converted, ok := fx.ConvertWithMap(raw, from, compareCurrency, fxRates)
	if !ok {
		return normalizedCost{amount: raw, comparable: false}
	}
	return normalizedCost{amount: converted, comparable: true}
}

// ValidateConstraintsJSON 校验管理面写入的约束结构;返回用户可读的错误信息。
func ValidateConstraintsJSON(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("constraints is not a valid JSON object")
	}
	allowedKeys := map[string]bool{
		"allowed_providers": true, "blocked_providers": true,
		"allowed_upstream_models": true, "blocked_upstream_models": true,
		"max_cost_per_million": true,
	}
	for key, value := range decoded {
		if !allowedKeys[key] {
			return fmt.Errorf("constraints does not support field %q", key)
		}
		if key == "max_cost_per_million" {
			if string(value) == "null" {
				continue
			}
			var number float64
			if err := json.Unmarshal(value, &number); err != nil || number < 0 {
				return fmt.Errorf("constraints.max_cost_per_million must be a non-negative number")
			}
			continue
		}
		var list []string
		if err := json.Unmarshal(value, &list); err != nil {
			return fmt.Errorf("constraints.%s must be an array of strings", key)
		}
	}
	return nil
}
