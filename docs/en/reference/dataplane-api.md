# Data Plane API (Multi-Protocol Gateway)

The Omniflux data plane provides a unified **multi-protocol and multimodal gateway** for applications, agents, and **AI coding clients** (**Codex CLI**, **Claude Code**, **Gemini CLI**), supporting cross-protocol translation across `openai_responses`, `openai_chat`, `anthropic_messages`, and `gemini_native` upstreams, streaming pass-through, automatic Responses-to-Chat bridging, and tiered prompt-cache billing.

## Authentication

Clients may authenticate with their native credential header or query format using an `of_`-prefixed Omniflux API key:

- **OpenAI / Codex CLI**: `Authorization: Bearer <YOUR_OMNIFLUX_API_KEY>`
- **Anthropic / Claude Code**: `x-api-key: <YOUR_OMNIFLUX_API_KEY>` or `Authorization: Bearer <YOUR_OMNIFLUX_API_KEY>`
- **Google Gemini CLI / GenAI SDK**: `x-goog-api-key: <YOUR_OMNIFLUX_API_KEY>` or `?key=<YOUR_OMNIFLUX_API_KEY>`

- Keys begin with the prefix `of_`;
- Permissions are enforced according to project authorization (`read` for listings, `invoke` for execution);
- Disabled or expired keys return `401`; disabled projects/applications return `403`. Blocked calls do not reach providers and incur zero cost.

## Endpoints

| Method & Path | Description | Required Permission |
| :--- | :--- | :---: |
| `GET /v1/models` | List authorized logical model services (OpenAI format) | `read` |
| `POST /v1/chat/completions` | Chat completions (vision images & SSE streaming) | `invoke` |
| `POST /v1/responses` | OpenAI Responses API (Codex CLI native protocol: `input_image`, PDF `input_file`, tools, reasoning, prompt caching, SSE streaming; auto-bridges when routed to `openai_chat` accounts) | `invoke` |
| `POST /v1/responses/compact` | OpenAI Responses context compaction | `invoke` |
| `POST /v1/messages` | Anthropic Messages API (Claude Code native protocol: images, PDFs, cache read/write token reporting, SSE streaming) | `invoke` |
| `POST /v1/messages/count_tokens` | Anthropic preflight token counting (falls back to local estimation if unsupported upstream) | `invoke` |
| `GET /v1beta/models` | Gemini Native model discovery (`models/{service}`) | `read` |
| `POST /v1beta/models/{model}:generateContent` | Gemini Native non-streaming content generation | `invoke` |
| `POST /v1beta/models/{model}:streamGenerateContent` | Gemini Native streaming generation (`?alt=sse` or JSON array chunks) | `invoke` |
| `POST /v1beta/models/{model}:countTokens` | Gemini Native token counting (falls back to local estimation if unsupported upstream) | `invoke` |
| `POST /v1/embeddings` | Text embeddings | `invoke` |
| `GET /v1/prompt-templates` | List authorized published prompt templates | `read` |
| `GET /v1/prompt-templates/{name}` | Retrieve details for a published prompt template | `read` |
| `POST /v1/prompt-templates/{name}/render` | Render template variables without invoking models | `invoke` |
| `GET /v1/agents` | List authorized published agents | `read` |
| `GET /v1/agents/{name}` | Retrieve details for a published agent | `read` |
| `POST /v1/agents/{name}/chat/completions` | Stateful multi-turn agent conversation | `invoke` |

## AI Coding Client Quick Connect

The Admin Console (**Access Control -> API Keys -> Quick Connect**) generates ready-to-use snippets and supports **one-click import into CC Switch** (`ccswitch://v1/import`) with provider name, endpoint, API format, model service, and API key prefilled.

### 1. Codex CLI (`~/.codex/config.toml`)

```toml
model_provider = "omniflux"
model = "coding-pro"

[model_providers.omniflux]
name = "Omniflux Gateway"
base_url = "http://127.0.0.1:8080/v1"
wire_api = "responses"
env_key = "OPENAI_API_KEY"
```

```bash
export OPENAI_API_KEY="$OMNIFLUX_API_KEY"
codex -m coding-pro
```

### 2. Claude Code

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:8080"
export ANTHROPIC_AUTH_TOKEN="$OMNIFLUX_API_KEY"
export ANTHROPIC_MODEL="coding-pro"
claude
```

### 3. Gemini CLI

```bash
export GOOGLE_GEMINI_BASE_URL="http://127.0.0.1:8080"
export GEMINI_API_KEY="$OMNIFLUX_API_KEY"
export GEMINI_MODEL="coding-pro"
gemini
```

> **Sticky Session Affinity (KV Cache Pinning)**: The gateway extracts session identifiers from `Session-Id`, `X-Session-Id`, `X-Codex-Session-Id`, `Conversation-Id`, `prompt_cache_key`, `previous_response_id`, or `metadata.user_id`. When `session_affinity` is enabled on a route policy, consecutive turns stay pinned to the same top-tier candidate and automatically fail over when unhealthy or circuit-open.

## Examples

### 1. Multimodal Vision via `/v1/responses`
```bash
curl -sS http://127.0.0.1:8080/v1/responses \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "coding-pro",
    "input": [{
      "role": "user",
      "content": [
        {"type": "input_text", "text": "Inspect this screenshot and suggest a fix."},
        {"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgo..."}
      ]
    }]
  }'
```

### 2. Non-Streaming Chat Completion
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

### 3. Streaming Response (SSE)
Once SSE headers are flushed, the gateway clears the HTTP write deadline so long code-generation streams stay open:
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

## Protocol-Native Error Format

Errors are encoded in the inbound protocol's native structure while keeping a stable machine `reason` field and the `X-Request-ID` response header:

- **OpenAI Chat / Responses**: `{"error": {"type": "...", "code": "...", "reason": "...", "message": "...", "request_id": "..."}}`
- **Anthropic Messages**: `{"type": "error", "error": {"type": "invalid_request_error", "code": "...", "reason": "...", "message": "...", "request_id": "..."}}`
- **Gemini Native**: `{"error": {"code": 400, "status": "INVALID_ARGUMENT", "error_code": "...", "reason": "...", "message": "...", "request_id": "..."}}`
