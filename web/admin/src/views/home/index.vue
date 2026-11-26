<script setup lang="ts">
import { onMounted, ref, computed } from 'vue';
import { formatCurrency } from '@/utils/currency';
import { fetchDashboard } from '@/service/api';
import { $t } from '@/locales';
import { useAuthStore } from '@/store/modules/auth';
import { useRouterPush } from '@/hooks/common/router';
import { localStg } from '@/utils/storage';
import { usePermission } from '@/hooks/common/permission';

defineOptions({
  name: 'Home'
});

const authStore = useAuthStore();
const { routerPush } = useRouterPush();
const { has } = usePermission();

const ready = ref<'loading' | 'ready' | 'unavailable'>('loading');
const loading = ref(true);
const dash = ref<Api.Management.Dashboard | null>(null);

interface QuickLink {
  title: string;
  description: string;
  route: string;
}

const quickLinks = computed<QuickLink[]>(() =>
  [
    {
      title: $t('business.provider-account'),
      description: $t('business.connect-upstream-model-providers'),
      route: '/model-management/providers',
      perm: 'provider:read'
    },
    {
      title: $t('business.model-service'),
      description: $t('business.configure-logical-models-for-business'),
      route: '/model-management/model-services',
      perm: 'model_service:read'
    },
    {
      title: 'API Key',
      description: $t('business.issue-access-credentials-for-business'),
      route: '/access/api-keys',
      perm: 'api_key:read'
    },
    {
      title: $t('business.budget'),
      description: $t('business.set-cost-control-windows'),
      route: '/observability/budgets',
      perm: 'budget:read'
    }
  ].filter(item => has(item.perm))
);

function go(route: string) {
  void routerPush({ path: route });
}

async function checkReadiness() {
  ready.value = 'loading';
  try {
    const response = await fetch('/readyz');
    ready.value = response.ok ? 'ready' : 'unavailable';
  } catch {
    ready.value = 'unavailable';
  }
}

async function loadDashboard() {
  loading.value = true;
  try {
    const { data, error } = await fetchDashboard();
    if (!error && data) {
      dash.value = data;
    }
  } finally {
    loading.value = false;
  }
}

function statCards() {
  return [
    {
      label: $t('business.enabled-model-services'),
      value: dash.value ? String(dash.value.active_services) : '-',
      route: '/model-management/model-services'
    },
    {
      label: $t('business.provider-account'),
      value: dash.value ? String(dash.value.active_providers) : '-',
      route: '/model-management/providers'
    },
    {
      label: $t('business.active-api-keys'),
      value: dash.value ? String(dash.value.active_api_keys) : '-',
      route: '/access/api-keys'
    },
    {
      label: $t('business.organization-members'),
      value: dash.value ? String(dash.value.user_count) : '-',
      route: '/access/users'
    }
  ];
}

const onboardingSteps = computed(() => [
  {
    title: $t('business.setupDepartment'),
    route: '/organization/departments',
    done: (dash.value?.department_count ?? 0) > 0
  },
  {
    title: $t('business.setupProject'),
    route: '/organization/projects',
    done: (dash.value?.project_count ?? 0) > 0
  },
  {
    title: $t('business.setupApplication'),
    route: '/organization/applications',
    done: (dash.value?.application_count ?? 0) > 0
  },
  {
    title: $t('business.setupProvider'),
    route: '/model-management/providers',
    done: (dash.value?.active_providers ?? 0) > 0
  },
  {
    title: $t('business.setupModelService'),
    route: '/model-management/model-services',
    done: (dash.value?.active_services ?? 0) > 0
  },
  {
    title: $t('business.setupApiKey'),
    route: '/access/api-keys',
    done: (dash.value?.active_api_keys ?? 0) > 0
  }
]);
const onboardingDone = computed(() => onboardingSteps.value.every(step => step.done));
const onboardingDoneCount = computed(() => onboardingSteps.value.filter(step => step.done).length);

type ChecklistPreference = { collapsed?: boolean; dismissed?: boolean };

// 折叠/关闭偏好存在本地浏览器，按用户区分，避免同一浏览器上的其他账号互相影响
const checklistPreference = ref<ChecklistPreference>(
  localStg.get('homeChecklist')?.[authStore.userInfo.userId] ?? {}
);

// 用户没有手动折叠/展开过时：清单全部完成默认折叠，未完成默认展开
const checklistCollapsed = computed(() => checklistPreference.value.collapsed ?? onboardingDone.value);

function saveChecklistPreference(patch: ChecklistPreference) {
  const next = { ...checklistPreference.value, ...patch };
  const all = localStg.get('homeChecklist') ?? {};
  all[authStore.userInfo.userId] = next;
  localStg.set('homeChecklist', all);
  checklistPreference.value = next;
}

function toggleChecklist() {
  saveChecklistPreference({ collapsed: !checklistCollapsed.value });
}

function dismissChecklist() {
  saveChecklistPreference({ dismissed: true });
}

function usageCards() {
  return [
    {
      label: $t('business.requests-in-last-7-days'),
      value: dash.value ? String(dash.value.requests_7d) : '-',
      route: '/observability/usage'
    },
    {
      label: $t('business.success-rate-in-last-7'),
      value: dash.value ? dash.value.success_rate_7d : '-',
      route: '/observability/usage'
    },
    {
      label: $t('business.cost-in-last-7-days'),
      value: dash.value?.costs.length
        ? dash.value.costs.map(item => formatCurrency(item.total, item.currency)).join(' · ')
        : '-',
      route: '/observability/usage'
    },
    {
      label: $t('business.active-budgets'),
      value: dash.value
        ? dash.value.exceeded_budgets > 0
          ? $t('business.activeBudgetsWithExceeded', {
              active: dash.value.active_budgets,
              exceeded: dash.value.exceeded_budgets
            })
          : String(dash.value.active_budgets)
        : '-',
      route: '/observability/budgets'
    }
  ];
}

onMounted(() => {
  void checkReadiness();
  void loadDashboard();
});
</script>

<template>
  <NSpace vertical :size="16">
    <NCard :bordered="false" class="card-wrapper">
      <NGrid responsive="screen" item-responsive :x-gap="16" :y-gap="16">
        <NGi span="24 m:16">
          <div class="flex-y-center gap-16px">
            <SystemLogo class="size-48px" />
            <div>
              <h2 class="m-0 text-22px font-semibold">
                {{ $t('business.omniflux-console') }}
              </h2>
              <p class="m-0 text-14px text-#6b7280">
                {{ $t('business.enterprise-ai-control-plane-and') }}
              </p>
            </div>
          </div>
        </NGi>
        <NGi span="24 m:8">
          <div class="flex items-center justify-end gap-12px">
            <NTag :type="ready === 'ready' ? 'success' : ready === 'unavailable' ? 'error' : 'info'" round>
              {{
                ready === 'ready'
                  ? $t('business.serviceReady')
                  : ready === 'unavailable'
                    ? $t('business.serviceUnavailable')
                    : $t('business.checking')
              }}
            </NTag>
            <NButton quaternary type="primary" @click="checkReadiness">{{ $t('business.refresh-status') }}</NButton>
          </div>
        </NGi>
      </NGrid>
    </NCard>

    <!-- 等首次加载完 dashboard 再渲染，避免已完成的清单先展开再折叠 -->
    <NCard
      v-if="dash && !checklistPreference.dismissed"
      :title="$t('business.initializationChecklist')"
      :bordered="false"
      class="card-wrapper"
    >
      <template #header-extra>
        <NSpace align="center" :size="8">
          <NTag :type="onboardingDone ? 'success' : 'info'" round>
            {{ onboardingDone ? $t('business.completed') : $t('business.inProgress') }}
            {{ onboardingDoneCount }}/{{ onboardingSteps.length }}
          </NTag>
          <NButton quaternary size="small" @click="toggleChecklist">
            <template #icon>
              <SvgIcon :icon="checklistCollapsed ? 'mdi:chevron-down' : 'mdi:chevron-up'" />
            </template>
            {{ checklistCollapsed ? $t('business.expandChecklist') : $t('business.collapseChecklist') }}
          </NButton>
          <NPopconfirm @positive-click="dismissChecklist">
            <template #trigger>
              <NButton quaternary size="small">
                <template #icon>
                  <SvgIcon icon="mdi:close" />
                </template>
                {{ $t('business.dismissChecklist') }}
              </NButton>
            </template>
            {{ $t('business.dismissChecklistConfirm') }}
          </NPopconfirm>
        </NSpace>
      </template>
      <NSpace v-if="!checklistCollapsed" vertical :size="10">
        <div v-for="step in onboardingSteps" :key="step.title" class="flex items-center justify-between gap-12px">
          <NSpace align="center" :size="8">
            <SvgIcon
              :icon="step.done ? 'mdi:check-circle' : 'mdi:circle-outline'"
              :class="step.done ? 'text-#18a058' : 'text-#9ca3af'"
            />
            <span>{{ step.title }}</span>
          </NSpace>
          <NButton text type="primary" @click="go(step.route)">
            {{ step.done ? $t('business.view') : $t('business.goSetup') }}
          </NButton>
        </div>
      </NSpace>
    </NCard>

    <NCard :title="$t('business.resource-overview')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NButton quaternary type="primary" :loading="loading" @click="loadDashboard">
          {{ $t('business.refresh') }}
        </NButton>
      </template>
      <NGrid responsive="screen" item-responsive :x-gap="16" :y-gap="16">
        <NGi v-for="card in statCards()" :key="card.label" span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="cursor-pointer card-wrapper" @click="go(card.route)">
            <div class="text-13px text-#6b7280">{{ card.label }}</div>
            <div class="mt-8px text-28px font-semibold">{{ card.value }}</div>
          </NCard>
        </NGi>
      </NGrid>
    </NCard>

    <NCard :title="$t('business.operations-in-last-7-days')" :bordered="false" class="card-wrapper">
      <NGrid responsive="screen" item-responsive :x-gap="16" :y-gap="16">
        <NGi v-for="card in usageCards()" :key="card.label" span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="cursor-pointer card-wrapper" @click="go(card.route)">
            <div class="text-13px text-#6b7280">{{ card.label }}</div>
            <div class="mt-8px text-28px font-semibold">{{ card.value }}</div>
          </NCard>
        </NGi>
      </NGrid>
    </NCard>

    <NCard :title="$t('business.quick-links')" :bordered="false" class="card-wrapper">
      <NGrid responsive="screen" item-responsive :x-gap="16" :y-gap="16">
        <NGi v-for="link in quickLinks" :key="link.title" span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="cursor-pointer card-wrapper" @click="go(link.route)">
            <div class="text-15px font-medium">{{ link.title }}</div>
            <p class="mt-6px mb-0 text-13px text-#6b7280">
              {{ link.description }}
            </p>
          </NCard>
        </NGi>
      </NGrid>
    </NCard>

    <NCard :title="$t('business.current-session')" :bordered="false" class="card-wrapper">
      <NDescriptions :column="1" label-placement="left" bordered>
        <NDescriptionsItem :label="$t('business.login-email')">{{ authStore.userInfo.userName }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.roles')">
          {{ authStore.userInfo.permissionGroupName || authStore.userInfo.role || authStore.userInfo.roles.join(' / ') }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.session-mode')">
          {{ $t('business.server-side-cookie-session') }}
        </NDescriptionsItem>
      </NDescriptions>
    </NCard>
  </NSpace>
</template>

<style scoped></style>
