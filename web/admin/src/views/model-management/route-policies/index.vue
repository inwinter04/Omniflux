<script setup lang="ts">
import { h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchCreateRoutePolicy, fetchDeleteRoutePolicy, fetchRoutePolicies, fetchUpdateRoutePolicy } from '@/service/api';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';

defineOptions({
  name: 'ModelManagementRoutePolicies'
});

const strategyOptions = [
  { label: '成本优先 (cost_first)', value: 'cost_first' },
  { label: '效果优先 (quality_first)', value: 'quality_first' },
  { label: '均衡 (balanced)', value: 'balanced' }
];

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
  first_token_timeout_ms: 15000
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
  { title: 'Fallback 上限', key: 'fallback_limit', width: 120 },
  { title: '上游超时(ms)', key: 'provider_timeout_ms', width: 120 },
  { title: '首 Token 超时(ms)', key: 'first_token_timeout_ms', width: 140 },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render(row) {
      return h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status) });
    }
  },
  { title: '更新时间', key: 'updated_at', width: 180, render: row => formatTime(row.updated_at) },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    render(row) {
      return h(NSpace, { size: 4 }, {
        default: () => [
          h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, { default: () => '编辑' }),
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
  showCreate.value = true;
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
      first_token_timeout_ms: formModel.first_token_timeout_ms
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

async function handleDelete(id: string) {
  const { error } = await fetchDeleteRoutePolicy(id);
  if (!error) {
    window.$message?.success('路由策略已删除');
    await loadData();
  }
}

onMounted(() => {
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
        评分权重与硬约束当前按服务端默认值生效,精细调度将在后续版本实现。
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
        <NFormItem label="Fallback 上限" path="fallback_limit">
          <NInputNumber v-model:value="formModel.fallback_limit" :min="0" class="w-full" />
        </NFormItem>
        <NFormItem label="上游超时(ms)" path="provider_timeout_ms">
          <NInputNumber v-model:value="formModel.provider_timeout_ms" :min="1" class="w-full" />
        </NFormItem>
        <NFormItem label="首 Token 超时(ms)" path="first_token_timeout_ms">
          <NInputNumber v-model:value="formModel.first_token_timeout_ms" :min="1" class="w-full" />
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
        <NFormItem label="Fallback 上限" path="fallback_limit">
          <NInputNumber v-model:value="editModel.fallback_limit" :min="0" class="w-full" />
        </NFormItem>
        <NFormItem label="上游超时(ms)" path="provider_timeout_ms">
          <NInputNumber v-model:value="editModel.provider_timeout_ms" :min="1" class="w-full" />
        </NFormItem>
        <NFormItem label="首 Token 超时(ms)" path="first_token_timeout_ms">
          <NInputNumber v-model:value="editModel.first_token_timeout_ms" :min="1" class="w-full" />
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
