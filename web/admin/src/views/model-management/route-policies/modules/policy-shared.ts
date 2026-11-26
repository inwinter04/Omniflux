import { computed } from 'vue';
import { $t } from '@/locales';

export const PRESET_WEIGHTS: Record<string, Api.Management.RoutePolicyWeights> = {
  balanced: { cost: 0.4, quality: 0.3, latency: 0.15, reliability: 0.15 },
  cost_first: { cost: 0.7, quality: 0.1, latency: 0.1, reliability: 0.1 },
  quality_first: { cost: 0.1, quality: 0.7, latency: 0.1, reliability: 0.1 },
  latency_first: { cost: 0.1, quality: 0.1, latency: 0.7, reliability: 0.1 },
  reliability_first: { cost: 0.1, quality: 0.1, latency: 0.1, reliability: 0.7 }
};

export function defaultRetryPolicy(): Api.Management.RouteRetryPolicy {
  return {
    retry_429: true,
    retry_5xx: true,
    retry_timeout: true,
    fallback_on_auth_error: true,
    fallback_on_not_found: true,
    fallback_on_rate_limit: true,
    fallback_on_server_error: true,
    fallback_on_ttft_timeout: true,
    fallback_on_client_error: false,
    max_retries_per_candidate: 0
  };
}

export function defaultCircuitBreaker(): Api.Management.RouteCircuitBreakerPolicy {
  return {
    enabled: true,
    failure_threshold: 5,
    cooldown_seconds: 30,
    half_open_max_calls: 1
  };
}

export const ALL_STRATEGIES = [
  'balanced',
  'cost_first',
  'quality_first',
  'latency_first',
  'reliability_first',
  'weighted',
  'round_robin',
  'strict_failover',
  'custom'
] as const;

export const SCORING_STRATEGIES = new Set([
  'balanced',
  'cost_first',
  'quality_first',
  'latency_first',
  'reliability_first',
  'custom'
]);

export function useStrategyOptions() {
  return computed(() => [
    { label: $t('business.balanced'), value: 'balanced' },
    { label: $t('business.cost-first'), value: 'cost_first' },
    { label: $t('business.quality-first'), value: 'quality_first' },
    { label: $t('business.strategy-latency-first'), value: 'latency_first' },
    { label: $t('business.strategy-reliability-first'), value: 'reliability_first' },
    { label: $t('business.strategy-weighted'), value: 'weighted' },
    { label: $t('business.strategy-round-robin'), value: 'round_robin' },
    { label: $t('business.strategy-strict-failover'), value: 'strict_failover' },
    { label: $t('business.custom'), value: 'custom' }
  ]);
}

export function useCapabilityOptions() {
  return computed(() => [
    { label: $t('business.cap-vision'), value: 'vision' },
    { label: $t('business.cap-tools'), value: 'tools' },
    { label: $t('business.cap-reasoning'), value: 'reasoning' },
    { label: $t('business.cap-json-mode'), value: 'json_mode' },
    { label: $t('business.cap-streaming'), value: 'streaming' }
  ]);
}

export const COMPARE_CURRENCY_OPTIONS = [
  { label: 'USD ($)', value: 'USD' },
  { label: 'CNY (¥)', value: 'CNY' },
  { label: 'EUR (€)', value: 'EUR' },
  { label: 'GBP (£)', value: 'GBP' },
  { label: 'JPY (¥)', value: 'JPY' },
  { label: 'HKD (HK$)', value: 'HKD' },
  { label: 'TWD (NT$)', value: 'TWD' },
  { label: 'KRW (₩)', value: 'KRW' }
];

export interface PolicyDraftModel {
  id: string;
  name: string;
  strategy: string;
  compare_currency: string;
  weights: Api.Management.RoutePolicyWeights;
  baseline_candidate_id: string | null;
  fallback_limit: number;
  provider_timeout_ms: number;
  first_token_timeout_ms: number;
  constraints: {
    allowed_providers: string[];
    blocked_providers: string[];
    allowed_upstream_models: string[];
    blocked_upstream_models: string[];
    max_cost_per_million: number | null;
  };
  rules: Api.Management.RouteRule[];
  retry_policy: Api.Management.RouteRetryPolicy;
  circuit_breaker: Api.Management.RouteCircuitBreakerPolicy;
}

export function createEmptyPolicyDraft(): PolicyDraftModel {
  return {
    id: '',
    name: '',
    strategy: 'balanced',
    compare_currency: 'USD',
    weights: { ...PRESET_WEIGHTS.balanced },
    baseline_candidate_id: null,
    fallback_limit: 3,
    provider_timeout_ms: 60000,
    first_token_timeout_ms: 15000,
    constraints: {
      allowed_providers: [],
      blocked_providers: [],
      allowed_upstream_models: [],
      blocked_upstream_models: [],
      max_cost_per_million: null
    },
    rules: [],
    retry_policy: defaultRetryPolicy(),
    circuit_breaker: defaultCircuitBreaker()
  };
}

export function draftFromPolicy(row: Api.Management.RoutePolicy): PolicyDraftModel {
  const c = row.constraints ?? {};
  const w = row.weights ?? PRESET_WEIGHTS[row.strategy] ?? PRESET_WEIGHTS.balanced;
  return {
    id: row.id,
    name: row.name,
    strategy: row.strategy || 'balanced',
    compare_currency: row.compare_currency || 'USD',
    weights: {
      cost: Number(w.cost ?? 0.4),
      quality: Number(w.quality ?? 0.3),
      latency: Number(w.latency ?? 0.15),
      reliability: Number(w.reliability ?? 0.15)
    },
    baseline_candidate_id: row.baseline_candidate_id ?? null,
    fallback_limit: row.fallback_limit ?? 3,
    provider_timeout_ms: row.provider_timeout_ms ?? 60000,
    first_token_timeout_ms: row.first_token_timeout_ms ?? 15000,
    constraints: {
      allowed_providers: [...(c.allowed_providers ?? [])],
      blocked_providers: [...(c.blocked_providers ?? [])],
      allowed_upstream_models: [...(c.allowed_upstream_models ?? [])],
      blocked_upstream_models: [...(c.blocked_upstream_models ?? [])],
      max_cost_per_million: c.max_cost_per_million ?? null
    },
    rules: (row.rules ?? []).map(rule => ({
      id: rule.id,
      name: rule.name,
      enabled: rule.enabled,
      priority: rule.priority,
      when: {
        min_input_tokens: rule.when?.min_input_tokens ?? null,
        max_input_tokens: rule.when?.max_input_tokens ?? null,
        stream: rule.when?.stream ?? null,
        capabilities: [...(rule.when?.capabilities ?? [])],
        project_ids: [...(rule.when?.project_ids ?? [])],
        application_ids: [...(rule.when?.application_ids ?? [])],
        api_key_ids: [...(rule.when?.api_key_ids ?? [])],
        keywords: [...(rule.when?.keywords ?? [])],
        metadata: rule.when?.metadata ? { ...rule.when.metadata } : undefined
      },
      target: {
        strategy: rule.target?.strategy || '',
        candidate_tags: [...(rule.target?.candidate_tags ?? [])],
        allowed_providers: [...(rule.target?.allowed_providers ?? [])],
        blocked_providers: [...(rule.target?.blocked_providers ?? [])],
        allowed_models: [...(rule.target?.allowed_models ?? [])]
      }
    })),
    retry_policy: { ...defaultRetryPolicy(), ...(row.retry_policy ?? {}) },
    circuit_breaker: { ...defaultCircuitBreaker(), ...(row.circuit_breaker ?? {}) }
  };
}

export function buildPolicyPayload(draft: PolicyDraftModel): Api.Management.CreateRoutePolicyParams {
  const constraints: Api.Management.RouteConstraints = {};
  if (draft.constraints.allowed_providers.length) constraints.allowed_providers = draft.constraints.allowed_providers;
  if (draft.constraints.blocked_providers.length) constraints.blocked_providers = draft.constraints.blocked_providers;
  if (draft.constraints.allowed_upstream_models.length)
    constraints.allowed_upstream_models = draft.constraints.allowed_upstream_models;
  if (draft.constraints.blocked_upstream_models.length)
    constraints.blocked_upstream_models = draft.constraints.blocked_upstream_models;
  if (draft.constraints.max_cost_per_million !== null && draft.constraints.max_cost_per_million > 0) {
    constraints.max_cost_per_million = draft.constraints.max_cost_per_million;
  }

  const sanitizedRules = draft.rules.map(rule => {
    const when: Api.Management.RouteRuleWhen = {};
    if (rule.when.min_input_tokens !== null && rule.when.min_input_tokens !== undefined) {
      when.min_input_tokens = rule.when.min_input_tokens;
    }
    if (rule.when.max_input_tokens !== null && rule.when.max_input_tokens !== undefined) {
      when.max_input_tokens = rule.when.max_input_tokens;
    }
    if (rule.when.stream !== null && rule.when.stream !== undefined) {
      when.stream = rule.when.stream;
    }
    if (rule.when.capabilities?.length) when.capabilities = rule.when.capabilities;
    if (rule.when.project_ids?.length) when.project_ids = rule.when.project_ids;
    if (rule.when.application_ids?.length) when.application_ids = rule.when.application_ids;
    if (rule.when.api_key_ids?.length) when.api_key_ids = rule.when.api_key_ids;
    if (rule.when.keywords?.length) when.keywords = rule.when.keywords;
    if (rule.when.metadata && Object.keys(rule.when.metadata).length) when.metadata = rule.when.metadata;

    const target: Api.Management.RouteRuleTarget = {};
    if (rule.target.strategy) target.strategy = rule.target.strategy;
    if (rule.target.candidate_tags?.length) target.candidate_tags = rule.target.candidate_tags;
    if (rule.target.allowed_providers?.length) target.allowed_providers = rule.target.allowed_providers;
    if (rule.target.blocked_providers?.length) target.blocked_providers = rule.target.blocked_providers;
    if (rule.target.allowed_models?.length) target.allowed_models = rule.target.allowed_models;

    return {
      id: rule.id.trim(),
      name: rule.name.trim(),
      enabled: rule.enabled,
      priority: rule.priority,
      when,
      target
    };
  });

  return {
    name: draft.name.trim(),
    strategy: draft.strategy,
    compare_currency: draft.compare_currency,
    weights: SCORING_STRATEGIES.has(draft.strategy) ? { ...draft.weights } : undefined,
    baseline_candidate_id: draft.baseline_candidate_id ?? '',
    fallback_limit: draft.fallback_limit,
    provider_timeout_ms: draft.provider_timeout_ms,
    first_token_timeout_ms: draft.first_token_timeout_ms,
    constraints,
    rules: sanitizedRules,
    retry_policy: { ...draft.retry_policy },
    circuit_breaker: { ...draft.circuit_breaker }
  };
}
