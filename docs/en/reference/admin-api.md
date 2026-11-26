# Admin API

The admin plane uses server-side session authentication (cookie after login), and data is isolated by organization. The API prefix is `/api/v1`.

## Operations

| Endpoint | Description |
| --- | --- |
| `GET /livez` | Process liveness probe |
| `GET /readyz` | Readiness check for PostgreSQL, Redis, and other key dependencies |
| `GET /metrics` | Prometheus metrics (process metrics + data plane business metrics) |
| `GET /admin/` | Admin console |

## Authentication

| Endpoint | Description |
| --- | --- |
| `POST /api/v1/auth/login` | Administrator login |
| `GET /api/v1/auth/registration-options` | Check whether public registration is open |
| `POST /api/v1/auth/register` | Register an account according to system settings; if email verification is required, a verification email is sent first |
| `POST /api/v1/auth/resend-verification` | Resend the public registration verification email; the response does not reveal whether the email exists |
| `POST /api/v1/auth/verify-email` | Consume a one-time verification token, activate the account, and establish a session |
| `POST /api/v1/auth/forgot-password` | Request a password reset email; the response does not reveal whether the email exists |
| `POST /api/v1/auth/reset-password` | Consume a one-time reset token and set a new password |
| `POST /api/v1/auth/accept-invitation` | Consume a one-time invitation token, set a password, activate the account, and establish a session |
| `GET /api/v1/auth/me` | Get the current user's information (including `role`, `roles`, `permissions`, `permissionGroupId`, `permissionGroupName`) |
| `POST /api/v1/auth/logout` | Log out |
| `GET /api/v1/dashboard` | Home page statistics overview |

## Organization Governance

### Organizations

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/organizations` | List organizations (platform administrators see all; others see only the current organization) |
| `POST /api/v1/organizations` | Create an organization (platform administrators only) |
| `PATCH /api/v1/organizations/{id}` | Rename an organization (the slug cannot be changed after creation) |

### Departments

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/departments` | List departments |
| `POST /api/v1/departments` | Create a department |
| `PATCH /api/v1/departments/{id}` | Update a department |
| `DELETE /api/v1/departments/{id}` | Delete a department (rejected if referenced by a project) |

### Projects

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/projects` | List projects |
| `POST /api/v1/projects` | Create a project |
| `PATCH /api/v1/projects/{id}` | Update a project |
| `PATCH /api/v1/projects/{id}/status` | Enable or disable a project; once disabled, its API Keys stop working immediately |
| `DELETE /api/v1/projects/{id}` | Delete a project (rejected if applications, Keys, or usage exist) |

### Applications

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/applications` | List applications |
| `POST /api/v1/applications` | Create an application |
| `PATCH /api/v1/applications/{id}` | Update an application |
| `PATCH /api/v1/applications/{id}/status` | Enable or disable an application; once disabled, its API Keys stop working immediately |
| `DELETE /api/v1/applications/{id}` | Delete an application (rejected if Keys or usage exist) |

## Users & Permission Governance

### Users

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/users` | List users (requires `user:read`; platform administrators can filter by `organization_id`) |
| `POST /api/v1/users` | Create a user (requires `user:manage`; supports specifying `role` or `permission_group_id`; platform administrators can specify the owning organization) |
| `GET /api/v1/users/invite-options` | Check whether email invitations are available (requires `user:manage`) |
| `POST /api/v1/users/invite` | Create a pending user and send a one-time email invitation (supports `role` or `permission_group_id`) |
| `POST /api/v1/users/{id}/invitation/resend` | Resend the invitation to a pending user and invalidate the old link |
| `DELETE /api/v1/users/{id}/invitation` | Revoke the invitation of a pending user |
| `PATCH /api/v1/users/{id}` | Update role / status / password / owning organization |

`POST /api/v1/users` passwords must be 8–128 characters containing both letters and numbers (no leading or trailing spaces); alternatively submit `generate_password: true` for the server to generate a 16-character readable initial password via `crypto/rand` (returned only once in `generated_password` with `Cache-Control: no-store`, never stored in plaintext or written to audit logs). The console provides a generate button beside the password field and shows a one-time credentials modal upon creation with one-click copy for the login URL, account, initial password, or all credentials.

### Permission Catalog, Permission Groups & Per-User Overrides

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/permissions/catalog` | Query all 7 modules / 26 permission nodes and 5 system presets (`pending` / `viewer` / `developer` / `project_admin` / `org_admin`) |
| `GET /api/v1/permission-groups` | List organization-level custom permission groups (platform administrators can filter by `organization_id`) |
| `POST /api/v1/permission-groups` | Create a custom permission group (requires `permission_group:manage`; cannot exceed the operator's own effective permissions) |
| `PATCH /api/v1/permission-groups/{id}` | Update a custom permission group's name, description, and permission nodes |
| `DELETE /api/v1/permission-groups/{id}` | Delete an unused custom permission group (returns `409` if still assigned to members) |
| `GET /api/v1/users/{id}/permissions` | View a member's assigned preset/permission group, per-user Allow/Deny overrides, and effective permissions |
| `PUT /api/v1/users/{id}/permissions` | Update a member's role/preset or custom permission group and configure per-user node-level Allow/Deny overrides |

Effective permissions are resolved in real time as `base permissions (custom permission group first, otherwise role preset) + per-user allow - per-user deny`, with `Deny` taking precedence over `Allow`. `org_owner` and `platform_admin` always retain full administrative permissions; modifying your own or a higher-privileged protected account's permissions is forbidden, as is granting permission nodes you do not possess.

## System Settings

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/system/settings` | Get the public registration configuration (platform administrators only) |
| `PATCH /api/v1/system/settings` | Update public registration, owning organization, default role, and email verification settings (platform administrators only) |
| `GET /api/v1/system/email/settings` | Get the email service configuration; the SMTP password is returned only as a saved/not-saved flag (platform administrators only) |
| `PATCH /api/v1/system/email/settings` | Update SMTP, sender, and public access URL settings (platform administrators only) |
| `POST /api/v1/system/email/test` | Send a test email using the saved configuration (platform administrators only) |
| `GET /api/v1/system/fx-rates` | Get FX synchronization settings and cached exchange rates |
| `PATCH /api/v1/system/fx-rates/settings` | Update auto-sync toggle and sync interval hours (1–168, platform administrators only) |
| `PUT /api/v1/system/fx-rates/{currency}` | Update a non-USD currency exchange rate and manual override flag (platform administrators only) |
| `POST /api/v1/system/fx-rates/sync` | Trigger an immediate FX rate pull from Frankfurter (ECB) and Open ER-API (platform administrators only) |

`PATCH /api/v1/system/settings` submits the full configuration: `public_registration_enabled`, `registration_organization_id`, `registration_role`, and `email_verification_required`. The organization may be empty only when public registration is disabled. Public registration is disabled by default. Before enabling it, you must select an existing organization and a default role of `pending` (default, zero-permission until assigned) / `viewer` / `developer`; each IP may submit at most 5 registrations per minute. When email verification is disabled, a session is established immediately after successful registration; when enabled, a pending account is created and an email is sent first, and a session is established only after verification. These operations are recorded as audit events `system.settings_updated`, `user.self_registered`, `email.verification_sent`, and `auth.email_verified`.

Email tokens are generated by the server with `crypto/rand`, hashed with HMAC-SHA256 before storage, and only the hash and expiry are stored. Verification, invitation, and password reset each use their own one-time token; resending invalidates the old token, and revoking an invitation immediately invalidates the current link.

## Budgets

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/budgets` | List budget windows |
| `POST /api/v1/budgets` | Create a budget window |
| `PATCH /api/v1/budgets/{id}` | Adjust the budget limit |
| `DELETE /api/v1/budgets/{id}` | Delete a budget window |

For organization-level budgets, `scope_id` may be omitted or set to `organization`; the server uses the current organization ID.

## Model Access

### Platform Proxies

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/proxies` | List proxy nodes, per-provider failover pools, policy settings, and provider options (platform administrators only) |
| `POST /api/v1/proxies` | Create a platform proxy with provider bindings and priorities (platform administrators only) |
| `PATCH /api/v1/proxies/{id}` | Update a platform proxy and its provider bindings (platform administrators only) |
| `DELETE /api/v1/proxies/{id}` | Delete a platform proxy and reconcile affected providers (platform administrators only) |
| `POST /api/v1/proxies/{id}/test` | Probe connectivity through a proxy node |
| `PUT /api/v1/proxies/providers/{providerKey}/order` | Update failover priority order and binding enablement for a provider pool |
| `POST /api/v1/proxies/providers/{providerKey}/switch` | Manually switch the sticky active proxy for a provider |
| `POST /api/v1/proxies/providers/{providerKey}/probe` | Probe all proxies in a provider pool and evaluate failover |
| `GET /api/v1/proxies/failover-events` | List proxy failover and switch history events |
| `GET /api/v1/proxies/settings` | Get proxy governance failover policy settings |
| `PATCH /api/v1/proxies/settings` | Update proxy failover policy (probe intervals, thresholds, cooldown, sticky/auto-soak failback) |

### Provider Accounts

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/provider-catalog` | Provider catalog, grouped by model vendor / cloud platform / third-party service; the same vendor offers plan and region variants |
| `GET /api/v1/provider-accounts` | List provider accounts (including active probe state, 24h heartbeat bar, active proxy summary, and 24h passive statistics) |
| `GET /api/v1/provider-accounts/availability` | Provider-level availability overview, 24h heartbeat bars, and scheduled check policy |
| `POST /api/v1/provider-accounts/test-all` | Probe all active provider accounts in the current organization and persist results |
| `POST /api/v1/provider-accounts` | Create a provider account (credentials are stored encrypted; for openai/anthropic types, base_url must not include `/v1`, and the gateway appends paths such as `/v1/chat/completions` automatically) |
| `PATCH /api/v1/provider-accounts/{id}` | Update a provider account (the API Key can be rotated; leave it blank to keep it unchanged) |
| `PATCH /api/v1/provider-accounts/{id}/status` | Enable or disable an account and refresh the upstream engine |
| `GET /api/v1/provider-accounts/{id}/models` | Fetch the upstream model list using the saved credentials, and fill in known input/output prices from models.dev |
| `GET /api/v1/provider-accounts/{id}/balance` | Query supported account balances and plan quotas, returned in the original currency; quotas include used/remaining percentage and reset time |
| `POST /api/v1/provider-accounts/{id}/test` | Manual connectivity and credential probe (persists check history and updates health state) |
| `GET /api/v1/provider-accounts/{id}/health-history` | Query 24h/7d availability, avg/P95 latency, and recent check records for an account |
| `GET /api/v1/provider-health/settings` | Get scheduled provider health check settings |
| `PATCH /api/v1/provider-health/settings` | Update scheduled provider health check settings (platform administrators only) |
| `DELETE /api/v1/provider-accounts/{id}` | Delete a provider account |

The console lets you select discovered models and carry them into the new model service form, where price, quality score, priority, and weight can be adjusted before saving. There is no unified automatic upstream source for the quality score, so a neutral default of 0.5 is used.

### Route Policies

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/route-policies` | List route policies with bound model services and 24h traffic/fallback/TTFT stats |
| `POST /api/v1/route-policies` | Create a route policy (9 strategies, compare currency, 4D weights, conditional rules, retry policy, circuit breaker) |
| `PATCH /api/v1/route-policies/{id}` | Update policy fields (omitted fields preserved; increments `version`) |
| `PATCH /api/v1/route-policies/{id}/status` | Enable or disable a route policy (returns 409 `POLICY_IN_USE` while bound to model services) |
| `POST /api/v1/route-policies/{id}/clone` | Clone an existing route policy |
| `POST /api/v1/route-policies/simulate` | Zero-cost router Dry-Run simulator (supports draft params, FX normalization, conditional rules, failover trace) |
| `POST /api/v1/route-policies/{id}/playground` | Execute a live route playground call (`source=route_playground`) and record decision trail |
| `GET /api/v1/route-policies/{id}/overview` | Policy overview with baseline cost savings, hit share, and per-model performance |
| `GET /api/v1/route-policies/{id}/metrics` | Real-time policy metrics (TTFT P50/P95, OTPS, RPM/TPM, time series, error distribution) |
| `GET /api/v1/route-policies/{id}/decisions` | Paginated policy decisions with `decision_detail` and `attempt_trail` |
| `DELETE /api/v1/route-policies/{id}` | Delete a route policy |

Route policies support 9 scheduling modes (`balanced`, `cost_first`, `quality_first`, `latency_first`, `reliability_first`, `weighted`, `round_robin`, `strict_failover`, `custom`); `model_candidates` also support `context_window`, `capabilities` (`vision`, `tools`, `reasoning`, `json_mode`, `streaming`), and `tags`, and mixed settlement currencies within one model service.

### Model Services

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/model-services` | List model services (including candidates) |
| `POST /api/v1/model-services` | Create a model service and its candidates |
| `PATCH /api/v1/model-services/{id}` | Update a model service (candidates are upserted by stable key; removed candidates keep their history) |
| `PATCH /api/v1/model-services/{id}/status` | Enable or disable a model service |
| `PATCH /api/v1/model-candidates/{id}/status` | Enable or disable a single candidate model |
| `GET /api/v1/model-candidates/{id}` | Look up the model service a candidate belongs to, used for audit resource navigation |
| `GET /api/v1/model-services/{id}/grants` | List organization/department/project grants of a model service |
| `POST /api/v1/model-services/{id}/grants` | Add or update a grant; supports `read` and `invoke` |
| `DELETE /api/v1/model-services/{id}/grants/{grantID}` | Revoke a grant |
| `DELETE /api/v1/model-services/{id}` | Delete a model service |

By default, a model service is created with an organization-level `read + invoke` grant; after the organization grant is revoked, it can be opened only to specific departments or projects. `read` governs `GET /v1/models`, and `invoke` governs chat, embeddings, and agent execution; the project that owns the API Key determines the visible scope. Account and service lists additionally return the success rate, average latency, last call, number of available candidates, and health status for the last 24 hours; no traffic is marked "unverified", which is not the same as healthy.

A provider account declares its settlement currency, and all candidates of one model service must use the same currency; different services may use different currencies. The public price catalog provides only USD reference prices, and other currencies are not converted automatically. Costs are written to usage in the actual settlement currency and aggregated by currency, and budgets accumulate only costs in the same currency. Account balances keep the vendor's original currency and unit, with no implicit exchange-rate conversion; plans such as Coding Plan are returned as separate quota windows with `kind=quota`, distinct from monetary balances.

Usage summaries and details cover business API calls, prompt debugging, and agent debugging, and distinguish them by `source`: `api`, `prompt_debug`, `agent_debug`. With multiple currencies, `cost_currency=MIXED` and the amount for each currency is returned separately in `costs`, with no cross-currency summing.

### API Keys

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/api-keys` | List API Keys; `expired` means enabled but past its validity period (the data plane rejects such Keys) |
| `POST /api/v1/api-keys` | Create an API Key (plaintext is returned only once; a Key-level rate limit can be set) |
| `PATCH /api/v1/api-keys/{id}` | Update status and Key-level rate limit; omitted fields stay unchanged, and passing `null` for `rate_limit_per_min` clears the limit |
| `DELETE /api/v1/api-keys/{id}` | Soft-delete an API Key |

## Enterprise Assets

Asset types are `prompt_template` and `agent`. Assets are always isolated by organization; creating and updating produces an immutable draft version, which becomes visible to the data plane only after publishing. Updates and rollbacks must carry `expected_version` to prevent concurrent overwrites; a rollback copies a previously published version as a new version and does not modify history.

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/assets?type={assetType}` | List assets, optionally filtered by type |
| `POST /api/v1/assets` | Create a PromptTemplate or Agent with draft v1 |
| `GET /api/v1/assets/{id}` | Asset details, latest configuration, and version chain |
| `PATCH /api/v1/assets/{id}` | Save a new draft version |
| `POST /api/v1/assets/{id}/publish` | Publish the latest draft |
| `POST /api/v1/assets/{id}/rollback` | Roll back and produce a new published version |
| `PATCH /api/v1/assets/{id}/status` | Disable, re-enable, or archive an asset |
| `GET /api/v1/assets/{id}/versions/{versionID}` | Get a version's configuration snapshot |
| `GET /api/v1/assets/{id}/versions/diff?from=...&to=...` | Get a unified diff between two versions |
| `POST /api/v1/assets/{id}/render` | Render an admin-side preview using a specified version |
| `POST /api/v1/assets/{id}/run` | Run against a real model; a single model supports SSE streaming, multiple models run a comparison |
| `GET /api/v1/assets/{id}/runs` | List the latest 100 prompt model run records |
| `GET /api/v1/assets/{id}/agent/sessions` | List the current administrator's Agent debug sessions |
| `POST /api/v1/assets/{id}/agent/chat` | Send an Agent message; supports non-streaming and `started/delta/result/error` SSE events; invalid input returns 400 (404 if the session or version does not exist), `code` is an uppercase error code, `reason` is a kebab-case identifier, and the English explanation is in `detail` |
| `GET /api/v1/assets/{id}/agent/runs` | List the latest 100 Agent run records |
| `GET /api/v1/assets/{id}/agent/sessions/{sessionID}` | Get a session's configuration and full message history |
| `DELETE /api/v1/assets/{id}/agent/sessions/{sessionID}` | Delete the current administrator's debug session and its messages |
| `GET /api/v1/assets/{id}/evaluation-sets` | List evaluation sets and test cases |
| `POST /api/v1/assets/{id}/evaluation-sets` | Create an evaluation set (1-50 cases) |
| `DELETE /api/v1/assets/{id}/evaluation-sets/{setID}` | Delete an evaluation set, keeping historical runs |
| `PATCH /api/v1/assets/{id}/evaluation-sets/{setID}` | Edit an evaluation set's name, description, and full case set |
| `GET /api/v1/evaluation-sets` | Organization-level evaluation set governance list, including owning template, run count, match rate, and latest run |
| `POST /api/v1/assets/{id}/evaluate` | Batch-run an evaluation set and output a summary of match rate, cost, and latency |
| `GET /api/v1/assets/{id}/grants` | List effective grants |
| `POST /api/v1/assets/{id}/grants` | Add or update organization/department/project grants |
| `DELETE /api/v1/assets/{id}/grants/{grantID}` | Revoke a grant |

`asset_grants.actions` supports `read` and `invoke`. The data plane resolves grants by the project that owns the API Key; `read` governs configuration reads, and `invoke` governs rendering or Agent calls; admin-side write operations are controlled by RBAC. Archiving is irreversible, and all lifecycle and grant changes are written to the audit log.

An Agent configuration includes `prompt_template_id`, `prompt_version_id`, `model_service_id`, optional generation parameters, and `max_history_messages`. Each session pins the Agent version and initial variables at creation time, and later messages reuse these settings; admin-side sessions are isolated by the user who created them. Run records store the request/response, tokens, cost, latency, and errors; audit metadata does not store message or variable contents.

Model debugging uses the existing model service routing, fallback, timeout, and price configuration. A single streaming run returns `started/delta/result/error` SSE events; a comparison covers at most 4 models. Run records store the version, tokens, cost, latency, provider, schema validation, and errors. `output_schema` is a standard JSON Schema with external references disallowed; model output is validated again on the server.

Batch evaluation supports three deterministic rules, `exact`, `contains`, and `regex`, with at most 50 cases and 4 models per run, and limited execution concurrency. Results can be used to compare match rate, cost, and latency; they do not represent human or model subjective scoring in open-ended semantics.

## Usage and Audit

| Endpoint | Description |
| --- | --- |
| `GET /api/v1/usage/summary` | Usage summary; supports the same filters as the details, and aggregates by model / model service / provider / route policy / error type / project / application / API Key / department / status / source |
| `GET /api/v1/usage/records` | Paginated usage details; supports filtering by project / application / Key / model / status / source / time |
| `GET /api/v1/usage/trend` | Daily aggregated trend; supports the dimensions overview / model / model service / project / project+application / project+model service / project+model service+application / department+project / department+project+application / department+project+model service / department+project+model service+application / application / API Key / provider / department / status / source / route policy / error type |
| `GET /api/v1/usage/records/export` | Export usage details as CSV using the current filters |
| `GET /api/v1/audit-events` | Paginated audit events; supports filtering by identity / action / resource type (exact match) / result / time, and includes the affected resource's name and whether it still exists (`resource_exists`); `action` uses contains matching |
| `GET /api/v1/audit-events/{id}` | Audit event details |

The audit list returns, for each event, `resource_name` (the name of the affected resource) and `resource_exists` (whether the resource still exists; a soft-deleted API Key counts as nonexistent): if the resource still exists, the current name is used; if deleted, the name recorded in the audit snapshot is used. For grant events, whether the owning model service / asset still exists is what counts. Delete operations write the deleted resource's name into the pre-change snapshot (for budget records, the owning scope and quota), and grant changes record the owning model service or asset.

## Error Format

Admin plane errors return only machine-readable fields, not localized text:

```json
{
  "code": "ORG_NOT_FOUND",
  "reason": "organization-does-not-exist",
  "params": {},
  "detail": ""
}
```

| Field | Description |
| --- | --- |
| `code` | Stable, coarse-grained error code |
| `reason` | Stable, fine-grained reason; the console frontend renders text in the current language from it |
| `params` | Structured error parameters, e.g. retry seconds, field name, version number |
| `detail` | Optional technical detail or raw upstream error; not to be used as UI text |

The console frontend renders its Chinese/English text only from `reason` and `params`; do not rely on natural language returned by the backend.
