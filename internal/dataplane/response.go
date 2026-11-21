package dataplane

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"
)

// usageSnapshot 是流式过程中累积的 token 用量。
type usageSnapshot struct {
	InputTokens     int64
	OutputTokens    int64
	CachedTokens    int64
	ReasoningTokens int64
}

func (u usageSnapshot) hasTokens() bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.CachedTokens > 0 || u.ReasoningTokens > 0
}

// usageInfo 是从 Bifrost 响应中提取的用量。
type usageInfo usageSnapshot

func usageFromBifrost(u *schemas.BifrostLLMUsage) usageInfo {
	if u == nil {
		return usageInfo{}
	}
	info := usageInfo{
		InputTokens:     int64(u.PromptTokens),
		OutputTokens:    int64(u.CompletionTokens),
		ReasoningTokens: int64(u.ReasoningTokens),
	}
	if u.PromptTokensDetails != nil {
		info.CachedTokens = int64(u.PromptTokensDetails.CachedReadTokens)
	}
	return info
}

func (u usageInfo) tokens() (input, output, cached, reasoning int64) {
	return u.InputTokens, u.OutputTokens, u.CachedTokens, u.ReasoningTokens
}

func (u usageInfo) hasTokens() bool {
	return usageSnapshot(u).hasTokens()
}

// routingEngineLogs 读取 Bifrost 本次请求的路由引擎日志,
// 包含主候选失败、每次 fallback/重试的尝试与错误摘要。
func routingEngineLogs(bctx *schemas.BifrostContext) []schemas.RoutingEngineLogEntry {
	if bctx == nil {
		return nil
	}
	return bctx.GetRoutingEngineLogs()
}

// attemptTrailJSON 生成 policy_decisions.attempt_trail:
//   - attempts: 最终结果摘要(成功记实际服务的候选,失败/中断记错误);
//   - engine_logs: Bifrost 路由引擎的过程日志,保证 fallback 生效时主候选的失败原因可追溯。
func attemptTrailJSON(chain routeChain, served candidate, outcome, errorMessage string, logs []schemas.RoutingEngineLogEntry) []byte {
	trail := []map[string]any{}
	if outcome == "success" {
		trail = append(trail, map[string]any{
			"candidate_id":   served.ID,
			"provider":       served.Provider,
			"model":          served.UpstreamModel,
			"outcome":        outcome,
		})
	} else {
		entry := map[string]any{"outcome": outcome}
		if errorMessage != "" {
			entry["error"] = errorMessage
		}
		trail = append(trail, entry)
	}
	engineLogs := make([]map[string]any, 0, len(logs))
	for _, log := range logs {
		engineLogs = append(engineLogs, map[string]any{
			"engine":    log.Engine,
			"level":     string(log.Level),
			"message":   log.Message,
			"timestamp": log.Timestamp,
		})
	}
	data, err := json.Marshal(map[string]any{"attempts": trail, "engine_logs": engineLogs})
	if err != nil {
		return []byte("{}")
	}
	return data
}

// --- OpenAI 兼容响应结构 ---

type chatCompletionChoice struct {
	Index        int                  `json:"index"`
	Message      *schemas.ChatMessage `json:"message"`
	FinishReason *string              `json:"finish_reason"`
}

type chatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatCompletionResponse struct {
	ID      string                `json:"id"`
	Object  string                `json:"object"`
	Created int                   `json:"created"`
	Model   string                `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   chatCompletionUsage   `json:"usage"`
}

// chatCompletionResponseFrom 把 Bifrost 响应映射为 OpenAI 兼容响应;model 字段回显请求的模型服务名。
func chatCompletionResponseFrom(servedModel, requestedModel string, resp *schemas.BifrostChatResponse, usage usageInfo) chatCompletionResponse {
	model := requestedModel
	if model == "" {
		model = servedModel
	}
	choices := make([]chatCompletionChoice, 0, len(resp.Choices))
	for i, choice := range resp.Choices {
		choices = append(choices, chatCompletionChoice{
			Index:        i,
			Message:      choice.Message,
			FinishReason: choice.FinishReason,
		})
	}
	return chatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: resp.Created,
		Model:   model,
		Choices: choices,
		Usage: chatCompletionUsage{
			PromptTokens:     int(usage.InputTokens),
			CompletionTokens: int(usage.OutputTokens),
			TotalTokens:      int(usage.InputTokens + usage.OutputTokens),
		},
	}
}

// --- 小工具 ---

func intPtr(v int) *int { return &v }

func stringPtr(v string) *string { return &v }

// stringPtrNil 在 cond 为 false 时返回 NULL。
func stringPtrNil(v string, cond bool) *string {
	if !cond {
		return nil
	}
	return &v
}

func uuidPtr(id string, ok bool) *uuid.UUID {
	if !ok {
		return nil
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil
	}
	return &parsed
}
