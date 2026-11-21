---
layout: home

hero:
  name: Omniflux
  text: All intelligence, one current.
  tagline: 企业 AI 服务控制平面,把分散在多个供应商和私有部署环境中的模型能力,纳入统一的治理、路由、计量、审计与可观测体系。
  actions:
    - theme: brand
      text: 快速开始
      link: /guide/quick-start
    - theme: alt
      text: 数据面 API
      link: /api/dataplane
    - theme: alt
      text: 管理面 API
      link: /api/admin
    - theme: alt
      text: GitHub
      link: https://github.com/inwinter04/Omniflux

features:
  - title: 统一模型服务
    details: 通过逻辑模型服务隔离业务调用与真实 Provider 模型,业务侧始终使用同一套 OpenAI 兼容接口。
  - title: 治理化路由
    details: 基于硬约束、成本、质量、延迟与可靠性评分选择候选模型,决策过程完整留痕。
  - title: 多租户治理
    details: 组织、部门、项目、应用、用户、角色与 API Key 分层管理,组织间数据隔离。
  - title: 计量与归因
    details: 按项目、应用、API Key、Provider 和模型归因请求、Token 与成本。
  - title: 审计与决策留痕
    details: 管理操作、登录事件与 PolicyDecision 全部可追溯。
  - title: 可观测性
    details: 内置 liveness/readiness 探针、结构化日志,以及覆盖请求量、延迟、Token 与成本的数据面 Prometheus 业务指标。
  - title: 现代管理台
    details: 基于 Vue 3、TypeScript、Naive UI 和 SoybeanAdmin 的服务端会话管理后台。
---
