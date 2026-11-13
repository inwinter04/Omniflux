<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useAuthStore } from '@/store/modules/auth';

defineOptions({
  name: 'Home'
});

const authStore = useAuthStore();
const ready = ref<'loading' | 'ready' | 'unavailable'>('loading');

interface Capability {
  title: string;
  description: string;
  state: string;
}

const capabilities = ref<Capability[]>([
  {
    title: '统一模型网关',
    description: 'OpenAI-compatible API、流式响应、多 Provider 接入',
    state: '实现中'
  },
  {
    title: '路由与策略决策',
    description: '硬约束过滤、成本 / 质量 / 延迟评分、fallback 审计',
    state: '实现中'
  },
  {
    title: '预算与计量',
    description: '预留结算模型、请求归因、成本报表',
    state: '实现中'
  },
  {
    title: '审计与可观测',
    description: '管理变更审计、PolicyDecision、metrics / trace',
    state: '实现中'
  }
]);

async function checkReadiness() {
  ready.value = 'loading';

  try {
    const response = await fetch('/readyz');
    ready.value = response.ok ? 'ready' : 'unavailable';
  } catch {
    ready.value = 'unavailable';
  }
}

onMounted(() => {
  void checkReadiness();
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

    <NGrid responsive="screen" item-responsive :x-gap="16" :y-gap="16">
      <NGi v-for="item in capabilities" :key="item.title" span="24 s:12 m:6">
        <NCard :bordered="false" class="h-full card-wrapper">
          <div class="min-h-120px flex flex-col justify-between">
            <div>
              <h3 class="m-0 text-16px font-semibold">{{ item.title }}</h3>
              <p class="mt-8px mb-0 text-13px text-#6b7280">{{ item.description }}</p>
            </div>
            <NTag size="small" round type="info">{{ item.state }}</NTag>
          </div>
        </NCard>
      </NGi>
    </NGrid>

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