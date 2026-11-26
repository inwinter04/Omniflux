# 智能体运行时 (Agent Runtime)

当业务需求超越单次 Prompt 问答，走向具有多轮上下文记忆、固定业务规则与专属模型绑定的复合场景时，Omniflux 的 **Agent 运行时** 为其提供了开箱即用的工程化支撑。

## 什么是 Omniflux Agent

Omniflux Agent 将以下三大要素组合为一种可发布的企业资产：
1. **固定的提示词资产版本**（PromptTemplate 及其特定 Version）；
2. **绑定的逻辑模型服务**（Model Service）；
3. **固定的超参数集合**（Temperature、Top P、Max Tokens、Frequency Penalty 等）。

通过发布一个 Agent，业务系统不再需要自行管理长篇复杂的 System Prompt 和模型调度逻辑，只需将其作为一个黑盒助手直接调用。

## 会话保持机制 (Session Management)

Agent 运行时原生支持**服务端多轮会话管理**：

1. **首轮交互（创建会话）**：
   - 客户端调用 `/v1/agents/{name}/chat/completions`，传入 `message` 以及模板所要求的 `variables`；
   - 服务端初始化会话，并在 HTTP 响应头中返回：
     - `X-Agent-Session-Id`：当前会话唯一标识符；
     - `X-Agent-Run-Id`：本轮调用的执行批次号。
2. **后续轮次（沿用会话）**：
   - 客户端后续调用时，在请求体中携带 `session_id`；
   - 服务端自动装载该会话的历史对话上下文，客户端此时**无需且不允许再修改模板 variables**，保障会话业务上下文一致性。

## 版本锁定与平滑升级机制

- **旧会话锁定初次版本**：当一个多轮会话启动时，它将永久固定在当时生效的 Agent 版本上。即使管理员在后台发布了 Agent 的全新版本，正在进行中的老会话仍保持原有业务行为，不会受到突变影响。
- **新会话自动接入新版**：之后创建的所有新 Session，将自动采用最新发布的 Agent 资产版本。

## 数据面调用实战

### 1. 首轮调用示例（启动会话）

```bash
curl -i -sS http://127.0.0.1:8080/v1/agents/order-assistant/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "message": "你好，我想查询我的订单状态。",
    "variables": {
      "user_id": "U10086",
      "region": "华东"
    }
  }'
```

*注意响应头中返回的 Session ID*：
```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Agent-Session-Id: ses_01JXXXXXXXXXXXX
X-Agent-Run-Id: run_01JYYYYYYYYYYYY
```

### 2. 后续轮次调用示例

```bash
curl -sS http://127.0.0.1:8080/v1/agents/order-assistant/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "ses_01JXXXXXXXXXXXX",
    "message": "我的订单号是 20261126-001，请帮我看看发货了吗？"
  }'
```

### 3. 流式交互支持
只需在请求体中加入 `"stream": true`，接口即刻以标准 SSE（Server-Sent Events）格式实时吐出回复片段，数据流结束时返回 `data: [DONE]`。
