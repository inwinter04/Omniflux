# Omniflux

<p align="center">
  <b>All intelligence, one current.</b><br/>
  <a href="README.md">简体中文</a> · <a href="README.en-US.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/inwinter04/Omniflux/releases"><img src="https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.27+"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache--2.0-blue" alt="License: Apache-2.0"></a>
  <img src="https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white" alt="PostgreSQL 17">
  <img src="https://img.shields.io/badge/Redis-8-DC382D?logo=redis&logoColor=white" alt="Redis 8">
</p>

Omniflux 是一个面向企业的 AI 服务控制平面,用于把分散在多个供应商和私有部署环境中的模型能力,纳入统一的治理、路由、计量、审计与可观测体系。

项目自 v0.1.0 起进入稳定化迭代,配置项、数据模型与管理 API 的破坏性变更将在 Release 说明中标注。

## 特性

- **统一模型服务**:通过逻辑模型服务隔离业务调用与真实 Provider 模型。
- **治理化路由**:基于硬约束、成本、质量、延迟与可靠性评分选择候选模型,并记录路由决策。
- **多租户治理**:组织、部门、项目、应用、用户、角色与 API Key 分层管理。
- **计量与归因**:按项目、应用、API Key、Provider 和模型归因请求、Token 与成本。
- **审计与决策留痕**:管理操作、登录事件与 PolicyDecision 可追溯。
- **可观测性**:内置 liveness/readiness 探针、结构化日志,以及数据面 Prometheus 业务指标。
- **现代管理台**:基于 Vue 3、TypeScript、Naive UI 和 SoybeanAdmin 的服务端会话管理后台。

## 架构

<p align="center">
  <img src="docs/assets/architecture.zh-CN.png" alt="Omniflux 架构图" width="100%" />
</p>

## 快速开始

### 1. 启动基础设施

```bash
docker compose up -d
```

### 2. 配置环境变量

| 变量 | 必填 | 说明 |
| --- | --- | --- |
| `OMNIFLUX_DATABASE_URL` | 是 | PostgreSQL 连接串 |
| `OMNIFLUX_MASTER_KEY` | 是 | Base64 编码的 32 字节 AES 密钥 |
| `OMNIFLUX_API_KEY_PEPPER` | 是 | API Key 哈希 pepper,至少 32 字符 |
| `OMNIFLUX_BOOTSTRAP_EMAIL` | 是 | 初始管理员邮箱 |
| `OMNIFLUX_BOOTSTRAP_PASSWORD` | 是 | 初始管理员密码,至少 16 字符 |

完整变量列表(监听地址、限流、OpenTelemetry 等)见[文档站 · 快速开始](https://inwinter04.github.io/Omniflux/guide/quick-start)。

### 3. 启动服务

使用 Docker:

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

或使用源码(管理台构建产物已内嵌,无需先构建前端):

```bash
go run ./cmd/omniflux
```

打开管理台 `http://127.0.0.1:8080/admin/`,用初始邮箱与密码登录。空数据库首次启动会自动创建平台管理员,请勿在生产环境使用示例值。

## 数据面 API

业务系统使用 API Key(`Authorization: Bearer`)调用 OpenAI 兼容接口:

| 接口 | 说明 |
| --- | --- |
| `GET /v1/models` | 可用模型服务列表 |
| `POST /v1/chat/completions` | 对话接口(支持流式) |
| `POST /v1/embeddings` | 向量化接口 |

路由按候选配置执行:硬约束过滤 → 优先级分组 → 成本/质量/延迟/可靠性评分 → fallback;每次调用写入用量与决策留痕,预算与限流按 Key 生效。详见[文档站 · 数据面 API](https://inwinter04.github.io/Omniflux/api/dataplane)。

完整管理面接口清单见[文档站 · 管理面 API](https://inwinter04.github.io/Omniflux/api/admin)。

## 开发

```bash
make infra          # 启动本地 PostgreSQL 与 Redis
make test           # go test -race ./...
make vet            # go vet ./...
make build-admin    # 构建管理台并同步到服务端
make docs-build     # 构建文档站
```

项目结构与前端开发说明见[文档站 · 开发](https://inwinter04.github.io/Omniflux/guide/development)。

## 安全说明

- 管理员密码使用 Argon2id 哈希。
- 管理台会话保存在服务端,Cookie 使用 `HttpOnly` 与 `SameSite=Strict`。
- Provider 凭据使用 AES-256-GCM 加密存储。

## License

[Apache-2.0](LICENSE)
