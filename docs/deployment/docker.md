# Docker 镜像部署

使用 Docker 容器化技术是生产环境部署 Omniflux 的官方推荐方式。

## 镜像多阶段构建解析

项目根目录提供的 `Dockerfile` 采用轻量安全的多阶段构建（Multi-stage Build）：
1. **阶段 1 (admin-builder)**：基于 Node 22 环境构建 Vue 3 + TypeScript 管理台静态产物；
2. **阶段 2 (go-builder)**：基于 Golang 环境将管理台静态资源内嵌打包进单个 Go 二进制文件；
3. **阶段 3 (运行时)**：基于干净小巧的 Alpine Linux 最小运行时镜像，不包含任何多余开发依赖，镜像体积仅约数十兆。

## 快速构建与启动

### 1. 本地构建镜像
```bash
docker build -t omniflux:latest .
```

### 2. 独立运行容器
```bash
docker run -d \
  --name omniflux \
  -p 8080:8080 \
  -e OMNIFLUX_HTTP_ADDR="0.0.0.0:8080" \
  -e OMNIFLUX_DATABASE_URL="postgres://user:password@pg-host:5432/omniflux?sslmode=require" \
  -e OMNIFLUX_REDIS_ADDR="redis-host:6379" \
  -e OMNIFLUX_MASTER_KEY="<YOUR_BASE64_32_BYTE_KEY>" \
  -e OMNIFLUX_API_KEY_PEPPER="<YOUR_RANDOM_PEPPER_MIN_32_CHARS>" \
  -e OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com" \
  -e OMNIFLUX_BOOTSTRAP_PASSWORD="<YOUR_STRONG_PASSWORD>" \
  --restart unless-stopped \
  omniflux:latest
```

## 生产级 Docker Compose 编排模板

以下是一份完整的单机生产环境 `docker-compose.prod.yml` 模板：

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

## 反向代理与 HTTPS 配置注意

在生产环境中，Omniflux 管理台设置的会话 Cookie 为 `SameSite=Strict` 与 `HttpOnly`。

当部署在反向代理（如 Nginx）后面时，请务必启用 HTTPS，此时系统会自动附加 `Secure` 标记。同时，对于数据面 Server-Sent Events (SSE) 流式传输，请在反向代理中**显式关闭缓冲**：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # 关键配置：针对 SSE 流式响应禁用代理缓存与缓冲
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 600s;
}
```
