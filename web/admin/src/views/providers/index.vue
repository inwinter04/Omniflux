<script setup lang="ts">
import { h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchCreateProviderAccount, fetchDeleteProviderAccount, fetchProviderAccounts } from '@/service/api';

defineOptions({
  name: 'Providers'
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
const creating = ref(false);
const formRef = ref<FormInst | null>(null);
const formModel = reactive({
  provider: 'openai',
  name: '',
  region: '',
  base_url: '',
  api_key: ''
});

const formRules: FormRules = {
  name: { required: true, message: '请填写账号名称', trigger: 'blur' },
  api_key: { required: true, message: '请填写 API Key', trigger: 'blur' }
};

function formatTime(value: string) {
  return new Date(value).toLocaleString();
}

const columns: DataTableColumns<Api.Management.ProviderAccount> = [
  { title: '名称', key: 'name', width: 160 },
  { title: 'Provider', key: 'provider', width: 160 },
  { title: '区域', key: 'region', width: 100, render: row => row.region ?? '-' },
  { title: 'Base URL', key: 'base_url', render: row => row.base_url ?? '-' },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render(row) {
      return h(NTag, { type: row.status === 'active' ? 'success' : 'default', size: 'small', round: true }, { default: () => row.status });
    }
  },
  { title: '创建时间', key: 'created_at', width: 180, render: row => formatTime(row.created_at) },
  {
    title: '操作',
    key: 'actions',
    width: 100,
    render(row) {
      return h(
        NPopconfirm,
        { onPositiveClick: () => handleDelete(row.id) },
        {
          trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '删除' }),
          default: () => '删除后凭据一并清除,确认删除?'
        }
      );
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

async function handleCreate() {
  await formRef.value?.validate();
  creating.value = true;
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
    creating.value = false;
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
          <NButton type="primary" :loading="creating" @click="handleCreate">创建</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>


