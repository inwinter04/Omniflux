<p align="center">
  <img src="docs/assets/github-banner.svg" alt="Omniflux — All intelligence, one current." width="100%" />
</p>

<p align="center">
  <a href="README.md">简体中文</a> · <a href="README.en-US.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/inwinter04/Omniflux/releases"><img src="https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.27+"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache--2.0-blue" alt="License: Apache-2.0"></a>
  <img src="https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white" alt="PostgreSQL 17">
  <img src="https://img.shields.io/badge/Redis-8-DC382D?logo=redis&logoColor=white" alt="Redis 8">
</p>

Omniflux is an enterprise AI service control plane that brings model services from multiple providers and private deployments into a governed system for routing, metering, auditing, and observability.

Starting from v0.1.0, Omniflux is in a stabilization phase; breaking changes to configuration, data models, and administrative APIs will be noted in the release notes.

## Features

- **Unified model services** — separate business-facing logical models from provider-specific deployments.
- **Governed routing** — select candidates using hard constraints plus cost, quality, latency, and reliability scores.
- **Multi-tenant governance** — organize departments, projects, applications, users, roles, and API keys.
- **Metering and attribution** — attribute requests, tokens, and cost to projects, applications, keys, providers, and models.
- **Auditable decisions** — retain administrative actions, sign-in events, and policy decisions.
- **Operational visibility** — liveness/readiness probes, structured logs, and Prometheus metrics.
- **Enterprise asset governance** — immutable PromptTemplate versions, publishing, rollback, organization/department/project grants, Markdown editing, real model debugging, multi-model comparison, evaluation sets, and invocation audits.
- **Modern admin console** — Vue 3, TypeScript, Naive UI, SoybeanAdmin, and server-backed sessions.

## Architecture

<p align="center">
  <img src="docs/assets/architecture.en-US.png" alt="Omniflux architecture diagram" width="100%" />
</p>

## Quick start

### 1. Start infrastructure

```bash
docker compose up -d
```

### 2. Configure environment variables

| Variable | Required | Description |
| --- | --- | --- |
| `OMNIFLUX_DATABASE_URL` | Yes | PostgreSQL connection string |
| `OMNIFLUX_MASTER_KEY` | Yes | Base64-encoded 32-byte AES key |
| `OMNIFLUX_API_KEY_PEPPER` | Yes | Pepper for API key hashing, at least 32 characters |
| `OMNIFLUX_BOOTSTRAP_EMAIL` | Yes | Initial administrator email |
| `OMNIFLUX_BOOTSTRAP_PASSWORD` | Yes | Initial administrator password, at least 16 characters |

The full variable list (HTTP address, rate limiting, OpenTelemetry) is documented in the [docs site · Quick start](https://inwinter04.github.io/Omniflux/guide/quick-start) (Chinese).

### 3. Start the service

With Docker:

```bash
docker build -t omniflux:latest .
docker run -d -p 8080:8080 \
  -e OMNIFLUX_DATABASE_URL="postgres://user:pass@host:5432/omniflux?sslmode=require" \
  -e OMNIFLUX_MASTER_KEY="<base64-encoded-32-byte-key>" \
  -e OMNIFLUX_API_KEY_PEPPER="<random-pepper-at-least-32-characters>" \
  -e OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com" \
  -e OMNIFLUX_BOOTSTRAP_PASSWORD="<strong-password>" \
  omniflux:latest
```

Or from source (the admin console build is embedded in the server binary):

```bash
go run ./cmd/omniflux
```

Open the admin console at `http://127.0.0.1:8080/admin/` and sign in with the bootstrap credentials. An empty database is seeded with a platform administrator on first start; never use the sample values in production.

## Data plane API

Business systems call OpenAI-compatible endpoints with an API key (`Authorization: Bearer`):

| Endpoint | Description |
| --- | --- |
| `GET /v1/models` | Available model services |
| `POST /v1/chat/completions` | Chat completions (streaming supported) |
| `POST /v1/embeddings` | Embeddings |

Routing applies hard constraints, then priority groups, then cost/quality/latency/reliability scoring with fallback; every call is metered with decision records, budgets and per-key rate limits enforced. See the [docs site · Data plane API](https://inwinter04.github.io/Omniflux/api/dataplane) (Chinese).

The full admin API reference is on the [docs site · Admin API](https://inwinter04.github.io/Omniflux/api/admin) (Chinese).

## Development

```bash
make infra          # start local PostgreSQL and Redis
make test           # go test -race ./...
make vet            # go vet ./...
make build-admin    # build the admin console and sync it into the server
make docs-build     # build the docs site
```

Project layout and frontend development notes: [docs site · Development](https://inwinter04.github.io/Omniflux/guide/development) (Chinese).

## Security notes

- Administrator passwords are hashed with Argon2id.
- Admin sessions are stored server-side; cookies use `HttpOnly` and `SameSite=Strict`.
- Provider credentials are encrypted at rest with AES-256-GCM.

## License

[Apache-2.0](LICENSE)
