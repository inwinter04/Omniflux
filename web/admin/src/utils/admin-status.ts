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
    unhealthy: $t('business.unhealthy'),
    no_data: $t('business.not-verified'),
    no_candidates: $t('business.no-candidates-configured'),
    disabled: $t('business.disabled-e5ola')
  };
  return labels[status] ?? status;
}

export function healthTagType(status: string): 'success' | 'warning' | 'error' | 'info' | 'default' {
  if (status === 'healthy') return 'success';
  if (status === 'degraded' || status === 'no_data') return 'warning';
  if (status === 'unhealthy') return 'error';
  return 'default';
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
  'application.created': 'business.create-application-ar6opf',
  'application.updated': 'business.update-application',
  'application.deleted': 'business.delete-application',
  'provider_account.created': 'business.create-provider-account-1fmjpgf',
  'provider_account.updated': 'business.update-provider-account',
  'provider_account.deleted': 'business.delete-provider-account',
  'route_policy.created': 'business.create-route-policy-167s75c',
  'route_policy.updated': 'business.update-route-policy',
  'route_policy.deleted': 'business.delete-route-policy',
  'model_service.created': 'business.create-model-service-11kri7x',
  'model_service.updated': 'business.update-model-service',
  'model_service.deleted': 'business.delete-model-service',
  'api_key.created': 'business.create-api-key',
  'api_key.status_changed': 'business.change-api-key-status',
  'api_key.deleted': 'business.delete-api-key',
  'asset.created': 'business.create-asset',
  'asset.draft_created': 'business.save-asset-draft',
  'asset.published': 'business.publish-asset',
  'asset.rolled_back': 'business.roll-back-asset',
  'asset.status_changed': 'business.change-asset-status',
  'asset_grant.upserted': 'business.save-asset-grant',
  'asset_grant.revoked': 'business.revoke-asset-grant',
  'prompt_template.previewed': 'business.preview-prompttemplate',
  'prompt_template.invoked': 'business.invoke-prompttemplate'
};

export function auditActionText(action: string) {
  const key = actionTextKeyMap[action];
  return key ? $t(key) : action;
}

const resourceTextKeyMap: Record<string, App.I18n.I18nKey> = {
  project: 'business.project',
  application: 'business.application',
  provider_account: 'business.provider-account',
  route_policy: 'business.route-policy',
  model_service: 'business.model-service',
  api_key: 'business.api-key',
  user: 'business.user',
  session: 'business.session',
  asset: 'business.enterprise-asset',
  asset_grant: 'business.asset-grant'
};

export function auditResourceText(resourceType: string) {
  const key = resourceTextKeyMap[resourceType];
  return key ? $t(key) : resourceType;
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
