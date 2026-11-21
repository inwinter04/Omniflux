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
| `DELETE /api/v1/projects/{id}` | 删除项目(存在应用/Key/用量时拒绝) |

### 应用

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/applications` | 应用列表 |
| `POST /api/v1/applications` | 创建应用 |
| `PATCH /api/v1/applications/{id}` | 更新应用 |
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

## 模型接入

### Provider 账号

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/provider-accounts` | Provider 账号列表(含近 24h 被动健康统计) |
| `POST /api/v1/provider-accounts` | 创建 Provider 账号(凭据加密入库;openai/anthropic 类 base_url 不含 `/v1`,网关会自动拼接 `/v1/chat/completions` 等路径) |
| `PATCH /api/v1/provider-accounts/{id}` | 更新 Provider 账号 |
| `POST /api/v1/provider-accounts/{id}/test` | 手动连通性测试 |
| `DELETE /api/v1/provider-accounts/{id}` | 删除 Provider 账号 |

### 路由策略

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/route-policies` | 路由策略列表 |
| `POST /api/v1/route-policies` | 创建路由策略(含硬约束) |
| `PATCH /api/v1/route-policies/{id}` | 更新策略参数(constraints 创建后不可改) |
| `DELETE /api/v1/route-policies/{id}` | 删除路由策略 |

### 模型服务

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/model-services` | 模型服务列表(含候选) |
| `POST /api/v1/model-services` | 创建模型服务与候选 |
| `PATCH /api/v1/model-services/{id}` | 更新模型服务(候选全量替换) |
| `DELETE /api/v1/model-services/{id}` | 删除模型服务 |

### API Key

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/api-keys` | API Key 列表 |
| `POST /api/v1/api-keys` | 创建 API Key(明文仅返回一次,可设置 Key 级限流) |
| `PATCH /api/v1/api-keys/{id}` | 更新状态与 Key 级限流 |
| `DELETE /api/v1/api-keys/{id}` | 软删除 API Key |

## 用量与审计

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/usage/summary` | 用量汇总 |
| `GET /api/v1/usage/records` | 用量明细分页 |
| `GET /api/v1/audit-events` | 审计事件分页 |
| `GET /api/v1/audit-events/{id}` | 审计事件详情 |
