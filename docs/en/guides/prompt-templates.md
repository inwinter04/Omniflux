# Prompt Templates

Prompt engineering often suffers from prompts scattered across codebases without version control or safety boundaries. Omniflux treats prompts as **first-class governed enterprise assets**.

## Core Capabilities

1. **Immutable Versioning**: Every prompt update creates a new incremental version. Published versions are locked to guarantee deterministic inference.
2. **Strict Variable Validation**: Declare typed variables with required constraints and default values.
3. **One-Click Rollback**: Instantly repoint the published version pointer to a prior stable release.
4. **Interactive Sandbox**: Experiment with real models and compare parameter responses side-by-side prior to publishing.

## Syntax & Security

Omniflux utilizes a safe, whitelisted subset of **Go Templates**:
- Variable syntax: <code v-text="'{{ .variable_name }}'"></code>
- Arbitrary function execution and reflection are prohibited to eliminate template injection risks.

### Variable Attributes
- **Type**: `string`, `number`, `integer`, `boolean`
- **Required**: Rejects requests missing mandatory parameters
- **Default**: Fallback value when omitted
- **Description**: Documentation for client consumers

## Data Plane Rendering API

The template rendering endpoint formats text and validates schemas without invoking models:

### 1. List Available Published Templates
```bash
curl -sS http://127.0.0.1:8080/v1/prompt-templates \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY"
```

### 2. Render Template Content
```bash
curl -sS http://127.0.0.1:8080/v1/prompt-templates/customer-service/render \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "variables": {
      "customer_name": "Alice",
      "issue_type": "Billing"
    }
  }'
```

Returns structured JSON:
```json
{
  "name": "customer-service",
  "version": 3,
  "rendered_text": "You are a customer support agent. Customer Alice submitted a Billing inquiry..."
}
```

## Privacy & Audit Safeguards

Audit logs capture execution timestamps, caller identities, and latency. To preserve data privacy, **variable values and rendered text contents are never recorded in audit logs**.
