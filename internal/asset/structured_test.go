package asset

import (
	"encoding/json"
	"testing"
)

func structuredSpec(t *testing.T) PromptSpec {
	t.Helper()
	spec, err := ParsePromptSpec(json.RawMessage(`{
		"template":"生成客户资料",
		"output_schema":{"type":"object","properties":{"name":{"type":"string"},"age":{"type":"integer"}},"required":["name","age"],"additionalProperties":false}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestStructuredOutput(t *testing.T) {
	spec := structuredSpec(t)
	if _, ok := StructuredResponseFormat(spec); !ok {
		t.Fatal("expected native response format")
	}
	for _, content := range []string{
		`{"name":"Ada","age":36}`,
		"```json\n{\"name\":\"Ada\",\"age\":36}\n```",
	} {
		if _, err := ValidateStructuredOutput(spec, content); err != nil {
			t.Fatalf("valid output rejected: %v", err)
		}
	}
	for _, content := range []string{
		`not-json`,
		`{"name":"Ada","age":"thirty-six"}`,
		`{"name":"Ada"}`,
	} {
		if _, err := ValidateStructuredOutput(spec, content); err == nil {
			t.Fatalf("invalid output accepted: %s", content)
		}
	}
}

func TestOutputSchemaRejectsReferences(t *testing.T) {
	_, err := ParsePromptSpec(json.RawMessage(`{
		"template":"test",
		"output_schema":{"$ref":"https://example.com/schema.json"}
	}`))
	if err == nil {
		t.Fatal("expected external reference to be rejected")
	}
}
