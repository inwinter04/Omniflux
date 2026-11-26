# Bootstrap & Operations

Operational guides for administrator credentials, health probes, and Prometheus metrics.

## Bootstrap & Account Governance

- **Automatic Bootstrap**: Initializes the Platform Admin account upon first run if the database is empty.
- **Registration**: Closed by default. Platform Admins can enable public registration with a default assigned organization and role (Developer or Viewer).
- **SMTP Integration**: Configurable email integration for team invitations, email verification, and password resets.
- **One-Time Passwords**: Generated initial passwords are shown once in the console UI with a copy button.

## Offline Administrator Password Reset

Reset admin credentials via the CLI if email recovery is unavailable:

### From Source
```bash
go run ./cmd/omniflux reset-password \
  --email admin@example.com \
  --password "NewStrongPassword123456!"
```

### Using Docker
```bash
docker run --rm \
  -e OMNIFLUX_DATABASE_URL="postgres://user:pass@host:5432/omniflux?sslmode=require" \
  omniflux:latest reset-password \
  --email admin@example.com \
  --password "NewStrongPassword123456!"
```

Requires only `OMNIFLUX_DATABASE_URL`. Passwords must be 8–128 characters containing both letters and numbers (no leading or trailing spaces).

## Health Probes

| Endpoint | Probe Type | Description |
| :--- | :--- | :--- |
| `GET /livez` | Liveness | Returns 200 as long as the HTTP process is responsive. |
| `GET /readyz` | Readiness | Returns 200 if PostgreSQL and Redis connections are established; returns 503 otherwise. |

## Prometheus Metrics

Scrape Prometheus metrics at `/metrics`:
- `omniflux_http_requests_total`
- `omniflux_http_request_duration_seconds`
- Model metrics: Token throughput, provider response latencies, fallback frequencies, rate limit rejections.
