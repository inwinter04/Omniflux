import type { RouteMeta } from 'vue-router';
import type { RouteKey } from '@elegant-router/types';
import ElegantVueRouter from '@elegant-router/vue/vite';

export function setupElegantRouter() {
  return ElegantVueRouter({
    layouts: {
      base: 'src/layouts/base-layout/index.vue',
      blank: 'src/layouts/blank-layout/index.vue'
    },
    routePathTransformer(routeName, routePath) {
      const key = routeName as RouteKey;

      if (key === 'login') {
        const modules: UnionKey.LoginModule[] = ['pwd-login', 'code-login', 'register', 'reset-pwd', 'bind-wechat'];

        const moduleReg = modules.join('|');

        return `/login/:module(${moduleReg})?`;
      }

      return routePath;
    },
    onRouteMetaGen(routeName) {
      const key = routeName as RouteKey;

      const constantRoutes: RouteKey[] = ['login', '403', '404', '500'];

      const meta: Partial<RouteMeta> = {
        title: key,
        i18nKey: `route.${key}` as App.I18n.I18nKey
      };

      const menuOrder: Partial<Record<RouteKey, number>> = {
        home: 1,
        organization: 2,
        organization_projects: 1,
        organization_applications: 2,
        'organization_api-keys': 3,
        organization_budgets: 4,
        'model-management': 3,
        'model-management_providers': 1,
        'model-management_route-policies': 2,
        'model-management_model-services': 3,
        observability: 4,
        observability_usage: 1,
        observability_audit: 2
      };

      const menuIcons: Partial<Record<RouteKey, string>> = {
        home: 'mdi:monitor-dashboard',
        organization: 'mdi:sitemap-outline',
        organization_projects: 'mdi:folder-multiple-outline',
        organization_applications: 'mdi:application-brackets-outline',
        'organization_api-keys': 'mdi:key-chain-variant',
        organization_budgets: 'mdi:wallet-outline',
        'model-management': 'mdi:cube-outline',
        'model-management_providers': 'mdi:cloud-lock-outline',
        'model-management_route-policies': 'mdi:routes',
        'model-management_model-services': 'mdi:cube-send',
        observability: 'mdi:chart-box-outline',
        observability_usage: 'mdi:chart-bar',
        observability_audit: 'mdi:text-box-search-outline'
      };

      if (menuOrder[key] !== undefined) {
        meta.order = menuOrder[key];
      }

      if (menuIcons[key]) {
        meta.icon = menuIcons[key];
      }

      if (constantRoutes.includes(key)) {
        meta.constant = true;
      }

      return meta;
    }
  });
}



