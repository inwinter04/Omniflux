# Metering, Budgets & Rate Limits

Omniflux provides multi-dimensional attribution, multi-currency cost accounting, and resilient budget cutoff mechanisms.

## 5-Dimensional Metering Model

Every data plane request writes an immutable `usage_records` entry, supporting cross-dimensional analysis:
1. **Organization**: Corporate entity cost distribution.
2. **Department**: Administrative unit billing reports.
3. **Project**: Product and engineering budget tracking.
4. **Application**: Microservice-level usage patterns.
5. **API Key / Provider / Model**: Key-level consumption and vendor ROI evaluation.

Captured metrics include:
- `prompt_tokens`, `completion_tokens`, and `total_tokens`
- `cost` and `currency` (calculated from model price schedules)
- Total round-trip `latency_ms` and streaming `first_token_ms`

## Multi-Currency & Package Plans

- **Native Multi-Currency**: Model services declare settlement currencies (USD, CNY, etc.). Financial reports preserve native figures without forced conversion inaccuracies.
- **Coding & Token Package Plans**: Clear accounting separation between pay-as-you-go balances and monthly recurring token quotas.

## Budget Enforcement

Projects can enforce hard spending ceilings:
- **Pre-Call Check**: Before dispatching a request, Omniflux checks if cumulative historical settled spending exceeds the project budget. If exceeded, the call is blocked immediately with a `budget_exceeded` error.
- **Post-Call Settlement**: Token costs are recorded atomically upon response completion.
- **In-Flight Concurrency**: Highly concurrent parallel requests arriving at the exact millisecond may slightly overshoot before cutoff, consistent with standard distributed gateway behavior.

## Distributed Rate Limiting

Built on Redis for high-throughput concurrency protection:
1. **Global Default (`OMNIFLUX_RATE_LIMIT_PER_MIN`)**: Fallback threshold (default 60 req/min).
2. **Per-Key Limits (`rate_limit_per_min`)**: Custom limits assigned to individual API keys.
3. **Enforcement**: Exceeded requests return HTTP 429 `rate_limit_error` without contacting upstreams or consuming tokens.

## Automated Reconciliation

Background tasks continuously scan for dangling `pending` transactions caused by client disconnects or unexpected network termination, resolving unfinalized usage records into consistent states.
