# 用户初始化与运维保障

本篇归纳了在生产运维 Omniflux 过程中的核心操作指南，包括离线管理员密码重置、健康探针检测与全链路可观测性配置。

## 初始管理员账号与注册体系

### 1. 自动 Bootstrap
在首次运行且数据库用户表为空时，服务会读取 `OMNIFLUX_BOOTSTRAP_EMAIL` 与 `OMNIFLUX_BOOTSTRAP_PASSWORD` 创建顶级**平台管理员（Platform Admin）**。此后该环境变量将被忽略。

### 2. 公开注册与邀请机制
- **默认关闭注册**：为保障企业安全性，新安装的系统默认关闭自主注册入口。
- **开启自主注册**：平台管理员登录后，可在「系统设置」中开启自主注册，并指定新注册用户的归属组织与默认角色（如“开发者”或“查看者”）。
- **邮件服务集成 (SMTP)**：配置 SMTP 凭据（密码经 AES-256 加密保存）后，系统支持：
  - 发送邮件邀请成员加入组织；
  - 注册邮箱真伪验证；
  - 找回密码与安全链接重置。
- **一次性随机密码**：管理员在后台创建用户时，支持生成高强度随机初始密码，创建成功后界面仅回显一次并提供一键复制按钮。

## 离线重置管理员密码 (Reset Password)

如果管理员遗忘了密码且未配置邮件找回，可使用服务端内置的命令行工具 `reset-password` 进行离线重置。

### 源码环境执行
```bash
go run ./cmd/omniflux reset-password \
  --email admin@example.com \
  --password "NewStrongPassword123456!"
```

### Docker 容器化环境执行
利用正在运行的容器或启一个临时容器直接连接数据库重置：
```bash
docker run --rm \
  -e OMNIFLUX_DATABASE_URL="postgres://user:pass@db-host:5432/omniflux?sslmode=require" \
  omniflux:latest reset-password \
  --email admin@example.com \
  --password "NewStrongPassword123456!"
```

::: tip 命令说明
此命令仅依赖数据库连接串 `OMNIFLUX_DATABASE_URL`，无需启动主 HTTP 服务。新密码与初始密码要求一致，必须为 8–128 位且同时包含字母和数字（首尾不能有空格）。若目标邮箱不存在，命令将明确报错退出。
:::

## 探针检查与容器健康检测

Omniflux 内置了标准的云原生探针端点，适合接入 Kubernetes 或 Docker 容器健康检查：

| 端点路径 | 探针类型 | 说明 |
| :--- | :--- | :--- |
| `GET /livez` | 存活探针 (Liveness) | 检查服务进程是否存活。只要 Web 容器正在响应即返回 200。 |
| `GET /readyz` | 就绪探针 (Readiness) | 检查核心外部依赖（PostgreSQL 与 Redis）是否联通就绪。就绪返回 200，故障返回 503。 |

## 指标采集与 Prometheus 监控

系统在 `/metrics` 路径对外输出 Prometheus 兼容指标，涵盖：
- `omniflux_http_requests_total`：HTTP 请求总数（按 path, status, method 标记）；
- `omniflux_http_request_duration_seconds`：请求耗时分布直方图；
- 数据面模型调用业务指标：涵盖 Token 吞吐量、各 Provider 响应延迟分布、Fallback 触发频次以及限流拦截率。
