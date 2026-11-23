<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRouterPush } from '@/hooks/common/router';
import { fetchDashboard } from '@/service/api';
import { useAuthStore } from '@/store/modules/auth';
import { formatCurrency } from '@/utils/currency';

defineOptions({
  name: 'Home'
});

const authStore = useAuthStore();
const { routerPush } = useRouterPush();

const ready = ref<'loading' | 'ready' | 'unavailable'>('loading');
const loading = ref(true);
const dash = ref<Api.Management.Dashboard | null>(null);

interface QuickLink {
  title: string;
  description: string;
  route: string;
}

const quickLinks = ref<QuickLink[]>([
  { title: '供应商账号', description: '接入上游模型供应商', route: '/model-management/providers' },
  { title: '模型服务', description: '配置业务调用的逻辑模型', route: '/model-management/model-services' },
  { title: 'API Key', description: '为业务签发访问凭证', route: '/access/api-keys' },
  { title: '预算', description: '设置成本控制窗口', route: '/observability/budgets' }
]);

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
    { label: '启用模型服务', value: dash.value ? String(dash.value.active_services) : '-', route: '/model-management/model-services' },
    { label: '供应商账号', value: dash.value ? String(dash.value.active_providers) : '-', route: '/model-management/providers' },
    { label: '活跃 API Key', value: dash.value ? String(dash.value.active_api_keys) : '-', route: '/access/api-keys' },
    { label: '组织成员', value: dash.value ? String(dash.value.user_count) : '-', route: '/access/users' }
  ];
}

function usageCards() {
  return [
    { label: '近 7 天请求', value: dash.value ? String(dash.value.requests_7d) : '-', route: '/observability/usage' },
    { label: '近 7 天成功率', value: dash.value ? dash.value.success_rate_7d : '-', route: '/observability/usage' },
    {
      label: '近 7 天成本',
      value: dash.value?.costs.length
        ? dash.value.costs.map(item => formatCurrency(item.total, item.currency)).join(' · ')
        : '-',
      route: '/observability/usage'
    },
    {
      label: '进行中预算',
      value: dash.value
        ? dash.value.exceeded_budgets > 0
          ? `${dash.value.active_budgets} (超限 ${dash.value.exceeded_budgets})`
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
              <h2 class="m-0 text-22px font-semibold">Omniflux 控制台</h2>
              <p class="m-0 text-14px text-#6b7280">企业 AI 控制平面与共享资产平台</p>
            </div>
          </div>
        </NGi>
        <NGi span="24 m:8">
          <div class="flex items-center justify-end gap-12px">
            <NTag :type="ready === 'ready' ? 'success' : ready === 'unavailable' ? 'error' : 'info'" round>
              {{ ready === 'ready' ? '服务就绪' : ready === 'unavailable' ? '服务异常' : '检查中' }}
            </NTag>
            <NButton quaternary type="primary" @click="checkReadiness">刷新状态</NButton>
          </div>
        </NGi>
      </NGrid>
    </NCard>

    <NCard title="资源概览" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NButton quaternary type="primary" :loading="loading" @click="loadDashboard">刷新</NButton>
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

    <NCard title="近 7 天运营" :bordered="false" class="card-wrapper">
      <NGrid responsive="screen" item-responsive :x-gap="16" :y-gap="16">
        <NGi v-for="card in usageCards()" :key="card.label" span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="cursor-pointer card-wrapper" @click="go(card.route)">
            <div class="text-13px text-#6b7280">{{ card.label }}</div>
            <div class="mt-8px text-28px font-semibold">{{ card.value }}</div>
          </NCard>
        </NGi>
      </NGrid>
    </NCard>

    <NCard title="快速入口" :bordered="false" class="card-wrapper">
      <NGrid responsive="screen" item-responsive :x-gap="16" :y-gap="16">
        <NGi v-for="link in quickLinks" :key="link.title" span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="cursor-pointer card-wrapper" @click="go(link.route)">
            <div class="text-15px font-medium">{{ link.title }}</div>
            <p class="mt-6px mb-0 text-13px text-#6b7280">{{ link.description }}</p>
          </NCard>
        </NGi>
      </NGrid>
    </NCard>

    <NCard title="当前会话" :bordered="false" class="card-wrapper">
      <NDescriptions :column="1" label-placement="left" bordered>
        <NDescriptionsItem label="登录邮箱">{{ authStore.userInfo.userName }}</NDescriptionsItem>
        <NDescriptionsItem label="权限角色">{{ authStore.userInfo.roles.join(' / ') }}</NDescriptionsItem>
        <NDescriptionsItem label="会话模式">服务端 Cookie Session</NDescriptionsItem>
      </NDescriptions>
    </NCard>
  </NSpace>
</template>

<style scoped></style>
