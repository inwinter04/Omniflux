# Dynamic Routing & Auditing

In a multi-model ecosystem, selecting the optimal provider based on cost, quality, latency, and real-time reliability is critical. Omniflux delivers an industrial-grade dynamic routing engine with full decision traceability.

## Routing Pipeline

When a request arrives at a logical model service, the engine processes it in four sequential stages:

![Omniflux Routing Pipeline](/assets/routing-pipeline.en-US.png)

### Stage 1: Hard Constraints Filter
Eliminates candidates that fail compliance or budget policies:
- **Allowed / Blocked Providers**: Restrict traffic to approved vendors.
- **Allowed / Blocked Upstream Models**: Exclude unverified or restricted model identifiers.
- **Price Ceiling**: Filter out candidates whose blended token unit price exceeds policy limits.

### Stage 2: Priority Bucketing
Candidates passing stage 1 are sorted by `Priority` in ascending order.
- The engine always evaluates candidates in the highest priority bucket first.
- Lower priority groups serve as secondary fallback tiers if all higher-priority candidates fail.

### Stage 3: 4D Dynamic Scoring
Within the active priority group, candidates are scored using configured policy weights:

$$\text{Total Score} = w_c \cdot S_{\text{cost}} + w_q \cdot S_{\text{quality}} + w_l \cdot S_{\text{latency}} + w_r \cdot S_{\text{reliability}}$$

Data sources for each dimension:
1. **Quality ($S_{\text{quality}}$)**: Based on `quality_score` (0.0~1.0) and offline benchmark evaluations.
2. **Cost ($S_{\text{cost}}$)**: Inversely proportional to the candidate's prompt/completion token pricing.
3. **Latency ($S_{\text{latency}}$)**: Based on 7-day rolling time-to-first-token and round-trip averages.
4. **Reliability ($S_{\text{reliability}}$)**: Based on 7-day rolling success rates, adjusted for recent 24-hour anomalies.

Ties are resolved via weighted random selection based on candidate `weight` attributes.

### Stage 4: Automated Fallback Chain
The highest-scoring candidate becomes the **Primary Target**. Remaining qualified candidates form an ordered **Fallback Chain**:
- If the primary target returns 5xx errors, triggers rate limits (429), or times out, Omniflux **automatically and seamlessly retries the next candidate**.
- Max retry depth is governed by `fallback_limit` to prevent latency spirals.

## Timeout & Streaming Protections

- **Provider Timeout (`provider_timeout_ms`)**: Maximum duration for a single upstream call before aborting and retrying.
- **First Token Timeout (`first_token_timeout_ms`)**: In streaming requests, if the upstream fails to yield the first token within the specified interval, the connection is aborted and failed over.

## Full Decision Tracking (PolicyDecision)

Every routing choice is recorded in `policy_decisions`:
- Timestamp and matched logical service;
- Specific reasons why any candidate was disqualified by hard constraints;
- Detailed 4D score breakdown across all candidates;
- Final primary target and active fallback chain.

Inspect complete traces under **Usage & Auditing → Policy Decision Details** in the console.
