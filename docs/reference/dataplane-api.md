# 数据面 API (多协议网关)

Omniflux 数据面提供面向应用程序、Agent 与 **AI 编程客户端（Codex CLI、Claude Code、Gemini CLI）** 的统一多协议接口，支持在客户端协议与上游账号协议（`openai_responses` / `openai_chat` / `anthropic_messages` / `gemini_native`）之间进行多模态请求转换、流式透传、跨协议回退与分级缓存计费。

## 鉴权认证机制

不同协议客户端可按原生习惯传递 `of_` 前缀的 Omniflux API Key：

- **OpenAI / Codex CLI**: `Authorization: Bearer <YOUR_OMNIFLUX_API_KEY>`
- **Anthropic / Claude Code**: `x-api-key: <YOUR_OMNIFLUX_API_KEY>` 或 `Authorization: Bearer <YOUR_OMNIFLUX_API_KEY>`
- **Google Gemini CLI / GenAI SDK**: `x-goog-api-key: <YOUR_OMNIFLUX_API_KEY>` 或查询参数 `?key=<YOUR_OMNIFLUX_API_KEY>`

- API Key 必须以 `of_` 开头；
- 系统根据 Key 对应的组织与项目，严格校验对目标模型服务或资产的调用权限（`read` 查验、`invoke` 执行）；
- 若 Key 被停用或过期，返回 `401`；若所属项目或应用被停用，返回 `403`。被拦截的请求不会进入上游，也不会计入 Token 消耗。

## 核心端点概览

| HTTP 方法与路径 | 功能说明 | 所需权限 |
| :--- | :--- | :---: |
| `GET /v1/models` | 获取当前项目授权可用的全部具体模型（`upstream_model`）及已开启 `expose_router` 的智能路由入口模型（`router_model_id`，`owned_by: "omniflux-router"`） | `read` |
| `GET /v1/quota` | 查询当前 API Key 额度与近 30 天用量（优先预算窗口，可选开启 60s 缓存上游余量，否则返回用量汇总；供 CC Switch 等客户端使用） | `read` |
| `POST /v1/chat/completions` | OpenAI Chat Completions 接口（支持图文多模态、具体模型直选与 SSE 流式保活） | `invoke` |
| `POST /v1/responses` | OpenAI Responses 接口（Codex CLI 主协议，支持具体模型直选、`input_image`、PDF `input_file`、工具调用、Prompt Caching 与 SSE 流式保活；当路由到 `openai_chat` 账号时自动桥接） | `invoke` |
| `POST /v1/responses/compact` | OpenAI Responses 上下文压缩接口 | `invoke` |
| `POST /v1/messages` | Anthropic Messages 接口（Claude Code 主协议，支持 `image`/`document` 多模态、Prompt Caching 读/写计费与 SSE 流式保活） | `invoke` |
| `POST /v1/messages/count_tokens` | Anthropic Token 计数预检（上游不支持时自动降级本地估算） | `invoke` |
| `GET /v1beta/models` | Gemini Native 模型发现接口（具体模型及智能路由服务，`models/{id}` 格式） | `read` |
| `POST /v1beta/models/{model}:generateContent` | Gemini Native 非流式生成接口 | `invoke` |
| `POST /v1beta/models/{model}:streamGenerateContent` | Gemini Native 流式生成接口（支持 `?alt=sse` 与原生 JSON 数组分块流） | `invoke` |
| `POST /v1beta/models/{model}:countTokens` | Gemini Native Token 计数接口（上游不支持时自动降级本地估算） | `invoke` |
| `POST /v1/embeddings` | 文本向量化接口（兼容 OpenAI Embeddings 协议） | `invoke` |
| `GET /v1/prompt-templates` | 获取当前项目被授权的已发布提示词模板列表 | `read` |
| `GET /v1/prompt-templates/{name}` | 读取特定已发布提示词模板的配置详情与变量约束 | `read` |
| `POST /v1/prompt-templates/{name}/render` | 传入变量渲染已发布提示词模板正文（不调用模型） | `invoke` |
| `GET /v1/agents` | 获取当前项目被授权的已发布智能体列表 | `read` |
| `GET /v1/agents/{name}` | 读取特定智能体的配置与发布详情 | `read` |
| `POST /v1/agents/{name}/chat/completions` | 调用 Agent 进行多轮对话交互（会话上下文保持，支持流式） | `invoke` |

## AI 编程客户端接入示例

管理台的 **访问控制 → API Key → 客户端配置** 向导支持直接生成下列配置，并提供 **CC Switch 一键导入**（基于 `ccswitch://v1/import` 协议自动写入网关地址、协议格式与 API Key）。

### 1. Codex CLI (`~/.codex/config.toml`)

```toml
model_provider = "omniflux"
model = "coding-pro"

[model_providers.omniflux]
name = "Omniflux Gateway"
base_url = "http://127.0.0.1:8080/v1"
wire_api = "responses"
env_key = "OPENAI_API_KEY"
```

```bash
export OPENAI_API_KEY="$OMNIFLUX_API_KEY"
codex -m coding-pro
```

### 2. Claude Code

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:8080"
export ANTHROPIC_AUTH_TOKEN="$OMNIFLUX_API_KEY"
export ANTHROPIC_MODEL="coding-pro"
claude
```

### 3. Gemini CLI

```bash
export GOOGLE_GEMINI_BASE_URL="http://127.0.0.1:8080"
export GEMINI_API_KEY="$OMNIFLUX_API_KEY"
export GEMINI_MODEL="coding-pro"
gemini
```

> **粘性会话亲和 (KV Cache Pinning)**：网关会自动提取 `Session-Id`、`X-Session-Id`、`X-Codex-Session-Id`、`Conversation-Id`、`prompt_cache_key` / `previous_response_id` / `metadata.user_id`；当路由策略启用 `session_affinity` 时，同一会话优先固定路由至同优先级组内的同一候选节点以提高缓存命中率，并在节点异常或熔断时自动无缝转移。
>
> **双端适配与智能编排 (`auto_orchestrator`)**：普通模型服务默认只向外暴露各可用候选的具体 `upstream_model`，便于 Codex CLI、Claude Code、CC Switch 与 Dify / Cherry Studio 等工具直接列举并自由切换底层模型；仅需向调用方暴露单一自动编排入口（如 `omniflux/auto`）时开启 `expose_router = true` 并绑定 `auto_orchestrator` 策略。`auto_orchestrator` 默认采用 `adaptive` 模式：短问答按轮动态分流降本，进入多步工具链或长上下文编程会话时自动锁定缓存只升不降。

## 调用示例实战

### 1. Responses API 多模态图像理解

```bash
curl -sS http://127.0.0.1:8080/v1/responses \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "coding-pro",
    "input": [{
      "role": "user",
      "content": [
        {"type": "input_text", "text": "请分析这张报错截图并给出修复建议"},
        {"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgo..."}
      ]
    }]
  }'
```

### 2. 标准对话 (非流式 Chat Completions)

```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "general-chat",
    "messages": [
      {"role": "system", "content": "你是一名严谨的助手。"},
      {"role": "user", "content": "什么是控制平面？"}
    ],
    "temperature": 0.7
  }'
```

### 3. 流式响应 (Server-Sent Events)

设置 `"stream": true`，服务端将在响应头设置 `Content-Type: text/event-stream` 并关闭写超时限制，确保长代码生成与多步工具调用流不被中断：

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "general-chat",
    "stream": true,
    "messages": [
      {"role": "user", "content": "请用五句话介绍量子计算。"}
    ]
  }'
```

## 按协议适配的错误响应与机器字段

网关根据客户端入口协议返回对应的原生错误信封，同时所有错误响应均携带稳定的 `reason` 字段与 `X-Request-ID` 响应头：

- **OpenAI Chat / Responses (`/v1/chat/completions`, `/v1/responses`)**:
```json
{
  "error": {
    "type": "invalid_request_error",
    "code": "invalid_request",
    "reason": "missing_model",
    "message": "The model parameter is required.",
    "param": "model",
    "params": {},
    "request_id": "req_01JXXXXXXXXXXXX"
  }
}
```
- **Anthropic Messages (`/v1/messages`)**:
```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "code": "invalid_request",
    "reason": "missing_model",
    "message": "The model parameter is required.",
    "request_id": "req_01JXXXXXXXXXXXX"
  }
}
```
- **Gemini Native (`/v1beta/models/...`)**:
```json
{
  "error": {
    "code": 400,
    "status": "INVALID_ARGUMENT",
    "error_code": "invalid_request",
    "reason": "missing_model",
    "message": "The model parameter is required.",
    "request_id": "req_01JXXXXXXXXXXXX"
  }
}
```
