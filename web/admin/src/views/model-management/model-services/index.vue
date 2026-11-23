<script setup lang="ts">
import { h, onMounted, reactive, ref } from 'vue';
import { NButton, NDataTable, NPopconfirm, NSpace, NTag } from 'naive-ui';
import { useRoute, useRouter } from 'vue-router';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
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
import { formatTime, healthStatusText, healthSummary, healthTagType, statusTagType, statusText } from '@/utils/admin-status';
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
const formModel = reactive({ name: '', route_policy_id: null as string | null, candidates: [] as CandidateForm[] });
const editModel = reactive({ id: '', name: '', route_policy_id: null as string | null, candidates: [] as CandidateForm[] });
const formRules: FormRules = { name: { required: true, message: '请填写模型服务名称', trigger: 'blur' } };

function accountLabel(accountId: string | null) {
  return accountOptions.value.find(item => item.value === accountId)?.label ?? '-';
}

const candidateColumns: DataTableColumns<Api.Management.ModelCandidate> = [
  { title: '上游模型', key: 'upstream_model', minWidth: 160 },
  { title: '供应商账号', key: 'provider_account_id', minWidth: 180, render: row => accountLabel(row.provider_account_id) },
  { title: '优先级', key: 'priority', width: 80 },
  { title: '权重', key: 'weight', width: 70 },
  { title: '输入价格/百万', key: 'input_cost_per_million', width: 150, render: row => `${row.input_cost_per_million} ${row.cost_currency}` },
  { title: '输出价格/百万', key: 'output_cost_per_million', width: 150, render: row => `${row.output_cost_per_million} ${row.cost_currency}` },
  { title: '质量分', key: 'quality_score', width: 80 },
  { title: '状态', key: 'status', width: 100, render: row => h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status, 'candidate') }) },
  {
    title: '操作', key: 'actions', width: 80,
    render: row => h(NButton, {
      size: 'small', quaternary: true, type: row.status === 'active' ? 'warning' : 'success',
      onClick: () => toggleCandidateStatus(row)
    }, { default: () => row.status === 'active' ? '停用' : '启用' })
  }
];

const columns: DataTableColumns<Api.Management.ModelService> = [
  {
    type: 'expand',
    renderExpand(row) {
      return h(NDataTable, {
        columns: candidateColumns, data: row.candidates, size: 'small', bordered: false,
        rowKey: (candidate: Api.Management.ModelCandidate) => candidate.id
      });
    }
  },
  { title: '名称', key: 'name', width: 180 },
  { title: '路由策略', key: 'route_policy_id', width: 160, render: row => policyOptions.value.find(item => item.value === row.route_policy_id)?.label ?? '-' },
  { title: '状态', key: 'status', width: 90, render: row => h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status, 'service') }) },
  {
    title: '健康', key: 'health_status', width: 180,
    render: row => h('span', { title: healthSummary(row) }, [
      h(NTag, { type: healthTagType(row.health_status), size: 'small', round: true }, { default: () => healthStatusText(row.health_status) }),
      h('span', { class: 'ml-6px text-12px text-#6b7280' }, `${row.active_candidates}/${row.candidates.length} 个可用候选`)
    ])
  },
  { title: '候选数', key: 'candidates', width: 90, render: row => String(row.candidates.length) },
  { title: '创建时间', key: 'created_at', width: 180, render: row => formatTime(row.created_at) },
  {
    title: '操作', key: 'actions', width: 300,
    render: row => h(NSpace, { size: 4 }, {
      default: () => [
        h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, { default: () => '编辑' }),
        h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openGrants(row) }, { default: () => '授权' }),
        h(NButton, {
          size: 'small', quaternary: true, type: row.status === 'active' ? 'warning' : 'success',
          onClick: () => toggleServiceStatus(row)
        }, { default: () => row.status === 'active' ? '停用' : '启用' }),
        h(NPopconfirm, { onPositiveClick: () => handleDelete(row.id) }, {
          trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '删除' }),
          default: () => '存在用量记录时将无法删除，候选会一并删除，确认删除？'
        })
      ]
    })
  }
];

async function loadData() {
  loading.value = true;
  try {
    const [servicesRes, accountsRes, policiesRes] = await Promise.all([fetchModelServices(), fetchProviderAccounts(), fetchRoutePolicies()]);
    if (!servicesRes.error && servicesRes.data) list.value = servicesRes.data;
    if (!accountsRes.error && accountsRes.data) {
      accountOptions.value = accountsRes.data
        .slice()
        .sort((a, b) => (a.status === 'active' ? 0 : 1) - (b.status === 'active' ? 0 : 1))
        .map(item => ({ label: `${item.name}${item.status === 'disabled' ? '（已停用）' : ''}`, value: item.id, currency: item.billing_currency }));
    }
    if (!policiesRes.error && policiesRes.data) policyOptions.value = policiesRes.data.map(item => ({ label: item.name, value: item.id }));
  } finally {
    loading.value = false;
  }
}

function makeCandidate(): CandidateForm {
  return {
    provider_account_id: accountOptions.value[0]?.value ?? null,
    upstream_model: '', priority: 100, weight: 1,
    input_cost_per_million: 0, output_cost_per_million: 0, quality_score: 0.5,
    cost_currency: accountOptions.value[0]?.currency ?? 'USD', status: 'active'
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
    .split(',').filter(Boolean).slice(0, 50);
  if (!accountID || !modelIDs.length) return;
  const { data } = await fetchProviderModels(accountID);
  const discovered = new Map((data?.models ?? []).map(item => [item.id, item]));
  formModel.name = modelIDs.length === 1 ? modelIDs[0] : `${accountLabel(accountID)} 服务`;
  formModel.route_policy_id = null;
  formModel.candidates = modelIDs.map(id => {
    const model = discovered.get(id);
    return {
      provider_account_id: accountID,
      upstream_model: id,
      priority: 100,
      weight: 1,
      input_cost_per_million: (accountOptions.value.find(item => item.value === accountID)?.currency ?? 'USD') === 'USD' ? model?.input_cost_per_million ?? 0 : 0,
      output_cost_per_million: (accountOptions.value.find(item => item.value === accountID)?.currency ?? 'USD') === 'USD' ? model?.output_cost_per_million ?? 0 : 0,
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
    window.$message?.error('至少需要一个模型候选');
    return false;
  }
  if (candidates.some(item => !item.provider_account_id || !item.upstream_model.trim())) {
    window.$message?.error('每个候选都需要选择供应商账号和上游模型');
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
      name: formModel.name.trim(), route_policy_id: formModel.route_policy_id,
      candidates: toCandidateParams(formModel.candidates)
    });
    if (!error) {
      window.$message?.success('模型服务已创建');
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
      name: editModel.name.trim(), route_policy_id: editModel.route_policy_id,
      candidates: toCandidateParams(editModel.candidates)
    });
    if (!error) {
      window.$message?.success('模型服务已更新');
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
    window.$message?.success(status === 'active' ? '模型服务已启用' : '模型服务已停用');
    await loadData();
  }
}

async function toggleCandidateStatus(row: Api.Management.ModelCandidate) {
  const status = row.status === 'active' ? 'disabled' : 'active';
  const { error } = await fetchUpdateModelCandidateStatus(row.id, status);
  if (!error) {
    window.$message?.success(status === 'active' ? '候选模型已启用' : '候选模型已停用');
    await loadData();
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteModelService(id);
  if (!error) {
    window.$message?.success('模型服务已删除');
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
    <NCard title="模型服务" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" :disabled="accountOptions.length === 0" @click="openCreate">{{ accountOptions.length === 0 ? '请先创建供应商账号' : '新建模型服务' }}</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1100" :row-key="row => row.id" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建模型服务" class="w-1200px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem label="服务名称" path="name"><NInput v-model:value="formModel.name" placeholder="业务调用的逻辑模型名称" /></NFormItem>
        <NFormItem label="路由策略"><NSelect v-model:value="formModel.route_policy_id" :options="policyOptions" clearable placeholder="可选，不选则使用默认超时配置" /></NFormItem>
        <NDivider title-placement="left">候选模型</NDivider>
        <CandidateEditor v-model:candidates="formModel.candidates" :account-options="accountOptions" />
      </NForm>
      <template #footer><NSpace justify="end"><NButton @click="showCreate = false">取消</NButton><NButton type="primary" :loading="submitting" @click="handleCreate">创建</NButton></NSpace></template>
    </NModal>

    <ModelServiceGrantsModal v-model:show="grantsVisible" :service="activeService" />
    <NModal v-model:show="showEdit" preset="card" title="编辑模型服务" class="w-1200px">
      <NForm ref="editFormRef" :model="editModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem label="服务名称" path="name"><NInput v-model:value="editModel.name" /></NFormItem>
        <NFormItem label="路由策略"><NSelect v-model:value="editModel.route_policy_id" :options="policyOptions" clearable placeholder="可选，不选则使用默认超时配置" /></NFormItem>
        <NDivider title-placement="left">候选模型（保存后全量替换）</NDivider>
        <CandidateEditor v-model:candidates="editModel.candidates" :account-options="accountOptions" />
      </NForm>
      <template #footer><NSpace justify="end"><NButton @click="showEdit = false">取消</NButton><NButton type="primary" :loading="submitting" @click="handleUpdate">保存</NButton></NSpace></template>
    </NModal>
  </div>
</template>
