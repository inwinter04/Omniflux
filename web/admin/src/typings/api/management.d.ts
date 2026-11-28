declare namespace Api {
  /**
   * namespace Management
   *
   * backend api module: omniflux admin management APIs
   */
  namespace Management {
    interface Department {
      id: string;
      name: string;
    }

    interface Organization {
      id: string;
      name: string;
      slug: string;
      created_at: string;
    }

    interface CreateOrganizationParams {
      name: string;
      slug: string;
    }

    interface UpdateOrganizationParams {
      name: string;
    }

    interface SystemSettings {
      public_registration_enabled: boolean;
      registration_organization_id: string | null;
      registration_role: 'pending' | 'developer' | 'viewer';
      email_verification_required: boolean;
      gateway_base_url: string;
      expose_upstream_balance_to_clients: boolean;
      updated_by: string | null;
      updated_at: string;
    }

    interface UpdateSystemSettingsParams {
      public_registration_enabled: boolean;
      registration_organization_id: string | null;
      email_verification_required: boolean;
      registration_role: 'pending' | 'developer' | 'viewer';
      gateway_base_url: string;
      expose_upstream_balance_to_clients: boolean;
    }

    interface GatewayInfo {
      gateway_base_url: string;
      configured: boolean;
    }

    interface Project {
      id: string;
      name: string;
      department_id: string;
      status: string;
    }

    interface CreateProjectParams {
      name: string;
      department_id: string;
    }

    interface Application {
      id: string;
      project_id: string;
      name: string;
      status: string;
    }

    interface CreateApplicationParams {
      name: string;
      project_id: string;
    }

    type ProviderProxyType = 'none' | 'http' | 'socks5';
    type PlatformProxyType = 'http' | 'socks5';
    type ApiProtocol =
      | 'openai_chat'
      | 'openai_responses'
      | 'anthropic_messages'
      | 'gemini_native'
      | 'azure_openai'
      | 'bedrock'
      | 'vertex';

    interface HeartbeatBucket {
      bucket_start: string;
      status: 'healthy' | 'degraded' | 'unhealthy' | 'no_data';
      total: number;
      ok_count: number;
      avg_latency_ms: number | null;
      last_reason?: string;
    }

    interface ProxyBindingDetail {
      provider_key: string;
      provider_label: string;
      priority: number;
      enabled: boolean;
      status: string;
      last_latency_ms: number | null;
      last_reason?: string;
      last_checked_at: string | null;
    }

    interface PlatformProxyBindingInput {
      provider_key: string;
      priority: number;
      enabled: boolean;
    }

    interface ProxyGovernanceSettings {
      enabled: boolean;
      probe_interval_seconds: number;
      fast_probe_interval_seconds: number;
      probe_timeout_seconds: number;
      fail_threshold: number;
      recover_threshold: number;
      switch_cooldown_seconds: number;
      failback_mode: 'sticky' | 'auto_after_soak';
      failback_soak_seconds: number;
      updated_by?: string | null;
      updated_at?: string | null;
    }

    interface ProxyPoolMember {
      proxy_id: string;
      proxy_name: string;
      proxy_type: PlatformProxyType;
      proxy_url?: string;
      proxy_enabled: boolean;
      binding_enabled: boolean;
      priority: number;
      is_active: boolean;
      status: string;
      consecutive_failures: number;
      consecutive_successes: number;
      ejection_count: number;
      cooldown_until: string | null;
      healthy_since: string | null;
      last_latency_ms: number | null;
      last_reason?: string;
      last_error_detail?: string;
      last_checked_at: string | null;
      availability_24h: number | null;
      heartbeat_24h: HeartbeatBucket[];
    }

    interface ProviderProxyPool {
      provider_key: string;
      provider_label: string;
      active_proxy_id: string | null;
      active_proxy_name: string | null;
      active_proxy_type: PlatformProxyType | null;
      active_proxy_status: string;
      active_since: string | null;
      last_switch_reason?: string | null;
      healthy_count: number;
      total_count: number;
      active_accounts: number;
      members: ProxyPoolMember[];
    }

    interface ProxyFailoverEvent {
      id: string;
      provider_key: string;
      from_proxy_id: string | null;
      from_proxy_name: string | null;
      to_proxy_id: string | null;
      to_proxy_name: string | null;
      trigger: 'auto_failover' | 'auto_failback' | 'manual' | 'config_change';
      reason: string;
      detail?: string | null;
      created_at: string;
    }

    interface ProxyTargetProbeResult {
      provider_key: string;
      target_url: string;
      ok: boolean;
      status: string;
      reason: string;
      status_code?: number;
      latency_ms: number;
      error_detail?: string;
    }

    interface PoolProbeSummary {
      provider_key: string;
      results: ProxyTargetProbeResult[];
      switched: boolean;
      active_proxy_id?: string | null;
      event?: ProxyFailoverEvent | null;
    }

    interface PlatformProxy {
      id: string;
      name: string;
      provider_keys: string[];
      provider_labels: string[];
      bindings: ProxyBindingDetail[];
      proxy_type: PlatformProxyType;
      proxy_url?: string;
      enabled: boolean;
      probe_url?: string;
      updated_at?: string;
      active_accounts: number;
      health_status: string;
      availability_24h: number | null;
      avg_latency_ms_24h: number | null;
      heartbeat_24h: HeartbeatBucket[];
    }

    interface PlatformProxyProviderOption {
      value: string;
      label: string;
    }

    interface PlatformProxyListResult {
      proxies: PlatformProxy[];
      pools: ProviderProxyPool[];
      settings: ProxyGovernanceSettings;
      provider_options: PlatformProxyProviderOption[];
    }

    interface CreatePlatformProxyParams {
      provider_keys?: string[];
      bindings?: PlatformProxyBindingInput[];
      name: string;
      proxy_type: PlatformProxyType;
      proxy_url: string;
      enabled?: boolean;
      probe_url?: string;
    }

    interface UpdatePlatformProxyParams extends Omit<CreatePlatformProxyParams, 'proxy_url'> {
      proxy_url?: string;
    }

    interface ProviderAccount {
      id: string;
      provider: string;
      api_protocol: string;
      name: string;
      region: string | null;
      base_url: string | null;
      proxy_id: string | null;
      proxy_name: string | null;
      proxy_type: ProviderProxyType;
      proxy_status: string;
      proxy_pool_size: number;
      billing_currency: string;
      status: string;
      /** 近 24 小时被动健康统计;无流量时为 null */
      requests_24h: number;
      success_rate_24h: number | null;
      avg_latency_ms_24h: number | null;
      avg_ttft_ms_24h: number | null;
      last_used_at: string | null;
      active_candidates: number;
      health_status: 'healthy' | 'degraded' | 'unhealthy' | 'no_data' | 'no_candidates' | 'disabled';
      health_reason: string;
      active_check_status: 'unknown' | 'healthy' | 'degraded' | 'unhealthy' | 'unsupported';
      active_check_reason?: string;
      active_check_status_code?: number | null;
      active_check_latency_ms?: number | null;
      active_check_error?: string;
      last_checked_at: string | null;
      last_healthy_at: string | null;
      availability_24h: number | null;
      heartbeat_24h: HeartbeatBucket[];
      created_at: string;
      updated_at: string;
    }

    interface ProviderHealthSettings {
      enabled: boolean;
      interval_seconds: number;
      timeout_seconds: number;
      degraded_latency_ms: number;
      failure_threshold: number;
      updated_by?: string | null;
      updated_at?: string | null;
    }

    interface ProviderAvailabilityItem {
      provider: string;
      label: string;
      total_accounts: number;
      active_accounts: number;
      healthy_accounts: number;
      degraded_accounts: number;
      unhealthy_accounts: number;
      unknown_accounts: number;
      unsupported_accounts: number;
      health_status: 'healthy' | 'degraded' | 'unhealthy' | 'no_data' | 'disabled';
      health_reason: string;
      availability_24h: number | null;
      avg_latency_ms_24h: number | null;
      requests_24h: number;
      success_rate_24h: number | null;
      avg_ttft_ms_24h: number | null;
      proxy_pool_size: number;
      active_proxy_id: string | null;
      active_proxy_name: string | null;
      active_proxy_type: ProviderProxyType;
      active_proxy_status: string;
      last_checked_at: string | null;
      heartbeat_24h: HeartbeatBucket[];
    }

    interface ProviderAvailabilityOverview {
      providers: ProviderAvailabilityItem[];
      settings: ProviderHealthSettings;
      summary: {
        total_providers: number;
        total_accounts: number;
        healthy_accounts: number;
        degraded_accounts: number;
        unhealthy_accounts: number;
        overall_availability_24h: number | null;
      };
    }

    interface AccountProbeResult {
      account_id: string;
      provider: string;
      ok: boolean;
      status: string;
      reason: string;
      status_code?: number;
      latency_ms: number;
      proxy_id?: string | null;
      proxy_name?: string | null;
      detail?: string;
      checked_at: string;
      consecutive_failures: number;
    }

    interface BatchAccountProbeResult {
      total: number;
      checked: number;
      healthy: number;
      degraded: number;
      unhealthy: number;
      unsupported: number;
      results: AccountProbeResult[];
    }

    interface ProviderAccountHealthCheckRecord {
      id: number;
      trigger: 'auto' | 'manual';
      ok: boolean;
      status: string;
      reason: string;
      http_status: number | null;
      latency_ms: number | null;
      proxy_name: string | null;
      error_detail?: string;
      checked_at: string;
    }

    interface ProviderAccountHealthHistory {
      account_id: string;
      provider: string;
      name: string;
      current_status: string;
      current_reason: string;
      current_http_status: number | null;
      current_latency_ms: number | null;
      current_proxy_name: string | null;
      current_error_detail?: string;
      consecutive_failures: number;
      last_checked_at: string | null;
      last_healthy_at: string | null;
      health_status: 'healthy' | 'degraded' | 'unhealthy' | 'no_data' | 'no_candidates' | 'disabled';
      health_reason: string;
      requests_24h: number;
      success_rate_24h: number | null;
      avg_ttft_ms_24h: number | null;
      passive_avg_latency_ms_24h: number | null;
      availability_24h: number | null;
      availability_7d: number | null;
      avg_latency_ms_24h: number | null;
      p95_latency_ms_24h: number | null;
      heartbeat_24h: HeartbeatBucket[];
      checks: ProviderAccountHealthCheckRecord[];
    }

    interface ProviderCatalogEntry {
      id: string;
      name: string;
      group: string;
      family_id: string;
      version_kind: string;
      region_kind: string;
      default_protocol?: string;
      base_url: string;
      api_base_url: string;
      website_url: string;
      api_key_url: string;
      balance_kind: string;
      editable_url: boolean;
      pricing_kind: 'payg' | 'plan' | 'unknown';
      plan_kind: 'coding' | 'token' | 'agent' | '' | string;
      quota_kind: 'none' | 'balance' | 'plan_usage' | 'balance_and_plan' | string;
      quota_support: 'supported' | 'unsupported' | 'unconfirmed';
      credential_kind: 'inference_api_key' | 'oauth_device' | 'volc_aksk' | 'console_only' | 'unknown';
      quota_docs_url: string;
    }

    interface ProviderCatalogGroup {
      group: string;
      entries: ProviderCatalogEntry[];
    }

    interface ProviderModel {
      id: string;
      name: string;
      input_cost_per_million: number | null;
      output_cost_per_million: number | null;
    }

    interface ProviderModelsResponse {
      models: ProviderModel[];
      pricing_source: string;
      pricing_kind: ProviderCatalogEntry['pricing_kind'];
      plan_kind: ProviderCatalogEntry['plan_kind'];
      quota_kind: ProviderCatalogEntry['quota_kind'];
      quota_support: ProviderCatalogEntry['quota_support'];
      credential_kind: ProviderCatalogEntry['credential_kind'];
      quota_docs_url: string;
    }

    interface ProviderBalanceEntry {
      label_key: string;
      kind: 'balance' | 'quota';
      remaining: number | null;
      total: number | null;
      used: number | null;
      unit: string;
      next_reset_time?: number;
      next_reset_time_text?: string;
    }

    interface ProviderPlanQuotaEntry {
      id: string;
      label_key: string;
      window_kind: 'rolling' | 'weekly' | 'subscription_month' | 'unknown' | string;
      window_seconds?: number;
      used_percent: number | null;
      remaining_percent: number | null;
      used?: number | null;
      remaining?: number | null;
      total?: number | null;
      unit: string;
      reset_at?: string;
    }

    interface ProviderUsageSummary {
      account_id: string;
      account_name: string;
      provider: string;
      pricing_kind: ProviderCatalogEntry['pricing_kind'];
      plan_kind: ProviderCatalogEntry['plan_kind'];
      quota_kind: ProviderCatalogEntry['quota_kind'];
      quota_support: ProviderCatalogEntry['quota_support'];
      quota_status: 'ok' | 'query_failed' | string;
      balances: ProviderBalanceEntry[];
      plan_quotas: ProviderPlanQuotaEntry[];
      queried_at: string;
    }

    interface ProviderUsageSummariesResponse {
      items: ProviderUsageSummary[];
      queried_at: string;
    }

    interface ProviderBalanceResponse {
      pricing_kind: ProviderCatalogEntry['pricing_kind'];
      plan_kind: ProviderCatalogEntry['plan_kind'];
      quota_kind: ProviderCatalogEntry['quota_kind'];
      quota_support: ProviderCatalogEntry['quota_support'];
      quota_status: 'ok' | 'unsupported' | string;
      balances: ProviderBalanceEntry[];
      plan_quotas: ProviderPlanQuotaEntry[];
      queried_at: string;
    }

    interface CreateProviderAccountParams {
      provider: string;
      api_protocol?: string;
      name: string;
      region?: string;
      base_url?: string;
      billing_currency?: string;
      api_key: string;
    }

    interface RouteConstraints {
      allowed_providers?: string[];
      blocked_providers?: string[];
      allowed_upstream_models?: string[];
      blocked_upstream_models?: string[];
      max_cost_per_million?: number;
    }

    interface RoutePolicyWeights {
      cost: number;
      quality: number;
      latency: number;
      reliability: number;
    }

    interface RouteRuleWhen {
      min_input_tokens?: number | null;
      max_input_tokens?: number | null;
      stream?: boolean | null;
      capabilities?: string[];
      project_ids?: string[];
      application_ids?: string[];
      api_key_ids?: string[];
      keywords?: string[];
      metadata?: Record<string, string[]>;
    }

    interface RouteRuleTarget {
      strategy?: string;
      candidate_tags?: string[];
      allowed_providers?: string[];
      blocked_providers?: string[];
      allowed_models?: string[];
    }

    interface RouteRule {
      id: string;
      name: string;
      enabled: boolean;
      priority: number;
      when: RouteRuleWhen;
      target: RouteRuleTarget;
    }

    interface RouteRetryPolicy {
      retry_429: boolean;
      retry_5xx: boolean;
      retry_timeout: boolean;
      fallback_on_auth_error: boolean;
      fallback_on_not_found: boolean;
      fallback_on_rate_limit: boolean;
      fallback_on_server_error: boolean;
      fallback_on_ttft_timeout: boolean;
      fallback_on_client_error: boolean;
      max_retries_per_candidate: number;
    }

    interface RouteCircuitBreakerPolicy {
      enabled: boolean;
      failure_threshold: number;
      cooldown_seconds: number;
      half_open_max_calls: number;
    }

    interface RouteSessionAffinityPolicy {
      enabled: boolean;
      ttl_seconds: number;
      key_strategy: 'auto' | 'user_id' | 'header_only';
    }

    interface OrchestratorTiers {
      simple?: string[];
      medium?: string[];
      complex?: string[];
      reasoning?: string[];
    }

    interface OrchestratorThresholds {
      simple_medium: number;
      medium_complex: number;
      complex_reasoning: number;
    }

    interface RouteOrchestratorConfig {
      session_mode: 'adaptive' | 'per_turn' | 'sticky_upgrade' | string;
      sticky_context_tokens: number;
      long_context_threshold: number;
      thresholds: OrchestratorThresholds;
      tiers?: OrchestratorTiers;
    }

    interface OrchestratorDecision {
      tier: string;
      raw_tier: string;
      score: number;
      session_mode: string;
      cache_protected?: boolean;
      upgraded_from_tier?: string;
      auto_bucketed?: boolean;
      signals?: string[];
      dimensions?: Record<string, number>;
    }

    interface BoundModelServiceSummary {
      id: string;
      name: string;
      status: string;
      candidate_count: number;
    }

    interface RoutePolicy {
      id: string;
      name: string;
      version: number;
      strategy: string;
      compare_currency: string;
      constraints: RouteConstraints;
      weights: RoutePolicyWeights;
      rules: RouteRule[];
      retry_policy: RouteRetryPolicy;
      circuit_breaker: RouteCircuitBreakerPolicy;
      session_affinity?: RouteSessionAffinityPolicy;
      orchestrator_config?: RouteOrchestratorConfig;
      baseline_candidate_id: string | null;
      fallback_limit: number;
      provider_timeout_ms: number;
      first_token_timeout_ms: number;
      status: string;
      bound_services: BoundModelServiceSummary[];
      requests_24h: number;
      success_rate_24h: number | null;
      fallback_rate_24h: number | null;
      avg_ttft_ms_24h: number | null;
      avg_latency_ms_24h: number | null;
      created_at: string;
      updated_at: string;
    }

    interface CreateRoutePolicyParams {
      name: string;
      strategy?: string;
      compare_currency?: string;
      weights?: RoutePolicyWeights;
      rules?: RouteRule[];
      retry_policy?: RouteRetryPolicy;
      circuit_breaker?: RouteCircuitBreakerPolicy;
      session_affinity?: RouteSessionAffinityPolicy;
      orchestrator_config?: RouteOrchestratorConfig;
      baseline_candidate_id?: string | null;
      fallback_limit?: number;
      provider_timeout_ms?: number;
      first_token_timeout_ms?: number;
      constraints?: RouteConstraints;
    }

    interface ModelCandidate {
      id: string;
      provider_account_id: string;
      provider: string;
      upstream_model: string;
      priority: number;
      weight: number;
      input_cost_per_million: number;
      output_cost_per_million: number;
      cache_read_cost_per_million: number | null;
      cache_write_cost_per_million: number | null;
      quality_score: number;
      cost_currency: string;
      context_window: number | null;
      capabilities: string[];
      tags: string[];
      status: string;
    }

    interface ModelService {
      id: string;
      name: string;
      expose_router: boolean;
      router_model_id: string | null;
      route_policy_id: string | null;
      status: string;
      success_rate_24h: number | null;
      requests_24h: number;
      avg_latency_ms_24h: number | null;
      avg_ttft_ms_24h: number | null;
      last_used_at: string | null;
      active_candidates: number;
      health_status: 'healthy' | 'degraded' | 'unhealthy' | 'no_data' | 'no_candidates' | 'disabled';
      health_reason: string;
      created_at: string;
      updated_at: string;
      candidates: ModelCandidate[];
    }

    interface CreateCandidateParams {
      provider_account_id: string;
      upstream_model: string;
      priority: number;
      weight: number;
      input_cost_per_million: number;
      output_cost_per_million: number;
      cache_read_cost_per_million?: number | null;
      cache_write_cost_per_million?: number | null;
      context_window?: number | null;
      capabilities?: string[];
      tags?: string[];
      status?: 'active' | 'disabled';
    }

    interface ModelServiceGrant {
      id: string;
      model_service_id: string;
      subject_type: 'organization' | 'department' | 'project';
      subject_id: string;
      subject_name: string;
      actions: ('read' | 'invoke')[];
      created_at: string;
      updated_at: string;
    }

    interface CreateModelServiceParams {
      name: string;
      expose_router?: boolean;
      router_model_id?: string | null;
      route_policy_id?: string | null;
      candidates: CreateCandidateParams[];
    }

    interface ApiKey {
      id: string;
      name: string;
      key_prefix: string;
      project_id: string;
      application_id: string;
      /** 每分钟请求上限;null 表示沿用全局默认 */
      rate_limit_per_min: number | null;
      status: string;
      expires_at: string | null;
      /** 启用中但已过有效期;数据面会拒绝这类 Key */
      expired: boolean;
      last_used_at: string | null;
      created_at: string;
    }

    interface CreateApiKeyParams {
      name: string;
      project_id: string;
      application_id: string;
      expires_at?: string;
      rate_limit_per_min?: number;
    }

    interface UpdateApiKeyParams {
      status?: 'active' | 'disabled';
      /** 不传表示保持不变,传 null 表示清除 Key 级限流 */
      rate_limit_per_min?: number | null;
    }

    /** 明文 key 仅在创建响应中出现一次 */
    interface CreateApiKeyResult {
      id: string;
      key: string;
    }

    interface UpdateProjectParams {
      name: string;
    }

    interface UpdateApplicationParams {
      name: string;
    }

    interface UpdateProviderAccountParams {
      name: string;
      api_protocol?: string;
      region?: string;
      base_url?: string;
      billing_currency?: string;
      api_key?: string;
    }

    type UpdateRoutePolicyParams = CreateRoutePolicyParams;

    type UpdateModelServiceParams = CreateModelServiceParams;

    type AssetStatus = 'draft' | 'published' | 'disabled' | 'archived';

    interface PromptVariable {
      name: string;
      type: 'string' | 'number' | 'integer' | 'boolean';
      description?: string;
      required: boolean;
      default?: unknown;
    }

    interface PromptSpec {
      template: string;
      variables: PromptVariable[];
      model_service_id?: string;
      output_schema?: Record<string, unknown>;
    }

    interface AgentSpec {
      prompt_template_id: string;
      prompt_version_id: string;
      model_service_id: string;
      temperature?: number;
      top_p?: number;
      max_tokens?: number;
      max_history_messages: number;
    }

    interface Asset {
      id: string;
      asset_type: 'prompt_template' | 'agent';
      name: string;
      description: string;
      owner_email: string;
      status: AssetStatus;
      latest_version: number;
      current_version_id: string | null;
      current_version: number | null;
      has_draft: boolean;
      created_at: string;
      updated_at: string;
    }

    interface AssetVersion<S = PromptSpec> {
      id: string;
      version: number;
      spec?: S;
      source_version_id: string | null;
      created_by_email: string;
      created_at: string;
      published_by_email: string | null;
      published_at: string | null;
      current: boolean;
    }

    interface AssetDetail<S = PromptSpec> extends Asset {
      spec: S;
      versions: AssetVersion<S>[];
    }

    interface CreateAssetParams<S = PromptSpec> {
      asset_type?: 'prompt_template' | 'agent';
      name: string;
      description: string;
      spec: S;
    }

    interface UpdateAssetParams<S = PromptSpec> extends CreateAssetParams<S> {
      expected_version: number;
    }

    interface AssetDiff {
      from_version: number;
      to_version: number;
      asset_type: 'prompt_template' | 'agent';
      from_spec: Record<string, unknown>;
      to_spec: Record<string, unknown>;
    }

    interface AssetGrant {
      id: string;
      asset_id: string;
      subject_type: 'organization' | 'department' | 'project';
      subject_id: string;
      subject_name: string;
      actions: ('read' | 'invoke')[];
      created_at: string;
      updated_at: string;
    }

    interface UpsertAssetGrantParams {
      subject_type: 'organization' | 'department' | 'project';
      subject_id: string;
      actions: ('read' | 'invoke')[];
    }

    interface PromptRenderResult {
      version_id: string;
      version: number;
      model_service_id: string;
      rendered: string;
    }

    interface PromptRun {
      id: string;
      group_id: string;
      kind: 'single' | 'compare' | 'evaluation';
      status: 'running' | 'success' | 'failed' | 'interrupted';
      version: number;
      version_id: string;
      model_service_id: string;
      model_service_name: string;
      rendered_prompt: string;
      response: string;
      structured_output?: unknown;
      schema_valid: boolean | null;
      error?: string;
      provider: string;
      upstream_model: string;
      input_tokens: number;
      output_tokens: number;
      cached_tokens: number;
      reasoning_tokens: number;
      cost: number;
      cost_currency: string;
      first_token_ms: number | null;
      total_ms: number;
      score: number | null;
      matched: boolean | null;
      evaluation_set_id: string | null;
      evaluation_case_id: string | null;
      created_at: string;
    }

    interface RunPromptParams {
      version_id: string;
      variables: Record<string, unknown>;
      model_service_ids?: string[];
      stream?: boolean;
    }

    interface RunPromptResult {
      group_id: string;
      kind: 'single' | 'compare';
      runs: PromptRun[];
    }

    interface AgentSession {
      id: string;
      title: string;
      version: number;
      variables: Record<string, unknown>;
      created_at: string;
      updated_at: string;
    }

    interface AgentMessage {
      id: string;
      run_id: string | null;
      role: 'user' | 'assistant';
      content: string;
      created_at: string;
    }

    interface AgentRun {
      id: string;
      session_id: string;
      status: 'running' | 'success' | 'failed' | 'interrupted';
      version: number;
      version_id: string;
      model_service_id: string;
      model_service_name: string;
      request: string;
      response: string;
      error?: string;
      provider: string;
      upstream_model: string;
      input_tokens: number;
      output_tokens: number;
      cached_tokens: number;
      reasoning_tokens: number;
      cost: number;
      cost_currency: string;
      first_token_ms: number | null;
      total_ms: number;
      created_at: string;
    }

    interface AgentChatParams {
      version_id?: string;
      session_id?: string;
      variables?: Record<string, unknown>;
      message: string;
      stream: boolean;
    }

    interface EvaluationCase {
      id?: string;
      input: Record<string, unknown>;
      match_type: 'exact' | 'contains' | 'regex';
      expected: string;
    }

    interface EvaluationSet {
      id: string;
      name: string;
      description: string;
      cases: EvaluationCase[];
      created_at: string;
      updated_at: string;
    }

    interface CreateEvaluationSetParams {
      name: string;
      description: string;
      cases: EvaluationCase[];
    }

    interface EvaluationSetOverview {
      id: string;
      asset_id: string;
      asset_name: string;
      name: string;
      description: string;
      case_count: number;
      run_count: number;
      accuracy: number | null;
      last_run_at: string | null;
      created_at: string;
      updated_at: string;
    }

    interface EvaluationSummary {
      total: number;
      successful: number;
      matched: number;
      accuracy: number;
      average_latency_ms: number;
      total_cost: number;
    }

    interface Paged<T> {
      total: number;
      page: number;
      page_size: number;
      records: T[];
    }

    interface UsageModelSummary {
      provider: string;
      upstream_model: string;
      cost_currency: string;
      requests: number;
      input_tokens: number;
      output_tokens: number;
      cost: string;
    }

    interface UsageDaySummary {
      day: string;
      requests: number;
    }

    interface UsageTrendPoint {
      day: string;
      name: string;
      requests: number;
    }

    interface UsageScopeSummary {
      name: string;
      requests: number;
      input_tokens: number;
      output_tokens: number;
      cost: string;
      cost_currency: string;
    }

    interface UsageSummary {
      costs: { currency: string; total: string }[];
      total_requests: number;
      success_requests: number;
      failed_requests: number;
      total_input_tokens: number;
      total_output_tokens: number;
      total_cache_creation_tokens?: number;
      total_cost: string;
      cost_currency: string;
      by_model: UsageModelSummary[];
      by_project: UsageScopeSummary[];
      by_application: UsageScopeSummary[];
      by_api_key: UsageScopeSummary[];
      by_department: UsageScopeSummary[];
      by_model_service: UsageScopeSummary[];
      by_state: UsageScopeSummary[];
      by_source: UsageScopeSummary[];
      by_provider: UsageScopeSummary[];
      by_protocol?: UsageScopeSummary[];
      by_route_policy: UsageScopeSummary[];
      by_error_type: UsageScopeSummary[];
      by_day: UsageDaySummary[];
    }

    interface UsageRecord {
      id: string;
      request_id: string;
      created_at: string;
      project_name: string | null;
      application_name: string | null;
      key_name: string | null;
      model_service: string;
      route_policy_id: string | null;
      state: string;
      status_code: number | null;
      provider: string | null;
      upstream_model: string | null;
      input_tokens: number;
      output_tokens: number;
      cached_tokens: number;
      cache_creation_tokens?: number;
      reasoning_tokens: number;
      protocol?: string;
      first_token_ms: number | null;
      total_ms: number | null;
      cost: string;
      cost_currency: string;
      source: 'api' | 'prompt_debug' | 'agent_debug';
      error_type: string | null;
      error_message: string | null;
    }

    interface UsageRecordFilters {
      project_id?: string;
      application_id?: string;
      api_key_id?: string;
      model_service_id?: string;
      state?: string;
      source?: string;
      protocol?: string;
      start_time?: string;
      end_time?: string;
    }

    interface CreateDepartmentParams {
      name: string;
    }

    interface User {
      id: string;
      email: string;
      role: string;
      permission_group_id?: string | null;
      permission_group_name?: string | null;
      override_count?: number;
      status: string;
      last_login_at: string | null;
      created_at: string;
    }

    interface CreateUserParams {
      email: string;
      password?: string;
      generate_password: boolean;
      role?: string;
      permission_group_id?: string;
      organization_id?: string;
    }

    interface CreateUserResult {
      id: string;
      generated_password?: string;
    }

    interface UpdateUserParams {
      role?: string;
      status?: string;
      password?: string;
      organization_id?: string;
    }

    interface PermissionModule {
      key: string;
      permissions: string[];
    }

    interface SystemPreset {
      role: string;
      permissions: string[];
    }

    interface PermissionCatalog {
      modules: PermissionModule[];
      presets: SystemPreset[];
    }

    interface PermissionGroup {
      id: string;
      organization_id: string;
      name: string;
      description: string;
      permissions: string[];
      member_count: number;
      created_at: string;
      updated_at: string;
    }

    interface SavePermissionGroupParams {
      name: string;
      description: string;
      permissions: string[];
      organization_id?: string;
    }

    interface UserPermissionsDetail {
      user_id: string;
      email: string;
      role: string;
      permission_group_id: string | null;
      permission_group_name: string | null;
      base_permissions: string[];
      allow: string[];
      deny: string[];
      effective_permissions: string[];
    }

    interface UpdateUserPermissionsParams {
      role?: string;
      permission_group_id?: string | null;
      allow: string[];
      deny: string[];
    }

    interface EmailSettings {
      enabled: boolean;
      smtp_host: string;
      smtp_port: number;
      smtp_username: string;
      from_address: string;
      from_name: string;
      encryption: 'starttls' | 'tls' | 'none';
      public_base_url: string;
      has_password: boolean;
    }

    interface UpdateEmailSettingsParams {
      enabled: boolean;
      smtp_host: string;
      smtp_port: number;
      smtp_username: string;
      smtp_password?: string;
      from_address: string;
      from_name: string;
      encryption: 'starttls' | 'tls' | 'none';
      public_base_url: string;
    }

    interface InviteUserParams {
      email: string;
      role?: string;
      permission_group_id?: string;
      organization_id?: string;
    }

    interface Dashboard {
      active_services: number;
      active_providers: number;
      active_api_keys: number;
      user_count: number;
      department_count: number;
      project_count: number;
      application_count: number;
      requests_7d: number;
      success_rate_7d: string;
      cost_7d: string;
      cost_currency: string;
      costs: { currency: string; total: string }[];
      active_budgets: number;
      exceeded_budgets: number;
    }

    interface Budget {
      id: string;
      scope_type: 'organization' | 'project' | 'application' | 'api_key';
      scope_id: string;
      scope_name: string;
      window_start: string;
      window_end: string;
      limit_amount: string;
      spent_amount: string;
      currency: string;
      exceeded: boolean;
    }

    interface CreateBudgetParams {
      scope_type: string;
      scope_id: string;
      window_start: string;
      window_end: string;
      limit_amount: string;
      currency?: string;
    }

    interface ProviderTestResult {
      ok: boolean;
      status?: string;
      reason: string;
      detail?: string;
      status_code?: number;
      latency_ms?: number;
      proxy_name?: string | null;
      checked_at?: string;
    }

    interface AuditEventFilters {
      actor_type?: string;
      action?: string;
      resource_type?: string;
      resource_id?: string;
      result?: string;
      start_time?: string;
      end_time?: string;
    }

    interface AuditEventDetail extends AuditEvent {
      before_state: unknown;
      after_state: unknown;
      snapshot_names: Record<string, string>;
      ip_address: string | null;
      user_agent: string | null;
    }

    interface AuditEvent {
      id: string;
      created_at: string;
      actor_type: string;
      actor_name: string;
      action: string;
      resource_type: string;
      resource_id: string | null;
      resource_name: string;
      /** 受影响资源是否仍然存在;已删除时不提供直达入口 */
      resource_exists: boolean;
      result: string;
    }

    interface FXRateSettings {
      auto_sync_enabled: boolean;
      sync_interval_hours: number;
      primary_source: string;
      fallback_source: string;
      last_synced_at: string | null;
      last_sync_status: 'never' | 'ok' | 'partial' | 'error';
      last_sync_source: string | null;
      last_sync_error: string | null;
      updated_by?: string | null;
      updated_at?: string | null;
    }

    interface FXRateEntry {
      currency: string;
      rate_per_usd: number;
      source: 'builtin' | 'frankfurter_ecb' | 'open_er_api' | 'manual';
      effective_date: string | null;
      manual_override: boolean;
      updated_by?: string | null;
      updated_at: string;
    }

    interface FXRatesPayload {
      settings: FXRateSettings;
      rates: FXRateEntry[];
    }

    interface CandidateEvaluation {
      candidate_id: string;
      provider: string;
      account_id: string;
      account_name?: string;
      upstream_model: string;
      priority: number;
      weight: number;
      selected: boolean;
      rank?: number;
      excluded_reason?: string;
      circuit_open: boolean;
      account_health?: string;
      cost_currency: string;
      compare_currency: string;
      estimated_cost: number;
      cost_comparable: boolean;
      cost_score: number;
      quality_score: number;
      latency_ms?: number | null;
      latency_metric?: string;
      latency_score: number;
      reliability_rate: number;
      reliability_score: number;
      total_score: number;
      weight_probability?: number | null;
    }

    interface RouteDecision {
      chain_candidate_ids: string[];
      policy_id?: string;
      policy_version: number;
      strategy: string;
      effective_strategy: string;
      compare_currency: string;
      weights: RoutePolicyWeights;
      matched_rule_id?: string;
      matched_rule_name?: string;
      random_seed?: number;
      warnings?: string[];
      orchestrator?: OrchestratorDecision;
      evaluations: CandidateEvaluation[];
    }

    interface SimulatedFailoverStep {
      attempt: number;
      candidate_id: string;
      provider: string;
      upstream_model: string;
      account_name: string;
      outcome: 'simulated_failure' | 'served';
    }

    interface SimulatedSavings {
      compare_currency: string;
      baseline_candidate_id: string | null;
      baseline_upstream_model: string | null;
      baseline_cost: number | null;
      selected_cost: number | null;
      saved_cost: number | null;
      savings_rate: number | null;
    }

    interface SimulateRoutePolicyParams {
      model_service_id: string;
      policy_id?: string | null;
      strategy?: string;
      compare_currency?: string;
      weights?: RoutePolicyWeights;
      constraints?: RouteConstraints;
      rules?: RouteRule[];
      retry_policy?: RouteRetryPolicy;
      circuit_breaker?: RouteCircuitBreakerPolicy;
      session_affinity?: RouteSessionAffinityPolicy;
      orchestrator_config?: RouteOrchestratorConfig;
      baseline_candidate_id?: string | null;
      fallback_limit?: number;
      provider_timeout_ms?: number;
      first_token_timeout_ms?: number;
      estimated_input_tokens?: number;
      estimated_output_tokens?: number;
      stream?: boolean;
      required_capabilities?: string[];
      keyword_text?: string;
      metadata?: Record<string, string>;
      reasoning_effort?: string;
      tool_count?: number;
      tool_turn_count?: number;
      rr_counter?: number;
      simulate_failed_candidate_ids?: string[];
    }

    interface SimulateRoutePolicyResult {
      decision: RouteDecision;
      fx_rates: Record<string, number>;
      estimated_savings: SimulatedSavings;
      simulated_failover: SimulatedFailoverStep[];
    }

    interface PlaygroundRoutePolicyParams {
      model_service_id: string;
      prompt: string;
      stream?: boolean;
      required_capabilities?: string[];
      metadata?: Record<string, string>;
    }

    interface PlaygroundAttemptRecord {
      candidate_id: string;
      provider: string;
      account_id: string;
      upstream_model: string;
      status_code?: number;
      error_code?: string;
      error_message?: string;
      latency_ms: number;
      retry_count?: number;
      outcome: string;
    }

    interface PlaygroundRoutePolicyResult {
      request_id: string;
      model_service_id: string;
      model_service_name: string;
      reply: string;
      served_provider: string;
      served_model: string;
      input_tokens: number;
      output_tokens: number;
      cached_tokens: number;
      reasoning_tokens: number;
      cost: number;
      currency: string;
      total_ms: number;
      attempts: PlaygroundAttemptRecord[];
      decision: RouteDecision;
    }

    interface RoutePolicyOverview {
      policy_id: string;
      compare_currency: string;
      days: number;
      summary: {
        requests: number;
        success_requests: number;
        fallback_requests: number;
        success_rate: number | null;
        fallback_rate: number | null;
        avg_ttft_ms: number | null;
        avg_latency_ms: number | null;
        otps: number | null;
        input_tokens: number;
        output_tokens: number;
        actual_cost: number;
      };
      baseline: {
        compare_currency: string;
        actual_cost: number;
        candidate_id?: string;
        upstream_model?: string;
        provider?: string;
        baseline_cost?: number;
        saved_cost?: number;
        savings_rate?: number | null;
      };
      by_model: {
        candidate_id: string | null;
        provider: string;
        upstream_model: string;
        requests: number;
        success_requests: number;
        fallback_requests: number;
        hit_share: number;
        success_rate: number;
        avg_ttft_ms: number | null;
        avg_latency_ms: number | null;
        otps: number | null;
        input_tokens: number;
        output_tokens: number;
        actual_cost: number;
        avg_cost_per_req: number;
      }[];
      bound_services: BoundModelServiceSummary[];
      candidate_options: {
        id: string;
        model_service: string;
        provider: string;
        account_name: string;
        upstream_model: string;
        cost_currency: string;
        input_cost_per_m: number;
        output_cost_per_m: number;
      }[];
    }

    interface RoutePolicyMetrics {
      kpis: {
        requests: number;
        rpm: number;
        tpm: number;
        success_rate: number | null;
        fallback_rate: number | null;
        ttft_p50_ms: number | null;
        ttft_p95_ms: number | null;
        avg_latency_ms: number | null;
        otps: number | null;
        total_cost: number;
        compare_currency: string;
      };
      granularity: 'hour' | 'day';
      series: {
        bucket: string;
        requests: number;
        success_requests: number;
        failed_requests: number;
        fallback_requests: number;
        avg_ttft_ms: number | null;
        avg_latency_ms: number | null;
        total_tokens: number;
        cost: number;
      }[];
      error_distribution: { error_type: string; count: number }[];
      available_models: string[];
    }

    interface RoutePolicyDecisionRecord {
      id: string;
      request_id: string;
      policy_version: number | null;
      strategy: string;
      matched_rule_id: string | null;
      matched_rule_name: string | null;
      decision: string;
      reason: string;
      selected_candidate_id: string | null;
      model_service_id: string | null;
      model_service_name: string | null;
      source: string;
      state: string;
      served_provider: string | null;
      served_model: string | null;
      first_token_ms: number | null;
      total_ms: number | null;
      cost: number | null;
      cost_currency: string | null;
      decision_detail: Partial<RouteDecision>;
      attempt_trail: { attempts?: PlaygroundAttemptRecord[] };
      created_at: string;
    }

    interface RoutePolicyDecisionsPage {
      items: RoutePolicyDecisionRecord[];
      total: number;
      page: number;
      page_size: number;
    }
  }
}
