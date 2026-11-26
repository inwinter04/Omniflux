---
layout: home

hero:
  name: Omniflux
  text: All intelligence, one current.
  tagline: 企业 AI 服务控制平面。把分散在多个供应商和私有部署环境中的模型能力，纳入统一的治理、路由、计量、审计与可观测体系。
  image:
    src: /omniflux-hero.svg
    alt: Omniflux 海浪标志
  actions:
    - theme: brand
      text: 快速开始 (5分钟)
      link: /introduction/quick-start
    - theme: alt
      text: 了解架构设计
      link: /introduction/what-is-omniflux
    - theme: alt
      text: 查看 API 参考
      link: /reference/dataplane-api
    - theme: alt
      text: GitHub
      link: https://github.com/inwinter04/Omniflux

features:
  - title: 统一逻辑模型服务
    details: 彻底解耦业务调用与底层物理模型。对外暴露统一标准的 OpenAI 兼容协议，屏蔽上游供应商差异与变动。
    link: /concepts/model-services
  - title: 4D 治理化智能路由
    details: 基于硬约束过滤，融合成本、质量、延迟与可靠性四维动态打分，支持多级故障转移 (Fallback) 与决策留痕。
    link: /concepts/routing
  - title: 工业级多租户治理
    details: 组织 → 部门 → 项目 → 应用 → API Key 五层管理体系。租户间严格数据隔离，支持精细到单 Key 的频控与授权。
    link: /concepts/organization-model
  - title: 精细化计量与预算熔断
    details: 五维立体归因统计 Token 与多币种成本。支持月度预算超额后付熔断，结合 Redis 窗口实现高并发频控。
    link: /concepts/metering-budget
  - title: 企业级资产与 Agent 运行时
    details: 提示词模板不可变版本管理、严格变量验证与一键回滚；Agent 原生多轮会话保持与版本平滑演进。
    link: /guides/prompt-templates
  - title: 生产就绪与全链路可观测
    details: 单二进制内置 Vue 3 管理台，提供健康探针 (/livez, /readyz)、Prometheus 指标大盘与全量审计留痕。
    link: /deployment/requirements
---
