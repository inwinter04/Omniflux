# Omniflux

<p align="center">
  <b>All intelligence, one current.</b><br/>
  <a href="README.md">简体中文</a> · <a href="README.en-US.md">English</a>
</p>

Omniflux is an enterprise AI service control plane that brings model services from multiple providers and private deployments into a governed system for routing, metering, auditing, and observability.

Omniflux is under active development. Configuration, data models, and administrative APIs may change before the first stable release.

## Features

- **Unified model services** — separate business-facing logical models from provider-specific deployments.
- **Governed routing** — select candidates using hard constraints plus cost, quality, latency, and reliability scores.
- **Multi-tenant governance** — organize departments, projects, applications, users, roles, and API keys.
- **Metering and attribution** — attribute requests, tokens, and cost to projects, applications, keys, providers, and models.
- **Auditable decisions** — retain administrative actions, sign-in events, and policy decisions.
- **Operational visibility** — liveness/readiness probes, structured logs, and Prometheus metrics.
- **Modern admin console** — Vue 3, TypeScript, Naive UI, SoybeanAdmin, and server-backed sessions.

## Architecture

<p align="center">
  <img src="docs/assets/architecture.en-US.png" alt="Omniflux architecture diagram" width="100%" />
</p>
## API surface

| Endpoint | Description |
| --- | --- |
| `GET /livez` | Liveness probe |
| `GET /readyz` | Readiness check for PostgreSQL, Redis, and core dependencies |
| `GET /metrics` | Prometheus metrics |
| `GET /admin/` | Admin console |
| `POST /api/v1/auth/login` | Administrator sign-in |
| `GET /api/v1/auth/me` | Current administrator profile |
| `POST /api/v1/auth/logout` | Sign out |

The data-plane model APIs are under development.

## Quick start

### Requirements

- Go 1.27+
- Node.js 20+
- Corepack / pnpm
- PostgreSQL 17
- Redis 8

### 1. Start infrastructure

```bash
docker compose up -d
```

### 2. Configure environment variables

| Variable | Required | Description |
| --- | --- | --- |
| `OMNIFLUX_ENV` | No | Runtime environment; defaults to `development` |
| `OMNIFLUX_HTTP_ADDR` | No | HTTP listen address; defaults to `127.0.0.1:8080` |
| `OMNIFLUX_DATABASE_URL` | Yes | PostgreSQL connection string |
| `OMNIFLUX_REDIS_ADDR` | No | Redis address; defaults to `localhost:6379` |
| `OMNIFLUX_MASTER_KEY` | Yes | Base64-encoded 32-byte AES key |
| `OMNIFLUX_API_KEY_PEPPER` | Yes | API key hashing pepper, at least 32 characters |
| `OMNIFLUX_BOOTSTRAP_EMAIL` | Yes | Initial administrator email |
| `OMNIFLUX_BOOTSTRAP_PASSWORD` | Yes | Initial administrator password, at least 16 characters |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | OpenTelemetry OTLP endpoint |

Example:

```bash
export OMNIFLUX_DATABASE_URL="postgres://omniflux:omniflux@localhost:5432/omniflux?sslmode=disable"
export OMNIFLUX_REDIS_ADDR="localhost:6379"
export OMNIFLUX_MASTER_KEY="<base64-encoded-32-byte-key>"
export OMNIFLUX_API_KEY_PEPPER="<random-pepper-at-least-32-characters>"
export OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com"
export OMNIFLUX_BOOTSTRAP_PASSWORD="<strong-password>"
```

The initial administrator is created only when the database is empty. Never use example credentials in production.

### 3. Docker deployment

For production, build the image which bundles the admin console and the server binary:

```bash
docker build -t omniflux:latest .
```

Then run it with environment variables:

```bash
docker run -d -p 8080:8080 \
  -e OMNIFLUX_DATABASE_URL="postgres://user:pass@host:5432/omniflux?sslmode=require" \
  -e OMNIFLUX_REDIS_ADDR="host:6379" \
  -e OMNIFLUX_MASTER_KEY="<base64-encoded-32-byte-key>" \
  -e OMNIFLUX_API_KEY_PEPPER="<random-pepper-at-least-32-characters>" \
  -e OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com" \
  -e OMNIFLUX_BOOTSTRAP_PASSWORD="<strong-password>" \
  omniflux:latest
```

Session cookies use `SameSite=Strict` + `HttpOnly`; put the service behind a reverse proxy with HTTPS in production (the cookie automatically gets the `Secure` flag).

```bash
corepack pnpm --dir web/admin install
make build-admin
```

### 4. Start the service

```bash
go run ./cmd/omniflux
```

Open the console:

```text
http://127.0.0.1:8080/admin/
```

The service listens on loopback by default. To expose it externally, set `OMNIFLUX_HTTP_ADDR=0.0.0.0:8080` and place it behind a reverse proxy with authentication and network policy controls.

## Frontend development

Start the backend:

```bash
go run ./cmd/omniflux
```

Then start the Vite dev server:

```bash
make admin-dev
```

Vite proxies `/api` to the Go service.

## Project layout

```text
cmd/omniflux/           Service entry point
internal/               Domain modules, migrations, storage, and admin APIs
web/admin/              Vue 3 + Naive UI admin console
scripts/                Build support scripts
docker-compose.yml      Local PostgreSQL and Redis
```

## Development checks

```bash
go test -race ./...
go vet ./...
corepack pnpm --dir web/admin typecheck
corepack pnpm --dir web/admin build
```

## Security notes

- Administrator passwords are hashed with Argon2id.
- Admin sessions are stored server-side and issued with `HttpOnly`, `SameSite=Strict` cookies.
- Provider credentials are encrypted with AES-256-GCM.
- API keys store only a keyed hash; the plaintext is shown once at creation.
- Audit events are append-only at the application layer.
- Never commit or publish real keys in logs, examples, commit messages, or issues.

