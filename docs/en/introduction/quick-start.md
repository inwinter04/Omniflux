# Quick Start

Get Omniflux up and running locally in **under 5 minutes**, configure your first model service, and dispatch an OpenAI-compatible chat completion request.

## Prerequisites

Ensure you have installed:
- **Docker** and **Docker Compose**
- **Go 1.27+** (only if building/running from source)
- **curl** or any HTTP client

## 1. Start Local Infrastructure

Omniflux requires PostgreSQL 17 and Redis 8. A ready-to-run `docker-compose.yml` is provided at the repository root:

```bash
docker compose up -d
```

Verify that both containers (`omniflux-postgres` on port 5432 and `omniflux-redis` on port 6379) are healthy:
```bash
docker compose ps
```

## 2. Set Required Environment Variables

Export the essential configuration variables in your shell:

```bash
# Database and Redis connections (matching docker-compose defaults)
export OMNIFLUX_DATABASE_URL="postgres://omniflux:omniflux@localhost:5432/omniflux?sslmode=disable"
export OMNIFLUX_REDIS_ADDR="localhost:6379"

# Security master key: Base64-encoded 32-byte key for AES-256-GCM encryption of upstream credentials
export OMNIFLUX_MASTER_KEY="MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

# API Key hashing pepper: Secret string of at least 32 characters
export OMNIFLUX_API_KEY_PEPPER="super-secret-pepper-must-be-at-least-32-chars-long"

# Bootstrap administrator credentials (8–128 characters containing both letters and numbers, no leading/trailing spaces)
export OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com"
export OMNIFLUX_BOOTSTRAP_PASSWORD="admin-strong-password-1234"
```

::: tip Production Note
The sample keys above are for local testing only! Generate cryptographically random keys for production deployments. Refer to [Environment Configuration](/en/deployment/configuration).
:::

## 3. Launch Omniflux

The Vue 3 management console is statically embedded into the Go binary. Simply run the service:

```bash
go run ./cmd/omniflux
```

When you see the log line below, the server is ready:
```text
{"level":"info","time":"...","message":"http server listening on 127.0.0.1:8080"}
```

## 4. Console Login & Onboarding

Open the management console in your browser:
```text
http://127.0.0.1:8080/admin/
```

Log in using your bootstrap credentials:
- **Email**: `admin@example.com`
- **Password**: `admin-strong-password-1234`

Complete the initial onboarding flow:
1. **Add a Provider Account**: Navigate to **Model Access → Provider Accounts**, click **New**, select a provider (e.g. `DeepSeek`, `OpenAI`, or `Custom OpenAI Compatible`), and paste your provider API key.
2. **Create a Routing Policy**: Navigate to **Model Access → Routing Policies**, click **New**, name it `default-policy`, and configure weights (e.g., Quality: 0.4, Cost: 0.3, Latency: 0.2, Reliability: 0.1).
3. **Create a Model Service**: Navigate to **Model Access → Model Services**, click **New**, set the identifier to `general-chat`, attach your routing policy, and add a candidate model (select your provider account and the upstream model name like `deepseek-chat`).
4. **Issue an API Key**: Navigate to **Access Control → API Keys**, click **Create**, select the default organization/project, and record the generated key (starts with `of_`).

## 5. Send Your First Request

### Option A: Using cURL

```bash
export OMNIFLUX_API_KEY="of_xxxxxxxxxxxxxxxxxxxx"

curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "general-chat",
    "messages": [
      {"role": "user", "content": "Hello, please introduce yourself."}
    ]
  }'
```

You will receive a standard OpenAI-compatible response:
```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "created": 1732600000,
  "model": "general-chat",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! I am an AI assistant governed by Omniflux..."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 14,
    "completion_tokens": 26,
    "total_tokens": 40
  }
}
```

### Option B: Using the Official Python OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8080/v1",
    api_key="of_xxxxxxxxxxxxxxxxxxxx"
)

response = client.chat.completions.create(
    model="general-chat",
    messages=[{"role": "user", "content": "Prove you are working in one sentence."}],
    stream=True
)

for chunk in response:
    content = chunk.choices[0].delta.content or ""
    print(content, end="", flush=True)
print()
```

## 6. Verify Decision Records & Auditing

Return to the management console:
- Under **Usage & Auditing → Call Records**, inspect the request log showing token consumption, actual upstream model used, latency, and calculated costs.
- Click **Policy Decision Trace** to review the complete scoring breakdown and why that candidate was selected.
