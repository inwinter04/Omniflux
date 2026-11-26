<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { NButton, NDropdown, NSpace, NTag, useDialog } from 'naive-ui';
import type { DataTableColumns, DropdownOption, SelectGroupOption, SelectOption } from 'naive-ui';
import {
  formatTime,
  routeStrategyTagType,
  routeStrategyText,
  statusTagType,
  statusText
} from '@/utils/admin-status';
import { providerGroupLabel } from '@/utils/provider-catalog';
import {
  fetchCloneRoutePolicy,
  fetchDeleteRoutePolicy,
  fetchModelServices,
  fetchProviderCatalog,
  fetchRoutePolicies,
  fetchUpdateRoutePolicyStatus
} from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';
import { useStrategyOptions, type PolicyDraftModel } from './modules/policy-shared';
import PolicyEditorDrawer from './modules/policy-editor-drawer.vue';
import PolicySimulatorModal from './modules/policy-simulator-modal.vue';
import PolicyPlaygroundModal from './modules/policy-playground-modal.vue';
import PolicyDetailDrawer from './modules/policy-detail-drawer.vue';

defineOptions({
  name: 'ModelManagementRoutePolicies'
});

const router = useRouter();
const dialog = useDialog();
const { has } = usePermission();
const canManage = computed(() => has('route_policy:manage'));

const loading = ref(true);
const policies = ref<Api.Management.RoutePolicy[]>([]);
const modelServices = ref<Api.Management.ModelService[]>([]);
const providerOptions = ref<(SelectOption | SelectGroupOption)[]>([
  { label: 'OpenAI', value: 'openai' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'Gemini', value: 'gemini' },
  { label: 'OpenAI Compatible', value: 'openai_compatible' }
]);

const searchText = ref('');
const strategyFilter = ref<string | null>(null);
const statusFilter = ref<string | null>(null);

const strategyOptions = useStrategyOptions();
const statusOptions = computed<SelectOption[]>(() => [
  { label: $t('business.enable'), value: 'active' },
  { label: $t('business.disabled-e5ola'), value: 'disabled' }
]);

const editorShow = ref(false);
const editorMode = ref<'create' | 'edit'>('create');
const editorPolicy = ref<Api.Management.RoutePolicy | null>(null);

const simulatorShow = ref(false);
const simulatorPolicyId = ref<string | null>(null);
const simulatorDraft = ref<PolicyDraftModel | null>(null);

const playgroundShow = ref(false);
const playgroundPolicy = ref<Api.Management.RoutePolicy | null>(null);

const detailShow = ref(false);
const detailPolicy = ref<Api.Management.RoutePolicy | null>(null);

const filteredPolicies = computed(() =>
  policies.value.filter(item => {
    if (searchText.value.trim()) {
      const q = searchText.value.trim().toLowerCase();
      const matchName = item.name.toLowerCase().includes(q);
      const matchSvc = (item.bound_services ?? []).some(s => s.name.toLowerCase().includes(q));
      if (!matchName && !matchSvc) return false;
    }
    if (strategyFilter.value && item.strategy !== strategyFilter.value) return false;
    if (statusFilter.value && item.status !== statusFilter.value) return false;
    return true;
  })
);

const kpiSummary = computed(() => {
  const total = policies.value.length;
  const active = policies.value.filter(p => p.status === 'active').length;
  const boundSvcSet = new Set<string>();
  let req24h = 0;
  let weightedFallback = 0;
  for (const p of policies.value) {
    for (const s of p.bound_services ?? []) boundSvcSet.add(s.id);
    req24h += p.requests_24h ?? 0;
    if (p.requests_24h && p.fallback_rate_24h !== null && p.fallback_rate_24h !== undefined) {
      weightedFallback += p.requests_24h * p.fallback_rate_24h;
    }
  }
  const fbPct = req24h > 0 ? `${Math.round((weightedFallback / req24h) * 1000) / 10}%` : '-';
  return { total, active, boundServices: boundSvcSet.size, req24h, fbPct };
});

async function loadCatalog() {
  if (!has('provider:read')) return;
  const { data, error } = await fetchProviderCatalog();
  if (error || !data) return;
  const options: (SelectOption | SelectGroupOption)[] = [];
  for (const group of data) {
    if (group.group === 'custom') continue;
    options.push({
      type: 'group',
      label: providerGroupLabel(group),
      key: group.group,
      children: group.entries.map(entry => ({ label: entry.name, value: entry.id }))
    });
  }
  options.push({
    type: 'group',
    label: $t('business.custom'),
    key: 'custom',
    children: [{ label: $t('business.custom-openai-compatible'), value: 'openai_compatible' }]
  });
  providerOptions.value = options;
}

async function loadData() {
  loading.value = true;
  try {
    const [polRes, svcRes] = await Promise.all([
      fetchRoutePolicies(),
      has('model_service:read') ? fetchModelServices() : Promise.resolve({ data: null, error: null })
    ]);
    if (!polRes.error && polRes.data) {
      policies.value = polRes.data;
    }
    if (!svcRes.error && svcRes.data) {
      modelServices.value = svcRes.data;
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  editorMode.value = 'create';
  editorPolicy.value = null;
  editorShow.value = true;
}

function openEdit(row: Api.Management.RoutePolicy) {
  editorMode.value = 'edit';
  editorPolicy.value = row;
  editorShow.value = true;
}

function openDetail(row: Api.Management.RoutePolicy) {
  detailPolicy.value = row;
  detailShow.value = true;
}

function openSimulateForPolicy(policyId: string | null) {
  simulatorDraft.value = null;
  simulatorPolicyId.value = policyId;
  simulatorShow.value = true;
}

function openSimulateForDraft(draft: PolicyDraftModel) {
  simulatorDraft.value = draft;
  simulatorPolicyId.value = null;
  simulatorShow.value = true;
}

function openPlayground(row: Api.Management.RoutePolicy) {
  playgroundPolicy.value = row;
  playgroundShow.value = true;
}

async function handleClone(row: Api.Management.RoutePolicy) {
  const { error } = await fetchCloneRoutePolicy(row.id);
  if (!error) {
    window.$message?.success($t('business.route-policy-cloned'));
    await loadData();
  }
}

async function handleDelete(row: Api.Management.RoutePolicy) {
  const { error } = await fetchDeleteRoutePolicy(row.id);
  if (!error) {
    window.$message?.success($t('business.route-policy-deleted'));
    await loadData();
  }
}

async function handleStatusChange(row: Api.Management.RoutePolicy, status: 'active' | 'disabled') {
  const { error } = await fetchUpdateRoutePolicyStatus(row.id, status);
  if (!error) {
    window.$message?.success(
      status === 'active' ? $t('business.route-policy-enabled') : $t('business.route-policy-disabled')
    );
    await loadData();
  }
}

function confirmDelete(row: Api.Management.RoutePolicy) {
  dialog.warning({
    title: $t('common.tip'),
    content: $t('business.deletion-is-blocked-when-the'),
    positiveText: $t('common.confirm'),
    negativeText: $t('common.cancel'),
    onPositiveClick: () => handleDelete(row)
  });
}

function moreActionOptions(row: Api.Management.RoutePolicy): DropdownOption[] {
  return [
    { label: $t('business.edit'), key: 'edit' },
    { label: $t('business.playground'), key: 'playground', disabled: row.status !== 'active' },
    { label: $t('business.clone-policy'), key: 'clone' },
    { type: 'divider', key: 'divider-lifecycle' },
    { label: row.status === 'active' ? $t('business.disabled') : $t('business.enable'), key: 'toggle-status' },
    {
      label: $t('business.delete'),
      key: 'delete',
      props: { style: { color: '#d03050' } }
    }
  ];
}

function handleMoreAction(row: Api.Management.RoutePolicy, key: string) {
  if (key === 'edit') {
    openEdit(row);
    return;
  }
  if (key === 'playground') {
    openPlayground(row);
    return;
  }
  if (key === 'clone') {
    void handleClone(row);
    return;
  }
  if (key === 'toggle-status') {
    const nextStatus: 'active' | 'disabled' = row.status === 'active' ? 'disabled' : 'active';
    dialog.warning({
      title: $t('common.tip'),
      content:
        nextStatus === 'disabled'
          ? $t('business.route-policy-disable-confirm')
          : $t('business.route-policy-enable-confirm'),
      positiveText: $t('common.confirm'),
      negativeText: $t('common.cancel'),
      onPositiveClick: () => handleStatusChange(row, nextStatus)
    });
    return;
  }
  if (key === 'delete') confirmDelete(row);
}

const columns = computed<DataTableColumns<Api.Management.RoutePolicy>>(() => [
  {
    title: $t('business.policy-name'),
    key: 'name',
    minWidth: 190,
    render: row =>
      h(NSpace, { align: 'center', size: 6 }, () => [
        h(
          NButton,
          { text: true, type: 'primary', onClick: () => openDetail(row) },
          { default: () => row.name }
        ),
        h(NTag, { size: 'tiny', bordered: false }, { default: () => `v${row.version}` })
      ])
  },
  {
    title: $t('business.strategy'),
    key: 'strategy',
    width: 170,
    render: row =>
      h(NSpace, { size: 4, align: 'center' }, () => [
        h(
          NTag,
          { size: 'small', type: routeStrategyTagType(row.strategy), round: true },
          { default: () => routeStrategyText(row.strategy) }
        ),
        h(NTag, { size: 'tiny', bordered: false, type: 'info' }, { default: () => row.compare_currency || 'USD' })
      ])
  },
  {
    title: $t('business.bound-services'),
    key: 'bound_services',
    minWidth: 180,
    render: row => {
      const svcs = row.bound_services ?? [];
      if (!svcs.length) return '-';
      return h(NSpace, { size: 4 }, () =>
        svcs.map(svc =>
          h(
            NTag,
            {
              size: 'tiny',
              bordered: true,
              class: 'cursor-pointer',
              onClick: () => router.push({ name: 'model-management_model-services' })
            },
            { default: () => `${svc.name} (${svc.candidate_count})` }
          )
        )
      );
    }
  },
  {
    title: $t('business.policy-24h-metrics'),
    key: 'requests_24h',
    width: 220,
    render: row => {
      if (!row.requests_24h) return $t('business.no-calls-in-the-last');
      const fb =
        row.fallback_rate_24h !== null && row.fallback_rate_24h !== undefined
          ? `${Math.round(row.fallback_rate_24h * 1000) / 10}%`
          : '-';
      const ttft =
        row.avg_ttft_ms_24h !== null && row.avg_ttft_ms_24h !== undefined
          ? `${Math.round(row.avg_ttft_ms_24h)} ms`
          : '-';
      return $t('business.policy-24h-summary', { requests: row.requests_24h, fallback: fb, ttft });
    }
  },
  {
    title: $t('business.rules-and-resilience'),
    key: 'rules',
    width: 175,
    render: row =>
      $t('business.policy-resilience-summary', {
        rules: row.rules?.length ?? 0,
        cb: row.circuit_breaker?.enabled ? $t('business.on') : $t('business.off'),
        fallbackLimit: row.fallback_limit
      })
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 105,
    render: row =>
      h(
        NTag,
        { size: 'small', type: statusTagType(row.status), round: true },
        { default: () => statusText(row.status, 'policy') }
      )
  },
  {
    title: $t('business.updated-at'),
    key: 'updated_at',
    width: 165,
    render: row => formatTime(row.updated_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 220,
    fixed: 'right',
    render: row =>
      h(NSpace, { size: 4 }, () => [
        h(NButton, { size: 'small', quaternary: true, onClick: () => openDetail(row) }, { default: () => $t('business.details') }),
        h(
          NButton,
          { size: 'small', quaternary: true, onClick: () => openSimulateForPolicy(row.id) },
          { default: () => $t('business.simulate') }
        ),
        ...(canManage.value
          ? [
              h(
                NDropdown,
                {
                  trigger: 'click',
                  options: moreActionOptions(row),
                  onSelect: (key: string | number) => handleMoreAction(row, String(key))
                },
                {
                  default: () =>
                    h(NButton, { size: 'small', quaternary: true }, { default: () => $t('business.more') })
                }
              )
            ]
          : [])
      ])
  }
]);

onMounted(() => {
  void loadCatalog();
  void loadData();
});
</script>

<template>
  <div class="flex flex-col gap-16px">
    <NGrid :cols="4" :x-gap="12">
      <NGi>
        <NCard size="small" :bordered="false" class="card-wrapper">
          <NStatistic :label="$t('business.total-policies')" :value="kpiSummary.total" />
        </NCard>
      </NGi>
      <NGi>
        <NCard size="small" :bordered="false" class="card-wrapper">
          <NStatistic :label="$t('business.active-policies')" :value="kpiSummary.active" />
        </NCard>
      </NGi>
      <NGi>
        <NCard size="small" :bordered="false" class="card-wrapper">
          <NStatistic :label="$t('business.bound-services')" :value="kpiSummary.boundServices" />
        </NCard>
      </NGi>
      <NGi>
        <NCard size="small" :bordered="false" class="card-wrapper">
          <NStatistic :label="$t('business.requests-and-fallback-24h')" :value="`${kpiSummary.req24h} / ${kpiSummary.fbPct}`" />
        </NCard>
      </NGi>
    </NGrid>

    <NCard :title="$t('route.model-management_route-policies')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace wrap>
          <NInput
            v-model:value="searchText"
            :placeholder="$t('business.search-policy-placeholder')"
            size="small"
            clearable
            class="min-w-200px w-220px"
          />
          <NSelect
            v-model:value="strategyFilter"
            :options="strategyOptions"
            :placeholder="$t('business.strategy')"
            size="small"
            clearable
            class="min-w-150px w-170px"
          />
          <NSelect
            v-model:value="statusFilter"
            :options="statusOptions"
            :placeholder="$t('business.status')"
            size="small"
            clearable
            class="min-w-120px w-140px"
          />
          <NButton size="small" secondary @click="openSimulateForPolicy(policies[0]?.id ?? null)">
            {{ $t('business.dry-run-simulator') }}
          </NButton>
          <NButton v-if="canManage" type="primary" size="small" @click="openCreate">
            {{ $t('business.create-policy') }}
          </NButton>
        </NSpace>
      </template>

      <NDataTable
        :columns="columns"
        :data="filteredPolicies"
        :loading="loading"
        :bordered="false"
        :scroll-x="1380"
        :row-key="(row: Api.Management.RoutePolicy) => row.id"
      />
    </NCard>

    <PolicyEditorDrawer
      v-model:show="editorShow"
      :mode="editorMode"
      :policy="editorPolicy"
      :provider-options="providerOptions"
      :model-services="modelServices"
      @saved="loadData"
      @simulate-draft="openSimulateForDraft"
    />

    <PolicySimulatorModal
      v-model:show="simulatorShow"
      :policies="policies"
      :model-services="modelServices"
      :initial-policy-id="simulatorPolicyId"
      :draft-override="simulatorDraft"
    />

    <PolicyPlaygroundModal
      v-model:show="playgroundShow"
      :policy="playgroundPolicy"
      :model-services="modelServices"
    />

    <PolicyDetailDrawer v-model:show="detailShow" :policy="detailPolicy" />
  </div>
</template>
