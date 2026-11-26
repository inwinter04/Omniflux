# Building from Source

Instructions for building, running, and contributing to Omniflux from source.

## Prerequisites

- **Go 1.27+**
- **Node.js 20+**
- **pnpm** (`corepack enable`)
- **Make**

## Build Steps

### 1. Clone & Start Dependencies
```bash
git clone https://github.com/inwinter04/Omniflux.git
cd Omniflux
make infra
```

### 2. Build Console Assets
```bash
# Installs dependencies, compiles the frontend, and embeds assets into internal/console/dist
make build-admin
```

### 3. Run the Backend
```bash
export OMNIFLUX_DATABASE_URL="postgres://omniflux:omniflux@localhost:5432/omniflux?sslmode=disable"
export OMNIFLUX_REDIS_ADDR="localhost:6379"
export OMNIFLUX_MASTER_KEY="MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
export OMNIFLUX_API_KEY_PEPPER="local-dev-pepper-must-be-at-least-32-chars-long"
export OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com"
export OMNIFLUX_BOOTSTRAP_PASSWORD="admin-password-16chars"

go run ./cmd/omniflux
```

Embedded `goose` migrations execute automatically on startup.

### 4. Compile Standalone Binary
```bash
go build -ldflags="-s -w" -o bin/omniflux ./cmd/omniflux
```
Produces a single executable containing the entire control plane and management console.
