package dataplane

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"
)

// AttemptRecord 结构化记录单次上游候选调用的结果与耗时。
type AttemptRecord struct {
	CandidateID   string `json:"candidate_id"`
	Provider      string `json:"provider"`
	UpstreamModel string `json:"upstream_model"`
	StatusCode    int    `json:"status_code,omitempty"`
	ErrorClass    string `json:"error_class,omitempty"`
	ErrorType     string `json:"error_type,omitempty"`
	ErrorMessage  string `json:"error_message,omitempty"`
	LatencyMS     int    `json:"latency_ms"`
	RetryCount    int    `json:"retry_count,omitempty"`
	Outcome       string `json:"outcome"` // success / failed / ttft_timeout / skipped_same_account
}

// attemptTrailFromRecords 生成结构化的 attempt_trail JSON，包含每次尝试明细及摘要。
func attemptTrailFromRecords(records []AttemptRecord) []byte {
	if records == nil {
		records = []AttemptRecord{}
	}
	data, err := json.Marshal(map[string]any{"attempts": records})
	if err != nil {
		return []byte(`{"attempts":[]}`)
	}
	return data
}

// usageSnapshot 是流式过程中累积的 token 用量。
type usageSnapshot struct {
	InputTokens         int64
	OutputTokens        int64
	CachedTokens        int64
	CacheCreationTokens int64
	ReasoningTokens     int64
}

func (u usageSnapshot) hasTokens() bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.CachedTokens > 0 || u.CacheCreationTokens > 0 || u.ReasoningTokens > 0
}

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
		info.CacheCreationTokens = int64(u.PromptTokensDetails.CachedWriteTokens)
	}
	return info
}

func usageFromResponses(u *schemas.ResponsesResponseUsage) usageSnapshot {
	if u == nil {
		return usageSnapshot{}
	}
	snap := usageSnapshot{
		InputTokens:  int64(u.InputTokens),
		OutputTokens: int64(u.OutputTokens),
	}
	if u.InputTokensDetails != nil {
		snap.CachedTokens = int64(u.InputTokensDetails.CachedReadTokens)
		snap.CacheCreationTokens = int64(u.InputTokensDetails.CachedWriteTokens)
	}
	if u.OutputTokensDetails != nil {
		snap.ReasoningTokens = int64(u.OutputTokensDetails.ReasoningTokens)
	}
	return snap
}

func (u usageInfo) tokens() (input, output, cached, reasoning int64) {
	return u.InputTokens, u.OutputTokens, u.CachedTokens, u.ReasoningTokens
}

func (u usageInfo) hasTokens() bool {
	return usageSnapshot(u).hasTokens()
}

// attemptTrailJSON 生成兼容的 attempt_trail(供 embeddings 等单入口调用使用)。
func attemptTrailJSON(chain routeChain, served candidate, outcome, errorMessage string, logs []schemas.RoutingEngineLogEntry) []byte {
	trail := []AttemptRecord{}
	if outcome == "success" {
		trail = append(trail, AttemptRecord{
			CandidateID:   served.ID,
			Provider:      served.Provider,
			UpstreamModel: served.UpstreamModel,
			Outcome:       outcome,
		})
	} else {
		trail = append(trail, AttemptRecord{
			ErrorMessage: errorMessage,
			Outcome:      outcome,
		})
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
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int                    `json:"created"`
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   chatCompletionUsage    `json:"usage"`
}

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

func intPtr(v int) *int       { return &v }
func stringPtr(v string) *string { return &v }

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
