# 环境变量配置指南

Omniflux 遵循标准的 12-Factor 原则，所有运行时行为均由系统环境变量控制。

## 核心环境变量字典

| 变量名 | 必填 | 默认值 | 作用详细说明 |
| :--- | :---: | :--- | :--- |
| `OMNIFLUX_DATABASE_URL` | **是** | - | PostgreSQL 数据库标准连接字符串，例如 `postgres://user:pass@127.0.0.1:5432/omniflux?sslmode=disable` |
| `OMNIFLUX_MASTER_KEY` | **是** | - | **高保密安全项**：Base64 编码的 32 字节（256 位）AES 密钥，用于对称加密落库的 Provider 凭据 |
| `OMNIFLUX_API_KEY_PEPPER` | **是** | - | **高保密安全项**：用于加盐哈希 API Key 的随机字符串，**长度必须至少达到 32 字符** |
| `OMNIFLUX_BOOTSTRAP_EMAIL` | **是** | - | 首次空数据库初始化时的初始管理员登录邮箱 |
| `OMNIFLUX_BOOTSTRAP_PASSWORD`| **是** | - | 初始管理员登录密码，**8–128 位且同时包含字母和数字（首尾不能有空格）** |
| `OMNIFLUX_ENV` | 否 | `development` | 运行环境标识：`development` 或 `production` |
| `OMNIFLUX_HTTP_ADDR` | 否 | `127.0.0.1:8080` | HTTP 服务监听的绑定 IP 与端口号，容器化部署时常设为 `0.0.0.0:8080` |
| `OMNIFLUX_REDIS_ADDR` | 否 | `localhost:6379` | Redis 服务连接地址（主机名:端口） |
| `OMNIFLUX_RATE_LIMIT_PER_MIN`| 否 | `60` | 数据面 API 的全局默认单 Key 限流阈值（次/分钟）；单个 Key 未独立配置时沿用此阈值 |
| `OTEL_EXPORTER_OTLP_ENDPOINT`| 否 | - | OpenTelemetry 追踪上报端点（如 `http://otel-collector:4317`） |

## 生产安全密钥生成指南

在生产环境中，严禁使用弱口令或演示用字符串。可通过系统原生终端命令生成强随机密钥：

### 1. 生成 32 字节 Base64 的 MASTER_KEY
```bash
# Linux / macOS
openssl rand -base64 32

# Windows PowerShell
[Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Minimum 0 -Maximum 256 }))
```

### 2. 生成 32+ 字符随机串的 API_KEY_PEPPER
```bash
# Linux / macOS
openssl rand -hex 24

# Windows PowerShell
-join ((65..90) + (97..122) + (48..57) | Get-Random -Count 40 | ForEach-Object {[char]$_})
```

::: danger 灾难恢复与密钥保管须知
`OMNIFLUX_MASTER_KEY` 是解密数据库中所有 Provider 凭据的唯一钥匙！一旦丢失，数据库中保存的所有外部 API 密钥将永久无法解密。请务必妥善保存在安全的密钥管理系统（如 HashiCorp Vault、AWS KMS 等）中。
:::

## 初始管理员启动守卫逻辑

::: warning 初始管理员启动守卫
`OMNIFLUX_BOOTSTRAP_EMAIL` 与 `OMNIFLUX_BOOTSTRAP_PASSWORD` **仅在数据库用户表完全为空**的首次初始化启动时生效。

一旦数据库中已存在任何用户（包括初始管理员被创建后），后续重启即便修改了这两个环境变量，系统也会自动忽略，并在日志中输出：
`bootstrap skipped, users already exist; bootstrap env vars ignored`

如果忘记管理员密码，请参考 [用户初始化与运维保障](/deployment/operations) 使用离线密码重置工具。
:::
