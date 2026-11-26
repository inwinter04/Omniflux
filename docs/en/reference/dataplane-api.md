# Data Plane API (OpenAI Compatible)

The Omniflux data plane provides a unified interface for applications, **fully compatible with OpenAI specifications**.

## Authentication

All data plane endpoints use Bearer Token authentication:

```http
Authorization: Bearer <YOUR_OMNIFLUX_API_KEY>
```

- Keys begin with the prefix `of_`;
- Permissions are enforced according to project authorization (`read` for listings, `invoke` for execution);
- Disabled or expired keys return `401 Unauthorized`; disabled projects/applications return `403 Forbidden`. Blocked calls do not reach providers and incur zero cost.

## Endpoints

| Method & Path | Description | Required Permission |
| :--- | :--- | :---: |
| `GET /v1/models` | List authorized logical model services | `read` |
| `POST /v1/chat/completions` | Chat completions (streaming & non-streaming) | `invoke` |
| `POST /v1/embeddings` | Text embeddings | `invoke` |
| `GET /v1/prompt-templates` | List authorized published prompt templates | `read` |
| `GET /v1/prompt-templates/{name}` | Retrieve details for a published prompt template | `read` |
| `POST /v1/prompt-templates/{name}/render` | Render template variables without invoking models | `invoke` |
| `GET /v1/agents` | List authorized published agents | `read` |
| `GET /v1/agents/{name}` | Retrieve details for a published agent | `read` |
| `POST /v1/agents/{name}/chat/completions` | Stateful multi-turn agent conversation | `invoke` |

## Examples

### 1. Non-Streaming Chat Completion
```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "general-chat",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "What is an AI control plane?"}
    ],
    "temperature": 0.7
  }'
```

### 2. Streaming Response (SSE)
```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "general-chat",
    "stream": true,
    "messages": [
      {"role": "user", "content": "Explain quantum computing in three sentences."}
    ]
  }'
```

## Error Format & Machine Fields

Errors use OpenAI-compatible payloads with deterministic machine-readable fields:

```json
{
  "error": {
    "type": "invalid_request_error",
    "code": "invalid_request",
    "reason": "missing_model",
    "message": "The model parameter is required.",
    "param": "model",
    "params": {},
    "request_id": "req_01JXXXXXXXXXXXX"
  }
}
```

| Field | Description |
| :--- | :--- |
| `type` | Broad category: `invalid_request_error`, `authentication_error`, `rate_limit_error`, `upstream_error` |
| `code` | Stable coarse-grained code |
| `reason` | **Recommended branch key**: Precise semantic error code (`model_access_denied`, `budget_exceeded`) |
| `message`| Developer-facing English message (do not parse programmatically) |
| `param`  | Relevant invalid parameter |
| `request_id` | Trace ID matching the `X-Request-ID` response header |
