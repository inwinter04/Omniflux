import type { RouteKey } from '@elegant-router/types';

/** Route-to-permission mapping for static RBAC filtering and route guards. */
export const ROUTE_PERMISSION_MAP: Partial<Record<RouteKey, string>> = {
  home: 'dashboard:read',
  organization_organizations: 'org:read',
  organization_departments: 'org:read',
  organization_projects: 'org:read',
  organization_applications: 'org:read',
  access_users: 'user:read',
  'access_permission-groups': 'permission_group:read',
  'access_api-keys': 'api_key:read',
  'model-management_providers': 'provider:read',
  'model-management_route-policies': 'route_policy:read',
  'model-management_model-services': 'model_service:read',
  'asset-registry_prompt-templates': 'prompt:read',
  'asset-registry_evaluations': 'evaluation:read',
  'asset-registry_agents': 'agent:read',
  observability_budgets: 'budget:read',
  observability_usage: 'usage:read',
  observability_audit: 'audit:read'
};

export function isPlatformAdminUser(userInfo: Pick<Api.Auth.UserInfo, 'role' | 'roles'>): boolean {
  return userInfo.role === 'platform_admin' || userInfo.roles.includes('platform_admin');
}

export function isOrgOwnerUser(userInfo: Pick<Api.Auth.UserInfo, 'role' | 'roles'>): boolean {
  return userInfo.role === 'org_owner' || userInfo.roles.includes('org_owner');
}

export function hasUserPermission(userInfo: Api.Auth.UserInfo, permission: string): boolean {
  if (isPlatformAdminUser(userInfo) || isOrgOwnerUser(userInfo)) {
    return true;
  }
  return Boolean(userInfo.permissions?.includes(permission));
}

export function hasAnyBusinessPermission(userInfo: Api.Auth.UserInfo): boolean {
  if (isPlatformAdminUser(userInfo) || isOrgOwnerUser(userInfo)) {
    return true;
  }
  return (userInfo.permissions?.length ?? 0) > 0;
}

export function canAccessRoute(routeName: string, routeRoles: string[], userInfo: Api.Auth.UserInfo): boolean {
  if (routeName === 'pending-access') {
    return true;
  }
  if (isPlatformAdminUser(userInfo)) {
    return true;
  }
  if (routeRoles.length > 0 && !routeRoles.some(role => userInfo.roles.includes(role))) {
    return false;
  }
  const requiredPerm = ROUTE_PERMISSION_MAP[routeName as RouteKey];
  if (requiredPerm) {
    return hasUserPermission(userInfo, requiredPerm);
  }
  return true;
}
