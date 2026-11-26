# Multi-Tenancy & Org Model

In enterprise environments, secure data isolation, hierarchical authorization, and clear cost attribution are paramount. Omniflux implements a structured multi-tenant model.

## Hierarchy Design

Omniflux organizes entities across **Organization → Department → Project → Application → API Key**, alongside Role-Based Access Control (RBAC):

![Omniflux Multi-Tenancy Organization Model](/assets/org-model.en-US.png)

### Entity Roles & Boundaries

| Level | Governance Role | Boundaries & Characteristics |
| :--- | :--- | :--- |
| **Organization** | Top-level tenant boundary | **Absolute data isolation**. Users, API keys, model services, billing, and logs never cross organization boundaries. |
| **Department** | Administrative/business grouping | Organizational cost aggregation unit; used for broad asset permission delegation. |
| **Project** | Core business & control unit | **Primary unit for budgets and asset authorization**. Model services and prompt templates are granted to projects. Projects carry monthly/quarterly financial budgets. |
| **Application** | Client/service entity | Specific caller service inside a project (e.g. "Customer Service Bot", "Dev Copilot") for granular attribution. |
| **API Key** | Data plane credential | Plaintext shown once upon creation; stored as a salted hash with secret pepper. Supports independent per-minute rate limits. |
| **Users & Roles** | Console access management | Administrators, developers, and viewers managing configuration and viewing analytics. |

## Role-Based Access Control (RBAC)

Omniflux defines four built-in console roles:
1. **Platform Admin**: Superuser across the platform; manages global system settings (SMTP, open registration, organizations).
2. **Organization Owner**: Full administration over the organization; manages departments, projects, members, and organization-wide provider credentials.
3. **Developer**: Manages model services, tests PromptTemplates, deploys Agents, issues API keys, and reviews usage within authorized projects.
4. **Viewer**: Read-only access to metrics, call logs, and audit records in authorized projects.

## Asset Authorization & Scope

Model Services, PromptTemplates, and Agents are governed assets. Permissions are granted to projects with two distinct scopes:
- `read`: Allows inspecting metadata and listings (e.g., `GET /v1/models` only shows services with read permissions).
- `invoke`: Allows actual execution (e.g., executing `/v1/chat/completions` or invoking an Agent).

Unauthorized requests receive an immediate HTTP 403 `model_access_denied` error. Such requests **never reach upstream models and incur zero cost**.

## Cascade Controls & Emergency Kill-Switches

To counter credential leaks or budget overruns, Omniflux supports instant, cascading controls:
- **Disable Project**: Invalidates all child API keys immediately (`project_disabled`).
- **Disable Application**: Blocks calls from a specific client (`application_disabled`).
- **Disable Key**: Instantly revokes a compromised key (`api_key_disabled`).

State changes take effect in **milliseconds** via Redis sync.
