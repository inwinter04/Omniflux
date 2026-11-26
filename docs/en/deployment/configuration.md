# Environment Configuration

Omniflux follows 12-Factor principles, configuring all runtime behavior through environment variables.

## Configuration Variables

| Variable | Required | Default | Description |
| :--- | :---: | :--- | :--- |
| `OMNIFLUX_DATABASE_URL` | **Yes** | - | PostgreSQL connection URL (e.g. `postgres://user:pass@host:5432/omniflux?sslmode=require`) |
| `OMNIFLUX_MASTER_KEY` | **Yes** | - | **Critical Secret**: Base64-encoded 32-byte (256-bit) AES key for encrypting stored credentials |
| `OMNIFLUX_API_KEY_PEPPER` | **Yes** | - | **Critical Secret**: Secret salt pepper for API key hashing (**min 32 characters**) |
| `OMNIFLUX_BOOTSTRAP_EMAIL` | **Yes** | - | Initial Platform Admin email on empty database initialization |
| `OMNIFLUX_BOOTSTRAP_PASSWORD`| **Yes** | - | Initial Platform Admin password (**8–128 characters containing both letters and numbers, no leading/trailing spaces**) |
| `OMNIFLUX_ENV` | No | `development` | Environment mode: `development` or `production` |
| `OMNIFLUX_HTTP_ADDR` | No | `127.0.0.1:8080` | Bind IP and port (use `0.0.0.0:8080` for Docker containers) |
| `OMNIFLUX_REDIS_ADDR` | No | `localhost:6379` | Redis host:port connection string |
| `OMNIFLUX_RATE_LIMIT_PER_MIN`| No | `60` | Global default requests/minute rate limit per API key |
| `OTEL_EXPORTER_OTLP_ENDPOINT`| No | - | OpenTelemetry OTLP collector endpoint |

## Secret Generation Examples

Generate cryptographically secure secrets in your terminal:

### 1. MASTER_KEY (32-byte Base64)
```bash
# Linux / macOS
openssl rand -base64 32

# Windows PowerShell
[Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Minimum 0 -Maximum 256 }))
```

### 2. API_KEY_PEPPER (32+ chars)
```bash
# Linux / macOS
openssl rand -hex 24

# Windows PowerShell
-join ((65..90) + (97..122) + (48..57) | Get-Random -Count 40 | ForEach-Object {[char]$_})
```

::: danger Security Warning
`OMNIFLUX_MASTER_KEY` is required to decrypt stored upstream credentials. If lost, existing provider accounts cannot be recovered. Back up this key in a secure vault (e.g. AWS Secrets Manager, HashiCorp Vault).
:::

## Bootstrap Safeguard

::: warning Bootstrap Protection
`OMNIFLUX_BOOTSTRAP_EMAIL` and `OMNIFLUX_BOOTSTRAP_PASSWORD` are processed **only when the database has zero existing users**.

Once initialized, subsequent restarts ignore these variables and log:
`bootstrap skipped, users already exist; bootstrap env vars ignored`

To reset an administrator password, refer to [Bootstrap & Operations](/en/deployment/operations).
:::
