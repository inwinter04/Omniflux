# What is Omniflux

Omniflux is an enterprise **AI Service Control Plane**.

As enterprises adopt generative AI, engineering teams inevitably integrate or deploy various Large Language Models (LLMs)—including DeepSeek, OpenAI, Zhipu GLM, Alibaba Qwen, Kimi, and internal self-hosted engines like vLLM and Ollama. However, fragmented integrations quickly lead to substantial governance challenges:

- **Tight Code Coupling**: Business applications hardcode specific vendor model identifiers and API keys, requiring code modifications and redeployments whenever swapping providers;
- **Black-box Costs and Usage**: Independent teams manage their own keys, resulting in zero unified visibility into token attribution, cross-team budget controls, and cost reconciliation;
- **Lack of High Availability and Failover**: Single-provider downtime, rate limits (429), or network blips directly break business services without automated, transparent failover;
- **Security and Access Control Deficits**: Inability to enforce multi-tenant isolation, granular access control, or audit trails explaining who called which model, when, and at what cost;
- **Scattered Prompt & Agent Assets**: Prompts and conversational agents are fragmented across scripts and microservices without immutable versioning, review processes, or centralized governance.

Omniflux adheres to the principle of **"All intelligence, one current"**, channeling dispersed model capabilities into a single governed control plane that provides enterprise-grade reliability, governance, and multi-tenancy.

## High-Level System Architecture

Omniflux adopts a decoupled dual-plane architecture:

![Omniflux Architecture Diagram](/assets/architecture.en-US.png)

### 1. Data Plane
- **High-Performance Standard Gateway**: Exposes OpenAI-compatible HTTP endpoints (`/v1/chat/completions`, `/v1/embeddings`, `/v1/models`, etc.). Client applications simply update their `base_url` and `api_key` for instant, zero-code migration.
- **Governed Execution Pipeline**: Requests follow a deterministic pipeline: **Authentication → Authorization Check → Budget & Rate Limit Inspection → Dynamic Routing → Upstream Forwarding & Fallback Retries → Metering Storage & Audit Logging**.
- **High-Throughput Streaming**: Native Server-Sent Events (SSE) streaming support with First-Token latency timeout protection (`first_token_timeout_ms`).

### 2. Control Plane
- **Modern Management Console**: Built with Vue 3, TypeScript, and Naive UI. Offers a visual dashboard for multi-tenancy, model service orchestration, routing policies, usage analytics, audit querying, and asset governance.
- **Standardized Admin APIs**: Comprehensive RESTful management endpoints (`/api/v1/*`) for CI/CD automation and enterprise workflow integration.

### 3. Infrastructure & Storage
- **PostgreSQL 17**: Persistent storage for multi-tenant metadata, encrypted credentials (AES-256-GCM), model service configurations, immutable assets, metering records, and audit events.
- **Redis 8**: High-performance distributed rate limiting and sliding window counters.

## Core Feature Matrix

| Domain | Key Capabilities | Business Value |
| :--- | :--- | :--- |
| **Model Abstraction** | Logical Model Services | Call logical names; map to multiple real models dynamically; shield vendor details. |
| **Intelligent Routing**| Hard constraints + 4D dynamic scoring (Cost/Quality/Latency/Reliability) | Automatically select the most cost-effective and healthiest provider; priority grouping & weighted balancing. |
| **Failover & HA** | Automated Fallback Chains + Real-time Health Profiles | Transparently retry subsequent candidates upon upstream failure; automatic penalty and circuit breaking. |
| **Multi-Tenancy** | Organization → Department → Project → Application → API Key | Strict tenant boundaries with fine-grained per-key rate limits and role-based access. |
| **Cost & Budget** | 5D metering attribution + Multi-currency + Budget cutoff | Attribute token consumption and expenditure accurately; enforce project-level budget ceilings. |
| **Asset Governance** | PromptTemplates + Stateful Agent Runtime | Immutable prompt versions, variable validation, instant rollback, and session-aware conversational agents. |
| **Observability** | PolicyDecision tracking + Prometheus metrics + Audit logs | Full transparency into why every routing choice was made, enabling optimization and compliance. |

## Next Steps

- Experience an end-to-end tutorial in 5 minutes: [Quick Start](/en/introduction/quick-start).
- Understand multi-tenant isolation and roles: [Multi-Tenancy & Org Model](/en/concepts/organization-model).
- Discover how logical model services operate: [Model Services & Providers](/en/concepts/model-services).
- Review production infrastructure recommendations: [Requirements & Architecture](/en/deployment/requirements).
