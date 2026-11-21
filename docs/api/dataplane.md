# 数据面 API

面向业务系统的 OpenAI 兼容接口,使用 API Key 认证。

## 认证

```text
Authorization: Bearer <API Key>
```

API Key 在管理台"访问控制 → API Keys"中签发,明文仅创建时返回一次。

## 端点

| 接口 | 说明 |
| --- | --- |
| `GET /v1/models` | 可用模型服务列表 |
| `POST /v1/chat/completions` | OpenAI 兼容对话接口(支持流式) |
| `POST /v1/embeddings` | OpenAI 兼容向量化接口 |

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

## 路由行为

数据面按模型服务的候选配置路由:

1. 绑定路由策略时先应用硬约束(`constraints`:allowed/blocked providers、allowed/blocked upstream models、混合单价上限),被排除的候选不参与路由;
2. 剩余候选按 priority 升序分组,组内按 `weights`(cost/quality/latency/reliability)评分降序排列,评分并列时按 weight 加权随机;
3. 候选的 `quality_score`(0-1,默认 0.5)参与 quality 评分,latency 与 reliability 来自近 7 天用量统计;Provider 账号的健康可见性来自近 24 小时用量统计(路由已按成功率自动降权);
4. 主候选之后的候选作为 fallback 链交给上游网关处理,`fallback_limit` 与 `provider_timeout_ms` 生效;流式请求的 `first_token_timeout_ms` 在未收到首个 token 时中断上游并按失败定稿。

自动质量评估(EvaluationSet/EvaluationRun)将在后续版本实现。

## 计量与对账

每次调用都会写入 `usage_records` 与 `policy_decisions`;服务中断遗留的 pending 用量由后台维护任务按失败对账。

## 预算与限流

预算采用后付口径:请求前按已结算用量拦截,请求成功后累加实际成本,并发在途请求可能小幅越过限额(与主流开源网关一致)。

API Key 可配置 Key 级限流(`rate_limit_per_min`),未设置时沿用全局默认阈值(`OMNIFLUX_RATE_LIMIT_PER_MIN`,默认 60);限流基于 Redis 固定窗口实现。
