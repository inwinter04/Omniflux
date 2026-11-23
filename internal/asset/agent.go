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
		return spec, fmt.Errorf("Agent configuration format is invalid: %w", err)
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
		"prompt_template": spec.PromptTemplateID,
		"prompt_version":  spec.PromptVersionID,
		"model_service":   spec.ModelServiceID,
	} {
		if _, err := uuid.Parse(value); err != nil {
			return fmt.Errorf("%s ID is invalid", field)
		}
	}
	if spec.Temperature != nil && (math.IsNaN(*spec.Temperature) || math.IsInf(*spec.Temperature, 0) || *spec.Temperature < 0 || *spec.Temperature > 2) {
		return errors.New("temperature must be between 0 and 2")
	}
	if spec.TopP != nil && (math.IsNaN(*spec.TopP) || math.IsInf(*spec.TopP, 0) || *spec.TopP <= 0 || *spec.TopP > 1) {
		return errors.New("top_p must be greater than 0 and no more than 1")
	}
	if spec.MaxTokens < 0 || spec.MaxTokens > MaxAgentOutputTokens {
		return fmt.Errorf("max output tokens must be between 0 and %d", MaxAgentOutputTokens)
	}
	if spec.MaxHistoryMessages < 2 || spec.MaxHistoryMessages > MaxAgentHistoryMessages || spec.MaxHistoryMessages%2 != 0 {
		return fmt.Errorf("max history messages must be an even number between 2 and %d", MaxAgentHistoryMessages)
	}
	return nil
}
