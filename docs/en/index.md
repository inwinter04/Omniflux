---
layout: home

hero:
  name: Omniflux
  text: All intelligence, one current.
  tagline: Enterprise AI service control plane. Bringing model capabilities from multiple providers and private deployments into a unified system for governance, routing, metering, auditing, and observability.
  image:
    src: /omniflux-hero.svg
    alt: Omniflux Wave Logo
  actions:
    - theme: brand
      text: Quick Start (5 mins)
      link: /en/introduction/quick-start
    - theme: alt
      text: Architecture Design
      link: /en/introduction/what-is-omniflux
    - theme: alt
      text: API Reference
      link: /en/reference/dataplane-api
    - theme: alt
      text: GitHub
      link: https://github.com/inwinter04/Omniflux

features:
  - title: Unified Logical Model Services
    details: Completely decouples business applications from underlying physical models. Exposes a standardized OpenAI-compatible interface, shielding upstream vendor differences and updates.
    link: /en/concepts/model-services
  - title: 4D Governed Dynamic Routing
    details: Based on hard constraint filtering and multi-dimensional dynamic scoring across cost, quality, latency, and reliability. Supports automated fallback chains and full decision tracking.
    link: /en/concepts/routing
  - title: Enterprise Multi-Tenancy
    details: 5-layer hierarchy across Organization → Department → Project → Application → API Key. Strict tenant isolation with per-key rate limiting and fine-grained authorization.
    link: /en/concepts/organization-model
  - title: Fine-Grained Metering & Budgets
    details: 5-dimensional token and multi-currency cost attribution. Supports post-settlement budget cutoffs and Redis-based sliding window rate limiting.
    link: /en/concepts/metering-budget
  - title: Enterprise Assets & Agent Runtime
    details: Immutable version management for PromptTemplates with strict variable validation and rollback; native stateful multi-turn session management for Agents.
    link: /en/guides/prompt-templates
  - title: Production-Ready & Full Observability
    details: Single self-contained binary embedding Vue 3 admin console. Provides liveness/readiness probes, Prometheus metrics dashboards, and immutable audit logs.
    link: /en/deployment/requirements
---
