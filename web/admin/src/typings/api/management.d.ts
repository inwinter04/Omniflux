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

    interface ProviderAccount {
      id: string;
      provider: string;
      name: string;
      region: string | null;
      base_url: string | null;
      status: string;
      /** 近 24 小时被动健康统计;无流量时为 null */
      requests_24h: number;
      success_rate_24h: number | null;
      created_at: string;
      updated_at: string;
    }

    interface CreateProviderAccountParams {
      provider: string;
      name: string;
      region?: string;
      base_url?: string;
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
      status: string;
    }

    interface ModelService {
      id: string;
      name: string;
      route_policy_id: string | null;
      status: string;
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
      rate_limit_per_min?: number;
    }

    /** 明文 key 仅在创建响应中出现一次 */
    interface CreateApiKeyResult {
      id: string;
      key: string;
      hint: string;
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
    }

    interface UpdateRoutePolicyParams {
      name: string;
      strategy?: string;
      fallback_limit?: number;
      provider_timeout_ms?: number;
      first_token_timeout_ms?: number;
    }

    type UpdateModelServiceParams = CreateModelServiceParams;

    interface Paged<T> {
      total: number;
      page: number;
      page_size: number;
      records: T[];
    }

    interface UsageModelSummary {
      provider: string;
      upstream_model: string;
      requests: number;
      input_tokens: number;
      output_tokens: number;
      cost: string;
    }

    interface UsageDaySummary {
      day: string;
      requests: number;
    }

    interface UsageSummary {
      total_requests: number;
      success_requests: number;
      failed_requests: number;
      total_input_tokens: number;
      total_output_tokens: number;
      total_cost: string;
      cost_currency: string;
      by_model: UsageModelSummary[];
      by_day: UsageDaySummary[];
    }

    interface UsageRecord {
      id: string;
      created_at: string;
      project_name: string;
      application_name: string;
      key_name: string;
      model_service: string;
      state: string;
      status_code: number | null;
      provider: string | null;
      upstream_model: string | null;
      input_tokens: number;
      output_tokens: number;
      cost: string;
      total_ms: number | null;
    }

    interface CreateDepartmentParams {
      name: string;
    }

    interface User {
      id: string;
      email: string;
      role: string;
      status: string;
      last_login_at: string | null;
      created_at: string;
    }

    interface CreateUserParams {
      email: string;
      password: string;
      role: string;
      organization_id?: string;
    }

    interface UpdateUserParams {
      role?: string;
      status?: string;
      password?: string;
      organization_id?: string;
    }

    interface Dashboard {
      active_services: number;
      active_providers: number;
      active_api_keys: number;
      user_count: number;
      requests_7d: number;
      success_rate_7d: string;
      cost_7d: string;
      cost_currency: string;
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
      message: string;
    }

    interface AuditEventDetail extends AuditEvent {
      before_state: unknown;
      after_state: unknown;
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
      result: string;
    }
  }
}








