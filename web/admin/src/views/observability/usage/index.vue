<script setup lang="ts">
import { h, onMounted, ref, computed, watch } from 'vue';
import { NButton, NPagination, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { formatTime, usageStateTagType, usageStateText } from '@/utils/admin-status';
import { formatCurrency } from '@/utils/currency';
import { fetchUsageRecords, fetchUsageSummary } from '@/service/api';
import { $t } from '@/locales';
import { useAppStore } from '@/store/modules/app';
import { useEcharts } from '@/hooks/common/echarts';

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

function formatCost(value: string, currency = 'USD') {
  return formatCurrency(value, currency);
}

function sourceText(source: string) {
  return (
    {
      api: $t('business.business-calls'),
      prompt_debug: $t('business.prompt-debugging'),
      agent_debug: $t('business.agent-debugging')
    }[source] ?? source
  );
}

const modelColumns = computed<DataTableColumns<Api.Management.UsageModelSummary>>(() => [
  {
    title: $t('business.provider-c4b9p'),
    key: 'provider',
    width: 140,
    render: row => row.provider || '-'
  },
  {
    title: $t('business.upstream-model'),
    key: 'upstream_model',
    minWidth: 160,
    render: row => row.upstream_model || '-'
  },
  {
    title: $t('business.requests'),
    key: 'requests',
    width: 100,
    render: row => formatNumber(row.requests)
  },
  {
    title: $t('business.input-tokens'),
    key: 'input_tokens',
    width: 130,
    render: row => formatNumber(row.input_tokens)
  },
  {
    title: $t('business.output-tokens'),
    key: 'output_tokens',
    width: 130,
    render: row => formatNumber(row.output_tokens)
  },
  {
    title: $t('business.cost'),
    key: 'cost',
    width: 140,
    render: row => formatCost(row.cost, row.cost_currency)
  }
]);

const recordColumns = computed<DataTableColumns<Api.Management.UsageRecord>>(() => [
  {
    title: $t('business.time'),
    key: 'created_at',
    width: 180,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.project'),
    key: 'project_name',
    width: 120,
    render: row => row.project_name ?? $t('business.admin-console')
  },
  {
    title: $t('business.application'),
    key: 'application_name',
    width: 120,
    render: row => row.application_name ?? sourceText(row.source)
  },
  {
    title: 'API Key',
    key: 'key_name',
    width: 140,
    render: row => row.key_name ?? sourceText(row.source)
  },
  { title: $t('business.model-service'), key: 'model_service', width: 140 },
  {
    title: $t('business.upstream-model'),
    key: 'upstream_model',
    width: 140,
    render: row => row.upstream_model ?? '-'
  },
  {
    title: $t('business.status'),
    key: 'state',
    width: 90,
    render(row) {
      return h(
        NTag,
        { type: usageStateTagType(row.state), size: 'small', round: true },
        { default: () => usageStateText(row.state) }
      );
    }
  },
  {
    title: $t('business.input'),
    key: 'input_tokens',
    width: 100,
    render: row => formatNumber(row.input_tokens)
  },
  {
    title: $t('business.output'),
    key: 'output_tokens',
    width: 100,
    render: row => formatNumber(row.output_tokens)
  },
  {
    title: $t('business.cost'),
    key: 'cost',
    width: 140,
    render: row => formatCost(row.cost, row.cost_currency)
  },
  {
    title: $t('business.source-i6yz'),
    key: 'source',
    width: 120,
    render: row => sourceText(row.source)
  },
  {
    title: $t('business.latency-ms'),
    key: 'total_ms',
    width: 100,
    render: row => (row.total_ms != null ? String(row.total_ms) : '-')
  }
]);

const successRate = ref('-');

const appStore = useAppStore();
const { domRef: trendRef, updateOptions: updateTrend } = useEcharts(() => ({
  tooltip: { trigger: 'axis' },
  grid: { left: '3%', right: '4%', bottom: '3%', top: '12%' },
  xAxis: { type: 'category', boundaryGap: false, data: [] as string[] },
  yAxis: { type: 'value', minInterval: 1 },
  series: [
    {
      color: '#8e9dff',
      name: $t('business.requests'),
      type: 'line',
      smooth: true,
      data: [] as number[]
    }
  ]
}));

watch(
  () => appStore.locale,
  () => {
    void updateTrend((_options, optionsFactory) => optionsFactory());
  }
);

async function loadSummary() {
  summaryLoading.value = true;
  try {
    const { data, error } = await fetchUsageSummary(days.value);
    if (!error && data) {
      summary.value = data;
      successRate.value =
        data.total_requests > 0 ? `${Math.round((data.success_requests / data.total_requests) * 100)}%` : '-';
      updateTrend(options => {
        options.xAxis.data = data.by_day.map(item => item.day);
        options.series[0].data = data.by_day.map(item => item.requests);
        return options;
      });
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
      <template #header>{{ $t('business.usage-report') }}</template>
      <template #header-extra>
        <NSpace>
          <NSelect
            v-model:value="days"
            size="small"
            class="w-120px"
            :options="[
              { label: $t('business.lastNDays', { days: 7 }), value: 7 },
              { label: $t('business.lastNDays', { days: 14 }), value: 14 },
              { label: $t('business.lastNDays', { days: 30 }), value: 30 },
              { label: $t('business.lastNDays', { days: 90 }), value: 90 }
            ]"
            @update:value="refreshAll"
          />
          <NButton quaternary type="primary" :loading="summaryLoading || recordsLoading" @click="refreshAll">
            {{ $t('business.refresh') }}
          </NButton>
        </NSpace>
      </template>

      <NGrid responsive="screen" item-responsive :x-gap="16" :y-gap="16">
        <NGi span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="card-wrapper">
            <div class="text-13px text-#6b7280">
              {{ $t('business.total-requests') }}
            </div>
            <div class="mt-8px text-24px font-semibold">
              {{ summary ? formatNumber(summary.total_requests) : '-' }}
            </div>
          </NCard>
        </NGi>
        <NGi span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="card-wrapper">
            <div class="text-13px text-#6b7280">
              {{ $t('business.success-rate') }}
            </div>
            <div class="mt-8px text-24px font-semibold">{{ successRate }}</div>
          </NCard>
        </NGi>
        <NGi span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="card-wrapper">
            <div class="text-13px text-#6b7280">
              {{ $t('business.total-tokens') }}
            </div>
            <div class="mt-8px text-24px font-semibold">
              {{ summary ? formatNumber(summary.total_input_tokens + summary.total_output_tokens) : '-' }}
            </div>
          </NCard>
        </NGi>
        <NGi span="24 s:12 m:6">
          <NCard size="small" :bordered="false" class="card-wrapper">
            <div class="text-13px text-#6b7280">
              {{ $t('business.cost-by-currency') }}
            </div>
            <div v-if="summary?.costs.length" class="mt-8px">
              <div v-for="item in summary.costs" :key="item.currency" class="text-20px font-semibold">
                {{ formatCost(item.total, item.currency) }}
              </div>
            </div>
            <div v-else class="mt-8px text-24px font-semibold">-</div>
          </NCard>
        </NGi>
      </NGrid>
    </NCard>

    <NCard :title="$t('business.request-trend')" :bordered="false" class="mt-16px card-wrapper">
      <div ref="trendRef" class="h-320px" />
    </NCard>

    <NCard :title="$t('business.by-model')" :bordered="false" class="mt-16px card-wrapper">
      <NDataTable
        :columns="modelColumns"
        :data="summary?.by_model ?? []"
        :loading="summaryLoading"
        :bordered="false"
        size="small"
      />
    </NCard>

    <NCard :title="$t('business.call-details')" :bordered="false" class="mt-16px card-wrapper">
      <NDataTable
        :columns="recordColumns"
        :data="records"
        :loading="recordsLoading"
        :bordered="false"
        size="small"
        :scroll-x="1400"
      />
      <div class="mt-12px flex justify-end">
        <NPagination :page="page" :page-size="pageSize" :item-count="total" @update:page="handlePageChange" />
      </div>
    </NCard>
  </div>
</template>

<style scoped></style>
