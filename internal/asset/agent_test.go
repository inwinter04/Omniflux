package asset

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func validAgentSpecJSON() []byte {
	raw, _ := json.Marshal(map[string]any{
		"prompt_template_id":   uuid.NewString(),
		"prompt_version_id":    uuid.NewString(),
		"model_service_id":     uuid.NewString(),
		"temperature":          0.7,
		"top_p":                0.9,
		"max_tokens":           2048,
		"max_history_messages": 20,
	})
	return raw
}

func TestParseAgentSpec(t *testing.T) {
	spec, err := ParseAgentSpec(validAgentSpecJSON())
	if err != nil {
		t.Fatal(err)
	}
	if spec.MaxHistoryMessages != 20 || spec.MaxTokens != 2048 || spec.Temperature == nil || *spec.Temperature != 0.7 {
		t.Fatalf("unexpected spec: %#v", spec)
	}
}

func TestParseAgentSpecDefaults(t *testing.T) {
	var value map[string]any
	if err := json.Unmarshal(validAgentSpecJSON(), &value); err != nil {
		t.Fatal(err)
	}
	delete(value, "max_history_messages")
	raw, _ := json.Marshal(value)
	spec, err := ParseAgentSpec(raw)
	if err != nil {
		t.Fatal(err)
	}
	if spec.MaxHistoryMessages != DefaultAgentHistoryMessages {
		t.Fatalf("history default = %d", spec.MaxHistoryMessages)
	}
}

func TestParseAgentSpecRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{name: "unknown field", mutate: func(value map[string]any) { value["system_prompt"] = "unexpected" }, want: "unknown field"},
		{name: "prompt id", mutate: func(value map[string]any) { value["prompt_template_id"] = "bad" }, want: "提示词模板 ID"},
		{name: "temperature", mutate: func(value map[string]any) { value["temperature"] = 2.1 }, want: "温度"},
		{name: "top p", mutate: func(value map[string]any) { value["top_p"] = 0 }, want: "Top P"},
		{name: "tokens", mutate: func(value map[string]any) { value["max_tokens"] = 40000 }, want: "最大输出 Token"},
		{name: "odd history", mutate: func(value map[string]any) { value["max_history_messages"] = 21 }, want: "偶数"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(validAgentSpecJSON(), &value); err != nil {
				t.Fatal(err)
			}
			test.mutate(value)
			raw, _ := json.Marshal(value)
			if _, err := ParseAgentSpec(raw); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}
