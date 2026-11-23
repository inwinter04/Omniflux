<script setup lang="ts">
import { h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { fetchCreateRoutePolicy, fetchDeleteRoutePolicy, fetchProviderCatalog, fetchRoutePolicies, fetchUpdateRoutePolicy, fetchUpdateRoutePolicyStatus } from '@/service/api';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';

defineOptions({
  name: 'ModelManagementRoutePolicies'
});

const strategyOptions = [
  { label: '成本优先', value: 'cost_first' },
  { label: '效果优先', value: 'quality_first' },
  { label: '均衡', value: 'balanced' }
];

// 厂商目录驱动的约束选项;目录加载失败时保留协议级兜底选项。
const legacyProviderOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'Azure OpenAI', value: 'azure_openai' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'Gemini', value: 'gemini' },
  { label: 'Vertex AI', value: 'vertex' },
  { label: 'AWS Bedrock', value: 'bedrock' },
  { label: 'OpenAI Compatible', value: 'openai_compatible' }
];

const constraintProviderOptions = ref<SelectOption[]>(legacyProviderOptions);

async function loadCatalog() {
  const { data, error } = await fetchProviderCatalog();
  if (error || !data) return;
  const options: SelectOption[] = [];
  for (const group of data) {
    if (group.group === 'custom') continue;
    options.push({
      type: 'group',
      label: group.label,
      key: group.group,
      children: group.entries.map(entry => ({ label: entry.name, value: entry.id }))
    });
  }
  options.push({ type: 'group', label: '自定义', key: 'custom', children: [{ label: '自定义 OpenAI 兼容', value: 'openai_compatible' }] });
  constraintProviderOptions.value = options;
}

const loading = ref(true);
const list = ref<Api.Management.RoutePolicy[]>([]);

const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({
  name: '',
  strategy: 'balanced',
  fallback_limit: 2,
  provider_timeout_ms: 60000,
  first_token_timeout_ms: 15000,
  allowed_providers: [] as string[],
  blocked_providers: [] as string[],
  allowed_upstream_models: [] as string[],
  blocked_upstream_models: [] as string[],
  max_cost_per_million: null as number | null
});
const editModel = reactive({
  id: '',
  name: '',
  strategy: 'balanced',
  fallback_limit: 2,
  provider_timeout_ms: 60000,
  first_token_timeout_ms: 15000
});

const formRules: FormRules = {
  name: { required: true, message: '请填写策略名称', trigger: 'blur' }
};
const editRules: FormRules = {
  name: { required: true, message: '请填写策略名称', trigger: 'blur' }
};

function strategyLabel(value: string) {
  return strategyOptions.find(item => item.value === value)?.label ?? value;
}

const columns: DataTableColumns<Api.Management.RoutePolicy> = [
  { title: '名称', key: 'name', width: 160 },
  { title: '策略', key: 'strategy', width: 180, render: row => strategyLabel(row.strategy) },
  { title: '版本', key: 'version', width: 70 },
  { title: '故障转移上限', key: 'fallback_limit', width: 120 },
  { title: '上游超时（毫秒）', key: 'provider_timeout_ms', width: 120 },
  { title: '首字超时（毫秒）', key: 'first_token_timeout_ms', width: 140 },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render(row) {
      return h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status, 'policy') });
    }
  },
  { title: '更新时间', key: 'updated_at', width: 180, render: row => formatTime(row.updated_at) },
  {
    title: '操作',
    key: 'actions',
    width: 230,
    render(row) {
      return h(NSpace, { size: 4 }, {
        default: () => [
          h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, { default: () => '编辑' }),
          h(NButton, { size: 'small', quaternary: true, type: row.status === 'active' ? 'warning' : 'success', onClick: () => toggleStatus(row) }, { default: () => row.status === 'active' ? '停用' : '启用' }),
          h(
            NPopconfirm,
            { onPositiveClick: () => handleDelete(row.id) },
            {
              trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '删除' }),
              default: () => '被模型服务引用或有决策记录时将无法删除,确认删除?'
            }
          )
        ]
      });
    }
  }
];

async function loadData() {
  loading.value = true;
  try {
    const { data, error } = await fetchRoutePolicies();
    if (!error && data) {
      list.value = data;
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  formModel.name = '';
  formModel.strategy = 'balanced';
  formModel.fallback_limit = 2;
  formModel.provider_timeout_ms = 60000;
  formModel.first_token_timeout_ms = 15000;
  formModel.allowed_providers = [];
  formModel.blocked_providers = [];
  formModel.allowed_upstream_models = [];
  formModel.blocked_upstream_models = [];
  formModel.max_cost_per_million = null;
  showCreate.value = true;
}

function buildConstraints(): Api.Management.RouteConstraints | undefined {
  const c: Api.Management.RouteConstraints = {};
  if (formModel.allowed_providers.length > 0) c.allowed_providers = formModel.allowed_providers;
  if (formModel.blocked_providers.length > 0) c.blocked_providers = formModel.blocked_providers;
  if (formModel.allowed_upstream_models.length > 0) c.allowed_upstream_models = formModel.allowed_upstream_models;
  if (formModel.blocked_upstream_models.length > 0) c.blocked_upstream_models = formModel.blocked_upstream_models;
  if (formModel.max_cost_per_million != null) c.max_cost_per_million = formModel.max_cost_per_million;
  return Object.keys(c).length > 0 ? c : undefined;
}

function openEdit(row: Api.Management.RoutePolicy) {
  editModel.id = row.id;
  editModel.name = row.name;
  editModel.strategy = row.strategy;
  editModel.fallback_limit = row.fallback_limit;
  editModel.provider_timeout_ms = row.provider_timeout_ms;
  editModel.first_token_timeout_ms = row.first_token_timeout_ms;
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchCreateRoutePolicy({
      name: formModel.name.trim(),
      strategy: formModel.strategy,
      fallback_limit: formModel.fallback_limit,
      provider_timeout_ms: formModel.provider_timeout_ms,
      first_token_timeout_ms: formModel.first_token_timeout_ms,
      constraints: buildConstraints()
    });
    if (!error) {
      window.$message?.success('路由策略已创建');
      showCreate.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function handleUpdate() {
  await editFormRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchUpdateRoutePolicy(editModel.id, {
      name: editModel.name.trim(),
      strategy: editModel.strategy,
      fallback_limit: editModel.fallback_limit,
      provider_timeout_ms: editModel.provider_timeout_ms,
      first_token_timeout_ms: editModel.first_token_timeout_ms
    });
    if (!error) {
      window.$message?.success('路由策略已更新');
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(row: Api.Management.RoutePolicy) {
  const status = row.status === 'active' ? 'disabled' : 'active';
  const { error } = await fetchUpdateRoutePolicyStatus(row.id, status);
  if (!error) {
    window.$message?.success(status === 'active' ? '路由策略已启用' : '路由策略已停用');
    await loadData();
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteRoutePolicy(id);
  if (!error) {
    window.$message?.success('路由策略已删除');
    await loadData();
  }
}

onMounted(() => {
  void loadCatalog();
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard title="路由策略" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" @click="openCreate">新建策略</NButton>
        </NSpace>
      </template>
      <NAlert type="info" :bordered="false" class="mb-12px">
        评分权重按所选调度策略生效;硬约束在路由前过滤候选,仅创建时可设置,更新策略不改变 constraints。
      </NAlert>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1200" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建路由策略" class="w-520px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="140">
        <NFormItem label="策略名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="例如:默认均衡策略" />
        </NFormItem>
        <NFormItem label="调度策略" path="strategy">
          <NSelect v-model:value="formModel.strategy" :options="strategyOptions" />
        </NFormItem>
        <NFormItem label="故障转移上限" path="fallback_limit">
          <NInputNumber v-model:value="formModel.fallback_limit" :min="0" class="w-full" />
        </NFormItem>
        <NFormItem label="上游超时（毫秒）" path="provider_timeout_ms">
          <NInputNumber v-model:value="formModel.provider_timeout_ms" :min="1" class="w-full" />
        </NFormItem>
        <NFormItem label="首字超时（毫秒）" path="first_token_timeout_ms">
          <NInputNumber v-model:value="formModel.first_token_timeout_ms" :min="1" class="w-full" />
        </NFormItem>

        <NDivider title-placement="left">硬约束(可选,仅创建时可设置)</NDivider>
        <NFormItem label="允许的供应商" path="allowed_providers">
          <NSelect v-model:value="formModel.allowed_providers" multiple clearable tag :options="constraintProviderOptions" placeholder="留空不限制" />
        </NFormItem>
        <NFormItem label="禁止的供应商" path="blocked_providers">
          <NSelect v-model:value="formModel.blocked_providers" multiple clearable tag :options="constraintProviderOptions" placeholder="留空不限制" />
        </NFormItem>
        <NFormItem label="允许上游模型" path="allowed_upstream_models">
          <NSelect v-model:value="formModel.allowed_upstream_models" multiple clearable filterable tag placeholder="输入模型名后回车" />
        </NFormItem>
        <NFormItem label="禁止上游模型" path="blocked_upstream_models">
          <NSelect v-model:value="formModel.blocked_upstream_models" multiple clearable filterable tag placeholder="输入模型名后回车" />
        </NFormItem>
        <NFormItem label="混合单价上限" path="max_cost_per_million">
          <NInputNumber v-model:value="formModel.max_cost_per_million" :min="0" class="w-full" placeholder="美元/百万，留空不限制" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">创建</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" title="编辑路由策略" class="w-520px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="140">
        <NFormItem label="策略名称" path="name">
          <NInput v-model:value="editModel.name" placeholder="策略名称" />
        </NFormItem>
        <NFormItem label="调度策略" path="strategy">
          <NSelect v-model:value="editModel.strategy" :options="strategyOptions" />
        </NFormItem>
        <NFormItem label="故障转移上限" path="fallback_limit">
          <NInputNumber v-model:value="editModel.fallback_limit" :min="0" class="w-full" />
        </NFormItem>
        <NFormItem label="上游超时（毫秒）" path="provider_timeout_ms">
          <NInputNumber v-model:value="editModel.provider_timeout_ms" :min="1" class="w-full" />
        </NFormItem>
        <NFormItem label="首字超时（毫秒）" path="first_token_timeout_ms">
          <NInputNumber v-model:value="editModel.first_token_timeout_ms" :min="1" class="w-full" />
        </NFormItem>
        <NAlert type="info" :bordered="false">切换策略会按新策略模板重置评分权重;硬约束仅在创建时设置,如需调整请新建策略。</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showEdit = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleUpdate">保存</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>
