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
| `GET /api/v1/auth/registration-options` | 查询公开注册是否开放 |
| `POST /api/v1/auth/register` | 按系统设置注册账号；需要邮箱验证时先发送验证邮件 |
| `POST /api/v1/auth/resend-verification` | 重发公开注册邮箱验证邮件；响应不暴露邮箱是否存在 |
| `POST /api/v1/auth/verify-email` | 消费一次性验证令牌，激活账号并建立会话 |
| `POST /api/v1/auth/forgot-password` | 申请密码重置邮件；响应不暴露邮箱是否存在 |
| `POST /api/v1/auth/reset-password` | 消费一次性重置令牌并设置新密码 |
| `POST /api/v1/auth/accept-invitation` | 消费一次性邀请令牌，设置密码、激活账号并建立会话 |
| `GET /api/v1/auth/me` | 获取当前用户信息（含 `role`、`roles`、`permissions`、`permissionGroupId`、`permissionGroupName`） |
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
| `PATCH /api/v1/projects/{id}/status` | 启用或停用项目;停用后名下 API Key 立即停止调用 |
| `DELETE /api/v1/projects/{id}` | 删除项目(存在应用/Key/用量时拒绝) |

### 应用

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/applications` | 应用列表 |
| `POST /api/v1/applications` | 创建应用 |
| `PATCH /api/v1/applications/{id}` | 更新应用 |
| `PATCH /api/v1/applications/{id}/status` | 启用或停用应用;停用后名下 API Key 立即停止调用 |
| `DELETE /api/v1/applications/{id}` | 删除应用(存在 Key/用量时拒绝) |

## 用户与权限治理

### 用户

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/users` | 用户列表（需 `user:read` 权限；平台管理员可按 `organization_id` 过滤） |
| `POST /api/v1/users` | 创建用户（需 `user:manage` 权限；支持指定 `role` 或 `permission_group_id`，平台管理员可指定归属组织） |
| `GET /api/v1/users/invite-options` | 查询邮件邀请是否可用（需 `user:manage` 权限） |
| `POST /api/v1/users/invite` | 创建待激活用户并发送一次性邮件邀请（支持 `role` 或 `permission_group_id`） |
| `POST /api/v1/users/{id}/invitation/resend` | 重发待激活用户邀请并作废旧链接 |
| `DELETE /api/v1/users/{id}/invitation` | 撤销待激活用户邀请 |
| `PATCH /api/v1/users/{id}` | 更新角色/状态/密码/归属组织 |

`POST /api/v1/users` 密码需满足 8–128 位且同时包含字母和数字（首尾不能有空格）；也可提交 `generate_password: true` 由服务端用 `crypto/rand` 生成 16 位可读初始密码（仅在响应 `generated_password` 返回一次，响应带 `Cache-Control: no-store`，明文不落库、不写审计）。管理台在密码框旁提供随机生成按钮，并在创建成功后统一弹出一次性凭据框（含登录地址、账号、初始密码与全量一键复制）。

### 权限目录、权限组与个人微调

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/permissions/catalog` | 查询全部 7 个模块 / 26 个权限节点目录及 5 个系统预设（`pending`/`viewer`/`developer`/`project_admin`/`org_admin`） |
| `GET /api/v1/permission-groups` | 当前组织自定义权限组列表（平台管理员可按 `organization_id` 过滤） |
| `POST /api/v1/permission-groups` | 创建组织级自定义权限组（需 `permission_group:manage`，且不可超出操作者自身有效权限） |
| `PATCH /api/v1/permission-groups/{id}` | 更新自定义权限组名称、说明与权限节点 |
| `DELETE /api/v1/permission-groups/{id}` | 删除未被成员使用的自定义权限组（被占用时返回 `409`） |
| `GET /api/v1/users/{id}/permissions` | 查看成员所属预设/权限组、个人 Allow/Deny 覆盖及最终生效权限 |
| `PUT /api/v1/users/{id}/permissions` | 调整成员身份或自定义权限组，并设置个人节点级 Allow/Deny 覆盖 |

最终生效权限按“基础权限（权限组优先，否则取角色预设） + 个人 `allow` − 个人 `deny`”实时解析，且 `Deny` 优先于 `Allow`；`org_owner` 与 `platform_admin` 始终保留全量管理权限，禁止修改本人或上级受保护账号权限，禁止越权授予自身不具备的权限节点。

## 系统设置

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/system/settings` | 查询公开注册、统一网关访问地址与客户端额度透出配置(仅平台管理员) |
| `PATCH /api/v1/system/settings` | 修改公开注册、归属组织、默认角色、邮箱验证、统一网关地址与上游额度透出配置(仅平台管理员) |
| `GET /api/v1/system/gateway-info` | 查询系统统一配置的网关访问地址（所有已登录用户可读，未配置时返回空值由客户端自动识别） |
| `GET /api/v1/system/email/settings` | 查询邮件服务配置；SMTP 密码只返回是否已保存(仅平台管理员) |
| `PATCH /api/v1/system/email/settings` | 修改 SMTP、发件人与公开访问地址配置(仅平台管理员) |
| `POST /api/v1/system/email/test` | 使用已保存配置发送测试邮件(仅平台管理员) |
| `GET /api/v1/system/fx-rates` | 查询汇率同步设置与实时换算汇率表 |
| `PATCH /api/v1/system/fx-rates/settings` | 更新汇率自动同步开关与同步周期(1-168 小时，仅平台管理员) |
| `PUT /api/v1/system/fx-rates/{currency}` | 设置非 USD 货币汇率及手工锁定标记(仅平台管理员) |
| `POST /api/v1/system/fx-rates/sync` | 立即从 Frankfurter (ECB) 与 Open ER-API 拉取权威汇率(仅平台管理员) |

`PATCH /api/v1/system/settings` 按完整配置提交 `public_registration_enabled`、`registration_organization_id`、`registration_role`、`email_verification_required`、可选 `gateway_base_url`（留空则由管理台自动识别当前访问地址）以及可选 `expose_upstream_balance_to_clients`（默认 `false`；开启后未命中网关预算的 API Key 调用 `GET /v1/quota` 时将展示带 60 秒缓存的上游余额/套餐余量）；仅在关闭公开注册时允许组织为空。公开注册默认关闭。开启前必须选择已存在组织和 `pending`（默认，零权限待分配）/ `viewer` / `developer` 默认角色;同一 IP 每分钟最多提交 5 次注册。关闭邮箱验证时注册成功后立即建立会话；开启邮箱验证时先创建待验证账号并发送邮件，验证后才建立会话。操作写入审计事件 `system.settings_updated`、`user.self_registered`、`email.verification_sent` 与 `auth.email_verified`。

邮件令牌由服务端 `crypto/rand` 生成，HMAC-SHA256 哈希后入库，只保存哈希与有效期。验证、邀请和密码重置分别使用一次性令牌；重发会作废旧令牌，撤销邀请会立即失效当前链接。

## 预算

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/budgets` | 预算窗口列表 |
| `POST /api/v1/budgets` | 创建预算窗口 |
| `PATCH /api/v1/budgets/{id}` | 调整预算限额 |
| `DELETE /api/v1/budgets/{id}` | 删除预算窗口 |

组织级预算的 `scope_id` 可省略或传 `organization`，服务端使用当前组织 ID。

## 模型接入

### 平台代理

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/proxies` | 平台代理节点列表、按厂商聚合的故障转移代理池、策略配置与可选厂商（仅平台管理员） |
| `POST /api/v1/proxies` | 创建平台代理，支持多厂商绑定与优先级，仅平台管理员可操作 |
| `PATCH /api/v1/proxies/{id}` | 更新平台代理及适用厂商绑定，仅平台管理员可操作 |
| `DELETE /api/v1/proxies/{id}` | 删除平台代理并自动同步受影响厂商，仅平台管理员可操作 |
| `POST /api/v1/proxies/{id}/test` | 立即探测代理节点连通性 |
| `PUT /api/v1/proxies/providers/{providerKey}/order` | 调整厂商代理池的主备优先级与绑定启停 |
| `POST /api/v1/proxies/providers/{providerKey}/switch` | 手动切换厂商当前生效的粘性代理 |
| `POST /api/v1/proxies/providers/{providerKey}/probe` | 立即探测厂商代理池并按防抖策略评估切换 |
| `GET /api/v1/proxies/failover-events` | 查询代理切换与故障转移历史事件 |
| `GET /api/v1/proxies/settings` | 查询代理故障转移防抖策略配置 |
| `PATCH /api/v1/proxies/settings` | 更新代理故障转移防抖策略（探测间隔、快速确认间隔、连续失败/恢复阈值、冷却期、粘性/稳定回切模式） |

平台代理是跨组织生效的平台级配置，仅平台管理员可查询和变更。同一厂商可绑定多个代理并设置 `priority` 优先级（数值越小越优先），同时通过**粘性主备切换**与**非对称滞回防抖**保证日常出口 IP 稳定：单次抖动只触发快速复测确认，连续失败达阈值且备选代理验活通过后才执行单厂商热切换；所有代理均异常时保持当前节点不动。代理类型支持 `http` 和 `socks5`，创建时必须填写代理地址；更新时保持原类型且地址留空表示沿用当前地址。

请求与响应字段：

| 字段 | 位置 | 说明 |
| --- | --- | --- |
| `provider_keys` / `bindings` | 请求/响应 | 适用厂商稳定标识或含 `provider_key` / `priority` / `enabled` 的绑定列表 |
| `name` | 请求/响应 | 代理名称；同一平台内唯一 |
| `proxy_type` | 请求/响应 | 代理类型，取值 `http` / `socks5` |
| `proxy_url` | 请求/响应 | 代理地址；响应只返回脱敏后的地址 |
| `enabled` | 请求/响应 | 代理节点全局启用开关 |
| `probe_url` | 请求/响应 | 可选自定义 HTTP/HTTPS 探测地址，留空按绑定厂商默认接口域名探测 |
| `pools` | `GET` 响应 | 按厂商聚合的主备代理池，含当前生效代理、24h 心跳状态条与故障计数 |
| `settings` | `GET` 响应 | 代理治理防抖与回切策略 |
| `provider_labels` | 响应 | 厂商展示名数组，由前端按当前语言渲染 |
| `updated_at` | 响应 | 最近更新时间 |
| `active_accounts` | 响应 | 该代理覆盖厂商当前启用的供应商账号数量 |
| `provider_options` | `GET` 响应 | 可选厂商列表，每项包含 `value` 与 `label` |

`proxy_url` 必须与 `proxy_type` 匹配：`http` 接受 `http://` / `https://`，`socks5` 接受 `socks5://` / `socks5h://`。地址必须包含主机和显式端口，不允许携带 path、query 或 fragment；可以包含用户名和密码。代理地址使用 AES-256-GCM 加密存储，原始地址不写入审计事件或日志。

创建、更新、删除或主备切换后只对该厂商执行热更新（不影响其他厂商及在途请求）。代理对适用厂商的真实调用、供应商账号连通性测试、模型发现和余额查询统一生效。审计事件为 `platform_proxy.created`、`platform_proxy.updated`、`platform_proxy.deleted`、`platform_proxy.switched`、`platform_proxy.order_updated` 与 `platform_proxy.settings_updated`；更新事件包含 `proxy_url_changed` 布尔值，但不会记录代理地址原文。

### 供应商账号

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/provider-catalog` | 厂商目录,按模型厂商/云平台（含官方 `openai`、`anthropic`、`gemini`）/第三方服务/自定义分类,同厂商提供套餐和地区版本及默认 `default_protocol` |
| `GET /api/v1/provider-accounts` | 供应商账号列表(含 `health_reason`、`avg_ttft_ms_24h`、主动探测状态、24h 心跳条与被动流量成功率;综合健康仅按成功率降级) |
| `GET /api/v1/provider-accounts/availability` | 按厂商聚合的整体健康、`health_reason`、24h 探测心跳、被动调用成功率与 `avg_ttft_ms_24h`、自动巡检配置 |
| `GET /api/v1/provider-accounts/usage-summaries` | 批量查询当前组织内支持自动查询的账号余额/套餐额度摘要 |
| `POST /api/v1/provider-accounts/test-all` | 一键批量检测当前组织下全部启用账号并记录结果 |
| `POST /api/v1/provider-accounts` | 创建供应商账号(凭据加密入库;支持指定 `api_protocol`:`openai_responses`/`openai_chat`/`anthropic_messages`/`gemini_native`/`azure_openai`/`bedrock`/`vertex`;OpenAI/Anthropic/Gemini/兼容账号按账号隔离,支持各自独立的 `base_url` 与协议) |
| `PATCH /api/v1/provider-accounts/{id}` | 更新供应商账号(支持调整 `api_protocol`、名称、端点与 API Key 轮换,API Key 留空保持不变) |
| `PATCH /api/v1/provider-accounts/{id}/status` | 启用或停用账号,并刷新上游引擎 |
| `GET /api/v1/provider-accounts/{id}/models` | 使用已保存凭据获取上游模型列表,并补全 models.dev 中的已知输入/输出价格;套餐类账号返回 0 价格与 `pricing_kind=plan` |
| `GET /api/v1/provider-accounts/{id}/balance` | 查询支持的账户余额与套餐额度,响应区分 `balances` 与 `plan_quotas`;不支持自动查询时返回 `quota_support=unsupported` |
| `POST /api/v1/provider-accounts/{id}/test` | 手动连通性与凭据检测(结果落库并刷新健康态) |
| `GET /api/v1/provider-accounts/{id}/health-history` | 查询单账号综合健康与 `health_reason`、主动探测状态、24h/7d 探测可用率与延迟、24h 被动调用量/成功率/TTFT/总耗时及近期检测明细 |
| `GET /api/v1/provider-health/settings` | 查询账号定时可用性巡检策略 |
| `PATCH /api/v1/provider-health/settings` | 更新账号定时可用性巡检策略（仅平台管理员） |
| `DELETE /api/v1/provider-accounts/{id}` | 删除供应商账号 |

供应商账号列表会返回当前厂商生效代理的名称、类型与代理池规模，但代理地址不在账号接口中回显，也不通过账号接口修改。代理配置统一在平台代理接口中治理。后台会按固定周期零 Token 费用探测启用账号并持久化最近 7 天结果；在数据面路由同优先级组中，主动探测为 `unhealthy` 的账号会被自动软降权到末尾（不改变账号启用状态）。

管理台支持勾选发现的模型并带入新建模型服务表单,保存前可调整价格、质量分、优先级和权重。质量分没有统一的上游自动来源,默认使用中性值 0.5。

### 路由策略

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/route-policies` | 路由策略列表(含绑定模型服务与 24h 请求量/转移率/首字延迟) |
| `POST /api/v1/route-policies` | 创建路由策略(支持 10 种调度模式，含 `auto_orchestrator` 智能编排、比价币种、四维权重、条件规则、错误码韧性、熔断与 `session_affinity` 粘性会话亲和) |
| `PATCH /api/v1/route-policies/{id}` | 更新策略参数(未传字段保留原值，自增 `version`) |
| `PATCH /api/v1/route-policies/{id}/status` | 启用或停用路由策略(被模型服务绑定时禁止停用，返回 409 `POLICY_IN_USE`) |
| `POST /api/v1/route-policies/{id}/clone` | 复制既有策略为新策略 |
| `POST /api/v1/route-policies/simulate` | 路由器零消耗 Dry-Run 仿真(支持未保存草稿参数、跨币种归一化、条件规则与故障转移演练) |
| `POST /api/v1/route-policies/{id}/playground` | 管理台路由在线体验试跑(记录 `source=route_playground` 用量与完整决策轨迹) |
| `GET /api/v1/route-policies/{id}/overview` | 策略总览(基线成本节省对比、模型命中占比与各模型绩效对比) |
| `GET /api/v1/route-policies/{id}/metrics` | 策略实时监控(TTFT P50/P95、OTPS、RPM/TPM、时序曲线与错误分布) |
| `GET /api/v1/route-policies/{id}/decisions` | 分页查询路由决策快照与尝试链 (`attempt_trail`) |
| `DELETE /api/v1/route-policies/{id}` | 删除路由策略 |

路由策略支持 `auto_orchestrator`、`balanced`、`cost_first`、`quality_first`、`latency_first`、`reliability_first`、`weighted`、`round_robin`、`strict_failover` 与 `custom` 十种调度策略，并通过 `session_affinity`（`enabled`、`ttl_seconds`、`key_strategy`: `auto`/`user_id`/`header_only`）与 `orchestrator_config`（`session_mode`: `adaptive`/`per_turn`/`sticky_upgrade`、`sticky_context_tokens`、`long_context_threshold`、`thresholds` 与可选四档 `tiers`）协同工作；`model_candidates` 同时支持 `context_window`、`capabilities`（`vision`/`tools`/`reasoning`/`json_mode`/`streaming`/`prompt_caching`/`responses_api`/`pdf_input`）、`tags` 以及可选分级缓存价格 `cache_read_cost_per_million` / `cache_write_cost_per_million`，并允许同一模型服务混用不同结算币种候选。

### 模型服务

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/model-services` | 模型服务列表(含 `health_reason`、24h 被动健康统计、`avg_ttft_ms_24h`、`expose_router`、`router_model_id` 与候选列表;被动健康仅按成功率降级) |
| `POST /api/v1/model-services` | 创建模型服务与候选(支持 `expose_router` 与 `router_model_id`) |
| `PATCH /api/v1/model-services/{id}` | 更新模型服务(候选按稳定键 upsert,移除候选保留历史) |
| `PATCH /api/v1/model-services/{id}/status` | 启用或停用模型服务 |
| `PATCH /api/v1/model-candidates/{id}/status` | 启用或停用单个候选模型 |
| `GET /api/v1/model-candidates/{id}` | 查询候选所属模型服务,用于审计资源跳转 |
| `GET /api/v1/model-services/{id}/grants` | 查询模型服务组织/部门/项目授权 |
| `POST /api/v1/model-services/{id}/grants` | 新增或更新授权,支持 `read` 与 `invoke` |
| `DELETE /api/v1/model-services/{id}/grants/{grantID}` | 撤销授权 |
| `DELETE /api/v1/model-services/{id}` | 删除模型服务 |

模型服务默认 `expose_router = false`，即仅作为管理分组，向客户端 `GET /v1/models` 与 `GET /v1beta/models` 暴露各可用候选的具体 `upstream_model`（绝不透出管理台中文服务名）；只有显式开启 `expose_router = true` 并设置合规 ASCII `router_model_id`（如 `omniflux/auto`）的服务，才会额外暴露一个 `owned_by: "omniflux-router"` 的智能路由入口模型。无效 ID 返回 400 `SERVICE_ROUTER_MODEL_ID_INVALID`，同组织重复返回 409 `SERVICE_ROUTER_MODEL_ID_EXISTS`。模型服务默认创建组织级 `read + invoke` 授权,撤销组织授权后可仅开放给指定部门或项目。`read` 控制 `GET /v1/models`,`invoke` 控制聊天、向量化和智能体执行,API Key 所属项目决定可见范围。账号与服务列表额外返回近 24 小时成功率、平均延迟、最近调用、可用候选数及健康状态;无流量标记为“未验证”,不等同于健康。

供应商账号声明结算币种,同一模型服务的候选必须使用相同币种;不同服务可使用不同币种。公开价格目录只提供 USD 参考价,其他币种不自动换算。成本按实际结算币种写入用量并分组汇总,预算只累计同币种成本。账户余额保留厂商原始币种与单位,不进行隐式汇率换算。余额接口通过 `balances` 与 `plan_quotas` 区分货币余额和套餐窗口;厂商目录返回 `pricing_kind`、`quota_kind` 与 `quota_support`。未接入自动查询的套餐仅返回不支持状态,不伪造额度。

用量汇总与明细同时覆盖业务 API 调用、提示词调试和智能体调试,并通过 `source` 区分 `api`、`prompt_debug`、`agent_debug`;多币种时 `cost_currency=MIXED`,各币种金额在 `costs` 中分别返回,不进行跨币种求和。

### API Key

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/api-keys` | API Key 列表,`expired` 表示启用中但已过有效期(数据面会拒绝这类 Key) |
| `POST /api/v1/api-keys` | 创建 API Key(明文仅返回一次,可设置 Key 级限流) |
| `PATCH /api/v1/api-keys/{id}` | 更新状态与 Key 级限流;未传的字段保持不变,`rate_limit_per_min` 传 `null` 表示清除限流 |
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
| `GET /api/v1/usage/summary` | 用量汇总,支持与明细相同的筛选条件(含 `protocol`),并返回 `total_cache_creation_tokens` 与 `by_protocol` 在内的多维汇总 |
| `GET /api/v1/usage/records` | 用量明细分页,支持项目/应用/Key/模型/状态/来源/接入协议(`protocol`)/时间筛选,含 `cache_creation_tokens` 与 `protocol` 字段 |
| `GET /api/v1/usage/trend` | 按天聚合趋势,支持总览/模型/模型服务/项目/项目+应用/项目+模型服务/项目+模型服务+应用/部门+项目/部门+项目+应用/部门+项目+模型服务/部门+项目+模型服务+应用/应用/API Key/供应商/部门/状态/来源/路由策略/错误类型维度 |
| `GET /api/v1/usage/records/export` | 按当前筛选条件导出用量明细 CSV（24 列，含 `protocol` 与 `cache_creation_tokens`） |
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
