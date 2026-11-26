<script setup lang="ts">
import { h, onMounted, ref, reactive, computed, watch } from 'vue';
import { NButton, NPagination, NTag } from 'naive-ui';
import type { DataTableColumns, SelectGroupOption, SelectOption } from 'naive-ui';
import type { LineSeriesOption } from 'echarts/charts';
import { formatTime, usageStateTagType, usageStateText } from '@/utils/admin-status';
import { formatCurrency } from '@/utils/currency';
import {
  fetchApiKeys,
  fetchApplications,
  fetchModelServices,
  fetchProjects,
  exportUsageRecords,
  fetchUsageRecords,
  fetchUsageSummary,
  fetchUsageTrend
} from '@/service/api';
import { $t } from '@/locales';
import { useAppStore } from '@/store/modules/app';
import { useEcharts } from '@/hooks/common/echarts';
import { usePermission } from '@/hooks/common/permission';

defineOptions({
  name: 'ObservabilityUsage'
});

const { has } = usePermission();
const canExport = computed(() => has('usage:export'));

const summaryLoading = ref(true);
const recordsLoading = ref(true);
const exporting = ref(false);
// 近 N 天由服务端按当前时间往前推;今天、昨天、本周、本月、自定义则下发明确的起止时间。
type QuickRange = 'today' | 'yesterday' | 'thisWeek' | 'thisMonth' | 'last7' | 'last30' | 'custom';

const days = ref(7);
const quickRange = ref<QuickRange>('last7');
const quickRangePresets = computed<{ label: string; value: QuickRange }[]>(() => [
  { label: $t('business.today'), value: 'today' },
  { label: $t('business.yesterday'), value: 'yesterday' },
  { label: $t('business.thisWeek'), value: 'thisWeek' },
  { label: $t('business.thisMonth'), value: 'thisMonth' },
  { label: $t('business.lastNDays', { days: 7 }), value: 'last7' },
  { label: $t('business.lastNDays', { days: 30 }), value: 'last30' },
  { label: $t('business.custom'), value: 'custom' }
]);
const trendDimension = ref('overview');
// 趋势拆分维度:先给出总览,再按“资源 / 请求 / 部门内细分 / 项目内细分”分组,避免十几个选项排成一长串。
const trendDimensionOptions = computed<(SelectOption | SelectGroupOption)[]>(() => [
  { label: $t('business.trendOverview'), value: 'overview' },
  {
    type: 'group',
    key: 'resource',
    label: $t('business.trendGroupResource'),
    children: [
      { label: $t('business.model'), value: 'model' },
      { label: $t('business.model-service'), value: 'model_service' },
      { label: $t('business.project'), value: 'project' },
      { label: $t('business.application'), value: 'application' },
      { label: 'API Key', value: 'api_key' },
      { label: $t('business.provider-c4b9p'), value: 'provider' },
      { label: $t('business.department'), value: 'department' },
      { label: $t('business.routePolicy'), value: 'route_policy' }
    ]
  },
  {
    type: 'group',
    key: 'request',
    label: $t('business.trendGroupRequest'),
    children: [
      { label: $t('business.status'), value: 'state' },
      { label: $t('business.source-i6yz'), value: 'source' },
      { label: $t('business.errorType'), value: 'error_type' }
    ]
  },
  {
    type: 'group',
    key: 'department-drill',
    label: $t('business.trendGroupWithinDepartment'),
    children: [
      { label: $t('business.trendDepartmentProject'), value: 'department_project' },
      { label: $t('business.trendDepartmentProjectApplication'), value: 'department_project_application' },
      { label: $t('business.trendDepartmentProjectModelService'), value: 'department_project_model_service' },
      { label: $t('business.trendDepartmentProjectModelServiceApplication'), value: 'department_project_model_service_application' }
    ]
  },
  {
    type: 'group',
    key: 'project-drill',
    label: $t('business.trendGroupWithinProject'),
    children: [
      { label: $t('business.trendProjectApplication'), value: 'project_application' },
      { label: $t('business.trendProjectModelService'), value: 'project_model_service' },
      { label: $t('business.trendProjectModelServiceApplication'), value: 'project_model_service_application' }
    ]
  }
]);
const summary = ref<Api.Management.UsageSummary | null>(null);
const records = ref<Api.Management.UsageRecord[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = 20;
const showDetail = ref(false);
const detail = ref<Api.Management.UsageRecord | null>(null);
const filterRange = ref<[number, number] | null>(null);
const filters = reactive<{
  project_id: string | null;
  application_id: string | null;
  api_key_id: string | null;
  model_service_id: string | null;
  state: string | null;
  source: string | null;
}>({
  project_id: null,
  application_id: null,
  api_key_id: null,
  model_service_id: null,
  state: null,
  source: null
});
const projects = ref<Api.Management.Project[]>([]);
const applications = ref<Api.Management.Application[]>([]);
const apiKeys = ref<Api.Management.ApiKey[]>([]);
const modelServices = ref<Api.Management.ModelService[]>([]);

const projectFilterOptions = computed(() => projects.value.map(item => ({ label: item.name, value: item.id })));
const applicationFilterOptions = computed(() => applications.value.map(item => ({ label: item.name, value: item.id })));
const apiKeyFilterOptions = computed(() => apiKeys.value.map(item => ({ label: item.name, value: item.id })));
const modelServiceFilterOptions = computed(() => modelServices.value.map(item => ({ label: item.name, value: item.id })));
const stateFilterOptions = computed(() => [
  { label: $t('business.success'), value: 'success' },
  { label: $t('business.failed'), value: 'failed' },
  { label: $t('business.interrupted'), value: 'interrupted' },
  { label: $t('business.pending'), value: 'pending' }
]);
const sourceFilterOptions = computed(() => [
  { label: $t('business.business-calls'), value: 'api' },
  { label: $t('business.source-agent-api'), value: 'agent_api' },
  { label: $t('business.source-route-playground'), value: 'route_playground' },
  { label: $t('business.prompt-debugging'), value: 'prompt_debug' },
  { label: $t('business.agent-debugging'), value: 'agent_debug' }
]);

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
      agent_api: $t('business.source-agent-api'),
      route_playground: $t('business.source-route-playground'),
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
    ellipsis: { tooltip: true },
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

const scopeColumns = computed<DataTableColumns<Api.Management.UsageScopeSummary>>(() => [
  { title: $t('business.name'), key: 'name', ellipsis: { tooltip: true }, minWidth: 160 },
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
    ellipsis: { tooltip: true },
    width: 120,
    render: row => row.project_name ?? $t('business.admin-console')
  },
  {
    title: $t('business.application'),
    key: 'application_name',
    ellipsis: { tooltip: true },
    width: 120,
    render: row => row.application_name ?? sourceText(row.source)
  },
  {
    title: 'API Key',
    key: 'key_name',
    ellipsis: { tooltip: true },
    width: 140,
    render: row => row.key_name ?? sourceText(row.source)
  },
  { title: $t('business.model-service'), key: 'model_service', ellipsis: { tooltip: true }, width: 140 },
  {
    title: $t('business.upstream-model'),
    key: 'upstream_model',
    ellipsis: { tooltip: true },
    width: 140,
    render: row => row.upstream_model ?? '-'
  },
  {
    title: $t('business.status'),
    key: 'state',
    width: 105,
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
    width: 115,
    render: row => (row.total_ms != null ? String(row.total_ms) : '-')
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 95,
    // 操作列固定在右侧:列多于一屏宽度时,行内操作不用横向滚动也能点到。
    fixed: 'right',
    render: row =>
      h(
        NButton,
        { size: 'small', quaternary: true, type: 'primary', onClick: () => openDetail(row) },
        { default: () => $t('business.details') }
      )
  }
]);

function openDetail(row: Api.Management.UsageRecord) {
  detail.value = row;
  showDetail.value = true;
}

const successRate = ref('-');

const appStore = useAppStore();
const { domRef: trendRef, updateOptions: updateTrend } = useEcharts(() => ({
  tooltip: { trigger: 'axis' },
  legend: { data: [] as string[] },
  grid: { left: '3%', right: '4%', bottom: '3%', top: '12%' },
  xAxis: { type: 'category', boundaryGap: false, data: [] as string[] },
  yAxis: { type: 'value', minInterval: 1 },
  series: [] as LineSeriesOption[]
}));

watch(
  () => appStore.locale,
  () => {
    void updateTrend((_options, optionsFactory) => optionsFactory());
  }
);

function currentFilterParams(): Api.Management.UsageRecordFilters {
  const params: Api.Management.UsageRecordFilters = {};
  if (filters.project_id) params.project_id = filters.project_id;
  if (filters.application_id) params.application_id = filters.application_id;
  if (filters.api_key_id) params.api_key_id = filters.api_key_id;
  if (filters.model_service_id) params.model_service_id = filters.model_service_id;
  if (filters.state) params.state = filters.state;
  if (filters.source) params.source = filters.source;
  if (filterRange.value) {
    params.start_time = new Date(filterRange.value[0]).toISOString();
    params.end_time = new Date(filterRange.value[1]).toISOString();
  }
  return params;
}

async function loadSummary() {
  summaryLoading.value = true;
  try {
    const { data, error } = await fetchUsageSummary(days.value, currentFilterParams());
    if (!error && data) {
      summary.value = data;
      successRate.value =
        data.total_requests > 0 ? `${Math.round((data.success_requests / data.total_requests) * 100)}%` : '-';
    }
  } finally {
    summaryLoading.value = false;
  }
}

async function loadTrend() {
  const { data, error } = await fetchUsageTrend(currentFilterParams(), trendDimension.value, days.value);
  if (error || !data) return;
  const overviewName = $t('business.trendOverview');
  const dayList = Array.from(new Set(data.items.map(item => item.day))).sort();
  const names = Array.from(new Set(data.items.map(item => item.name || overviewName)));
  const series = names.map(name => ({
    name,
    type: 'line' as const,
    smooth: true,
    data: dayList.map(day => {
      const point = data.items.find(item => item.day === day && (item.name || overviewName) === name);
      return point?.requests ?? 0;
    })
  }));
  updateTrend(options => {
    options.legend.data = names;
    options.xAxis.data = dayList;
    options.series = series;
    return options;
  });
}

async function loadRecords() {
  recordsLoading.value = true;
  try {
    const { data, error } = await fetchUsageRecords(page.value, pageSize, currentFilterParams());
    if (!error && data) {
      records.value = data.records;
      total.value = data.total;
    }
  } finally {
    recordsLoading.value = false;
  }
}

// 选中快捷时间范围后立即刷新
function applyQuickRange(kind: QuickRange) {
  if (kind === 'last7' || kind === 'last30') {
    days.value = kind === 'last7' ? 7 : 30;
    const now = Date.now();
    filterRange.value = [now - days.value * 24 * 60 * 60 * 1000, now];
    applyFilters();
    return;
  }

  if (kind === 'custom') {
    // 切到自定义时,先用当前的近 7 天填进去,方便在此基础上微调
    const now = Date.now();
    filterRange.value = [now - days.value * 24 * 60 * 60 * 1000, now];
    applyFilters();
    return;
  }

  const today = new Date();
  const start = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  const end = new Date(start);

  if (kind === 'today') {
    end.setDate(end.getDate() + 1);
  } else if (kind === 'yesterday') {
    start.setDate(start.getDate() - 1);
  } else if (kind === 'thisWeek') {
    // 周一 0 点到下周一 0 点
    const daysFromMonday = (start.getDay() + 6) % 7;
    start.setDate(start.getDate() - daysFromMonday);
    end.setDate(end.getDate() + 7 - daysFromMonday);
  } else {
    start.setDate(1);
    end.setMonth(end.getMonth() + 1);
    end.setDate(1);
  }

  filterRange.value = [start.getTime(), end.getTime()];
  applyFilters();
}

function onCustomRangeChange() {
  // 自定义范围被清空时,回到默认的近 7 天
  if (!filterRange.value) days.value = 7;
  applyFilters();
}

function applyFilters() {
  page.value = 1;
  void loadSummary();
  void loadTrend();
  void loadRecords();
}

function resetFilters() {
  filters.project_id = null;
  filters.application_id = null;
  filters.api_key_id = null;
  filters.model_service_id = null;
  filters.state = null;
  filters.source = null;
  quickRange.value = 'last7';
  days.value = 7;
  filterRange.value = null;
  applyFilters();
}

async function exportCsv() {
  exporting.value = true;
  try {
    await exportUsageRecords(currentFilterParams());
  } finally {
    exporting.value = false;
  }
}

async function loadFilterOptions() {
  const [projectRes, applicationRes, apiKeyRes, modelServiceRes] = await Promise.all([
    has('org:read') ? fetchProjects() : Promise.resolve({ data: null, error: null }),
    has('org:read') ? fetchApplications() : Promise.resolve({ data: null, error: null }),
    has('api_key:read') ? fetchApiKeys() : Promise.resolve({ data: null, error: null }),
    has('model_service:read') ? fetchModelServices() : Promise.resolve({ data: null, error: null })
  ]);
  if (!projectRes.error && projectRes.data) projects.value = projectRes.data;
  if (!applicationRes.error && applicationRes.data) applications.value = applicationRes.data;
  if (!apiKeyRes.error && apiKeyRes.data) apiKeys.value = apiKeyRes.data;
  if (!modelServiceRes.error && modelServiceRes.data) modelServices.value = modelServiceRes.data;
}

function handlePageChange(p: number) {
  page.value = p;
  void loadRecords();
}

function refreshAll() {
  void loadSummary();
  void loadTrend();
  page.value = 1;
  void loadRecords();
}

onMounted(() => {
  void loadFilterOptions();
  refreshAll();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :bordered="false" class="card-wrapper">
      <template #header>{{ $t('business.usage-report') }}</template>
      <template #header-extra>
        <NButton v-if="canExport" :loading="exporting" @click="exportCsv">{{ $t('business.exportCsv') }}</NButton>
      </template>

      <NForm :model="filters" label-placement="top" :show-feedback="false" class="mb-12px">
        <NGrid cols="1 s:2 m:3 xl:6" responsive="screen" item-responsive :x-gap="12" :y-gap="12">
          <NFormItemGi span="1 s:2 m:3 xl:6" :label="$t('business.time')">
            <div class="w-full flex flex-wrap items-center justify-between gap-12px">
              <div class="flex flex-wrap items-center gap-12px">
                <NRadioGroup v-model:value="quickRange" @update:value="applyQuickRange">
                  <NRadioButton v-for="preset in quickRangePresets" :key="preset.value" :value="preset.value">
                    {{ preset.label }}
                  </NRadioButton>
                </NRadioGroup>
                <NDatePicker
                  v-if="quickRange === 'custom'"
                  v-model:value="filterRange"
                  type="datetimerange"
                  clearable
                  :start-placeholder="$t('business.start-time')"
                  :end-placeholder="$t('business.end-time')"
                  @update:value="onCustomRangeChange"
                />
              </div>
              <NSpace>
                <NButton type="primary" @click="applyFilters">{{ $t('common.search') }}</NButton>
                <NButton @click="resetFilters">{{ $t('common.reset') }}</NButton>
              </NSpace>
            </div>
          </NFormItemGi>
          <NFormItemGi :label="$t('business.project')">
            <NSelect
              v-model:value="filters.project_id"
              :options="projectFilterOptions"
              clearable
              filterable
              :placeholder="$t('business.allProjects')"
            />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.application')">
            <NSelect
              v-model:value="filters.application_id"
              :options="applicationFilterOptions"
              clearable
              filterable
              :placeholder="$t('business.allApplications')"
            />
          </NFormItemGi>
          <NFormItemGi label="API Key">
            <NSelect
              v-model:value="filters.api_key_id"
              :options="apiKeyFilterOptions"
              clearable
              filterable
              :placeholder="$t('business.allApiKeys')"
            />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.model-service')">
            <NSelect
              v-model:value="filters.model_service_id"
              :options="modelServiceFilterOptions"
              clearable
              filterable
              :placeholder="$t('business.allModelServices')"
            />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.status')">
            <NSelect
              v-model:value="filters.state"
              :options="stateFilterOptions"
              clearable
              :placeholder="$t('business.all')"
            />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.source-i6yz')">
            <NSelect
              v-model:value="filters.source"
              :options="sourceFilterOptions"
              clearable
              :placeholder="$t('business.all')"
            />
          </NFormItemGi>
        </NGrid>
      </NForm>

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
      <template #header-extra>
        <NSpace align="center">
          <NText depth="3">{{ $t('business.trendSplitBy') }}</NText>
          <NSelect
            v-model:value="trendDimension"
            class="w-380px"
            :options="trendDimensionOptions"
            @update:value="loadTrend"
          />
        </NSpace>
      </template>
      <div ref="trendRef" class="h-320px" />
    </NCard>

    <NCard :title="$t('business.usage-breakdown')" :bordered="false" class="mt-16px card-wrapper">
      <NTabs type="line" animated>
        <NTabPane name="model" :tab="$t('business.by-model')">
          <NDataTable
            :columns="modelColumns"
            :data="summary?.by_model ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
        <NTabPane name="project" :tab="$t('business.by-project')">
          <NDataTable
            :columns="scopeColumns"
            :data="summary?.by_project ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
        <NTabPane name="application" :tab="$t('business.by-application')">
          <NDataTable
            :columns="scopeColumns"
            :data="summary?.by_application ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
        <NTabPane name="apiKey" :tab="$t('business.by-api-key')">
          <NDataTable
            :columns="scopeColumns"
            :data="summary?.by_api_key ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
        <NTabPane name="department" :tab="$t('business.by-department')">
          <NDataTable
            :columns="scopeColumns"
            :data="summary?.by_department ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
        <NTabPane name="modelService" :tab="$t('business.by-model-service')">
          <NDataTable
            :columns="scopeColumns"
            :data="summary?.by_model_service ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
        <NTabPane name="state" :tab="$t('business.by-state')">
          <NDataTable
            :columns="scopeColumns"
            :data="summary?.by_state ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
        <NTabPane name="source" :tab="$t('business.by-source')">
          <NDataTable
            :columns="scopeColumns"
            :data="summary?.by_source ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
        <NTabPane name="provider" :tab="$t('business.by-provider')">
          <NDataTable
            :columns="scopeColumns"
            :data="summary?.by_provider ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
        <NTabPane name="routePolicy" :tab="$t('business.by-route-policy')">
          <NDataTable
            :columns="scopeColumns"
            :data="summary?.by_route_policy ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
        <NTabPane name="errorType" :tab="$t('business.by-error-type')">
          <NDataTable
            :columns="scopeColumns"
            :data="summary?.by_error_type ?? []"
            :loading="summaryLoading"
            :bordered="false"
            size="small"
          />
        </NTabPane>
      </NTabs>
    </NCard>

    <NCard :title="$t('business.call-details')" :bordered="false" class="mt-16px card-wrapper">
      <NDataTable
        :columns="recordColumns"
        :data="records"
        :loading="recordsLoading"
        :bordered="false"
        size="small"
        :scroll-x="1400"
      >
        <template #empty>
          <NEmpty :description="$t('business.noUsageRecords')" />
        </template>
      </NDataTable>
      <div class="mt-12px flex justify-end">
        <NPagination :page="page" :page-size="pageSize" :item-count="total" @update:page="handlePageChange" />
      </div>
    </NCard>

    <NModal v-model:show="showDetail" preset="card" :title="$t('business.usageDetail')" class="w-760px">
      <NDescriptions v-if="detail" :column="2" bordered size="small" label-placement="left">
        <NDescriptionsItem :label="$t('business.requestId')" :span="2">{{ detail.request_id }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.time')">{{ formatTime(detail.created_at) }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.status')">{{ usageStateText(detail.state) }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.project')">
          {{ detail.project_name ?? $t('business.admin-console') }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.application')">
          {{ detail.application_name ?? sourceText(detail.source) }}
        </NDescriptionsItem>
        <NDescriptionsItem label="API Key">{{ detail.key_name ?? sourceText(detail.source) }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.model-service')">{{ detail.model_service }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.routePolicy')">{{ detail.route_policy_id ?? '-' }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.provider-c4b9p')">{{ detail.provider ?? '-' }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.upstream-model')">{{ detail.upstream_model ?? '-' }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.input-tokens')">{{ formatNumber(detail.input_tokens) }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.output-tokens')">{{ formatNumber(detail.output_tokens) }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.cachedTokens')">{{ formatNumber(detail.cached_tokens) }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.reasoningTokens')">
          {{ formatNumber(detail.reasoning_tokens) }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.firstTokenLatency')">
          {{ detail.first_token_ms != null ? `${detail.first_token_ms} ms` : '-' }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.latency-ms')">
          {{ detail.total_ms != null ? `${detail.total_ms} ms` : '-' }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.cost')">
          {{ formatCost(detail.cost, detail.cost_currency) }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.source-i6yz')">{{ sourceText(detail.source) }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.errorType')">{{ detail.error_type ?? '-' }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.errorMessage')">{{ detail.error_message ?? '-' }}</NDescriptionsItem>
      </NDescriptions>
    </NModal>
  </div>
</template>

<style scoped></style>
