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
      updated_by: string | null;
      updated_at: string;
    }

    interface UpdateSystemSettingsParams {
      public_registration_enabled: boolean;
      registration_organization_id: string | null;
      email_verification_required: boolean;
      registration_role: 'pending' | 'developer' | 'viewer';
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
      last_used_at: string | null;
      active_candidates: number;
      health_status: 'healthy' | 'degraded' | 'unhealthy' | 'no_data' | 'no_candidates' | 'disabled';
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
      availability_24h: number | null;
      avg_latency_ms_24h: number | null;
      requests_24h: number;
      success_rate_24h: number | null;
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
      base_url: string;
      api_base_url: string;
      website_url: string;
      api_key_url: string;
      balance_kind: string;
      editable_url: boolean;
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

    interface ProviderBalanceEntry {
      label_key: string;
      kind: 'balance' | 'quota';
      remaining: number | null;
      total: number | null;
      used: number | null;
      unit: string;
      next_reset_time?: number;
    }

    interface CreateProviderAccountParams {
      provider: string;
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

    interface RoutePolicy {
      id: string;
      name: string;
      version: number;
      strategy: string;
      constraints: RouteConstraints;
      weights: unknown;
      fallback_limit: number;
      provider_timeout_ms: number;
      first_token_timeout_ms: number;
      status: string;
      created_at: string;
      updated_at: string;
    }

    interface CreateRoutePolicyParams {
      name: string;
      strategy?: string;
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
      quality_score: number;
      cost_currency: string;
      status: string;
    }

    interface ModelService {
      id: string;
      name: string;
      route_policy_id: string | null;
      status: string;
      success_rate_24h: number | null;
      requests_24h: number;
      avg_latency_ms_24h: number | null;
      last_used_at: string | null;
      active_candidates: number;
      health_status: 'healthy' | 'degraded' | 'unhealthy' | 'no_data' | 'no_candidates' | 'disabled';
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
      quality_score: number;
      cost_currency?: string;
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
      region?: string;
      base_url?: string;
      billing_currency?: string;
      api_key?: string;
    }

    interface UpdateRoutePolicyParams {
      name: string;
      strategy?: string;
      fallback_limit?: number;
      provider_timeout_ms?: number;
      first_token_timeout_ms?: number;
      constraints?: RouteConstraints;
    }

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
      reasoning_tokens: number;
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
  }
}
