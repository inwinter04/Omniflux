<script setup lang="ts">
import { computed, h, nextTick, reactive, ref, watch } from 'vue';
import { NButton, NPagination, NTag } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import type { BarSeriesOption, LineSeriesOption } from 'echarts/charts';
import { fetchRoutePolicyDecisions, fetchRoutePolicyMetrics, fetchRoutePolicyOverview } from '@/service/api';
import { formatTime, routeStrategyTagType, routeStrategyText, usageStateTagType, usageStateText } from '@/utils/admin-status';
import { formatCurrency } from '@/utils/currency';
import { useEcharts } from '@/hooks/common/echarts';
import { useAppStore } from '@/store/modules/app';
import { $t } from '@/locales';

const props = defineProps<{
  show: boolean;
  policy: Api.Management.RoutePolicy | null;
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
}>();

const appStore = useAppStore();
const activeTab = ref<'overview' | 'metrics' | 'decisions'>('overview');

const filters = reactive<{
  days: number;
  model_service_id: string | null;
  source: string | null;
  baseline_candidate_id: string | null;
  upstream_models: string[];
  state: string | null;
  fallback_only: boolean;
}>({
  days: 7,
  model_service_id: null,
  source: null,
  baseline_candidate_id: null,
  upstream_models: [],
  state: null,
  fallback_only: false
});

const overviewLoading = ref(false);
const overview = ref<Api.Management.RoutePolicyOverview | null>(null);

const metricsLoading = ref(false);
const metrics = ref<Api.Management.RoutePolicyMetrics | null>(null);

const decisionsLoading = ref(false);
const decisions = ref<Api.Management.RoutePolicyDecisionRecord[]>([]);
const decisionsTotal = ref(0);
const decisionPage = ref(1);
const decisionPageSize = 15;
const inspectVisible = ref(false);
const inspectedDecision = ref<Api.Management.RoutePolicyDecisionRecord | null>(null);

const dayOptions = computed<SelectOption[]>(() => [
  { label: $t('business.lastNDays', { days: 1 }), value: 1 },
  { label: $t('business.lastNDays', { days: 7 }), value: 7 },
  { label: $t('business.lastNDays', { days: 30 }), value: 30 }
]);

const sourceOptions = computed<SelectOption[]>(() => [
  { label: $t('business.all'), value: 'all' },
  { label: $t('business.business-calls'), value: 'api' },
  { label: $t('business.source-agent-api'), value: 'agent_api' },
  { label: $t('business.source-route-playground'), value: 'route_playground' }
]);

const stateOptions = computed<SelectOption[]>(() => [
  { label: $t('business.success'), value: 'success' },
  { label: $t('business.failed'), value: 'failed' },
  { label: $t('business.interrupted'), value: 'interrupted' }
]);

const boundServiceOptions = computed<SelectOption[]>(() =>
  (props.policy?.bound_services ?? []).map(s => ({ label: s.name, value: s.id }))
);

const baselineOptions = computed<SelectOption[]>(() =>
  (overview.value?.candidate_options ?? []).map(c => ({
    label: `${c.model_service} · ${c.upstream_model} (${c.input_cost_per_m}/${c.output_cost_per_m} ${c.cost_currency})`,
    value: c.id
  }))
);

const modelFilterOptions = computed<SelectOption[]>(() =>
  (metrics.value?.available_models ?? []).map(m => ({ label: m, value: m }))
);

const { domRef: pieRef, updateOptions: updatePie } = useEcharts(() => ({
  tooltip: { trigger: 'item' },
  legend: { bottom: '0%', left: 'center' },
  series: [
    {
      name: $t('business.model-hit-share'),
      type: 'pie',
      radius: ['42%', '70%'],
      center: ['50%', '44%'],
      label: { show: false },
      data: [] as { name: string; value: number }[]
    }
  ]
}));

const { domRef: trendRef, updateOptions: updateTrend } = useEcharts(() => ({
  tooltip: { trigger: 'axis' },
  legend: { data: [] as string[] },
  grid: { left: '4%', right: '5%', bottom: '8%', top: '14%', containLabel: true },
  xAxis: { type: 'category', data: [] as string[] },
  yAxis: [
    { type: 'value', name: $t('business.requests'), minInterval: 1 },
    { type: 'value', name: 'ms' }
  ],
  series: [] as (LineSeriesOption | BarSeriesOption)[]
}));

const { domRef: errorRef, updateOptions: updateErrorChart } = useEcharts(() => ({
  tooltip: { trigger: 'axis' },
  grid: { left: '4%', right: '6%', bottom: '8%', top: '10%', containLabel: true },
  xAxis: { type: 'value', minInterval: 1 },
  yAxis: { type: 'category', data: [] as string[] },
  series: [
    {
      type: 'bar',
      data: [] as number[]
    }
  ] as BarSeriesOption[]
}));

function renderOverviewChart() {
  const rows = overview.value?.by_model ?? [];
  updatePie(opts => {
    opts.series[0].data = rows.map(item => ({
      name: `${item.upstream_model} (${item.provider})`,
      value: item.requests
    }));
    return opts;
  });
}

function renderMetricsCharts() {
  const pts = metrics.value?.series ?? [];
  const reqName = $t('business.requests');
  const fbName = $t('business.fallback-requests');
  const ttftName = $t('business.avg-ttft');
  const latName = $t('business.avg-latency');
  updateTrend(opts => {
    opts.legend.data = [reqName, fbName, ttftName, latName];
    opts.xAxis.data = pts.map(p => p.bucket);
    opts.series = [
      { name: reqName, type: 'bar', yAxisIndex: 0, data: pts.map(p => p.requests) },
      { name: fbName, type: 'bar', yAxisIndex: 0, data: pts.map(p => p.fallback_requests) },
      { name: ttftName, type: 'line', yAxisIndex: 1, smooth: true, data: pts.map(p => p.avg_ttft_ms ?? 0) },
      { name: latName, type: 'line', yAxisIndex: 1, smooth: true, data: pts.map(p => p.avg_latency_ms ?? 0) }
    ];
    return opts;
  });

  const errs = metrics.value?.error_distribution ?? [];
  updateErrorChart(opts => {
    opts.yAxis.data = errs.map(e => e.error_type);
    opts.series[0].data = errs.map(e => e.count);
    return opts;
  });
}

async function loadOverview() {
  if (!props.policy) return;
  overviewLoading.value = true;
  try {
    const { data, error } = await fetchRoutePolicyOverview(props.policy.id, {
      days: filters.days,
      model_service_id: filters.model_service_id,
      source: filters.source === 'all' ? null : filters.source,
      baseline_candidate_id: filters.baseline_candidate_id
    });
    if (!error && data) {
      overview.value = data;
      await nextTick();
      renderOverviewChart();
    }
  } finally {
    overviewLoading.value = false;
  }
}

async function loadMetrics() {
  if (!props.policy) return;
  metricsLoading.value = true;
  try {
    const { data, error } = await fetchRoutePolicyMetrics(props.policy.id, {
      days: filters.days,
      model_service_id: filters.model_service_id,
      source: filters.source === 'all' ? null : filters.source,
      upstream_models: filters.upstream_models.length ? filters.upstream_models.join(',') : undefined
    });
    if (!error && data) {
      metrics.value = data;
      await nextTick();
      renderMetricsCharts();
    }
  } finally {
    metricsLoading.value = false;
  }
}

async function loadDecisions() {
  if (!props.policy) return;
  decisionsLoading.value = true;
  try {
    const { data, error } = await fetchRoutePolicyDecisions(props.policy.id, {
      page: decisionPage.value,
      page_size: decisionPageSize,
      model_service_id: filters.model_service_id,
      source: filters.source === 'all' ? null : filters.source,
      state: filters.state,
      fallback_only: filters.fallback_only
    });
    if (!error && data) {
      decisions.value = data.items ?? [];
      decisionsTotal.value = data.total ?? 0;
    }
  } finally {
    decisionsLoading.value = false;
  }
}

function reloadActiveTab() {
  if (activeTab.value === 'overview') void loadOverview();
  else if (activeTab.value === 'metrics') void loadMetrics();
  else void loadDecisions();
}

watch(
  () => [props.show, props.policy?.id],
  ([visible]) => {
    if (!visible || !props.policy) return;
    activeTab.value = 'overview';
    filters.baseline_candidate_id = props.policy.baseline_candidate_id ?? null;
    filters.model_service_id = null;
    filters.source = null;
    filters.upstream_models = [];
    filters.state = null;
    filters.fallback_only = false;
    decisionPage.value = 1;
    void loadOverview();
  }
);

watch(activeTab, async tab => {
  if (!props.show || !props.policy) return;
  if (tab === 'overview') {
    await loadOverview();
  } else if (tab === 'metrics') {
    await loadMetrics();
  } else {
    await loadDecisions();
  }
});

watch(
  () => appStore.locale,
  () => {
    void nextTick(() => {
      if (overview.value) renderOverviewChart();
      if (metrics.value) renderMetricsCharts();
    });
  }
);

function formatPct(val: number | null | undefined) {
  if (val === null || val === undefined) return '-';
  return `${Math.round(val * 1000) / 10}%`;
}

function formatMS(val: number | null | undefined) {
  if (val === null || val === undefined) return '-';
  return `${Math.round(val)} ms`;
}

const modelCompareColumns = computed<DataTableColumns<Api.Management.RoutePolicyOverview['by_model'][number]>>(() => [
  { title: $t('business.provider-c4b9p'), key: 'provider', width: 120 },
  { title: $t('business.upstream-model'), key: 'upstream_model', minWidth: 160, ellipsis: { tooltip: true } },
  { title: $t('business.requests'), key: 'requests', width: 95 },
  { title: $t('business.hit-share'), key: 'hit_share', width: 100, render: row => formatPct(row.hit_share) },
  { title: $t('business.success-rate'), key: 'success_rate', width: 115, render: row => formatPct(row.success_rate) },
  { title: $t('business.avg-ttft'), key: 'avg_ttft_ms', width: 105, render: row => formatMS(row.avg_ttft_ms) },
  { title: $t('business.avg-latency'), key: 'avg_latency_ms', width: 115, render: row => formatMS(row.avg_latency_ms) },
  {
    title: 'OTPS',
    key: 'otps',
    width: 85,
    render: row => (row.otps !== null && row.otps !== undefined ? `${row.otps.toFixed(1)} tok/s` : '-')
  },
  {
    title: $t('business.cost'),
    key: 'actual_cost',
    width: 115,
    render: row => formatCurrency(row.actual_cost, overview.value?.compare_currency ?? 'USD')
  }
]);

const decisionColumns = computed<DataTableColumns<Api.Management.RoutePolicyDecisionRecord>>(() => [
  { title: $t('business.time'), key: 'created_at', width: 165, render: row => formatTime(row.created_at) },
  { title: $t('business.model-service'), key: 'model_service_name', width: 150, render: row => row.model_service_name || '-' },
  {
    title: $t('business.strategy'),
    key: 'strategy',
    width: 110,
    render: row =>
      row.strategy
        ? h(NTag, { size: 'small', type: routeStrategyTagType(row.strategy), round: true }, { default: () => routeStrategyText(row.strategy) })
        : '-'
  },
  { title: $t('business.sim-matched-rule'), key: 'matched_rule_name', width: 140, render: row => row.matched_rule_name || '-' },
  {
    title: $t('business.served-model'),
    key: 'served_model',
    minWidth: 160,
    render: row => (row.served_model ? `${row.served_provider ?? ''}/${row.served_model}` : '-')
  },
  {
    title: $t('business.status'),
    key: 'state',
    width: 105,
    render: row => h(NTag, { size: 'small', type: usageStateTagType(row.state), round: true }, { default: () => usageStateText(row.state) })
  },
  {
    title: $t('business.fallback-count'),
    key: 'attempts',
    width: 105,
    render: row => {
      const cnt = row.attempt_trail?.attempts?.length ?? 0;
      return cnt > 1
        ? h(NTag, { size: 'small', type: 'warning', round: true }, { default: () => String(cnt) })
        : String(cnt || '-');
    }
  },
  {
    title: $t('business.latency-ms'),
    key: 'total_ms',
    width: 110,
    render: row => (row.total_ms !== null ? `${row.first_token_ms ?? '-'} / ${row.total_ms} ms` : '-')
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 95,
    render: row =>
      h(
        NButton,
        {
          size: 'small',
          quaternary: true,
          onClick: () => {
            inspectedDecision.value = row;
            inspectVisible.value = true;
          }
        },
        { default: () => $t('business.details') }
      )
  }
]);
</script>

<template>
  <NDrawer :show="show" :width="1060" @update:show="val => emit('update:show', val)">
    <NDrawerContent :title="policy ? `${policy.name} (v${policy.version})` : ''" closable>
      <template #header>
        <div class="flex flex-wrap items-center gap-8px">
          <span>{{ policy?.name }}</span>
          <NTag v-if="policy" size="small" round>v{{ policy.version }}</NTag>
          <NTag v-if="policy" size="small" :type="routeStrategyTagType(policy.strategy)" round>
            {{ routeStrategyText(policy.strategy) }}
          </NTag>
          <NTag v-if="policy" size="small" type="info" round>
            {{ policy.compare_currency }}
          </NTag>
          <NTag v-if="policy?.session_affinity?.enabled" size="small" type="success" round>
            {{ $t('business.session-affinity-tag', { ttl: policy.session_affinity.ttl_seconds }) }}
          </NTag>
        </div>
      </template>

      <div class="mb-12px flex flex-wrap items-center gap-10px">
        <NSelect
          v-model:value="filters.days"
          :options="dayOptions"
          size="small"
          class="min-w-140px w-150px"
          @update:value="reloadActiveTab"
        />
        <NSelect
          v-model:value="filters.model_service_id"
          :options="boundServiceOptions"
          :placeholder="$t('business.model-service')"
          size="small"
          clearable
          class="min-w-190px w-210px"
          @update:value="reloadActiveTab"
        />
        <NSelect
          v-model:value="filters.source"
          :options="sourceOptions"
          :placeholder="$t('business.source-i6yz')"
          size="small"
          clearable
          class="min-w-150px w-170px"
          @update:value="reloadActiveTab"
        />
        <NButton size="small" @click="reloadActiveTab">{{ $t('common.refresh') }}</NButton>
      </div>

      <NTabs v-model:value="activeTab" type="line" animated>
        <NTabPane name="overview" :tab="$t('business.policy-tab-overview')">
          <NSpin :show="overviewLoading">
            <div class="mb-12px flex flex-wrap items-center justify-between gap-12px">
              <NText depth="2">{{ $t('business.baseline-comparison-hint') }}</NText>
              <NSelect
                v-model:value="filters.baseline_candidate_id"
                :options="baselineOptions"
                :placeholder="$t('business.baseline-candidate')"
                size="small"
                clearable
                filterable
                class="w-320px"
                @update:value="loadOverview"
              />
            </div>

            <NGrid :cols="4" :x-gap="12" class="mb-16px">
              <NGi>
                <NCard size="small">
                  <NStatistic :label="$t('business.actual-spend')" :value="formatCurrency(overview?.summary.actual_cost ?? 0, overview?.compare_currency ?? 'USD')" />
                </NCard>
              </NGi>
              <NGi>
                <NCard size="small">
                  <NStatistic
                    :label="$t('business.baseline-spend')"
                    :value="overview?.baseline.baseline_cost !== undefined ? formatCurrency(overview.baseline.baseline_cost, overview.compare_currency) : '-'"
                  />
                </NCard>
              </NGi>
              <NGi>
                <NCard size="small">
                  <NStatistic
                    :label="$t('business.saved-cost')"
                    :value="overview?.baseline.saved_cost !== undefined ? formatCurrency(overview.baseline.saved_cost, overview.compare_currency) : '-'"
                  />
                </NCard>
              </NGi>
              <NGi>
                <NCard size="small">
                  <NStatistic
                    :label="$t('business.savings-rate')"
                    :value="formatPct(overview?.baseline.savings_rate)"
                  />
                </NCard>
              </NGi>
            </NGrid>

            <NGrid :cols="12" :x-gap="12" class="mb-16px">
              <NGi :span="5">
                <NCard size="small" :title="$t('business.model-hit-share')">
                  <div ref="pieRef" class="h-260px w-full" />
                </NCard>
              </NGi>
              <NGi :span="7">
                <NCard size="small" :title="$t('business.policy-key-indicators')">
                  <NDescriptions :column="2" bordered size="small">
                    <NDescriptionsItem :label="$t('business.requests')">
                      {{ overview?.summary.requests ?? 0 }}
                    </NDescriptionsItem>
                    <NDescriptionsItem :label="$t('business.success-rate')">
                      {{ formatPct(overview?.summary.success_rate) }}
                    </NDescriptionsItem>
                    <NDescriptionsItem :label="$t('business.fallback-rate')">
                      {{ formatPct(overview?.summary.fallback_rate) }}
                    </NDescriptionsItem>
                    <NDescriptionsItem :label="$t('business.avg-ttft')">
                      {{ formatMS(overview?.summary.avg_ttft_ms) }}
                    </NDescriptionsItem>
                    <NDescriptionsItem :label="$t('business.avg-latency')">
                      {{ formatMS(overview?.summary.avg_latency_ms) }}
                    </NDescriptionsItem>
                    <NDescriptionsItem label="OTPS">
                      {{ overview?.summary.otps !== null && overview?.summary.otps !== undefined ? `${overview.summary.otps.toFixed(1)} tok/s` : '-' }}
                    </NDescriptionsItem>
                  </NDescriptions>
                </NCard>
              </NGi>
            </NGrid>

            <NCard size="small" :title="$t('business.model-performance-comparison')">
              <NDataTable
                :columns="modelCompareColumns"
                :data="overview?.by_model ?? []"
                size="small"
                :bordered="false"
                :scroll-x="920"
              />
            </NCard>
          </NSpin>
        </NTabPane>

        <NTabPane name="metrics" :tab="$t('business.policy-tab-monitoring')">
          <NSpin :show="metricsLoading">
            <div class="mb-12px flex flex-wrap items-center gap-10px">
              <NSelect
                v-model:value="filters.upstream_models"
                :options="modelFilterOptions"
                :placeholder="$t('business.filter-upstream-models')"
                size="small"
                multiple
                clearable
                class="w-360px"
                @update:value="loadMetrics"
              />
            </div>

            <NGrid :cols="4" :x-gap="12" :y-gap="12" class="mb-16px">
              <NGi>
                <NCard size="small">
                  <NStatistic :label="$t('business.requests')" :value="metrics?.kpis.requests ?? 0" />
                </NCard>
              </NGi>
              <NGi>
                <NCard size="small">
                  <NStatistic label="RPM / TPM" :value="`${(metrics?.kpis.rpm ?? 0).toFixed(2)} / ${(metrics?.kpis.tpm ?? 0).toFixed(0)}`" />
                </NCard>
              </NGi>
              <NGi>
                <NCard size="small">
                  <NStatistic label="TTFT P50 / P95" :value="`${formatMS(metrics?.kpis.ttft_p50_ms)} / ${formatMS(metrics?.kpis.ttft_p95_ms)}`" />
                </NCard>
              </NGi>
              <NGi>
                <NCard size="small">
                  <NStatistic :label="$t('business.fallback-rate')" :value="formatPct(metrics?.kpis.fallback_rate)" />
                </NCard>
              </NGi>
            </NGrid>

            <NCard size="small" :title="$t('business.traffic-and-latency-trend')" class="mb-16px">
              <div ref="trendRef" class="h-300px w-full" />
            </NCard>
            <NCard size="small" :title="$t('business.error-distribution')">
              <div ref="errorRef" class="h-220px w-full" />
            </NCard>
          </NSpin>
        </NTabPane>

        <NTabPane name="decisions" :tab="$t('business.policy-tab-decisions')">
          <div class="mb-12px flex flex-wrap items-center gap-10px">
            <NSelect
              v-model:value="filters.state"
              :options="stateOptions"
              :placeholder="$t('business.status')"
              size="small"
              clearable
              class="min-w-140px w-160px"
              @update:value="() => { decisionPage = 1; loadDecisions(); }"
            />
            <NSpace align="center" :size="6">
              <NText>{{ $t('business.fallback-only') }}</NText>
              <NSwitch v-model:value="filters.fallback_only" size="small" @update:value="() => { decisionPage = 1; loadDecisions(); }" />
            </NSpace>
          </div>

          <NSpin :show="decisionsLoading">
            <NDataTable :columns="decisionColumns" :data="decisions" size="small" :bordered="false" :scroll-x="960" />
            <div class="mt-12px flex justify-end">
              <NPagination
                v-model:page="decisionPage"
                :item-count="decisionsTotal"
                :page-size="decisionPageSize"
                @update:page="loadDecisions"
              />
            </div>
          </NSpin>
        </NTabPane>
      </NTabs>

      <NModal
        v-model:show="inspectVisible"
        preset="card"
        :title="$t('business.decision-snapshot-detail')"
        class="w-860px max-w-95vw"
      >
        <template v-if="inspectedDecision">
          <NDescriptions bordered size="small" :column="2" class="mb-12px">
            <NDescriptionsItem :label="$t('business.sim-effective-strategy')">
              {{ routeStrategyText(inspectedDecision.decision_detail?.effective_strategy ?? inspectedDecision.strategy) }}
            </NDescriptionsItem>
            <NDescriptionsItem :label="$t('business.sim-matched-rule')">
              {{ inspectedDecision.matched_rule_name || '-' }}
            </NDescriptionsItem>
            <NDescriptionsItem
              v-if="inspectedDecision.decision_detail?.orchestrator"
              :label="$t('business.orchestrator-decision')"
              :span="2"
            >
              <div class="flex flex-wrap items-center gap-6px">
                <NTag type="primary" size="small" round>
                  {{
                    $t('business.orchestrator-tier-badge', {
                      tier: inspectedDecision.decision_detail.orchestrator.tier,
                      score: inspectedDecision.decision_detail.orchestrator.score.toFixed(2)
                    })
                  }}
                </NTag>
                <NTag
                  v-if="inspectedDecision.decision_detail.orchestrator.upgraded_from_tier"
                  type="warning"
                  size="small"
                  round
                >
                  {{
                    $t('business.orchestrator-upgraded-from', {
                      from: inspectedDecision.decision_detail.orchestrator.upgraded_from_tier
                    })
                  }}
                </NTag>
                <NTag v-if="inspectedDecision.decision_detail.orchestrator.cache_protected" type="info" size="small" round>
                  {{ $t('business.orchestrator-cache-protected') }}
                </NTag>
                <NTag v-if="inspectedDecision.decision_detail.orchestrator.auto_bucketed" size="small" round>
                  {{ $t('business.orchestrator-auto-bucketed') }}
                </NTag>
              </div>
            </NDescriptionsItem>
            <NDescriptionsItem
              v-if="inspectedDecision.decision_detail?.orchestrator?.signals?.length"
              :label="$t('business.orchestrator-signals')"
              :span="2"
            >
              <div class="flex flex-wrap gap-6px">
                <NTag
                  v-for="sig in inspectedDecision.decision_detail.orchestrator.signals"
                  :key="sig"
                  size="tiny"
                  :bordered="false"
                >
                  {{ sig }}
                </NTag>
              </div>
            </NDescriptionsItem>
          </NDescriptions>
          <NText strong>{{ $t('business.attempt-trail') }}</NText>
          <pre class="my-8px overflow-auto rounded bg-gray-100 p-8px text-12px dark:bg-gray-800">{{
            JSON.stringify(inspectedDecision.attempt_trail?.attempts ?? [], null, 2)
          }}</pre>
          <NText strong>{{ $t('business.candidate-evaluations') }}</NText>
          <pre class="my-8px max-h-280px overflow-auto rounded bg-gray-100 p-8px text-12px dark:bg-gray-800">{{
            JSON.stringify(inspectedDecision.decision_detail?.evaluations ?? [], null, 2)
          }}</pre>
        </template>
      </NModal>
    </NDrawerContent>
  </NDrawer>
</template>
