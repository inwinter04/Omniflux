# 管理面 API

管理面使用服务端会话认证(登录后 Cookie),数据按组织隔离。接口前缀 `/api/v1`。

## 运维

| 接口 | 说明 |
| --- | --- |
| `GET /livez` | 进程存活探针 |
| `GET /readyz` | PostgreSQL、Redis 与关键依赖就绪检查 |
| `GET /metrics` | Prometheus 指标(进程指标 + 数据面业务指标) |
| `GET /admin/` | 管理控制台 |

## 认证

| 接口 | 说明 |
| --- | --- |
| `POST /api/v1/auth/login` | 管理员登录 |
| `GET /api/v1/auth/me` | 获取当前管理员信息 |
| `POST /api/v1/auth/logout` | 退出登录 |
| `GET /api/v1/dashboard` | 首页统计概览 |

## 组织治理

### 组织

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/organizations` | 组织列表(平台管理员可见全部,其余仅当前组织) |
| `POST /api/v1/organizations` | 创建组织(仅平台管理员) |
| `PATCH /api/v1/organizations/{id}` | 修改组织名称(slug 创建后不可变更) |

### 部门

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/departments` | 部门列表 |
| `POST /api/v1/departments` | 创建部门 |
| `PATCH /api/v1/departments/{id}` | 更新部门 |
| `DELETE /api/v1/departments/{id}` | 删除部门(被项目引用时拒绝) |

### 项目

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/projects` | 项目列表 |
| `POST /api/v1/projects` | 创建项目 |
| `PATCH /api/v1/projects/{id}` | 更新项目 |
| `PATCH /api/v1/projects/{id}/status` | 启用或停用项目 |
| `DELETE /api/v1/projects/{id}` | 删除项目(存在应用/Key/用量时拒绝) |

### 应用

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/applications` | 应用列表 |
| `POST /api/v1/applications` | 创建应用 |
| `PATCH /api/v1/applications/{id}` | 更新应用 |
| `PATCH /api/v1/applications/{id}/status` | 启用或停用应用 |
| `DELETE /api/v1/applications/{id}` | 删除应用(存在 Key/用量时拒绝) |

## 用户

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/users` | 用户列表(需组织所有者;平台管理员可按 `organization_id` 过滤) |
| `POST /api/v1/users` | 创建用户(平台管理员可指定归属组织) |
| `PATCH /api/v1/users/{id}` | 更新角色/状态/密码/归属组织 |

## 预算

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/budgets` | 预算窗口列表 |
| `POST /api/v1/budgets` | 创建预算窗口 |
| `PATCH /api/v1/budgets/{id}` | 调整预算限额 |
| `DELETE /api/v1/budgets/{id}` | 删除预算窗口 |

组织级预算的 `scope_id` 可省略或传 `organization`，服务端使用当前组织 ID。

## 模型接入

### 供应商账号

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/provider-catalog` | 厂商目录,按模型厂商/云平台/第三方服务分类,同厂商提供套餐和地区版本 |
| `GET /api/v1/provider-accounts` | 供应商账号列表(含近 24h 被动健康统计) |
| `POST /api/v1/provider-accounts` | 创建供应商账号(凭据加密入库;openai/anthropic 类 base_url 不含 `/v1`,网关会自动拼接 `/v1/chat/completions` 等路径) |
| `PATCH /api/v1/provider-accounts/{id}` | 更新供应商账号(可轮换 API Key,留空保持不变) |
| `PATCH /api/v1/provider-accounts/{id}/status` | 启用或停用账号,并刷新上游引擎 |
| `GET /api/v1/provider-accounts/{id}/models` | 使用已保存凭据获取上游模型列表,并补全 models.dev 中的已知输入/输出价格 |
| `GET /api/v1/provider-accounts/{id}/balance` | 查询支持的账户余额与套餐限额,按原币种返回;限额含已用/剩余百分比及重置时间 |
| `POST /api/v1/provider-accounts/{id}/test` | 手动连通性测试 |
| `DELETE /api/v1/provider-accounts/{id}` | 删除供应商账号 |

管理台支持勾选发现的模型并带入新建模型服务表单,保存前可调整价格、质量分、优先级和权重。质量分没有统一的上游自动来源,默认使用中性值 0.5。

### 路由策略

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/route-policies` | 路由策略列表 |
| `POST /api/v1/route-policies` | 创建路由策略(含硬约束) |
| `PATCH /api/v1/route-policies/{id}` | 更新策略参数与硬约束 |
| `PATCH /api/v1/route-policies/{id}/status` | 启用或停用路由策略 |
| `DELETE /api/v1/route-policies/{id}` | 删除路由策略 |

### 模型服务

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/model-services` | 模型服务列表(含候选) |
| `POST /api/v1/model-services` | 创建模型服务与候选 |
| `PATCH /api/v1/model-services/{id}` | 更新模型服务(候选按稳定键 upsert,移除候选保留历史) |
| `PATCH /api/v1/model-services/{id}/status` | 启用或停用模型服务 |
| `PATCH /api/v1/model-candidates/{id}/status` | 启用或停用单个候选模型 |
| `GET /api/v1/model-candidates/{id}` | 查询候选所属模型服务,用于审计资源跳转 |
| `GET /api/v1/model-services/{id}/grants` | 查询模型服务组织/部门/项目授权 |
| `POST /api/v1/model-services/{id}/grants` | 新增或更新授权,支持 `read` 与 `invoke` |
| `DELETE /api/v1/model-services/{id}/grants/{grantID}` | 撤销授权 |
| `DELETE /api/v1/model-services/{id}` | 删除模型服务 |

模型服务默认创建组织级 `read + invoke` 授权,撤销组织授权后可仅开放给指定部门或项目。`read` 控制 `GET /v1/models`,`invoke` 控制聊天、向量化和智能体执行,API Key 所属项目决定可见范围。账号与服务列表额外返回近 24 小时成功率、平均延迟、最近调用、可用候选数及健康状态;无流量标记为“未验证”,不等同于健康。

供应商账号声明结算币种,同一模型服务的候选必须使用相同币种;不同服务可使用不同币种。公开价格目录只提供 USD 参考价,其他币种不自动换算。成本按实际结算币种写入用量并分组汇总,预算只累计同币种成本。账户余额保留厂商原始币种与单位,不进行隐式汇率换算;Coding Plan 等套餐按 `kind=quota` 返回独立限额窗口,与货币余额区分。

用量汇总与明细同时覆盖业务 API 调用、提示词调试和智能体调试,并通过 `source` 区分 `api`、`prompt_debug`、`agent_debug`;多币种时 `cost_currency=MIXED`,各币种金额在 `costs` 中分别返回,不进行跨币种求和。

### API Key

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/api-keys` | API Key 列表 |
| `POST /api/v1/api-keys` | 创建 API Key(明文仅返回一次,可设置 Key 级限流) |
| `PATCH /api/v1/api-keys/{id}` | 更新状态与 Key 级限流 |
| `DELETE /api/v1/api-keys/{id}` | 软删除 API Key |

## 企业资产

资产类型支持 `prompt_template` 与 `agent`。资产始终按组织隔离;创建和更新生成不可变草稿版本,发布后才对数据面可见。更新与回滚需携带 `expected_version` 防止并发覆盖,回滚会把历史已发布版本复制为新版本,不会修改历史。

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/assets?type={assetType}` | 资产列表,可按类型筛选 |
| `POST /api/v1/assets` | 创建 PromptTemplate 或 Agent 与草稿 v1 |
| `GET /api/v1/assets/{id}` | 资产详情、最新配置与版本链 |
| `PATCH /api/v1/assets/{id}` | 保存新草稿版本 |
| `POST /api/v1/assets/{id}/publish` | 发布最新草稿 |
| `POST /api/v1/assets/{id}/rollback` | 回滚并生成新的已发布版本 |
| `PATCH /api/v1/assets/{id}/status` | 停用、重新启用或归档资产 |
| `GET /api/v1/assets/{id}/versions/{versionID}` | 查询版本配置快照 |
| `GET /api/v1/assets/{id}/versions/diff?from=...&to=...` | 获取两个版本的 unified diff |
| `POST /api/v1/assets/{id}/render` | 使用指定版本渲染管理端预览 |
| `POST /api/v1/assets/{id}/run` | 运行真实模型;单模型支持 SSE 流式,多模型执行对比 |
| `GET /api/v1/assets/{id}/runs` | 查询最近 100 条提示词模型运行记录 |
| `GET /api/v1/assets/{id}/agent/sessions` | 查询当前管理员的 Agent 调试会话 |
| `POST /api/v1/assets/{id}/agent/chat` | 发送 Agent 消息,支持非流式与 `started/delta/result/error` SSE 事件;入参不合法返回 400(会话或版本不存在返回 404),`code` 为大写错误码,`reason` 为 kebab-case 标识,英文说明放在 `detail` |
| `GET /api/v1/assets/{id}/agent/runs` | 查询最近 100 条 Agent 运行记录 |
| `GET /api/v1/assets/{id}/agent/sessions/{sessionID}` | 查询会话配置与完整消息历史 |
| `DELETE /api/v1/assets/{id}/agent/sessions/{sessionID}` | 删除当前管理员的调试会话及消息 |
| `GET /api/v1/assets/{id}/evaluation-sets` | 查询测试集与测试用例 |
| `POST /api/v1/assets/{id}/evaluation-sets` | 创建测试集(1-50 个用例) |
| `DELETE /api/v1/assets/{id}/evaluation-sets/{setID}` | 删除测试集,保留历史运行 |
| `PATCH /api/v1/assets/{id}/evaluation-sets/{setID}` | 编辑测试集名称、说明和完整用例集 |
| `GET /api/v1/evaluation-sets` | 组织级测试集治理列表,含归属模板、运行数、匹配率和最近运行 |
| `POST /api/v1/assets/{id}/evaluate` | 批量运行测试集并输出匹配率、成本和延迟汇总 |
| `GET /api/v1/assets/{id}/grants` | 查询有效授权 |
| `POST /api/v1/assets/{id}/grants` | 新增或更新组织/部门/项目授权 |
| `DELETE /api/v1/assets/{id}/grants/{grantID}` | 撤销授权 |

`asset_grants.actions` 支持 `read` 与 `invoke`。数据面按 API Key 所属项目解析授权,`read` 控制配置读取,`invoke` 控制渲染或 Agent 调用;管理端写操作由 RBAC 控制。归档不可恢复,所有生命周期和授权变更都会写入审计。

Agent 配置包含 `prompt_template_id`、`prompt_version_id`、`model_service_id`、可选生成参数与 `max_history_messages`。每个会话固定创建时的 Agent 版本和初始变量,后续消息复用这些设置;管理端会话按创建用户隔离。运行记录保存请求/响应、Token、成本、延迟和错误,审计元数据不保存消息或变量内容。

模型调试使用现有模型服务路由、fallback、超时和价格配置。单次流式运行返回 `started/delta/result/error` SSE 事件;对比一次最多 4 个模型。运行记录保存版本、Token、成本、延迟、Provider、结构校验和错误。`output_schema` 为标准 JSON Schema,禁止外部引用;模型输出会再次在服务端校验。

批量评估支持 `exact`、`contains`、`regex` 三种确定性规则,一次最多 50 个用例、4 个模型,执行并发受限。测试结果可用于比较匹配率、成本和延迟,不代表开放语义下的人工或模型主观评分。

## 用量与审计

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/usage/summary` | 用量汇总,支持与明细相同的筛选条件,并按模型/模型服务/供应商/路由策略/错误类型/项目/应用/API Key/部门/状态/来源汇总 |
| `GET /api/v1/usage/records` | 用量明细分页,支持项目/应用/Key/模型/状态/来源/时间筛选 |
| `GET /api/v1/usage/trend` | 按天聚合趋势,支持总览/模型/模型服务/项目/项目+应用/项目+模型服务/项目+模型服务+应用/部门+项目/部门+项目+应用/部门+项目+模型服务/部门+项目+模型服务+应用/应用/API Key/供应商/部门/状态/来源/路由策略/错误类型维度 |
| `GET /api/v1/usage/records/export` | 按当前筛选条件导出用量明细 CSV |
| `GET /api/v1/audit-events` | 审计事件分页,支持身份/动作/资源类型(精确匹配)/结果/时间筛选,附带受影响资源的名称与是否仍然存在(`resource_exists`);`action` 为包含匹配 |
| `GET /api/v1/audit-events/{id}` | 审计事件详情 |

审计列表为每条事件返回 `resource_name`(受影响资源的名称)与 `resource_exists`(资源是否仍然存在,软删除的 API Key 视为不存在):资源仍存在时取当前名称,已删除时取审计快照里记录的名称;授权类事件以所属模型服务 / 资产是否仍在为准。删除类操作会把被删资源的名称写入变更前快照(预算记录所属范围与额度),授权类变更记录所属模型服务或资产。

## 错误格式

管理面错误只返回机器字段，不返回本地化文案：

```json
{
  "code": "ORG_NOT_FOUND",
  "reason": "organization-does-not-exist",
  "params": {},
  "detail": ""
}
```

| 字段 | 说明 |
| --- | --- |
| `code` | 稳定的粗粒度错误码 |
| `reason` | 稳定的细粒度原因，管理台前端据此渲染当前语言 |
| `params` | 结构化错误参数，例如重试秒数、字段名、版本号 |
| `detail` | 可选的技术细节或上游原始错误，不作为 UI 文案使用 |

管理台前端只根据 `reason` 和 `params` 渲染中英文文案；不要依赖后端返回自然语言。
