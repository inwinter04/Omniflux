# 数据面 API (OpenAI 兼容)

Omniflux 数据面提供面向客户端应用与业务系统的统一接口，**全面兼容 OpenAI 开放规范**。

## 鉴权认证机制

所有数据面接口均采用标准的 Bearer Token 形式进行认证：

```http
Authorization: Bearer <YOUR_OMNIFLUX_API_KEY>
```

- API Key 必须以 `of_` 开头；
- 系统根据 Key 对应的组织与项目，严格校验对目标模型服务或资产的调用权限（`read` 查验、`invoke` 执行）；
- 若 Key 被停用或过期，返回 `401 Unauthorized`；若所属项目或应用被停用，返回 `403 Forbidden`。被拦截的请求不会进入上游，也不会计入 Token 消耗。

## 核心端点概览

| HTTP 方法与路径 | 功能说明 | 所需权限 |
| :--- | :--- | :---: |
| `GET /v1/models` | 获取当前项目授权可调用的逻辑模型服务列表 | `read` |
| `POST /v1/chat/completions` | 标准对话生成接口（支持多轮对话与 SSE 流式返回） | `invoke` |
| `POST /v1/embeddings` | 文本向量化接口（兼容 OpenAI Embeddings 协议） | `invoke` |
| `GET /v1/prompt-templates` | 获取当前项目被授权的已发布提示词模板列表 | `read` |
| `GET /v1/prompt-templates/{name}` | 读取特定已发布提示词模板的配置详情与变量约束 | `read` |
| `POST /v1/prompt-templates/{name}/render` | 传入变量渲染已发布提示词模板正文（不调用模型） | `invoke` |
| `GET /v1/agents` | 获取当前项目被授权的已发布智能体列表 | `read` |
| `GET /v1/agents/{name}` | 读取特定智能体的配置与发布详情 | `read` |
| `POST /v1/agents/{name}/chat/completions` | 调用 Agent 进行多轮对话交互（会话上下文保持，支持流式） | `invoke` |

## 调用示例实战

### 1. 标准对话 (非流式)

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

### 2. 流式响应 (Server-Sent Events)

设置 `"stream": true`，服务端将在响应头设置 `Content-Type: text/event-stream`，并在每块数据前以 `data: {...}` 推送，流结束标志为 `data: [DONE]`：

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

## 统一错误码与机器可读字段

所有数据面错误均使用标准的 OpenAI 格式封装，同时提供稳定的机器判断标识：

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

| 字段名 | 类型 | 说明 |
| :--- | :--- | :--- |
| `type` | string | 错误宏观类别，例如 `invalid_request_error`、`authentication_error`、`rate_limit_error`、`upstream_error` |
| `code` | string | 稳定的粗粒度错误码 |
| `reason` | string | **最推荐的机器判断依据**，具备精细语义（如 `model_access_denied`、`budget_exceeded`、`provider_timeout`） |
| `message`| string | 供开发者排查阅读的英文可读提示，**请勿在业务代码中解析此文案** |
| `param`  | string | 导致出错的具体请求参数名（若有） |
| `request_id` | string | 全链路排查追踪 ID，可通过响应头 `X-Request-ID` 同步获取 |
