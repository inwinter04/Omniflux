import { $t, getLocale } from '@/locales';

/** 管理台业务状态、动作与资源的展示映射，文案统一走语言包。 */

export type StatusResource = 'account' | 'service' | 'candidate' | 'policy' | 'api_key';

export function statusText(status: string, resource?: StatusResource) {
  switch (status) {
    case 'active': {
      if (resource === 'service') return $t('business.available');
      if (resource === 'candidate') return $t('business.routing-enabled');
      if (resource === 'policy') return $t('business.active-hn8ec');
      if (resource === 'api_key') return $t('business.valid');
      return $t('business.normal');
    }
    case 'published':
      return $t('business.published');
    case 'draft':
      return $t('business.draft');
    case 'archived':
      return $t('business.archived');
    case 'disabled':
      return $t('business.disabled-e5ola');
    default:
      return status;
  }
}

export function statusTagType(status: string): 'success' | 'warning' | 'error' | 'info' | 'default' {
  switch (status) {
    case 'active':
      return 'success';
    case 'published':
      return 'success';
    case 'draft':
      return 'info';
    case 'archived':
      return 'default';
    case 'pending_email_verification':
      return 'warning';
    case 'disabled':
      return 'default';
    default:
      return 'default';
  }
}

export function healthStatusText(status: string) {
  const labels: Record<string, string> = {
    healthy: $t('business.healthy'),
    degraded: $t('business.degraded'),
    suspect: $t('business.proxy-suspect'),
    unhealthy: $t('business.unhealthy'),
    unknown: $t('business.not-verified'),
    unsupported: $t('business.unsupported'),
    no_data: $t('business.not-verified'),
    no_candidates: $t('business.no-candidates-configured'),
    disabled: $t('business.disabled-e5ola')
  };
  return labels[status] ?? status;
}

export function healthTagType(status: string): 'success' | 'warning' | 'error' | 'info' | 'default' {
  if (status === 'healthy') return 'success';
  if (status === 'degraded' || status === 'suspect' || status === 'no_data' || status === 'unknown') return 'warning';
  if (status === 'unhealthy') return 'error';
  return 'default';
}

export function probeReasonText(reason?: string) {
  const map: Record<string, App.I18n.I18nKey> = {
    ok: 'business.probe-reason-ok',
    connected: 'business.probe-reason-connected',
    slow_response: 'business.probe-reason-slow-response',
    credentials_rejected: 'business.probe-reason-credentials-rejected',
    rate_limited: 'business.probe-reason-rate-limited',
    v1_suffix_suspected: 'business.probe-reason-v1-suffix',
    proxy_error: 'business.probe-reason-proxy-error',
    proxy_timeout: 'business.probe-reason-proxy-timeout',
    proxy_auth_failed: 'business.probe-reason-proxy-auth-failed',
    proxy_bad_gateway: 'business.probe-reason-proxy-bad-gateway',
    probe_http_error: 'business.probe-reason-probe-http-error',
    upstream_http_error: 'business.probe-reason-upstream-http-error',
    network_error: 'business.probe-reason-network-error',
    unsupported: 'business.probe-reason-unsupported',
    probe_internal_error: 'business.probe-reason-internal-error'
  };
  if (!reason) return '-';
  const key = map[reason];
  return key ? $t(key) : reason;
}

export function failoverTriggerText(trigger?: string) {
  const map: Record<string, App.I18n.I18nKey> = {
    auto_failover: 'business.failover-trigger-auto',
    auto_failback: 'business.failover-trigger-failback',
    manual: 'business.failover-trigger-manual',
    config_change: 'business.failover-trigger-config'
  };
  if (!trigger) return '-';
  const key = map[trigger];
  return key ? $t(key) : trigger;
}

export function failoverReasonText(reason?: string | null) {
  const map: Record<string, App.I18n.I18nKey> = {
    active_proxy_unhealthy: 'business.failover-reason-unhealthy',
    primary_soak_complete: 'business.failover-reason-soak',
    manual_switch: 'business.failover-reason-manual',
    active_proxy_unavailable: 'business.failover-reason-unavailable',
    no_enabled_proxies: 'business.failover-reason-none'
  };
  if (!reason) return '-';
  const key = map[reason];
  return key ? $t(key) : reason;
}

export function healthSummary(row: {
  requests_24h: number;
  success_rate_24h: number | null;
  avg_latency_ms_24h: number | null;
  last_used_at: string | null;
  active_candidates: number;
}) {
  if (row.active_candidates === 0) return $t('business.no-candidate-models-are-available');
  if (row.requests_24h === 0) return $t('business.no-calls-in-the-last');
  const success = Math.round((row.success_rate_24h ?? 0) * 1000) / 10;
  const latency =
    row.avg_latency_ms_24h === null
      ? $t('business.no-data')
      : $t('business.latencyMs', { value: Math.round(row.avg_latency_ms_24h) });
  const lastUsed = row.last_used_at ? formatTime(row.last_used_at) : $t('business.no-records');
  return $t('business.healthSummary', {
    requests: row.requests_24h,
    success,
    latency,
    lastUsed
  });
}

export function usageStateText(state: string) {
  switch (state) {
    case 'pending':
      return $t('business.active');
    case 'success':
      return $t('business.success');
    case 'failed':
      return $t('business.failed');
    case 'interrupted':
      return $t('business.interrupted');
    default:
      return state;
  }
}

export function usageStateTagType(state: string): 'success' | 'warning' | 'error' | 'info' | 'default' {
  switch (state) {
    case 'success':
      return 'success';
    case 'failed':
      return 'error';
    case 'interrupted':
      return 'warning';
    case 'pending':
      return 'info';
    default:
      return 'default';
  }
}

const actionTextKeyMap: Record<string, App.I18n.I18nKey> = {
  'bootstrap.created': 'business.initialize-system',
  'auth.login': 'business.log-in',
  'auth.login_failed': 'business.login-failed',
  'project.created': 'business.create-project-argjkk',
  'project.updated': 'business.update-project',
  'project.deleted': 'business.delete-project',
  'project.status_changed': 'business.change-project-status',
  'application.created': 'business.create-application-ar6opf',
  'application.updated': 'business.update-application',
  'application.deleted': 'business.delete-application',
  'application.status_changed': 'business.change-application-status',
  'department.created': 'business.audit-create-department',
  'department.updated': 'business.update-department',
  'department.deleted': 'business.delete-department',
  'organization.created': 'business.audit-create-organization',
  'organization.updated': 'business.update-organization',
  'provider_account.created': 'business.create-provider-account-1fmjpgf',
  'provider_account.updated': 'business.update-provider-account',
  'provider_account.deleted': 'business.delete-provider-account',
  'provider_account.status_changed': 'business.change-provider-account-status',
  'platform_proxy.created': 'business.create-proxy',
  'platform_proxy.updated': 'business.update-platform-proxy',
  'platform_proxy.deleted': 'business.delete-platform-proxy',
  'platform_proxy.switched': 'business.switch-platform-proxy',
  'platform_proxy.order_updated': 'business.update-platform-proxy-order',
  'platform_proxy.settings_updated': 'business.update-proxy-governance-settings',
  'provider_health.settings_updated': 'business.update-provider-health-settings',
  'route_policy.created': 'business.create-route-policy-167s75c',
  'route_policy.updated': 'business.update-route-policy',
  'route_policy.deleted': 'business.delete-route-policy',
  'route_policy.status_changed': 'business.change-route-policy-status',
  'model_service.created': 'business.create-model-service-11kri7x',
  'model_service.updated': 'business.update-model-service',
  'model_service.deleted': 'business.delete-model-service',
  'model_service.status_changed': 'business.change-model-service-status',
  'model_service.access_denied': 'business.model-service-access-denied',
  'model_candidate.status_changed': 'business.change-model-candidate-status',
  'api_key.created': 'business.audit-create-api-key',
  'api_key.status_changed': 'business.change-api-key-status',
  'api_key.deleted': 'business.delete-api-key',
  'api_key.updated': 'business.update-api-key',
  'budget.created': 'business.audit-create-budget',
  'budget.updated': 'business.update-budget',
  'budget.deleted': 'business.delete-budget',
  'user.created': 'business.audit-create-user',
  'user.updated': 'business.update-user',
  'user.invited': 'business.invitation-email-sent',
  'user.invitation_resent': 'business.invitation-email-sent',
  'user.invitation_revoked': 'business.invitation-revoked',
  'auth.email_verified': 'business.verification-email-succeeded',
  'auth.password_reset_requested': 'business.reset-email-sent',
  'auth.password_reset': 'business.password-reset',
  'auth.invitation_accepted': 'business.accept-invitation',
  'email.verification_sent': 'business.verification-email-sent',
  'email.verification_resent': 'business.verification-email-sent',
  'user.self_registered': 'business.audit-self-registration',
  'system.settings_updated': 'business.update-system-settings',
  'evaluation_set.created': 'business.audit-create-evaluation-set',
  'evaluation_set.updated': 'business.update-evaluation-set',
  'evaluation_set.deleted': 'business.delete-evaluation-set',
  'asset.created': 'business.create-asset',
  'asset.draft_created': 'business.save-asset-draft',
  'asset.published': 'business.publish-asset',
  'asset.rolled_back': 'business.roll-back-asset',
  'asset.status_changed': 'business.change-asset-status',
  'asset_grant.upserted': 'business.save-asset-grant',
  'asset_grant.revoked': 'business.revoke-asset-grant',
  'model_service_grant.upserted': 'business.save-model-service-grant',
  'model_service_grant.revoked': 'business.revoke-model-service-grant',
  'prompt_template.previewed': 'business.preview-prompttemplate',
  'prompt_template.invoked': 'business.invoke-prompttemplate',
  'prompt_run.executed': 'business.execute-prompt-run',
  'prompt_evaluation.executed': 'business.run-evaluation',
  'agent.executed': 'business.execute-agent'
};

export function auditActionText(action: string) {
  const key = actionTextKeyMap[action];
  return key ? $t(key) : action;
}

const resourceTextKeyMap: Record<string, App.I18n.I18nKey> = {
  project: 'business.project',
  application: 'business.application',
  provider_account: 'business.provider-account',
  platform_proxy: 'business.proxy-governance',
  route_policy: 'business.route-policy',
  model_service: 'business.model-service',
  api_key: 'business.api-key',
  user: 'business.user',
  session: 'business.session',
  asset: 'business.enterprise-asset',
  asset_grant: 'business.asset-grant',
  model_service_grant: 'business.model-service-grant',
  model_candidate: 'business.model-candidate',
  evaluation_set: 'business.evaluation-set',
  budget: 'business.budget',
  organization: 'business.organization',
  department: 'business.department',
  system_settings: 'business.system-settings',
  email_settings: 'business.email-service'
};

export function auditResourceText(resourceType: string) {
  const key = resourceTextKeyMap[resourceType];
  return key ? $t(key) : resourceType;
}

/** 审计筛选下拉的候选,与上面的文案映射共用同一份清单。 */
export const auditActions = Object.keys(actionTextKeyMap).sort();

export const auditResourceTypes = Object.keys(resourceTextKeyMap).sort();

/** 审计快照字段的展示名称;未登记的字段原样显示字段名。 */
const snapshotFieldKeyMap: Record<string, App.I18n.I18nKey> = {
  actions: 'business.allowed-actions',
  application_id: 'business.application',
  asset_id: 'business.enterprise-asset',
  asset_type: 'business.asset-type',
  base_url: 'business.api-endpoint',
  billing_currency: 'business.currency',
  candidates: 'business.candidates',
  cases: 'business.case-count',
  constraints: 'business.hard-constraints',
  credentials_rotated: 'business.credentials-rotated',
  currency: 'business.currency',
  description: 'business.description',
  email: 'business.email',
  evaluation_set_id: 'business.evaluation-set',
  expires_at: 'business.expires-at',
  fallback_limit: 'business.failover-limit',
  first_token_timeout_ms: 'business.first-token-timeout-ms',
  group_id: 'business.run-group',
  kind: 'business.run-kind',
  limit_amount: 'business.limit',
  model_service_id: 'business.model-service',
  name: 'business.name',
  password_reset: 'business.reset-password',
  public_registration_enabled: 'business.enable-public-registration',
  registration_organization_id: 'business.registration-organization',
  registration_role: 'business.default-registration-role',
  email_verification_required: 'business.require-email-verification',
  project_id: 'business.project',
  provider: 'business.provider',
  provider_timeout_ms: 'business.upstream-timeout-ms',
  provider_keys: 'business.proxy-provider',
  provider_key: 'business.provider',
  proxy_url_changed: 'business.proxy-url-changed',
  proxy_type: 'business.proxy-type',
  enabled: 'business.enabled',
  from_proxy: 'business.from-proxy',
  to_proxy: 'business.to-proxy',
  trigger: 'business.failover-trigger',
  items_count: 'business.proxy-pool-items-count',
  probe_interval_seconds: 'business.probe-interval-seconds',
  fast_probe_interval_seconds: 'business.fast-probe-interval-seconds',
  probe_timeout_seconds: 'business.probe-timeout-seconds',
  fail_threshold: 'business.fail-threshold',
  recover_threshold: 'business.recover-threshold',
  switch_cooldown_seconds: 'business.switch-cooldown-seconds',
  failback_mode: 'business.failback-mode',
  failback_soak_seconds: 'business.failback-soak-seconds',
  interval_seconds: 'business.check-interval-seconds',
  timeout_seconds: 'business.check-timeout-seconds',
  degraded_latency_ms: 'business.degraded-latency-ms',
  failure_threshold: 'business.failure-threshold',
  rate_limit_per_min: 'business.rate-limit-minute',
  region: 'business.region',
  run_id: 'business.run-id',
  runs: 'business.run-count',
  role: 'business.role',
  route_policy_id: 'business.route-policy',
  scope_id: 'business.budget-subject',
  scope_name: 'business.subject',
  scope_type: 'business.scope-type',
  session_id: 'business.session',
  slug: 'business.organization-slug',
  source_version: 'business.source-version',
  status: 'business.status',
  strategy: 'business.strategy',
  summary: 'business.summary',
  subject_id: 'business.access-scope',
  subject_type: 'business.scope-type',
  version: 'business.version',
  version_id: 'business.version-id',
  current_version_id: 'business.version-id',
  weights: 'business.weight'
};

/** 不同资源的 status 取值含义不同,沿用各列表页的状态文案。 */
const snapshotStatusResource: Record<string, StatusResource> = {
  model_service: 'service',
  model_candidate: 'candidate',
  route_policy: 'policy',
  api_key: 'api_key',
  provider_account: 'account'
};

/** 范围 / 授权对象类型的取值:预算的 scope_type 与授权的 subject_type 共用一套。 */
const scopeTypeLabels: Record<string, App.I18n.I18nKey> = {
  organization: 'business.organization',
  department: 'business.department',
  system_settings: 'business.system-settings',
  email_settings: 'business.email-service',
  project: 'business.project',
  application: 'business.application',
  api_key: 'business.api-key'
};

/** 快照里取值是固定枚举的字段,统一翻译成文案。 */
const snapshotEnumLabels: Record<string, Record<string, App.I18n.I18nKey>> = {
  asset_type: { agent: 'business.agent', prompt_template: 'business.prompt-template' },
  kind: {
    single: 'business.run-single',
    compare: 'business.run-compare',
    evaluation: 'business.run-evaluation'
  },
  provider: { openai_compatible: 'business.custom-openai-compatible' },
  proxy_type: { none: 'business.proxy-none', http: 'business.proxy-http', socks5: 'business.proxy-socks5' },
  role: {
    platform_admin: 'business.platform-admin',
    org_owner: 'business.organization-owner',
    org_admin: 'business.organization-admin',
    project_admin: 'business.project-admin',
    developer: 'business.developer',
    viewer: 'business.viewer'
  },
  scope_type: scopeTypeLabels,
  subject_type: scopeTypeLabels,
  strategy: {
    cost_first: 'business.cost-first',
    quality_first: 'business.quality-first',
    balanced: 'business.balanced'
  }
};

/** 把审计快照里的一个值转成可读文本;资源 ID 解析得到名称时显示名称。 */
function snapshotValueText(key: string, value: unknown, resourceType: string, names: Record<string, string>): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'boolean') return $t(value ? 'common.yesOrNo.yes' : 'common.yesOrNo.no');
  const text = String(value);
  if (key === 'status') return statusText(text, snapshotStatusResource[resourceType]);
  // 预算限额以字符串保存(如 "20.00000000"),去掉无意义的尾零。
  if (key === 'limit_amount') {
    const amount = Number(text);
    return Number.isFinite(amount) ? String(amount) : text;
  }
  if (key === 'actions' && Array.isArray(value)) {
    const separator = getLocale() === 'zh-CN' ? '、' : ', ';
    return value.map(action => (action === 'read' ? $t('business.view') : $t('business.use'))).join(separator);
  }
  const enumKey = snapshotEnumLabels[key]?.[text];
  if (enumKey) return $t(enumKey);
  if (typeof value === 'string') return names[text] ?? text;
  if (typeof value === 'number') return text;
  return JSON.stringify(value);
}

export interface AuditSnapshotRow {
  key: string;
  label: string;
  before: string;
  after: string;
  changed: boolean;
}

/** 审计详情的“变更内容”:按字段列出变更前 / 变更后,字段顺序沿用变更后快照。 */
export function auditSnapshotRows(detail: Api.Management.AuditEventDetail): AuditSnapshotRow[] {
  const before = (detail.before_state ?? {}) as Record<string, unknown>;
  const after = (detail.after_state ?? {}) as Record<string, unknown>;
  const names = detail.snapshot_names ?? {};
  // 只在变更前出现的字段排在后面;预算的“预算对象”已能显示名称时,不重复列出“对象名称”。
  const keys = [
    ...Object.keys(after),
    ...Object.keys(before).filter(key => !(key in after))
  ].filter(key => !(key === 'scope_name' && names[String(after.scope_id ?? before.scope_id ?? '')]));
  return keys.map(key => {
    const text = (value: unknown) => snapshotValueText(key, value, detail.resource_type, names);
    const fieldKey = snapshotFieldKeyMap[key];
    return {
      key,
      label: fieldKey ? $t(fieldKey) : key,
      before: text(before[key] ?? null),
      after: text(after[key] ?? null),
      changed: JSON.stringify(before[key] ?? null) !== JSON.stringify(after[key] ?? null)
    };
  });
}

export function auditResultText(result: string) {
  return result === 'success' ? $t('business.success') : $t('business.failed');
}

export function auditResultTagType(result: string): 'success' | 'error' | 'default' {
  return result === 'success' ? 'success' : 'error';
}

export function actorTypeText(actorType: string) {
  switch (actorType) {
    case 'user':
      return $t('business.user');
    case 'api_key':
      return 'API Key';
    case 'system':
      return $t('business.system');
    default:
      return actorType;
  }
}

export function formatTime(value: string | null) {
  return value ? new Date(value).toLocaleString(getLocale()) : '-';
}
