package asset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const maxOutputSchemaBytes = 64 * 1024

func validateOutputSchema(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > maxOutputSchemaBytes {
		return fmt.Errorf("output schema cannot exceed %d bytes", maxOutputSchemaBytes)
	}
	_, err := compileOutputSchema(raw)
	return err
}

func compileOutputSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("output schema is not valid JSON: %w", err)
	}
	document, ok := resource.(map[string]any)
	if !ok {
		return nil, errors.New("output schema must be a JSON object")
	}
	if err := checkSchemaSafety(document, 0); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("output.schema.json", resource); err != nil {
		return nil, err
	}
	schema, err := compiler.Compile("output.schema.json")
	if err != nil {
		return nil, fmt.Errorf("output schema compilation failed: %w", err)
	}
	return schema, nil
}

func checkSchemaSafety(value any, depth int) error {
	if depth > 40 {
		return errors.New("output schema nesting cannot exceed 40 levels")
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch key {
			case "$ref", "$dynamicRef", "$recursiveRef":
				return errors.New("output schema does not support references; inline the full definition")
			case "$schema":
				return errors.New("output schema must not declare $schema")
			}
			if err := checkSchemaSafety(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := checkSchemaSafety(child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func StructuredResponseFormat(spec PromptSpec) (any, bool) {
	if len(spec.OutputSchema) == 0 {
		return nil, false
	}
	var schema any
	if err := json.Unmarshal(spec.OutputSchema, &schema); err != nil {
		return nil, false
	}
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "prompt_output",
			"strict": true,
			"schema": schema,
		},
	}, true
}

func ValidateStructuredOutput(spec PromptSpec, content string) (any, error) {
	if len(spec.OutputSchema) == 0 {
		return nil, nil
	}
	schema, err := compileOutputSchema(spec.OutputSchema)
	if err != nil {
		return nil, err
	}
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if newline := strings.IndexByte(content, '\n'); newline >= 0 {
			content = content[newline+1:]
		}
		if end := strings.LastIndex(content, "```"); end >= 0 {
			content = strings.TrimSpace(content[:end])
		}
	}
	value, err := jsonschema.UnmarshalJSON(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("model output is not valid JSON: %w", err)
	}
	if err := schema.Validate(value); err != nil {
		return nil, fmt.Errorf("model output does not conform to the schema: %w", err)
	}
	return value, nil
}
