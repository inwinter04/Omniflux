# Docker Deployment

Docker containerization is the recommended production deployment method for Omniflux.

## Multi-Stage Build

The root `Dockerfile` employs multi-stage compilation:
1. **Stage 1 (admin-builder)**: Compiles the Vue 3 + TypeScript console using Node 22;
2. **Stage 2 (go-builder)**: Embeds console assets and builds the Go binary;
3. **Stage 3 (Runtime)**: Minimal Alpine Linux base, producing an image size under 50MB.

## Build & Run

### 1. Build Image
```bash
docker build -t omniflux:latest .
```

### 2. Run Container
```bash
docker run -d \
  --name omniflux \
  -p 8080:8080 \
  -e OMNIFLUX_HTTP_ADDR="0.0.0.0:8080" \
  -e OMNIFLUX_DATABASE_URL="postgres://user:pass@host:5432/omniflux?sslmode=require" \
  -e OMNIFLUX_REDIS_ADDR="redis:6379" \
  -e OMNIFLUX_MASTER_KEY="<BASE64_32_BYTE_KEY>" \
  -e OMNIFLUX_API_KEY_PEPPER="<PEPPER_MIN_32_CHARS>" \
  -e OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com" \
  -e OMNIFLUX_BOOTSTRAP_PASSWORD="<STRONG_PASSWORD_MIN_16>" \
  --restart unless-stopped \
  omniflux:latest
```

## Production Docker Compose Template

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:17-alpine
    restart: always
    environment:
      POSTGRES_DB: omniflux
      POSTGRES_USER: omniflux
      POSTGRES_PASSWORD: StrongPostgresPassword123!
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U omniflux"]
      interval: 5s
      timeout: 5s
      retries: 5

  redis:
    image: redis:8-alpine
    restart: always
    command: redis-server --appendonly yes
    volumes:
      - redisdata:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 5s
      retries: 5

  omniflux:
    image: omniflux:latest
    restart: always
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      OMNIFLUX_ENV: "production"
      OMNIFLUX_HTTP_ADDR: "0.0.0.0:8080"
      OMNIFLUX_DATABASE_URL: "postgres://omniflux:StrongPostgresPassword123!@postgres:5432/omniflux?sslmode=disable"
      OMNIFLUX_REDIS_ADDR: "redis:6379"
      OMNIFLUX_MASTER_KEY: "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
      OMNIFLUX_API_KEY_PEPPER: "production-secure-pepper-must-be-very-long-32-chars"
      OMNIFLUX_BOOTSTRAP_EMAIL: "admin@company.com"
      OMNIFLUX_BOOTSTRAP_PASSWORD: "AdminStrongPass123456!"

volumes:
  pgdata:
  redisdata:
```

## Nginx Reverse Proxy Configuration

Session cookies use `SameSite=Strict` and `HttpOnly`. When deployed behind Nginx with HTTPS, cookies are automatically marked `Secure`. Ensure proxy buffering is disabled for streaming:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # Crucial for SSE streaming
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 600s;
}
```
