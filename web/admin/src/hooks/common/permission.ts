import { computed } from 'vue';
import { useAuthStore } from '@/store/modules/auth';

/** 管理台角色权限：后端仍做最终校验，这里只负责隐藏无权限入口。 */
export function usePermission() {
  const authStore = useAuthStore();
  const roles = computed(() => authStore.userInfo.roles);
  const canManage = computed(() => roles.value.includes('R_SUPER') || roles.value.includes('project_admin'));
  const canManageUsers = computed(() => roles.value.includes('org_owner') || roles.value.includes('platform_admin'));
  const isPlatformAdmin = computed(() => roles.value.includes('platform_admin'));

  return { canManage, canManageUsers, isPlatformAdmin };
}
