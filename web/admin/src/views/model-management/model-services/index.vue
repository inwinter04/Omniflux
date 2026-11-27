<script setup lang="ts">
import { h, onMounted, reactive, ref, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { NButton, NDataTable, NDropdown, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, DropdownOption, FormInst, FormItemRule, FormRules } from 'naive-ui';
import {
  formatTime,
  healthStatusText,
  healthSummary,
  healthTagType,
  statusTagType,
  statusText
} from '@/utils/admin-status';
import {
  fetchCreateModelService,
  fetchDeleteModelService,
  fetchModelServices,
  fetchProviderAccounts,
  fetchProviderModels,
  fetchRoutePolicies,
  fetchUpdateModelCandidateStatus,
  fetchUpdateModelService,
  fetchUpdateModelServiceStatus
} from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';
import CandidateEditor from './modules/candidate-editor.vue';
import ModelServiceGrantsModal from './modules/model-service-grants-modal.vue';

defineOptions({ name: 'ModelManagementModelServices' });

const { has } = usePermission();
const canManage = computed(() => has('model_service:manage'));

const route = useRoute();
const router = useRouter();

interface CandidateForm {
  provider_account_id: string | null;
  upstream_model: string;
  priority: number;
  weight: number;
  input_cost_per_million: number;
  output_cost_per_million: number;
  cache_read_cost_per_million: number | null;
  cache_write_cost_per_million: number | null;
  quality_score: number;
  cost_currency: string;
  context_window: number | null;
  capabilities: string[];
  tags: string[];
  status: 'active' | 'disabled';
}

const loading = ref(true);
const list = ref<Api.Management.ModelService[]>([]);
const accountOptions = ref<{ label: string; value: string; currency?: string }[]>([]);
const policyOptions = ref<{ label: string; value: string }[]>([]);
const showCreate = ref(false);
const showEdit = ref(false);
const editTab = ref<'candidates' | 'health' | 'grants'>('candidates');
const editService = ref<Api.Management.ModelService | null>(null);
const grantsVisible = ref(false);
const activeService = ref<Api.Management.ModelService | null>(null);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({
  name: '',
  expose_router: false,
  router_model_id: '',
  route_policy_id: null as string | null,
  candidates: [] as CandidateForm[]
});
const editModel = reactive({
  id: '',
  name: '',
  expose_router: false,
  router_model_id: '',
  route_policy_id: null as string | null,
  candidates: [] as CandidateForm[]
});

function makeRouterModelIDRule(isExposed: () => boolean): FormItemRule {
  return {
    validator: (_rule, value: string) => {
      if (!isExposed()) return true;
      const trimmed = (value ?? '').trim();
      if (!/^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,127}$/.test(trimmed)) {
        return new Error($t('business.router-model-id-invalid-tip'));
      }
      return true;
    },
    trigger: ['input', 'blur']
  };
}

const createFormRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-a-model-service-name'),
    trigger: 'blur'
  },
  router_model_id: makeRouterModelIDRule(() => formModel.expose_router)
}));

const editFormRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-a-model-service-name'),
    trigger: 'blur'
  },
  router_model_id: makeRouterModelIDRule(() => editModel.expose_router)
}));

function handleToggleExposeRouter(model: { expose_router: boolean; router_model_id: string }, val: boolean) {
  model.expose_router = val;
  if (val && !model.router_model_id.trim()) {
    model.router_model_id = 'omniflux/auto';
  }
}

function accountLabel(accountId: string | null) {
  return accountOptions.value.find(item => item.value === accountId)?.label ?? '-';
}

function rowMoreOptions(row: Api.Management.ModelService): DropdownOption[] {
  return [
    {
      label: row.status === 'active' ? $t('business.disabled') : $t('business.enable'),
      key: 'toggle-status'
    },
    { type: 'divider', key: 'divider-danger' },
    {
      label: $t('business.delete'),
      key: 'delete',
      props: { style: { color: '#d03050' } }
    }
  ];
}

function handleRowMoreAction(key: string | number, row: Api.Management.ModelService) {
  if (key === 'toggle-status') {
    if (row.status === 'active') {
      window.$dialog?.warning({
        title: $t('business.disabled'),
        content: $t('business.model-service-disable-confirm'),
        positiveText: $t('common.confirm'),
        negativeText: $t('common.cancel'),
        onPositiveClick: () => toggleServiceStatus(row)
      });
    } else {
      void toggleServiceStatus(row);
    }
    return;
  }
  if (key === 'delete') {
    window.$dialog?.warning({
      title: $t('business.delete'),
      content: $t('business.deletion-is-blocked-when-usage'),
      positiveText: $t('common.confirm'),
      negativeText: $t('common.cancel'),
      onPositiveClick: () => handleDelete(row.id)
    });
  }
}

const candidateColumns = computed<DataTableColumns<Api.Management.ModelCandidate>>(() => [
  {
    title: $t('business.upstream-model'),
    key: 'upstream_model',
    ellipsis: { tooltip: true },
    minWidth: 160
  },
  {
    title: $t('business.provider-account'),
    key: 'provider_account_id',
    minWidth: 180,
    render: row => accountLabel(row.provider_account_id)
  },
  { title: $t('business.priority'), key: 'priority', width: 90 },
  { title: $t('business.weight'), key: 'weight', width: 85 },
  {
    title: $t('business.input-price-million'),
    key: 'input_cost_per_million',
    width: 175,
    render: row =>
      row.cache_read_cost_per_million != null
        ? `${row.input_cost_per_million} ${row.cost_currency} (${$t('business.cache-read-short')}: ${row.cache_read_cost_per_million})`
        : `${row.input_cost_per_million} ${row.cost_currency}`
  },
  {
    title: $t('business.output-price-million'),
    key: 'output_cost_per_million',
    width: 175,
    render: row =>
      row.cache_write_cost_per_million != null
        ? `${row.output_cost_per_million} ${row.cost_currency} (${$t('business.cache-write-short')}: ${row.cache_write_cost_per_million})`
        : `${row.output_cost_per_million} ${row.cost_currency}`
  },
  { title: $t('business.quality-score'), key: 'quality_score', width: 90 },
  {
    title: $t('business.capabilities-and-tags'),
    key: 'metadata',
    minWidth: 190,
    render: row => {
      const items: ReturnType<typeof h>[] = [];
      if (row.context_window) {
        items.push(
          h(NTag, { size: 'tiny', type: 'info', bordered: false }, { default: () => `${Math.round(row.context_window! / 1000)}k` })
        );
      }
      for (const cap of row.capabilities ?? []) {
        items.push(h(NTag, { size: 'tiny', type: 'primary', bordered: false }, { default: () => cap }));
      }
      for (const tag of row.tags ?? []) {
        items.push(h(NTag, { size: 'tiny', bordered: true }, { default: () => tag }));
      }
      return items.length ? h(NSpace, { size: 4 }, { default: () => items }) : '-';
    }
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 100,
    render: row =>
      h(
        NTag,
        { type: statusTagType(row.status), size: 'small', round: true },
        { default: () => statusText(row.status, 'candidate') }
      )
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 95,
    render: row => {
      if (!canManage.value) return '';
      return h(
        NButton,
        {
          size: 'small',
          quaternary: true,
          type: row.status === 'active' ? 'warning' : 'success',
          onClick: () => toggleCandidateStatus(row)
        },
        {
          default: () => (row.status === 'active' ? $t('business.disabled') : $t('business.enable'))
        }
      );
    }
  }
]);

const columns = computed<DataTableColumns<Api.Management.ModelService>>(() => [
  {
    type: 'expand',
    renderExpand(row) {
      return h(NDataTable, {
        columns: candidateColumns.value,
        data: row.candidates,
        size: 'small',
        bordered: false,
        rowKey: (candidate: Api.Management.ModelCandidate) => candidate.id
      });
    }
  },
  { title: $t('business.name'), key: 'name', ellipsis: { tooltip: true }, width: 180 },
  {
    title: $t('business.router-exposure'),
    key: 'router_model_id',
    ellipsis: { tooltip: true },
    width: 170,
    render: row =>
      row.expose_router && row.router_model_id
        ? h(NTag, { type: 'primary', size: 'small', round: true }, { default: () => row.router_model_id })
        : h(NTag, { type: 'default', size: 'small', round: true }, { default: () => $t('business.concrete-models-only-tag') })
  },
  {
    title: $t('business.route-policy'),
    key: 'route_policy_id',
    ellipsis: { tooltip: true },
    width: 160,
    render: row => policyOptions.value.find(item => item.value === row.route_policy_id)?.label ?? '-'
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 90,
    render: row =>
      h(
        NTag,
        { type: statusTagType(row.status), size: 'small', round: true },
        { default: () => statusText(row.status, 'service') }
      )
  },
  {
    title: $t('business.healthy'),
    key: 'health_status',
    width: 180,
    render: row =>
      h('span', { title: healthSummary(row) }, [
        h(
          NTag,
          {
            type: healthTagType(row.health_status),
            size: 'small',
            round: true
          },
          { default: () => healthStatusText(row.health_status) }
        ),
        h(
          'span',
          { class: 'ml-6px text-12px text-#6b7280' },
          $t('business.availableCandidateCount', {
            active: row.active_candidates,
            total: row.candidates.length
          })
        )
      ])
  },
  {
    title: $t('business.candidates'),
    key: 'candidates',
    width: 110,
    render: row => String(row.candidates.length)
  },
  {
    title: $t('business.created-at'),
    key: 'created_at',
    width: 180,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 220,
    // 操作列固定在右侧:列多于一屏宽度时,行内操作不用横向滚动也能点到。
    fixed: 'right',
    render: row => {
      // 只读角色可查看授权范围:授权接口本身允许 viewer 读取。
      if (!canManage.value) {
        return h(
          NButton,
          { size: 'small', quaternary: true, type: 'primary', onClick: () => openGrants(row) },
          { default: () => $t('business.grants') }
        );
      }
      return h(
        NSpace,
        { size: 4 },
        {
          default: () => [
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'primary',
                onClick: () => openEdit(row)
              },
              { default: () => $t('business.edit') }
            ),
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'primary',
                onClick: () => openGrants(row)
              },
              { default: () => $t('business.grants') }
            ),
            h(
              NDropdown,
              {
                trigger: 'click',
                options: rowMoreOptions(row),
                onSelect: key => handleRowMoreAction(key, row)
              },
              {
                default: () => h(NButton, { size: 'small', quaternary: true }, { default: () => $t('business.more') })
              }
            )
          ]
        }
      );
    }
  }
]);

async function loadData() {
  loading.value = true;
  try {
    const [servicesRes, accountsRes, policiesRes] = await Promise.all([
      fetchModelServices(),
      has('provider:read') ? fetchProviderAccounts() : Promise.resolve({ data: null, error: null }),
      has('route_policy:read') ? fetchRoutePolicies() : Promise.resolve({ data: null, error: null })
    ]);
    if (!servicesRes.error && servicesRes.data) list.value = servicesRes.data;
    if (!accountsRes.error && accountsRes.data) {
      accountOptions.value = accountsRes.data
        .slice()
        .sort((a, b) => (a.status === 'active' ? 0 : 1) - (b.status === 'active' ? 0 : 1))
        .map(item => ({
          label: `${item.name}${item.status === 'disabled' ? $t('business.disabled-excglv') : ''}`,
          value: item.id,
          currency: item.billing_currency
        }));
    }
    if (!policiesRes.error && policiesRes.data)
      policyOptions.value = policiesRes.data.map(item => ({
        label: item.name,
        value: item.id
      }));
  } finally {
    loading.value = false;
  }
}

function makeCandidate(): CandidateForm {
  return {
    provider_account_id: accountOptions.value[0]?.value ?? null,
    upstream_model: '',
    priority: 100,
    weight: 1,
    input_cost_per_million: 0,
    output_cost_per_million: 0,
    cache_read_cost_per_million: null,
    cache_write_cost_per_million: null,
    quality_score: 0.5,
    cost_currency: accountOptions.value[0]?.currency ?? 'USD',
    context_window: null,
    capabilities: [],
    tags: [],
    status: 'active'
  };
}

function openCreate() {
  formModel.name = '';
  formModel.expose_router = false;
  formModel.router_model_id = '';
  formModel.route_policy_id = null;
  formModel.candidates = [makeCandidate()];
  showCreate.value = true;
}

async function openDiscoveredModels() {
  const accountID = typeof route.query.account === 'string' ? route.query.account : '';
  const modelIDs = (typeof route.query.models === 'string' ? route.query.models : '')
    .split(',')
    .filter(Boolean)
    .slice(0, 50);
  if (!accountID || !modelIDs.length) return;
  const { data } = await fetchProviderModels(accountID);
  const discovered = new Map((data?.models ?? []).map(item => [item.id, item]));
  formModel.name =
    modelIDs.length === 1 ? modelIDs[0] : $t('business.accountService', { account: accountLabel(accountID) });
  formModel.expose_router = false;
  formModel.router_model_id = '';
  formModel.route_policy_id = null;
  formModel.candidates = modelIDs.map(id => {
    const model = discovered.get(id);
    return {
      provider_account_id: accountID,
      upstream_model: id,
      priority: 100,
      weight: 1,
      input_cost_per_million:
        (accountOptions.value.find(item => item.value === accountID)?.currency ?? 'USD') === 'USD'
          ? (model?.input_cost_per_million ?? 0)
          : 0,
      output_cost_per_million:
        (accountOptions.value.find(item => item.value === accountID)?.currency ?? 'USD') === 'USD'
          ? (model?.output_cost_per_million ?? 0)
          : 0,
      cache_read_cost_per_million: null,
      cache_write_cost_per_million: null,
      quality_score: 0.5,
      cost_currency: accountOptions.value.find(item => item.value === accountID)?.currency ?? 'USD',
      context_window: null,
      capabilities: [],
      tags: [],
      status: 'active'
    };
  });
  showCreate.value = true;
  await router.replace({ query: {} });
}

function openEdit(row: Api.Management.ModelService) {
  editService.value = row;
  editTab.value = 'candidates';
  editModel.id = row.id;
  editModel.name = row.name;
  editModel.expose_router = Boolean(row.expose_router);
  editModel.router_model_id = row.router_model_id ?? '';
  editModel.route_policy_id = row.route_policy_id;
  editModel.candidates = row.candidates.map(item => ({
    provider_account_id: item.provider_account_id,
    upstream_model: item.upstream_model,
    priority: item.priority,
    weight: item.weight,
    input_cost_per_million: item.input_cost_per_million,
    output_cost_per_million: item.output_cost_per_million,
    cache_read_cost_per_million: item.cache_read_cost_per_million ?? null,
    cache_write_cost_per_million: item.cache_write_cost_per_million ?? null,
    quality_score: item.quality_score,
    cost_currency: item.cost_currency,
    context_window: item.context_window ?? null,
    capabilities: [...(item.capabilities ?? [])],
    tags: [...(item.tags ?? [])],
    status: item.status === 'disabled' ? 'disabled' : 'active'
  }));
  showEdit.value = true;
}

function validateCandidates(candidates: CandidateForm[]) {
  if (!candidates.length) {
    window.$message?.error($t('business.at-least-one-model-candidate'));
    return false;
  }
  if (candidates.some(item => !item.provider_account_id || !item.upstream_model.trim())) {
    window.$message?.error($t('business.every-candidate-needs-a-provider'));
    return false;
  }
  return true;
}

function toCandidateParams(candidates: CandidateForm[]) {
  return candidates.map(item => ({
    provider_account_id: item.provider_account_id!,
    upstream_model: item.upstream_model.trim(),
    priority: item.priority,
    weight: item.weight,
    input_cost_per_million: item.input_cost_per_million,
    output_cost_per_million: item.output_cost_per_million,
    cache_read_cost_per_million: item.cache_read_cost_per_million,
    cache_write_cost_per_million: item.cache_write_cost_per_million,
    quality_score: item.quality_score,
    cost_currency: item.cost_currency,
    context_window: item.context_window,
    capabilities: item.capabilities,
    tags: item.tags,
    status: item.status
  }));
}

async function handleCreate() {
  await formRef.value?.validate();
  if (!validateCandidates(formModel.candidates)) return;
  submitting.value = true;
  try {
    const { error } = await fetchCreateModelService({
      name: formModel.name.trim(),
      expose_router: formModel.expose_router,
      router_model_id: formModel.expose_router ? formModel.router_model_id.trim() : null,
      route_policy_id: formModel.route_policy_id,
      candidates: toCandidateParams(formModel.candidates)
    });
    if (!error) {
      window.$message?.success($t('business.model-service-created'));
      showCreate.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function handleUpdate() {
  await editFormRef.value?.validate();
  if (!validateCandidates(editModel.candidates)) return;
  submitting.value = true;
  try {
    const { error } = await fetchUpdateModelService(editModel.id, {
      name: editModel.name.trim(),
      expose_router: editModel.expose_router,
      router_model_id: editModel.expose_router ? editModel.router_model_id.trim() : null,
      route_policy_id: editModel.route_policy_id,
      candidates: toCandidateParams(editModel.candidates)
    });
    if (!error) {
      window.$message?.success($t('business.model-service-updated'));
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

function openGrants(row: Api.Management.ModelService) {
  activeService.value = row;
  grantsVisible.value = true;
}

async function toggleServiceStatus(row: Api.Management.ModelService) {
  const status = row.status === 'active' ? 'disabled' : 'active';
  const { error } = await fetchUpdateModelServiceStatus(row.id, status);
  if (!error) {
    window.$message?.success(
      status === 'active' ? $t('business.model-service-enabled') : $t('business.model-service-disabled')
    );
    await loadData();
  }
}

async function toggleCandidateStatus(row: Api.Management.ModelCandidate) {
  const status = row.status === 'active' ? 'disabled' : 'active';
  const { error } = await fetchUpdateModelCandidateStatus(row.id, status);
  if (!error) {
    window.$message?.success(
      status === 'active' ? $t('business.candidate-model-enabled') : $t('business.candidate-model-disabled')
    );
    await loadData();
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteModelService(id);
  if (!error) {
    window.$message?.success($t('business.model-service-deleted'));
    await loadData();
  }
}

onMounted(async () => {
  await loadData();
  const serviceID = typeof route.query.service === 'string' ? route.query.service : '';
  if (serviceID) {
    const service = list.value.find(item => item.id === serviceID);
    if (service) {
      // 审计里的授权事件带 grants=1,直接打开授权弹窗(标题已带服务名);
      // 不叠开编辑抽屉:同帧挂载时抽屉层级更高,会把弹窗盖住。
      if (route.query.grants === '1') openGrants(service);
      else openEdit(service);
    }
  }
  await openDiscoveredModels();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :title="$t('business.model-service')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton v-if="canManage" type="primary" :disabled="accountOptions.length === 0" @click="openCreate">
            {{ accountOptions.length === 0 ? $t('business.createProviderFirst') : $t('business.create-model-service') }}
          </NButton>
        </NSpace>
      </template>
      <NDataTable
        :columns="columns"
        :data="list"
        :loading="loading"
        :bordered="false"
        :scroll-x="1100"
        :row-key="row => row.id"
      />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-model-service')" class="w-1340px">
      <NForm ref="formRef" :model="formModel" :rules="createFormRules" label-placement="top">
        <NGrid :cols="24" :x-gap="16">
          <NFormItemGi :span="12" :label="$t('business.service-name')" path="name">
            <NInput v-model:value="formModel.name" :placeholder="$t('business.logical-model-name-used-by')" />
          </NFormItemGi>
          <NFormItemGi :span="12" :label="$t('business.route-policy')">
            <NSelect
              v-model:value="formModel.route_policy_id"
              :options="policyOptions"
              clearable
              :placeholder="$t('business.optional-leave-unselected-to-use')"
            />
          </NFormItemGi>
          <NFormItemGi :span="12" :label="$t('business.expose-router')">
            <div>
              <NSwitch
                :value="formModel.expose_router"
                @update:value="val => handleToggleExposeRouter(formModel, val)"
              />
              <NText depth="3" class="mt-4px block text-12px">
                {{ $t('business.expose-router-hint') }}
              </NText>
            </div>
          </NFormItemGi>
          <NFormItemGi v-if="formModel.expose_router" :span="12" :label="$t('business.router-model-id')" path="router_model_id">
            <NInput
              v-model:value="formModel.router_model_id"
              :placeholder="$t('business.router-model-id-placeholder')"
            />
          </NFormItemGi>
        </NGrid>
        <NDivider title-placement="left">{{ $t('business.candidate-models') }}</NDivider>
        <CandidateEditor v-model:candidates="formModel.candidates" :account-options="accountOptions" />
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">{{ $t('business.create') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <ModelServiceGrantsModal v-model:show="grantsVisible" :service="activeService" />
    <NDrawer v-model:show="showEdit" :width="1340" placement="right">
      <NDrawerContent :title="$t('business.edit-model-service')" closable>
        <NTabs v-model:value="editTab" type="line" animated>
          <NTabPane name="candidates" :tab="$t('business.candidate-models')">
            <NForm ref="editFormRef" :model="editModel" :rules="editFormRules" label-placement="top">
              <NGrid :cols="24" :x-gap="16">
                <NFormItemGi :span="12" :label="$t('business.service-name')" path="name">
                  <NInput v-model:value="editModel.name" />
                </NFormItemGi>
                <NFormItemGi :span="12" :label="$t('business.route-policy')">
                  <NSelect
                    v-model:value="editModel.route_policy_id"
                    :options="policyOptions"
                    clearable
                    :placeholder="$t('business.optional-leave-unselected-to-use')"
                  />
                </NFormItemGi>
                <NFormItemGi :span="12" :label="$t('business.expose-router')">
                  <div>
                    <NSwitch
                      :value="editModel.expose_router"
                      @update:value="val => handleToggleExposeRouter(editModel, val)"
                    />
                    <NText depth="3" class="mt-4px block text-12px">
                      {{ $t('business.expose-router-hint') }}
                    </NText>
                  </div>
                </NFormItemGi>
                <NFormItemGi
                  v-if="editModel.expose_router"
                  :span="12"
                  :label="$t('business.router-model-id')"
                  path="router_model_id"
                >
                  <NInput
                    v-model:value="editModel.router_model_id"
                    :placeholder="$t('business.router-model-id-placeholder')"
                  />
                </NFormItemGi>
              </NGrid>
              <NDivider title-placement="left">{{ $t('business.candidate-models-all-replaced-after') }}</NDivider>
              <CandidateEditor v-model:candidates="editModel.candidates" :account-options="accountOptions" />
            </NForm>
          </NTabPane>
          <NTabPane name="health" :tab="$t('business.healthAndUsage')">
            <NDescriptions v-if="editService" :column="2" bordered size="small" label-placement="left">
              <NDescriptionsItem :label="$t('business.status')">
                <NTag :type="statusTagType(editService?.status)" size="small" round>
                  {{ statusText(editService?.status, 'service') }}
                </NTag>
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.healthy')">
                <NTag :type="healthTagType(editService?.health_status)" size="small" round>
                  {{ healthStatusText(editService?.health_status) }}
                </NTag>
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.route-policy')">
                {{ policyOptions.find(item => item.value === editService?.route_policy_id)?.label ?? '-' }}
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.activeCandidates')">
                {{ editService?.active_candidates }} / {{ editService?.candidates.length }}
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.requests24h')">
                {{ editService?.requests_24h }}
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.successRate24h')">
                {{ editService?.success_rate_24h != null ? `${Math.round(editService?.success_rate_24h * 100)}%` : '-' }}
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.avgLatency24h')">
                {{ editService?.avg_latency_ms_24h != null ? `${Math.round(editService?.avg_latency_ms_24h)} ms` : '-' }}
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.last-used')">
                {{ formatTime(editService?.last_used_at) }}
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.created-at')" :span="2">
                {{ formatTime(editService?.created_at) }}
              </NDescriptionsItem>
            </NDescriptions>
          </NTabPane>
          <NTabPane name="grants" :tab="$t('business.grants')">
            <NAlert type="info" :bordered="false" class="mb-12px">{{ $t('business.modelGrantNotice') }}</NAlert>
            <NButton type="primary" @click="editService ? openGrants(editService) : undefined">
              {{ $t('business.grants') }}
            </NButton>
          </NTabPane>
        </NTabs>
        <template #footer>
          <NSpace justify="end">
            <NButton @click="showEdit = false">{{ $t('business.cancel') }}</NButton>
            <NButton type="primary" :loading="submitting" @click="handleUpdate">{{ $t('business.save') }}</NButton>
          </NSpace>
        </template>
      </NDrawerContent>
    </NDrawer>
  </div>
</template>
