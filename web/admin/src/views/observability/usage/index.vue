<script setup lang="ts">
import { h, onMounted, ref } from 'vue';
import { NButton, NPagination, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { fetchUsageRecords, fetchUsageSummary } from '@/service/api';
import { formatTime, usageStateTagType, usageStateText } from '@/utils/admin-status';

defineOptions({
  name: 'ObservabilityUsage'
});

const summaryLoading = ref(true);
const recordsLoading = ref(true);
const days = ref(7);
const summary = ref<Api.Management.UsageSummary | null>(null);
const records = ref<Api.Management.UsageRecord[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = 20;

function formatNumber(value: number) {
  return value.toLocaleString();
}

function formatCost(value: string) {
  return value;
}

const modelColumns: DataTableColumns<Api.Management.UsageModelSummary> = [
  { title: 'Provider', key: 'provider', width: 140 },
  { title: '上游模型', key: 'upstream_model', minWidth: 160 },
  { title: '请求数', key: 'requests', width: 100, render: row => formatNumber(row.requests) },
  { title: '输入 Token', key: 'input_tokens', width: 130, render: row => formatNumber(row.input_tokens) },
  { title: '输出 Token', key: 'output_tokens', width: 130, render: row => formatNumber(row.output_tokens) },
  { title: '成本', key: 'cost', width: 120, render: row => formatCost(row.cost) }
];

const recordColumns: DataTableColumns<Api.Management.UsageRecord> = [
  { title: '时间', key: 'created_at', width: 180, render: row => formatTime(row.created_at) },
  { title: '项目', key: 'project_name', width: 120 },
  { title: '应用', key: 'application_name', width: 120 },
  { title: 'API Key', key: 'key_name', width: 140 },
  { title: '模型服务', key: 'model_service', width: 140 },
  { title: '上游模型', key: 'upstream_model', width: 140, render: row => row.upstream_model ?? '-' },
  {
    title: '状态',
    key: 'state',
    width: 90,
    render(row) {
      return h(NTag, { type: usageStateTagType(row.state), size: 'small', round: true }, { default: () => usageStateText(row.state) });
    }
  },
  { title: '输入', key: 'input_tokens', width: 100, render: row => formatNumber(row.input_tokens) },
  { title: '输出', key: 'output_tokens', width: 100, render: row => formatNumber(row.output_tokens) },
  { title: '成本', key: 'cost', width: 110, render: row => formatCost(row.cost) },
  { title: '耗时(ms)', key: 'total_ms', width: 100, render: row => (row.total_ms != null ? String(row.total_ms) : '-') }
];

const successRate = ref('-');

async function loadSummary() {
  summaryLoading.value = true;
  try {
    const { data, error } = await fetchUsageSummary(days.value);
    if (!error && data) {
      summary.value = data;
      successRate.value = data.total_requests > 0
        ? `${Math.round((data.success_requests / data.total_requests) * 100)}%`
        : '-';
    }
  } finally {
    summaryLoading.value = false;
  }
}

async function loadRecords() {
  recordsLoading.value = true;
  try {
    const { data, error } = await fetchUsageRecords(page.value, pageSize);
    if (!error && data) {
      records.value = data.records;
      total.value = data.total;
    }
  } finally {
    recordsLoading.value = false;
  }
}

function handlePageChange(p: number) {
  page.value = p;
  void loadRecords();
}

function refreshAll() {
  void loadSummary();
  page.value = 1;
  void loadRecords();
}

onMounted(() => {
  refreshAll();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :bordered="false" class="card-wrapper">
      <template #header>用量报表</template>
      <template #header-extra>
        <NSpace>
          <NSelect v-model:value="days" size="small" class="w-120px" :options="[
            { label: '近 7 天', value: 7 },
            { label: '近 14 天', value: 14 },
            { label: '近 30 天', value: 30 },
            { label: '近 90 天', value: 90 }
          ]" @update:value="refreshAll" />
          <NButton quaternary type="primary" :loading="summaryLoading || recordsLoading" @click="refreshAll">刷新</NButton>
        </NSpace>
      </template>

      <NGrid responsive="screen" item-responsive :x-gap="16" :y-gap="16">
        <NGi span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="card-wrapper">
            <div class="text-13px text-#6b7280">总请求数</div>
            <div class="mt-8px text-24px font-semibold">{{ summary ? formatNumber(summary.total_requests) : '-' }}</div>
          </NCard>
        </NGi>
        <NGi span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="card-wrapper">
            <div class="text-13px text-#6b7280">成功率</div>
            <div class="mt-8px text-24px font-semibold">{{ successRate }}</div>
          </NCard>
        </NGi>
        <NGi span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="card-wrapper">
            <div class="text-13px text-#6b7280">总 Token</div>
            <div class="mt-8px text-24px font-semibold">
              {{ summary ? formatNumber(summary.total_input_tokens + summary.total_output_tokens) : '-' }}
            </div>
          </NCard>
        </NGi>
        <NGi span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="card-wrapper">
            <div class="text-13px text-#6b7280">总成本 ({{ summary?.cost_currency ?? 'USD' }})</div>
            <div class="mt-8px text-24px font-semibold">{{ summary ? formatCost(summary.total_cost) : '-' }}</div>
          </NCard>
        </NGi>
      </NGrid>
    </NCard>

    <NCard title="按模型分布" :bordered="false" class="mt-16px card-wrapper">
      <NDataTable
        :columns="modelColumns"
        :data="summary?.by_model ?? []"
        :loading="summaryLoading"
        :bordered="false"
        size="small"
      />
    </NCard>

    <NCard title="调用明细" :bordered="false" class="mt-16px card-wrapper">
      <NDataTable :columns="recordColumns" :data="records" :loading="recordsLoading" :bordered="false" size="small" :scroll-x="1400" />
      <div class="mt-12px flex justify-end">
        <NPagination :page="page" :page-size="pageSize" :item-count="total" @update:page="handlePageChange" />
      </div>
    </NCard>
  </div>
</template>

<style scoped></style>
