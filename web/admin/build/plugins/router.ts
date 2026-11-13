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
        projects: 2,
        applications: 3,
        providers: 4,
        'route-policies': 5,
        'model-services': 6,
        'api-keys': 7
      };

      const menuIcons: Partial<Record<RouteKey, string>> = {
        home: 'mdi:monitor-dashboard',
        projects: 'mdi:folder-multiple-outline',
        applications: 'mdi:application-brackets-outline',
        providers: 'mdi:cloud-lock-outline',
        'route-policies': 'mdi:routes',
        'model-services': 'mdi:cube-outline',
        'api-keys': 'mdi:key-chain-variant'
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

