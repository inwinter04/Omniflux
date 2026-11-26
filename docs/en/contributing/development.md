# Development & Contributing

Guidelines for contributing to the Omniflux codebase.

## Project Structure

| Path | Description |
| :--- | :--- |
| `cmd/omniflux/` | Main application entry point & CLI commands |
| `internal/apikeys/` | API Key issuance and hashing |
| `internal/app/` | Dependency injection & lifecycle management |
| `internal/asset/` | PromptTemplate and Agent asset domains |
| `internal/auth/` | Session state, Argon2id hashing, RBAC |
| `internal/bifrostx/` | Model gateway adapter |
| `internal/bootstrap/` | Bootstrap admin initialization |
| `internal/config/` | Environment variable parsing |
| `internal/console/` | Embedded console assets (`embed.FS`) |
| `internal/dataplane/` | HTTP routers and OpenAI protocol execution |
| `internal/httpx/` | Common middleware, logging, and error handling |
| `internal/infra/` | PostgreSQL and Redis clients |
| `internal/logging/` | Structured JSON logging |
| `internal/mail/` | SMTP mail sender |
| `internal/migrations/` | Embedded `goose` database migrations |
| `internal/vault/` | AES-256-GCM credential encryption |
| `web/admin/` | Vue 3 + Naive UI + SoybeanAdmin console |
| `docs/` | VitePress documentation site |
| `scripts/` | Automation scripts |
| `docker-compose.yml` | Local PostgreSQL 17 and Redis 8 |
| `Makefile` | Convenience commands |

## Local Development Workflow

### 1. Run Backend
```bash
make infra
make run
```

### 2. Run Management Console Dev Server
```bash
make admin-dev
```
Vite proxies `/api/*` calls to the Go backend with instant hot-reloading.

## CI Verification Checks

Run these validations before submitting pull requests:

```bash
go test -race ./...
go vet ./...
corepack pnpm --dir web/admin typecheck
corepack pnpm --dir web/admin build
make docs-build
```
