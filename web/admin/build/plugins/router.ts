import type { RouteMeta } from 'vue-router';
import type { RouteKey } from '@elegant-router/types';
import ElegantVueRouter from '@elegant-router/vue/vite';

export function setupElegantRouter() {
  return ElegantVueRouter({
    layouts: {
      base: 'src/layouts/base-layout/index.vue',
      blank: 'src/layouts/blank-layout/index.vue'
    },
    onRouteMetaGen(routeName) {
      const key = routeName as RouteKey;

      const constantRoutes: RouteKey[] = ['login', 'register', 'forgot-password', 'reset-password', 'verify-email', 'accept-invitation', '403', '404', '500'];

      const meta: Partial<RouteMeta> = {
        title: key,
        i18nKey: `route.${key}` as App.I18n.I18nKey
      };

      if (key === 'pending-access') {
        meta.hideInMenu = true;
      }

      const menuOrder: Partial<Record<RouteKey, number>> = {
        home: 1,
        organization: 2,
        organization_organizations: 1,
        organization_departments: 2,
        organization_projects: 3,
        organization_applications: 4,
        'model-management': 3,
        'model-management_providers': 1,
        'model-management_model-services': 2,
        'model-management_route-policies': 3,
        access: 4,
        access_users: 1,
        'access_permission-groups': 2,
        'access_api-keys': 3,
        'asset-registry': 5,
        'asset-registry_prompt-templates': 1,
        'asset-registry_agents': 2,
        'asset-registry_evaluations': 3,
        observability: 6,
        observability_usage: 1,
        observability_budgets: 2,
        observability_audit: 3,
        system: 7,
        system_settings: 1,
        system_proxies: 2
      };

      const menuIcons: Partial<Record<RouteKey, string>> = {
        home: 'mdi:monitor-dashboard',
        organization: 'mdi:sitemap-outline',
        organization_organizations: 'mdi:office-building-outline',
        organization_departments: 'mdi:division',
        organization_projects: 'mdi:folder-multiple-outline',
        organization_applications: 'mdi:application-brackets-outline',
        'model-management': 'mdi:cube-outline',
        'model-management_providers': 'mdi:cloud-lock-outline',
        'model-management_model-services': 'mdi:cube-send',
        'model-management_route-policies': 'mdi:routes',
        access: 'mdi:shield-account-outline',
        access_users: 'mdi:account-group-outline',
        'access_permission-groups': 'mdi:shield-key-outline',
        'access_api-keys': 'mdi:key-chain-variant',
        'asset-registry': 'mdi:archive-outline',
        'asset-registry_prompt-templates': 'mdi:file-document-edit-outline',
        'asset-registry_agents': 'mdi:robot-outline',
        'asset-registry_evaluations': 'mdi:clipboard-check-outline',
        observability: 'mdi:chart-box-outline',
        observability_usage: 'mdi:chart-bar',
        observability_budgets: 'mdi:wallet-outline',
        observability_audit: 'mdi:text-box-search-outline',
        system: 'mdi:cog-outline',
        system_settings: 'mdi:application-cog-outline',
        system_proxies: 'mdi:proxy'
      };

      const routeRoles: Partial<Record<RouteKey, string[]>> = {
        system: ['platform_admin'],
        system_settings: ['platform_admin'],
        system_proxies: ['platform_admin']
      };

      if (menuOrder[key] !== undefined) {
        meta.order = menuOrder[key];
      }

      if (menuIcons[key]) {
        meta.icon = menuIcons[key];
      }

      if (routeRoles[key]) {
        meta.roles = routeRoles[key];
      }

      if (constantRoutes.includes(key)) {
        meta.constant = true;
      }

      return meta;
    }
  });
}
