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
		return fmt.Errorf("输出 Schema 不能超过 %d 字节", maxOutputSchemaBytes)
	}
	_, err := compileOutputSchema(raw)
	return err
}

func compileOutputSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("输出 Schema 不是合法 JSON: %w", err)
	}
	document, ok := resource.(map[string]any)
	if !ok {
		return nil, errors.New("输出 Schema 必须是 JSON 对象")
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
		return nil, fmt.Errorf("输出 Schema 编译失败: %w", err)
	}
	return schema, nil
}

func checkSchemaSafety(value any, depth int) error {
	if depth > 40 {
		return errors.New("输出 Schema 嵌套层级不能超过 40")
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch key {
			case "$ref", "$dynamicRef", "$recursiveRef":
				return errors.New("输出 Schema 不支持引用，请内联完整定义")
			case "$schema":
				return errors.New("输出 Schema 不需要声明 $schema")
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
		return nil, fmt.Errorf("模型输出不是合法 JSON: %w", err)
	}
	if err := schema.Validate(value); err != nil {
		return nil, fmt.Errorf("模型输出不符合 Schema: %w", err)
	}
	return value, nil
}
