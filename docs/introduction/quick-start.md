# 快速开始

本指南将带你在本地环境于 **5 分钟内** 完成 Omniflux 的启动、配置，并通过标准 OpenAI 客户端发起你的第一次模型对话调用。

## 准备工作

确保本地已安装：
- **Docker** 与 **Docker Compose**
- **Go 1.27+**（仅通过源码运行时需要）
- **curl** 或任意 HTTP 调试工具

## 1. 启动本地基础设施

Omniflux 依赖 PostgreSQL 17 和 Redis 8。项目根目录下提供了开箱即用的 `docker-compose.yml`：

```bash
# 进入 Omniflux 项目根目录
docker compose up -d
```

确认容器正常运行：
```bash
docker compose ps
# 应看到 omniflux-postgres (5432) 与 omniflux-redis (6379) 均处于 healthy / running 状态
```

## 2. 设置必要的环境变量

在终端导出基础环境变量。Omniflux 对生产安全有严格要求，初始启动需要配置以下核心参数：

```bash
# 数据库与 Redis 连接串（本地默认与 docker-compose 对齐）
export OMNIFLUX_DATABASE_URL="postgres://omniflux:omniflux@localhost:5432/omniflux?sslmode=disable"
export OMNIFLUX_REDIS_ADDR="localhost:6379"

# 安全加密密钥：32 字节的 Base64 字符串（用于 AES-256 加密保存上游 Provider 的 API Key）
export OMNIFLUX_MASTER_KEY="MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

# API Key 哈希 Pepper：至少 32 字符的随机安全字符串
export OMNIFLUX_API_KEY_PEPPER="super-secret-pepper-must-be-at-least-32-chars-long"

# 首次启动初始管理员账号与密码（8–128 位且同时包含字母和数字，首尾不能有空格）
export OMNIFLUX_BOOTSTRAP_EMAIL="admin@example.com"
export OMNIFLUX_BOOTSTRAP_PASSWORD="admin-strong-password-1234"
```

::: tip 生产环境提示
以上示例密钥仅供本地测试使用！在生产部署时，请务必使用高强度安全生成器生成独立密钥。完整变量释义请参考 [环境变量配置指南](/deployment/configuration)。
:::

## 3. 启动 Omniflux 服务

前端管理台页面已被静态内嵌至 Go 二进制文件中，直接运行服务端即可同时提供数据面与管理台：

```bash
go run ./cmd/omniflux
```

终端打印包含如下日志，即说明启动成功：
```text
{"level":"info","time":"...","message":"http server listening on 127.0.0.1:8080"}
```

## 4. 首次登录管理台与配置接入

在浏览器中打开管理台控制面板：
```text
http://127.0.0.1:8080/admin/
```

使用初始化管理员账号登录：
- **邮箱**：`admin@example.com`
- **密码**：`admin-strong-password-1234`

登录后，按照以下向导完成基础业务链路配置：

1. **添加供应商账号 (Provider Account)**：
   - 进入「模型接入 → 供应商账号」，点击「新建」。
   - 厂商选择例如 `DeepSeek`、`智谱 GLM`、`通义千问` 或 `自定义 OpenAI 兼容`。
   - 输入你的真实供应商 API Key 并保存（凭据将自动通过 Master Key 进行 AES-256-GCM 加密存储）。
2. **创建路由策略 (Routing Policy)**：
   - 进入「模型接入 → 路由策略」，点击「新建」。
   - 设置名称为 `default-policy`，可配置四维权重（例如质量 0.4、成本 0.3、延迟 0.2、可用性 0.1）。
3. **创建模型服务 (Model Service)**：
   - 进入「模型接入 → 模型服务」，点击「新建」。
   - 服务标识设置为 `general-chat`（这是后续业务调用时传入的 `model` 参数）。
   - 绑定刚才创建的路由策略。
   - 添加候选模型（Candidate）：选择刚添加的 Provider 账号以及对应的上游真实模型名（例如 `deepseek-chat`）。
4. **签发 API Key**：
   - 进入「访问控制 → API Keys」，点击「创建」。
   - 选择默认组织与项目，填写 Key 名称。
   - **注意**：创建成功后弹出的 Key 明文（以 `of_` 开头）仅展示一次，请妥善复制保存。

## 5. 发起第一个请求

通过任何与 OpenAI 兼容的工具调用数据面：

### 选项 A：使用 cURL 发起对话

```bash
export OMNIFLUX_API_KEY="of_xxxxxxxxxxxxxxxxxxxx"

curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "general-chat",
    "messages": [
      {"role": "user", "content": "你好，请介绍一下你自己。"}
    ]
  }'
```

你将收到标准的 OpenAI 响应报文：
```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "created": 1732600000,
  "model": "general-chat",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "你好！我是由 Omniflux 治理驱动的 AI 助手..."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 15,
    "completion_tokens": 28,
    "total_tokens": 43
  }
}
```

### 选项 B：使用 Python OpenAI SDK

修改官方 SDK 的 `base_url` 与 `api_key`：

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8080/v1",
    api_key="of_xxxxxxxxxxxxxxxxxxxx"
)

response = client.chat.completions.create(
    model="general-chat",
    messages=[{"role": "user", "content": "请用一句话证明你工作正常。"}],
    stream=True
)

for chunk in response:
    content = chunk.choices[0].delta.content or ""
    print(content, end="", flush=True)
print()
```

## 6. 验证调用留痕与审计

回到管理台：
- 进入「用量与审计 → 调用明细」，你将看到刚刚发起的请求记录，包括归属项目、使用 Token 数量、上游实际模型、请求耗时与计算成本。
- 点击「路由决策留痕」，可以查看 Omniflux 在挑选该模型候选时的完整打分评估与判定理由。

至此，你已经完成了 Omniflux 从 0 到 1 的全流程体验！
