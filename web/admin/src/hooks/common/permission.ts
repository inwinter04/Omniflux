import { computed } from 'vue';
import { useAuthStore } from '@/store/modules/auth';
import { hasUserPermission, isOrgOwnerUser, isPlatformAdminUser } from '@/router/routes/permissions';

/** 管理台细粒度权限钩子：后端仍做最终校验，前端据此控制菜单、按钮与辅助请求。 */
export function usePermission() {
  const authStore = useAuthStore();
  const permissions = computed(() => authStore.userInfo.permissions ?? []);
  const isPlatformAdmin = computed(() => isPlatformAdminUser(authStore.userInfo));
  const isOrgOwner = computed(() => isOrgOwnerUser(authStore.userInfo));

  function has(permission: string): boolean {
    return hasUserPermission(authStore.userInfo, permission);
  }

  function hasAny(...codes: string[]): boolean {
    return codes.some(code => has(code));
  }

  return { permissions, has, hasAny, isPlatformAdmin, isOrgOwner };
}
