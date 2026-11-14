<script setup lang="ts">
import { h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchCreateProviderAccount, fetchDeleteProviderAccount, fetchProviderAccounts, fetchTestProviderAccount, fetchUpdateProviderAccount } from '@/service/api';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';

defineOptions({
  name: 'ModelManagementProviders'
});

const providerOptions = [
  { label: 'OpenAI', value: 'openai' },
  { label: 'Azure OpenAI', value: 'azure_openai' },
  { label: 'Anthropic', value: 'anthropic' },
  { label: 'Gemini', value: 'gemini' },
  { label: 'Vertex AI', value: 'vertex' },
  { label: 'AWS Bedrock', value: 'bedrock' },
  { label: 'OpenAI Compatible', value: 'openai_compatible' }
];

const loading = ref(true);
const list = ref<Api.Management.ProviderAccount[]>([]);

const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({
  provider: 'openai',
  name: '',
  region: '',
  base_url: '',
  api_key: ''
});
const editModel = reactive({
  id: '',
  provider: '',
  name: '',
  region: '',
  base_url: ''
});

const formRules: FormRules = {
  name: { required: true, message: '请填写账号名称', trigger: 'blur' },
  api_key: { required: true, message: '请填写 API Key', trigger: 'blur' }
};
const editRules: FormRules = {
  name: { required: true, message: '请填写账号名称', trigger: 'blur' }
};

function providerLabel(value: string) {
  return providerOptions.find(item => item.value === value)?.label ?? value;
}

const columns: DataTableColumns<Api.Management.ProviderAccount> = [
  { title: '名称', key: 'name', width: 160 },
  { title: 'Provider', key: 'provider', width: 160, render: row => providerLabel(row.provider) },
  { title: '区域', key: 'region', width: 100, render: row => row.region ?? '-' },
  { title: 'Base URL', key: 'base_url', render: row => row.base_url ?? '-' },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render(row) {
      return h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status) });
    }
  },
  {
    title: '近24h健康',
    key: 'success_rate_24h',
    width: 130,
    render(row) {
      if (row.requests_24h === 0) return '无流量';
      const pct = Math.round((row.success_rate_24h ?? 0) * 100);
      return `${pct}% (${row.requests_24h}次)`;
    }
  },
  { title: '创建时间', key: 'created_at', width: 180, render: row => formatTime(row.created_at) },
  {
    title: '操作',
    key: 'actions',
    width: 190,
    render(row) {
      return h(NSpace, { size: 4 }, {
        default: () => [
          h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, { default: () => '编辑' }),
          h(NButton, { size: 'small', quaternary: true, type: 'info', onClick: () => handleTest(row) }, { default: () => '测试' }),
          h(
            NPopconfirm,
            { onPositiveClick: () => handleDelete(row.id) },
            {
              trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '删除' }),
              default: () => '删除后凭据一并清除,确认删除?'
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
    const { data, error } = await fetchProviderAccounts();
    if (!error && data) {
      list.value = data;
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  formModel.provider = 'openai';
  formModel.name = '';
  formModel.region = '';
  formModel.base_url = '';
  formModel.api_key = '';
  showCreate.value = true;
}

function openEdit(row: Api.Management.ProviderAccount) {
  editModel.id = row.id;
  editModel.provider = row.provider;
  editModel.name = row.name;
  editModel.region = row.region ?? '';
  editModel.base_url = row.base_url ?? '';
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchCreateProviderAccount({
      provider: formModel.provider,
      name: formModel.name.trim(),
      region: formModel.region.trim() || undefined,
      base_url: formModel.base_url.trim() || undefined,
      api_key: formModel.api_key
    });
    if (!error) {
      window.$message?.success('Provider 账号已创建');
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
    const { error } = await fetchUpdateProviderAccount(editModel.id, {
      name: editModel.name.trim(),
      region: editModel.region.trim() || undefined,
      base_url: editModel.base_url.trim() || undefined
    });
    if (!error) {
      window.$message?.success('Provider 账号已更新');
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function handleTest(row: Api.Management.ProviderAccount) {
  const { data, error } = await fetchTestProviderAccount(row.id);
  if (!error && data) {
    if (data.ok) {
      window.$message?.success(`连通正常`);
    } else {
      window.$message?.error(data.message);
    }
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteProviderAccount(id);
  if (!error) {
    window.$message?.success('Provider 账号已删除');
    await loadData();
  }
}

onMounted(() => {
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard title="Provider 账号" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" @click="openCreate">新建账号</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1000" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建 Provider 账号" class="w-520px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem label="Provider" path="provider">
          <NSelect v-model:value="formModel.provider" :options="providerOptions" />
        </NFormItem>
        <NFormItem label="账号名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="例如:主账号-openai" />
        </NFormItem>
        <NFormItem label="区域" path="region">
          <NInput v-model:value="formModel.region" placeholder="可选,例如 us-east-1" />
        </NFormItem>
        <NFormItem label="Base URL" path="base_url">
          <NInput v-model:value="formModel.base_url" placeholder="可选,自部署或代理地址" />
        </NFormItem>
        <NFormItem label="API Key" path="api_key">
          <NInput v-model:value="formModel.api_key" type="password" show-password-on="click" placeholder="服务端加密入库,不会回显" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">创建</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" title="编辑 Provider 账号" class="w-520px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="100">
        <NFormItem label="Provider">
          <NTag :bordered="false">{{ providerLabel(editModel.provider) }}</NTag>
        </NFormItem>
        <NFormItem label="账号名称" path="name">
          <NInput v-model:value="editModel.name" placeholder="账号名称" />
        </NFormItem>
        <NFormItem label="区域" path="region">
          <NInput v-model:value="editModel.region" placeholder="可选,例如 us-east-1" />
        </NFormItem>
        <NFormItem label="Base URL" path="base_url">
          <NInput v-model:value="editModel.base_url" placeholder="可选,自部署或代理地址" />
        </NFormItem>
        <NAlert type="info" :bordered="false">API Key 与 Provider 类型创建后不可修改;更换密钥请删除后重新创建。</NAlert>
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

