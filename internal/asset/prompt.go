package asset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"text/template"

	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	maxTemplateBytes = 256 * 1024
	maxRenderedBytes = 1024 * 1024
	maxVariables     = 64
)

var variableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

type PromptVariable struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Default     any    `json:"default,omitempty"`
}

type PromptSpec struct {
	Template       string           `json:"template"`
	Variables      []PromptVariable `json:"variables"`
	ModelServiceID string           `json:"model_service_id,omitempty"`
	OutputSchema   json.RawMessage  `json:"output_schema,omitempty"`
}

func ParsePromptSpec(raw json.RawMessage) (PromptSpec, error) {
	var spec PromptSpec
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return spec, fmt.Errorf("Prompt configuration format is invalid: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return spec, err
	}
	if err := validatePromptSpec(spec); err != nil {
		return PromptSpec{}, err
	}
	if spec.Variables == nil {
		spec.Variables = []PromptVariable{}
	}
	return spec, nil
}

func RenderPrompt(spec PromptSpec, input map[string]any) (string, error) {
	if err := validatePromptSpec(spec); err != nil {
		return "", err
	}
	values := make(map[string]any, len(spec.Variables))
	for key, value := range input {
		values[key] = value
	}
	for _, variable := range spec.Variables {
		if _, exists := values[variable.Name]; !exists && variable.Default != nil {
			values[variable.Name] = variable.Default
		}
	}
	schema, err := compileInputSchema(spec.Variables)
	if err != nil {
		return "", err
	}
	normalized, err := normalizeJSON(values)
	if err != nil {
		return "", fmt.Errorf("variables are not valid JSON: %w", err)
	}
	if err := schema.Validate(normalized); err != nil {
		return "", fmt.Errorf("variable validation failed: %w", err)
	}
	tmpl, err := template.New("prompt").Option("missingkey=error").Parse(spec.Template)
	if err != nil {
		return "", fmt.Errorf("template parsing failed: %w", err)
	}
	writer := &limitedWriter{remaining: maxRenderedBytes}
	if err := tmpl.Execute(writer, normalized); err != nil {
		return "", fmt.Errorf("template rendering failed: %w", err)
	}
	return writer.String(), nil
}

func validatePromptSpec(spec PromptSpec) error {
	if strings.TrimSpace(spec.Template) == "" {
		return errors.New("template content is required")
	}
	if len(spec.Template) > maxTemplateBytes {
		return fmt.Errorf("template content cannot exceed %d bytes", maxTemplateBytes)
	}
	if len(spec.Variables) > maxVariables {
		return fmt.Errorf("variable count cannot exceed %d", maxVariables)
	}
	if spec.ModelServiceID != "" {
		if _, err := uuid.Parse(spec.ModelServiceID); err != nil {
			return errors.New("model_service_id is invalid")
		}
	}
	seen := make(map[string]bool, len(spec.Variables))
	for _, variable := range spec.Variables {
		if !variableNamePattern.MatchString(variable.Name) {
			return fmt.Errorf("variable name %q is invalid", variable.Name)
		}
		if seen[variable.Name] {
			return fmt.Errorf("variable %q is duplicated", variable.Name)
		}
		seen[variable.Name] = true
		switch variable.Type {
		case "string", "number", "integer", "boolean":
		default:
			return fmt.Errorf("variable %q has an unsupported type", variable.Name)
		}
		if len(variable.Description) > 500 {
			return fmt.Errorf("variable %q description cannot exceed 500 characters", variable.Name)
		}
		if variable.Default != nil {
			if err := validateDefault(variable); err != nil {
				return fmt.Errorf("variable %q default value is invalid: %w", variable.Name, err)
			}
		}
	}
	if _, err := compileInputSchema(spec.Variables); err != nil {
		return err
	}
	if err := validateOutputSchema(spec.OutputSchema); err != nil {
		return err
	}
	tmpl, err := template.New("prompt").Option("missingkey=error").Parse(spec.Template)
	if err != nil {
		return fmt.Errorf("template syntax is invalid: %w", err)
	}
	samples := make(map[string]any, len(spec.Variables))
	for _, variable := range spec.Variables {
		if variable.Default != nil {
			samples[variable.Name] = variable.Default
			continue
		}
		switch variable.Type {
		case "string":
			samples[variable.Name] = "sample"
		case "number", "integer":
			samples[variable.Name] = 1
		case "boolean":
			samples[variable.Name] = true
		}
	}
	if err := tmpl.Execute(io.Discard, samples); err != nil {
		return fmt.Errorf("template references an undefined variable or contains an invalid operation: %w", err)
	}
	return nil
}

func validateDefault(variable PromptVariable) error {
	schema, err := compileSchema(map[string]any{"type": variable.Type})
	if err != nil {
		return err
	}
	value, err := normalizeJSON(variable.Default)
	if err != nil {
		return err
	}
	return schema.Validate(value)
}

func compileInputSchema(variables []PromptVariable) (*jsonschema.Schema, error) {
	properties := make(map[string]any, len(variables))
	required := make([]string, 0, len(variables))
	for _, variable := range variables {
		properties[variable.Name] = map[string]any{"type": variable.Type}
		if variable.Required {
			required = append(required, variable.Name)
		}
	}
	return compileSchema(map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	})
}

func compileSchema(document map[string]any) (*jsonschema.Schema, error) {
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("prompt.schema.json", resource); err != nil {
		return nil, err
	}
	schema, err := compiler.Compile("prompt.schema.json")
	if err != nil {
		return nil, fmt.Errorf("variable schema compilation failed: %w", err)
	}
	return schema, nil
}

func normalizeJSON(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(raw))
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON object")
	}
	return nil
}

type limitedWriter struct {
	strings.Builder
	remaining int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, fmt.Errorf("rendered result cannot exceed %d bytes", maxRenderedBytes)
	}
	n, err := w.Builder.Write(p)
	w.remaining -= n
	return n, err
}
