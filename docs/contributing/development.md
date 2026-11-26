# 本地开发与代码规范

感谢你对 Omniflux 做出贡献！本篇文档将介绍仓库的代码组织架构、前后端协同开发循环与持续集成门禁标准。

## 项目整体结构

Omniflux 遵循标准的清晰分层架构：

| 目录 / 文件 | 说明 |
| :--- | :--- |
| `cmd/omniflux/` | 服务主入口与 CLI 子命令（如 `reset-password`） |
| `internal/apikeys/` | API Key 签发、加盐哈希校验与生命周期 |
| `internal/app/` | 依赖装配与应用生命周期容器 |
| `internal/asset/` | PromptTemplate 与 Agent 企业资产模型 |
| `internal/auth/` | 会话 Session、Argon2id 密码哈希与 RBAC 鉴权 |
| `internal/bifrostx/` | 模型网关核心适配层 |
| `internal/bootstrap/` | 初始管理员自动初始化检查 |
| `internal/config/` | 全量运行时配置与环境变量解析 |
| `internal/console/` | 前端管理台静态资源嵌入包 (`embed.FS`) |
| `internal/dataplane/` | 数据面 HTTP 路由、OpenAI 协议解析与执行引擎 |
| `internal/httpx/` | 统一 HTTP 中间件、日志与标准错误格式化 |
| `internal/infra/` | PostgreSQL 客户端与 Redis 连接池 |
| `internal/logging/` | 高性能结构化 JSON 日志 |
| `internal/mail/` | SMTP 邮件发送服务 |
| `internal/migrations/` | 嵌入式数据库迁移脚本 (`goose`) |
| `internal/vault/` | 凭据 AES-256-GCM 加密与密钥托管 |
| `web/admin/` | 基于 Vue 3 + Naive UI + SoybeanAdmin 的管理台 |
| `docs/` | 基于 VitePress 的官方文档站源码 |
| `scripts/` | 自动化脚本与辅助工具 |
| `docker-compose.yml` | 本地开发用 PostgreSQL 17 与 Redis 8 |
| `Makefile` | 快捷工程指令集合 |

## 本地协同开发循环

### 1. 启动后端服务
```bash
make infra  # 确保本地数据库与 Redis 正常运行
make run    # 启动 Go 后端（监听 127.0.0.1:8080）
```

### 2. 启动前端 Vite 开发服务器
在开发管理台时，启动 Vite 前端热重载服务：
```bash
make admin-dev
```
Vite 会自动将所有 `/api/*` 请求透明反向代理至 Go 后端端口，修改 Vue/TS 页面可秒级热更新。

## 代码质量门禁规范

在向主分支提交 Pull Request 之前，必须确保以下本地验证全部通过：

```bash
# 1. 运行后端全量并发竞态单元测试
go test -race ./...

# 2. Go 静态语法与安全检查
go vet ./...

# 3. 前端 TypeScript 类型严谨检查
corepack pnpm --dir web/admin typecheck

# 4. 前端生产打包校验
corepack pnpm --dir web/admin build

# 5. 文档站静态构建校验（检查死链）
make docs-build
```

GitHub Actions CI 会在每次推送分支与创建 PR 时对上述检查进行自动化验证。
