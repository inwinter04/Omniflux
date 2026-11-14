<script setup lang="ts">
import { h, onMounted, reactive, ref } from 'vue';
import { NButton, NDatePicker, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import {
  fetchApiKeys,
  fetchApplications,
  fetchBudgets,
  fetchCreateBudget,
  fetchDeleteBudget,
  fetchProjects,
  fetchUpdateBudget
} from '@/service/api';
import { formatTime } from '@/utils/admin-status';

defineOptions({
  name: 'OrganizationBudgets'
});

const loading = ref(true);
const list = ref<Api.Management.Budget[]>([]);
const projects = ref<Api.Management.Project[]>([]);
const applications = ref<Api.Management.Application[]>([]);
const apiKeys = ref<Api.Management.ApiKey[]>([]);

const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({
  scope_type: 'organization',
  scope_id: null as string | null,
  window_range: null as [number, number] | null,
  limit_amount: null as number | null,
  currency: 'USD'
});
const editModel = reactive({
  id: '',
  limit_amount: null as number | null
});

const formRules: FormRules = {
  scope_id: { required: true, message: '请选择预算对象', trigger: 'change' },
  window_range: { required: true, message: '请选择时间窗口', trigger: 'change' },
  limit_amount: { required: true, message: '请填写预算限额', trigger: 'blur' }
};
const editRules: FormRules = {
  limit_amount: { required: true, message: '请填写预算限额', trigger: 'blur' }
};

const scopeTypeOptions = [
  { label: '组织', value: 'organization' },
  { label: '项目', value: 'project' },
  { label: '应用', value: 'application' },
  { label: 'API Key', value: 'api_key' }
];

const scopeOptions = ref<{ label: string; value: string }[]>([]);

function reloadScopeOptions() {
  switch (formModel.scope_type) {
    case 'organization':
      scopeOptions.value = [{ label: '当前组织', value: 'organization' }];
      break;
    case 'project':
      scopeOptions.value = projects.value.map(item => ({ label: item.name, value: item.id }));
      break;
    case 'application':
      scopeOptions.value = applications.value.map(item => ({ label: `${item.name}(项目:${projects.value.find(p => p.id === item.project_id)?.name ?? '-'})`, value: item.id }));
      break;
    case 'api_key':
      scopeOptions.value = apiKeys.value.map(item => ({ label: item.name, value: item.id }));
      break;
  }
}

function scopeTypeText(t: string) {
  return scopeTypeOptions.find(item => item.value === t)?.label ?? t;
}

function windowState(row: Api.Management.Budget) {
  const now = Date.now();
  const start = new Date(row.window_start).getTime();
  const end = new Date(row.window_end).getTime();
  if (now < start) return '未开始';
  if (now >= end) return '已结束';
  return '进行中';
}

function formatAmount(value: string) {
  return value;
}

const columns: DataTableColumns<Api.Management.Budget> = [
  { title: '范围', key: 'scope_type', width: 90, render: row => scopeTypeText(row.scope_type) },
  { title: '对象', key: 'scope_name', width: 140 },
  { title: '开始时间', key: 'window_start', width: 170, render: row => formatTime(row.window_start) },
  { title: '结束时间', key: 'window_end', width: 170, render: row => formatTime(row.window_end) },
  { title: '限额', key: 'limit_amount', width: 120, render: row => `${row.currency} ${formatAmount(row.limit_amount)}` },
  {
    title: '已用',
    key: 'spent_amount',
    width: 120,
    render(row) {
      const type = row.exceeded ? 'error' : 'default';
      return h(NTag, { type, size: 'small', round: true }, { default: () => `${row.currency} ${formatAmount(row.spent_amount)}` });
    }
  },
  { title: '窗口状态', key: 'window_state', width: 90, render: row => windowState(row) },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    render(row) {
      return h(NSpace, { size: 4 }, {
        default: () => [
          h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, { default: () => '调整限额' }),
          h(
            NPopconfirm,
            { onPositiveClick: () => handleDelete(row.id) },
            {
              trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '删除' }),
              default: () => '存在结算记录的窗口无法删除,确认删除?'
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
    const [budgetsRes, projectsRes, applicationsRes, keysRes] = await Promise.all([
      fetchBudgets(),
      fetchProjects(),
      fetchApplications(),
      fetchApiKeys()
    ]);
    if (!budgetsRes.error && budgetsRes.data) {
      list.value = budgetsRes.data;
    }
    if (!projectsRes.error && projectsRes.data) {
      projects.value = projectsRes.data;
    }
    if (!applicationsRes.error && applicationsRes.data) {
      applications.value = applicationsRes.data;
    }
    if (!keysRes.error && keysRes.data) {
      apiKeys.value = keysRes.data;
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  formModel.scope_type = 'organization';
  formModel.scope_id = 'organization';
  formModel.window_range = null;
  formModel.limit_amount = null;
  formModel.currency = 'USD';
  reloadScopeOptions();
  showCreate.value = true;
}

function handleScopeTypeChange() {
  formModel.scope_id = scopeOptions.value[0]?.value ?? null;
}

function openEdit(row: Api.Management.Budget) {
  editModel.id = row.id;
  editModel.limit_amount = Number(row.limit_amount);
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchCreateBudget({
      scope_type: formModel.scope_type,
      scope_id: formModel.scope_id!,
      window_start: new Date(formModel.window_range![0]).toISOString(),
      window_end: new Date(formModel.window_range![1]).toISOString(),
      limit_amount: String(formModel.limit_amount),
      currency: formModel.currency
    });
    if (!error) {
      window.$message?.success('预算已创建');
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
    const { error } = await fetchUpdateBudget(editModel.id, String(editModel.limit_amount));
    if (!error) {
      window.$message?.success('预算限额已调整');
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteBudget(id);
  if (!error) {
    window.$message?.success('预算已删除');
    await loadData();
  }
}

onMounted(() => {
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard title="预算" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" @click="openCreate">新建预算</NButton>
        </NSpace>
      </template>
      <NAlert type="info" :bordered="false" class="mb-12px">
        预算按组织/项目/应用/API Key 维度设置时间窗限额;数据面每次调用前检查,已用尽时拒绝新请求(429 budget_exceeded)。
      </NAlert>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1100" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建预算" class="w-560px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem label="范围类型" path="scope_type">
          <NSelect v-model:value="formModel.scope_type" :options="scopeTypeOptions" @update:value="reloadScopeOptions" />
        </NFormItem>
        <NFormItem label="预算对象" path="scope_id">
          <NSelect v-model:value="formModel.scope_id" :options="scopeOptions" placeholder="选择对象" />
        </NFormItem>
        <NFormItem label="时间窗口" path="window_range">
          <NDatePicker v-model:value="formModel.window_range" type="datetimerange" clearable class="w-full" />
        </NFormItem>
        <NFormItem label="限额" path="limit_amount">
          <NInputNumber v-model:value="formModel.limit_amount" :min="0" class="w-full" placeholder="窗口内允许的最大花费">
            <template #prefix>{{ formModel.currency }}</template>
          </NInputNumber>
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">创建</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" title="调整预算限额" class="w-480px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="100">
        <NFormItem label="新限额" path="limit_amount">
          <NInputNumber v-model:value="editModel.limit_amount" :min="0" class="w-full" />
        </NFormItem>
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
