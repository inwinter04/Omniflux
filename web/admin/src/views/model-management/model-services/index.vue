<script setup lang="ts">
import { h, onMounted, reactive, ref, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { NButton, NDataTable, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
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
import CandidateEditor from './modules/candidate-editor.vue';
import ModelServiceGrantsModal from './modules/model-service-grants-modal.vue';

defineOptions({ name: 'ModelManagementModelServices' });

const route = useRoute();
const router = useRouter();

interface CandidateForm {
  provider_account_id: string | null;
  upstream_model: string;
  priority: number;
  weight: number;
  input_cost_per_million: number;
  output_cost_per_million: number;
  quality_score: number;
  cost_currency: string;
  status: 'active' | 'disabled';
}

const loading = ref(true);
const list = ref<Api.Management.ModelService[]>([]);
const accountOptions = ref<{ label: string; value: string; currency?: string }[]>([]);
const policyOptions = ref<{ label: string; value: string }[]>([]);
const showCreate = ref(false);
const showEdit = ref(false);
const grantsVisible = ref(false);
const activeService = ref<Api.Management.ModelService | null>(null);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({
  name: '',
  route_policy_id: null as string | null,
  candidates: [] as CandidateForm[]
});
const editModel = reactive({
  id: '',
  name: '',
  route_policy_id: null as string | null,
  candidates: [] as CandidateForm[]
});
const formRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-a-model-service-name'),
    trigger: 'blur'
  }
}));

function accountLabel(accountId: string | null) {
  return accountOptions.value.find(item => item.value === accountId)?.label ?? '-';
}

const candidateColumns = computed<DataTableColumns<Api.Management.ModelCandidate>>(() => [
  {
    title: $t('business.upstream-model'),
    key: 'upstream_model',
    minWidth: 160
  },
  {
    title: $t('business.provider-account'),
    key: 'provider_account_id',
    minWidth: 180,
    render: row => accountLabel(row.provider_account_id)
  },
  { title: $t('business.priority'), key: 'priority', width: 80 },
  { title: $t('business.weight'), key: 'weight', width: 70 },
  {
    title: $t('business.input-price-million'),
    key: 'input_cost_per_million',
    width: 150,
    render: row => `${row.input_cost_per_million} ${row.cost_currency}`
  },
  {
    title: $t('business.output-price-million'),
    key: 'output_cost_per_million',
    width: 150,
    render: row => `${row.output_cost_per_million} ${row.cost_currency}`
  },
  { title: $t('business.quality-score'), key: 'quality_score', width: 80 },
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
    width: 80,
    render: row =>
      h(
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
      )
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
  { title: $t('business.name'), key: 'name', width: 180 },
  {
    title: $t('business.route-policy'),
    key: 'route_policy_id',
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
    width: 90,
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
    width: 300,
    render: row =>
      h(
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
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: row.status === 'active' ? 'warning' : 'success',
                onClick: () => toggleServiceStatus(row)
              },
              {
                default: () => (row.status === 'active' ? $t('business.disabled') : $t('business.enable'))
              }
            ),
            h(
              NPopconfirm,
              { onPositiveClick: () => handleDelete(row.id) },
              {
                trigger: () =>
                  h(
                    NButton,
                    { size: 'small', type: 'error', quaternary: true },
                    { default: () => $t('business.delete') }
                  ),
                default: () => $t('business.deletion-is-blocked-when-usage')
              }
            )
          ]
        }
      )
  }
]);

async function loadData() {
  loading.value = true;
  try {
    const [servicesRes, accountsRes, policiesRes] = await Promise.all([
      fetchModelServices(),
      fetchProviderAccounts(),
      fetchRoutePolicies()
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
    quality_score: 0.5,
    cost_currency: accountOptions.value[0]?.currency ?? 'USD',
    status: 'active'
  };
}

function openCreate() {
  formModel.name = '';
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
      quality_score: 0.5,
      cost_currency: accountOptions.value.find(item => item.value === accountID)?.currency ?? 'USD',
      status: 'active'
    };
  });
  showCreate.value = true;
  await router.replace({ query: {} });
}

function openEdit(row: Api.Management.ModelService) {
  editModel.id = row.id;
  editModel.name = row.name;
  editModel.route_policy_id = row.route_policy_id;
  editModel.candidates = row.candidates.map(item => ({
    provider_account_id: item.provider_account_id,
    upstream_model: item.upstream_model,
    priority: item.priority,
    weight: item.weight,
    input_cost_per_million: item.input_cost_per_million,
    output_cost_per_million: item.output_cost_per_million,
    quality_score: item.quality_score,
    cost_currency: item.cost_currency,
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
    quality_score: item.quality_score,
    cost_currency: item.cost_currency,
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
  await openDiscoveredModels();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :title="$t('business.model-service')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton type="primary" :disabled="accountOptions.length === 0" @click="openCreate">
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

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-model-service')" class="w-1200px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem :label="$t('business.service-name')" path="name">
          <NInput v-model:value="formModel.name" :placeholder="$t('business.logical-model-name-used-by')" />
        </NFormItem>
        <NFormItem :label="$t('business.route-policy')">
          <NSelect
            v-model:value="formModel.route_policy_id"
            :options="policyOptions"
            clearable
            :placeholder="$t('business.optional-leave-unselected-to-use')"
          />
        </NFormItem>
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
    <NModal v-model:show="showEdit" preset="card" :title="$t('business.edit-model-service')" class="w-1200px">
      <NForm ref="editFormRef" :model="editModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem :label="$t('business.service-name')" path="name">
          <NInput v-model:value="editModel.name" />
        </NFormItem>
        <NFormItem :label="$t('business.route-policy')">
          <NSelect
            v-model:value="editModel.route_policy_id"
            :options="policyOptions"
            clearable
            :placeholder="$t('business.optional-leave-unselected-to-use')"
          />
        </NFormItem>
        <NDivider title-placement="left">{{ $t('business.candidate-models-all-replaced-after') }}</NDivider>
        <CandidateEditor v-model:candidates="editModel.candidates" :account-options="accountOptions" />
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showEdit = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleUpdate">{{ $t('business.save') }}</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>
