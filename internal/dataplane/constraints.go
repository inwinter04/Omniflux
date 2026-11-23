package dataplane

import (
	"encoding/json"
	"fmt"
)

// policyConstraints 是路由策略的硬约束,来自 route_policies.constraints JSON。
// 空对象表示不限制;命中任一约束的候选会在评分前被过滤。
type policyConstraints struct {
	AllowedProviders     []string `json:"allowed_providers"`
	BlockedProviders     []string `json:"blocked_providers"`
	AllowedUpstreamModel []string `json:"allowed_upstream_models"`
	BlockedUpstreamModel []string `json:"blocked_upstream_models"`
	MaxCostPerMillion    *float64 `json:"max_cost_per_million"`
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

// (c policyConstraints) filter 按硬约束过滤候选:
//   - allowed_* 白名单非空时,不在名单内的候选被排除;
//   - blocked_* 黑名单命中的候选被排除;
//   - max_cost_per_million 超过混合单价上限的候选被排除。
func (c policyConstraints) filter(cands []candidate) []candidate {
	kept := make([]candidate, 0, len(cands))
	for _, cand := range cands {
		if len(c.AllowedProviders) > 0 && !containsString(c.AllowedProviders, cand.Provider) {
			continue
		}
		if containsString(c.BlockedProviders, cand.Provider) {
			continue
		}
		if len(c.AllowedUpstreamModel) > 0 && !containsString(c.AllowedUpstreamModel, cand.UpstreamModel) {
			continue
		}
		if containsString(c.BlockedUpstreamModel, cand.UpstreamModel) {
			continue
		}
		if c.MaxCostPerMillion != nil && costRate(cand) > *c.MaxCostPerMillion {
			continue
		}
		kept = append(kept, cand)
	}
	return kept
}

// validateConstraintsJSON 校验管理面写入的约束结构;返回用户可读的错误信息。
// 允许的键固定为五类,避免自由 JSON 里拼错键名后静默不生效。
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
