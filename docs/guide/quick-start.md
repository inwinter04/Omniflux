# 快速开始

本页引导你从零启动 Omniflux:基础设施 → 配置 → 部署 → 首次登录。

## 环境要求

- Go 1.27+(源码运行)
- Node.js 20+ / Corepack / pnpm(仅构建管理台需要)
- PostgreSQL 17
- Redis 8
- Docker(推荐,基础设施与镜像部署)

## 1. 启动基础设施

仓库根目录的 docker-compose.yml 提供 PostgreSQL 与 Redis:

```bash
docker compose up -d
```

## 2. 配置环境变量

| 变量 | 必填 | 说明 |
| --- | --- | --- |
| `OMNIFLUX_ENV` | 否 | 运行环境,默认 `development` |
| `OMNIFLUX_HTTP_ADDR` | 否 | HTTP 监听地址,默认 `127.0.0.1:8080` |
| `OMNIFLUX_DATABASE_URL` | 是 | PostgreSQL 连接串 |
| `OMNIFLUX_REDIS_ADDR` | 否 | Redis 地址,默认 `localhost:6379` |
| `OMNIFLUX_MASTER_KEY` | 是 | Base64 编码的 32 字节 AES 密钥,用于 Provider 凭据加密 |
| `OMNIFLUX_API_KEY_PEPPER` | 是 | API Key 哈希 pepper,至少 32 字符 |
| `OMNIFLUX_BOOTSTRAP_EMAIL` | 是 | 初始管理员邮箱 |
| `OMNIFLUX_BOOTSTRAP_PASSWORD` | 是 | 初始管理员密码,至少 16 字符 |
| `OMNIFLUX_RATE_LIMIT_PER_MIN` | 否 | 数据面全局默认限流(次/分钟),默认 `60`;API Key 的 `rate_limit_per_min` 可按 Key 覆盖 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 否 | OpenTelemetry OTLP endpoint |

示例:

```bash
export OMNIFLUX_DATABASE_URL="postgres://omniflux:omniflux@localhost:5432/omniflux?sslmode=disable"
export OMNIFLUX_REDIS_ADDR="localhost:6379"
export OMNIFLUX_MASTER_KEY="<base64-encoded-32-byte-key>"
export OMNIFLUX_API_KEY_PEPPER="<random-pepper-at-least-32-characters>"
export OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com"
export OMNIFLUX_BOOTSTRAP_PASSWORD="<strong-password>"
```

::: warning
空数据库首次启动时会创建初始管理员,请勿在生产环境使用示例值。数据库已有用户时,`OMNIFLUX_BOOTSTRAP_EMAIL` 与 `OMNIFLUX_BOOTSTRAP_PASSWORD` 不会生效,启动日志会输出 `bootstrap skipped, users already exist; bootstrap env vars ignored`。
:::

## 3. Docker 部署

生产环境推荐使用镜像部署,一条命令完成管理台与服务端构建:

```bash
docker build -t omniflux:latest .
```

运行时通过环境变量注入配置:

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

会话 Cookie 为 `SameSite=Strict` + `HttpOnly`,生产环境请置于反向代理之后并启用 HTTPS(此时 Cookie 自动附加 `Secure`)。

## 4. 源码运行

管理台的构建产物已内嵌在服务端,源码运行无需先构建前端:

```bash
go run ./cmd/omniflux
```

打开管理台:

```text
http://127.0.0.1:8080/admin/
```

## 5. 首次登录

使用 `OMNIFLUX_BOOTSTRAP_EMAIL` / `OMNIFLUX_BOOTSTRAP_PASSWORD` 登录管理台,初始账号为平台管理员。

登录后按顺序完成接入配置:创建组织与项目 → 添加 Provider 账号 → 创建路由策略 → 创建模型服务与候选 → 签发 API Key,之后即可从数据面调用模型服务。

::: tip Provider base_url 约束
openai / openai_compatible / anthropic 类 provider 的 base_url 不要以 `/v1` 结尾,网关会在其后自动拼接 `/v1/chat/completions`、`/v1/messages` 等路径。同一 provider 类型只允许一个 base_url,多个供应商请使用不同 provider 类型(例如 GLM 可走 anthropic 兼容端点)。
:::

## 6. 重置管理员密码

管理员密码只在空数据库首次启动时由 `OMNIFLUX_BOOTSTRAP_PASSWORD` 创建,之后修改环境变量不会更新已有账号。忘记密码时使用 `reset-password` 子命令:

```bash
go run ./cmd/omniflux reset-password --email admin@example.com --password "<new-password>"
```

命令只依赖 `OMNIFLUX_DATABASE_URL`,新密码与初始密码一样要求至少 16 字符。目标邮箱不存在时命令报错退出,不会静默成功。

Docker 部署时通过容器执行:

```bash
docker run --rm \
  -e OMNIFLUX_DATABASE_URL="postgres://user:pass@host:5432/omniflux?sslmode=require" \
  omniflux:latest reset-password --email admin@example.com --password "<new-password>"
```
