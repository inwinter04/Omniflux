# Omniflux

<p align="center">
  <b>All intelligence, one current.</b><br/>
  <a href="README.md">简体中文</a> · <a href="README.en-US.md">English</a>
</p>

Omniflux 是一个面向企业的 AI 服务控制平面，用于把分散在多个供应商和私有部署环境中的模型能力，纳入统一的治理、路由、计量、审计与可观测体系。

项目正在积极开发中。首个稳定版本发布前，配置项、数据模型和管理 API 仍可能调整。

## 特性

- **统一模型服务**：通过逻辑模型服务隔离业务调用与真实 Provider 模型。
- **治理化路由**：基于硬约束、成本、质量、延迟与可靠性评分选择候选模型，并记录路由决策。
- **多租户治理**：组织、部门、项目、应用、用户、角色与 API Key 分层管理。
- **计量与归因**：按项目、应用、API Key、Provider 和模型归因请求、Token 与成本。
- **审计与决策留痕**：管理操作、登录事件与 PolicyDecision 可追溯。
- **可观测性**：内置 liveness/readiness 探针、结构化日志与 Prometheus metrics。
- **现代管理台**：基于 Vue 3、TypeScript、Naive UI 和 SoybeanAdmin 的服务端会话管理后台。

## 架构

<p align="center">
  <img src="docs/assets/architecture.zh-CN.png" alt="Omniflux 架构图" width="100%" />
</p>
## 当前接口

| 接口 | 说明 |
| --- | --- |
| `GET /livez` | 进程存活探针 |
| `GET /readyz` | PostgreSQL、Redis 与关键依赖就绪检查 |
| `GET /metrics` | Prometheus 指标 |
| `GET /admin/` | 管理控制台 |
| `POST /api/v1/auth/login` | 管理员登录 |
| `GET /api/v1/auth/me` | 获取当前管理员信息 |
| `POST /api/v1/auth/logout` | 退出登录 |

| `GET /api/v1/organizations` | 组织列表(平台管理员可见全部,其余仅当前组织) |
| `POST /api/v1/organizations` | 创建组织(仅平台管理员) |
| `PATCH /api/v1/organizations/{id}` | 修改组织名称(slug 创建后不可变更) |
| `GET /api/v1/projects` | 项目列表 |
| `GET /api/v1/applications` | 应用列表 |
| `GET /api/v1/provider-accounts` | Provider 账号列表 |
| `POST /api/v1/provider-accounts` | 创建 Provider 账号(凭据加密入库) |
| `DELETE /api/v1/provider-accounts/{id}` | 删除 Provider 账号 |
| `GET /api/v1/route-policies` | 路由策略列表 |
| `POST /api/v1/route-policies` | 创建路由策略 |
| `GET /api/v1/model-services` | 模型服务列表(含候选) |
| `POST /api/v1/model-services` | 创建模型服务与候选 |
| `GET /api/v1/api-keys` | API Key 列表 |
| `POST /api/v1/api-keys` | 创建 API Key(明文仅返回一次) |
| `DELETE /api/v1/api-keys/{id}` | 禁用 API Key |
| `POST /v1/chat/completions` | OpenAI 兼容对话接口(支持流式),使用 API Key 认证 |

数据面按模型服务的候选配置路由:候选按 priority 升序分组,组内按 `weights`(cost/quality/latency/reliability)评分降序排列,评分并列时按 weight 加权随机;候选的 `quality_score`(0-1,默认 0.5)参与 quality 评分,latency 与 reliability 来自近 7 天用量统计。主候选之后的候选作为 fallback 链交给上游网关处理,`fallback_limit` 与 `provider_timeout_ms` 生效。自动质量评估(EvaluationSet/EvaluationRun)将在后续版本实现。每次调用都会写入 `usage_records` 与 `policy_decisions`。

## 快速开始

### 环境要求

- Go 1.27+
- Node.js 20+
- Corepack / pnpm
- PostgreSQL 17
- Redis 8

### 1. 启动基础设施

```bash
docker compose up -d
```

### 2. 配置环境变量

| 变量 | 必填 | 说明 |
| --- | --- | --- |
| `OMNIFLUX_ENV` | 否 | 运行环境，默认 `development` |
| `OMNIFLUX_HTTP_ADDR` | 否 | HTTP 监听地址，默认 `127.0.0.1:8080` |
| `OMNIFLUX_DATABASE_URL` | 是 | PostgreSQL 连接串 |
| `OMNIFLUX_REDIS_ADDR` | 否 | Redis 地址，默认 `localhost:6379` |
| `OMNIFLUX_MASTER_KEY` | 是 | Base64 编码的 32 字节 AES 密钥 |
| `OMNIFLUX_API_KEY_PEPPER` | 是 | API Key 哈希 pepper，至少 32 字符 |
| `OMNIFLUX_BOOTSTRAP_EMAIL` | 是 | 初始管理员邮箱 |
| `OMNIFLUX_BOOTSTRAP_PASSWORD` | 是 | 初始管理员密码，至少 16 字符 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 否 | OpenTelemetry OTLP endpoint |

示例：

```bash
export OMNIFLUX_DATABASE_URL="postgres://omniflux:omniflux@localhost:5432/omniflux?sslmode=disable"
export OMNIFLUX_REDIS_ADDR="localhost:6379"
export OMNIFLUX_MASTER_KEY="<base64-encoded-32-byte-key>"
export OMNIFLUX_API_KEY_PEPPER="<random-pepper-at-least-32-characters>"
export OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com"
export OMNIFLUX_BOOTSTRAP_PASSWORD="<strong-password>"
```

空数据库首次启动时会创建初始管理员。请勿在生产环境使用示例值。

### 3. Docker 部署

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

### 4. 构建管理台

```bash
corepack pnpm --dir web/admin install
make build-admin
```

### 4. 启动服务

```bash
go run ./cmd/omniflux
```

打开管理台：

```text
http://127.0.0.1:8080/admin/
```

默认服务只监听回环地址。若要对外暴露，请设置 `OMNIFLUX_HTTP_ADDR=0.0.0.0:8080`，并配合反向代理、认证与网络策略使用。

## 前端开发

先启动后端：

```bash
go run ./cmd/omniflux
```

再启动 Vite 开发服务器：

```bash
make admin-dev
```

Vite 会将 `/api` 代理到 Go 服务。

## 项目结构

```text
cmd/omniflux/           服务入口
internal/               领域模块、迁移、存储与管理接口
web/admin/              Vue 3 + Naive UI 管理台
scripts/                构建辅助脚本
docker-compose.yml      本地 PostgreSQL 与 Redis
```

## 开发验证

```bash
go test -race ./...
go vet ./...
corepack pnpm --dir web/admin typecheck
corepack pnpm --dir web/admin build
```

## 安全说明

- 管理员密码使用 Argon2id 哈希。
- 管理台会话保存在服务端，Cookie 使用 `HttpOnly` 与 `SameSite=Strict`。
- Provider 凭据使用 AES-256-GCM 加密存储。
- API Key 只保存 keyed hash，明文只在创建时返回一次。
- 审计事件采用追加式模型，应用层不提供更新与删除能力。
- 请勿在日志、示例、提交信息或 issue 中提交真实密钥。

