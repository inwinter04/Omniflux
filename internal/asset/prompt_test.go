package asset

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAndRenderPrompt(t *testing.T) {
	raw := json.RawMessage(`{
		"template":"Hello {{ .name }}, count={{ .count }}, enabled={{ .enabled }}, region={{ .region }}",
		"model_service_id":"",
		"variables":[
			{"name":"name","type":"string","required":true},
			{"name":"count","type":"integer","required":true},
			{"name":"enabled","type":"boolean","default":true},
			{"name":"region","type":"string","default":"cn"}
		]
	}`)
	spec, err := ParsePromptSpec(raw)
	if err != nil {
		t.Fatalf("ParsePromptSpec: %v", err)
	}
	rendered, err := RenderPrompt(spec, map[string]any{"name": "Ada", "count": 3})
	if err != nil {
		t.Fatalf("RenderPrompt: %v", err)
	}
	want := "Hello Ada, count=3, enabled=true, region=cn"
	if rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}
}

func TestPromptValidation(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown spec field", raw: `{"template":"hi","unexpected":true}`},
		{name: "empty template", raw: `{"template":" "}`},
		{name: "duplicate variables", raw: `{"template":"{{ .name }}","variables":[{"name":"name","type":"string"},{"name":"name","type":"string"}]}`},
		{name: "invalid variable name", raw: `{"template":"hi","variables":[{"name":"not-valid","type":"string"}]}`},
		{name: "unsupported type", raw: `{"template":"hi","variables":[{"name":"value","type":"object"}]}`},
		{name: "invalid default", raw: `{"template":"hi","variables":[{"name":"count","type":"integer","default":"three"}]}`},
		{name: "unknown placeholder", raw: `{"template":"{{ .missing }}","variables":[{"name":"name","type":"string"}]}`},
		{name: "unsafe function", raw: `{"template":"{{ env \"HOME\" }}"}`},
		{name: "invalid model service", raw: `{"template":"hi","model_service_id":"not-a-uuid"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParsePromptSpec(json.RawMessage(tt.raw)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRenderRejectsInvalidInput(t *testing.T) {
	spec, err := ParsePromptSpec(json.RawMessage(`{
		"template":"Hello {{ .name }}",
		"variables":[{"name":"name","type":"string","required":true}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []map[string]any{
		{},
		{"name": 42},
		{"name": "Ada", "extra": true},
	} {
		if _, err := RenderPrompt(spec, input); err == nil {
			t.Fatalf("expected input %#v to fail", input)
		}
	}
}

func TestRenderOutputLimit(t *testing.T) {
	spec, err := ParsePromptSpec(json.RawMessage(`{
		"template":"{{ .name }}",
		"variables":[{"name":"name","type":"string","required":true}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"name": strings.Repeat("x", maxRenderedBytes+1)}
	if _, err := RenderPrompt(spec, input); err == nil {
		t.Fatal("expected oversized output to fail")
	}
}
