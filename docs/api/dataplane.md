# 数据面 API

面向业务系统的 OpenAI 兼容接口,使用 API Key 认证。

## 认证

```text
Authorization: Bearer <API Key>
```

API Key 在管理台"访问控制 → API Keys"中签发,明文仅创建时返回一次。模型服务通过组织、部门或项目授权控制可见与可调用范围:`read` 决定 `GET /v1/models`,`invoke` 决定聊天、向量化和智能体执行。未授权调用返回 `model_access_denied`,不会进入上游模型。

## 端点

| 接口 | 说明 |
| --- | --- |
| `GET /v1/models` | 可用模型服务列表 |
| `POST /v1/chat/completions` | OpenAI 兼容对话接口(支持流式) |
| `POST /v1/embeddings` | OpenAI 兼容向量化接口 |
| `GET /v1/prompt-templates` | 当前项目可读取或调用的已发布模板 |
| `GET /v1/prompt-templates/{name}` | 读取已发布模板配置(需 `read` 授权) |
| `POST /v1/prompt-templates/{name}/render` | 渲染当前发布版本(需 `invoke` 授权) |
| `GET /v1/agents` | 当前项目可读取或调用的已发布 Agent |
| `GET /v1/agents/{name}` | 读取已发布 Agent 配置(需 `read` 授权) |
| `POST /v1/agents/{name}/chat/completions` | 调用 Agent 多轮对话(需 `invoke` 授权,支持流式) |

## 调用示例

```bash
# 非流式
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"e2e-chat","messages":[{"role":"user","content":"你好"}]}'

# 流式(stream=true 时返回 SSE,data: [DONE] 结束)
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"e2e-chat","stream":true,"messages":[{"role":"user","content":"你好"}]}'
```

## PromptTemplate 渲染

模板使用 Go Template 安全子集,通过 <code v-text="'{{ .variable }}'"></code> 引用已声明变量。变量支持 `string`、`number`、`integer`、`boolean`,可声明必填、默认值与描述;输入按 JSON Schema 严格校验,不允许未声明变量。模板可绑定一个模型服务作为默认执行目标,但渲染端点只返回文本,不会自动发起模型推理。

```bash
curl -sS http://127.0.0.1:8080/v1/prompt-templates/customer-service/render \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"variables":{"question":"如何申请退款？"}}'
```

渲染始终固定到当前发布版本。授权可通过组织、项目所属部门或项目本身授予;未发布、停用、归档或越权资产不可调用。成功调用和拒绝事件写入审计,变量值与渲染内容不会进入审计记录。

## Agent 多轮对话

Agent 将固定提示词版本、模型服务和生成参数组合为可发布资产。首次调用只传 `message` 与模板要求的 `variables`;响应头 `X-Agent-Session-Id` 返回新会话 ID。后续调用携带该 ID,不需要也不能修改变量。会话固定首次使用的 Agent 版本,即使之后发布新版本,旧对话仍保持原有上下文语义;新会话使用当前发布版本。

```bash
# 首轮:-i 用于查看 X-Agent-Session-Id、X-Agent-Run-Id 和版本响应头
curl -i -sS http://127.0.0.1:8080/v1/agents/customer-assistant/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"message":"你好","variables":{"customer":"Ada"}}'

# 后续轮次
curl -sS http://127.0.0.1:8080/v1/agents/customer-assistant/chat/completions \
  -H "Authorization: Bearer $OMNIFLUX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"<上一步返回的会话 ID>","message":"继续介绍"}'
```

非流式与流式响应体保持 OpenAI Chat Completions 格式。请求体使用 `message`、`session_id`、`variables`、`stream`,不接受任意上游参数;生成参数由已发布 Agent 版本控制。会话只允许同一组织、项目和 Agent 访问。每次调用复用模型路由、预算、fallback 和限流,写入 Agent 消息/运行记录、`usage_records` 与审计事件;审计不保存消息正文和变量值。

## 路由行为

数据面按模型服务的候选配置路由:

1. 绑定路由策略时先应用硬约束(`constraints`:allowed/blocked providers、allowed/blocked upstream models、混合单价上限),被排除的候选不参与路由;
2. 剩余候选按 priority 升序分组,组内按 `weights`(cost/quality/latency/reliability)评分降序排列,评分并列时按 weight 加权随机;
3. 候选的 `quality_score`(0-1,默认 0.5)参与 quality 评分,latency 与 reliability 来自近 7 天用量统计;Provider 账号的健康可见性来自近 24 小时用量统计(路由已按成功率自动降权);
4. 主候选之后的候选作为 fallback 链交给上游网关处理,`fallback_limit` 与 `provider_timeout_ms` 生效;流式请求的 `first_token_timeout_ms` 在未收到首个 token 时中断上游并按失败定稿。

测试集评估(EvaluationSet/EvaluationRun)已支持确定性规则匹配、成本与延迟汇总。

## 计量与对账

每次调用都会写入 `usage_records` 与 `policy_decisions`;服务中断遗留的 pending 用量由后台维护任务按失败对账。

## 预算与限流

预算采用后付口径:请求前按已结算用量拦截,请求成功后累加实际成本,并发在途请求可能小幅越过限额(与主流开源网关一致)。

API Key 可配置 Key 级限流(`rate_limit_per_min`),未设置时沿用全局默认阈值(`OMNIFLUX_RATE_LIMIT_PER_MIN`,默认 60);限流基于 Redis 固定窗口实现。

## 错误格式

所有数据面错误使用 OpenAI 兼容的错误包，并附带稳定的机器字段：

```json
{
  "error": {
    "type": "invalid_request_error",
    "code": "invalid_request",
    "reason": "missing_model",
    "message": "The model parameter is required.",
    "param": "model",
    "params": {},
    "request_id": "req_01J..."
  }
}
```

| 字段 | 说明 |
| --- | --- |
| `type` | OpenAI 风格错误类型，例如 `invalid_request_error`、`authentication_error`、`rate_limit_error`、`upstream_error` |
| `code` | 稳定的粗粒度错误码 |
| `reason` | 稳定的细粒度原因，客户端应优先根据它做分支 |
| `message` | 面向开发者的英文说明，不作为本地化文案使用 |
| `param` | 相关请求字段，可选 |
| `params` | 结构化错误参数，例如 `model`、`limit`、`currency` |
| `detail` | 可选的上游原始错误，已尽量限制在排障所需范围 |
| `request_id` | 请求追踪 ID，同时通过 `X-Request-ID` 响应头返回 |

客户端应使用 `code` / `reason` / `params` 做判断，不要解析 `message` 文本。流式请求开始后如果失败，会在 SSE `event: error` 中返回同样的 `error` 对象，然后发送 `data: [DONE]`。
