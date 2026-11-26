# Model Services & Providers

Traditional integration couples application code directly to provider model identifiers (e.g. `model="deepseek-chat"`). When a provider experiences an outage, changes pricing, or deprecates a model version, extensive code changes are required.

Omniflux introduces **Logical Model Services** to decouple business logic from physical model backends.

## Core Architecture

![Omniflux Logical Model Service & Intelligent Routing](/assets/model-services.en-US.png)

### 1. Provider Accounts
- Credentials for external LLM platforms or self-hosted clusters.
- **Strong Encryption**: API keys and base URLs are encrypted in PostgreSQL using AES-256-GCM with the master encryption key.
- **Multi-Account Balancing**: Create multiple accounts for the same provider to automatically balance loads and expand concurrency limits.

### 2. Logical Model Services
- Virtual model identifiers exposed to clients (e.g. `general-pro-chat`, `code-copilot`, `fast-embedding`).
- Clients specify this logical name in the `model` parameter of `/v1/chat/completions`.
- Supports independent state management (enabled/disabled), declared settlement currency (USD, CNY), and project-level authorization.

### 3. Candidate Models
- A logical service binds one or more candidate instances.
- Each candidate defines:
  - **Provider Account**: The credential and protocol to use.
  - **Upstream Model Identifier**: The real vendor model name (e.g. `deepseek-chat`, `gpt-4o-2024-08-06`).
  - **Priority**: Integer (lower values have higher priority).
  - **Weight**: Relative ratio for weighted distribution within the same priority level.
  - **Quality Score**: Baseline score between 0.0 and 1.0 used by the 4D routing algorithm.

## Base URL Configuration Rules

Omniflux comes with a preconfigured provider catalog:

::: warning Base URL Guidelines
- **Built-in Providers**: Official cloud vendor endpoints are managed automatically. **Leave Base URL empty**.
- **Custom OpenAI Compatible & Self-Hosted Engines** (Ollama, vLLM, LM Studio, LocalAI):
  - **Do NOT append `/v1` to the Base URL!**
  - For example, if Ollama listens on `http://localhost:11434`, configure `http://127.0.0.1:11434`.
  - The gateway appends `/v1/chat/completions` automatically. Appending `/v1` results in invalid `/v1/v1/chat/completions` paths and 404 errors.
:::

## Candidate Lifecycle & Health Profiles

Candidates are continuously tracked:
- **7-day Rolling Profile**: Historical latency and availability statistics feeding the routing algorithm.
- **24-hour Health Profile**: Real-time sliding window error rates. Candidates experiencing elevated errors are penalized automatically; failing candidates are temporarily isolated to prevent cascading retries.
