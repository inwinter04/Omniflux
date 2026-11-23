package asset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
)

const (
	DefaultAgentHistoryMessages = 20
	MaxAgentHistoryMessages     = 100
	MaxAgentOutputTokens        = 32768
)

type AgentSpec struct {
	PromptTemplateID   string   `json:"prompt_template_id"`
	PromptVersionID    string   `json:"prompt_version_id"`
	ModelServiceID     string   `json:"model_service_id"`
	Temperature        *float64 `json:"temperature,omitempty"`
	TopP               *float64 `json:"top_p,omitempty"`
	MaxTokens          int      `json:"max_tokens,omitempty"`
	MaxHistoryMessages int      `json:"max_history_messages"`
}

func ParseAgentSpec(raw json.RawMessage) (AgentSpec, error) {
	var spec AgentSpec
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return spec, fmt.Errorf("Agent 配置格式不正确: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return spec, err
	}
	if spec.MaxHistoryMessages == 0 {
		spec.MaxHistoryMessages = DefaultAgentHistoryMessages
	}
	if err := ValidateAgentSpec(spec); err != nil {
		return AgentSpec{}, err
	}
	return spec, nil
}

func ValidateAgentSpec(spec AgentSpec) error {
	for field, value := range map[string]string{
		"提示词模板": spec.PromptTemplateID,
		"提示词版本": spec.PromptVersionID,
		"模型服务":  spec.ModelServiceID,
	} {
		if _, err := uuid.Parse(value); err != nil {
			return fmt.Errorf("%s ID 不合法", field)
		}
	}
	if spec.Temperature != nil && (math.IsNaN(*spec.Temperature) || math.IsInf(*spec.Temperature, 0) || *spec.Temperature < 0 || *spec.Temperature > 2) {
		return errors.New("温度必须在 0 到 2 之间")
	}
	if spec.TopP != nil && (math.IsNaN(*spec.TopP) || math.IsInf(*spec.TopP, 0) || *spec.TopP <= 0 || *spec.TopP > 1) {
		return errors.New("Top P 必须大于 0 且不超过 1")
	}
	if spec.MaxTokens < 0 || spec.MaxTokens > MaxAgentOutputTokens {
		return fmt.Errorf("最大输出 Token 必须在 0 到 %d 之间", MaxAgentOutputTokens)
	}
	if spec.MaxHistoryMessages < 2 || spec.MaxHistoryMessages > MaxAgentHistoryMessages || spec.MaxHistoryMessages%2 != 0 {
		return fmt.Errorf("上下文消息数必须是 2 到 %d 之间的偶数", MaxAgentHistoryMessages)
	}
	return nil
}
