<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useMessage } from 'naive-ui';
import { $t } from '@/locales';
import { fetchGetUserInfo } from '@/service/api';
import { useAuthStore } from '@/store/modules/auth';
import { useRouteStore } from '@/store/modules/route';
import { hasAnyBusinessPermission } from '@/router/routes/permissions';

defineOptions({
  name: 'PendingAccess'
});

const router = useRouter();
const authStore = useAuthStore();
const routeStore = useRouteStore();
const message = useMessage();
const refreshing = ref(false);

const identityLabel = computed(() => {
  if (authStore.userInfo.permissionGroupName) {
    return authStore.userInfo.permissionGroupName;
  }
  if (authStore.userInfo.role === 'pending') {
    return $t('business.pending-role');
  }
  return authStore.userInfo.role || $t('business.pending-role');
});

async function handleRefresh() {
  refreshing.value = true;
  try {
    const { data, error } = await fetchGetUserInfo();
    if (error || !data) {
      return;
    }
    Object.assign(authStore.userInfo, data);
    if (!hasAnyBusinessPermission(authStore.userInfo)) {
      message.info($t('business.still-pending-access'));
      return;
    }
    routeStore.setIsInitAuthRoute(false);
    await routeStore.initAuthRoute();
    message.success($t('business.permissions-activated'));
    await router.replace({ name: routeStore.routeHome });
  } finally {
    refreshing.value = false;
  }
}

async function handleLogout() {
  await authStore.resetStore();
}
</script>

<template>
  <div class="flex-center min-h-screen bg-#f6f7f9 p-24px dark:bg-#101014">
    <NCard :bordered="false" class="w-full max-w-520px rd-12px shadow-sm">
      <div class="flex flex-col items-center text-center">
        <div class="mb-16px size-56px flex-center rounded-full bg-#fef3c7 text-28px text-#d97706 dark:bg-#451a03">
          <SvgIcon icon="mdi:shield-lock-outline" />
        </div>
        <h2 class="m-0 text-20px font-semibold">{{ $t('business.pending-access-title') }}</h2>
        <p class="mt-10px mb-20px text-14px leading-relaxed text-#6b7280 dark:text-#9ca3af">
          {{ $t('business.pending-access-desc') }}
        </p>
        <NDescriptions :column="1" bordered size="small" label-placement="left" class="w-full text-left">
          <NDescriptionsItem :label="$t('business.current-account')">
            {{ authStore.userInfo.userName }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.current-identity')">
            <NTag type="warning" size="small" round>{{ identityLabel }}</NTag>
          </NDescriptionsItem>
        </NDescriptions>
        <NSpace class="mt-24px w-full" justify="center" :size="12">
          <NButton type="primary" :loading="refreshing" @click="handleRefresh">
            {{ $t('business.refresh-permissions') }}
          </NButton>
          <NButton quaternary @click="handleLogout">
            {{ $t('common.logout') }}
          </NButton>
        </NSpace>
      </div>
    </NCard>
  </div>
</template>
