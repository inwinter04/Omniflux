<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectGroupOption } from 'naive-ui';
import {
  fetchCreateProviderAccount,
  fetchDeleteProviderAccount,
  fetchProviderAccounts,
  fetchProviderBalance,
  fetchProviderCatalog,
  fetchTestProviderAccount,
  fetchUpdateProviderAccount
} from '@/service/api';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';

defineOptions({
  name: 'ModelManagementProviders'
});

// ── 厂商目录 ────────────────────────────────────────────────
const catalogGroups = ref<Api.Management.ProviderCatalogGroup[]>([]);
const catalogLoading = ref(true);

const vendorOptions = computed<SelectGroupOption[]>(() =>
  catalogGroups.value.map(group => ({
    type: 'group',
    label: group.label,
    key: group.group,
    children: group.entries.map(entry => ({ label: entry.name, value: entry.id }))
  }))
);

function findVendor(id: string) {
  for (const group of catalogGroups.value) {
    const hit = group.entries.find(entry => entry.id === id);
    if (hit) return hit;
  }
  return null;
}

function providerLabel(value: string) {
  return findVendor(value)?.name ?? value;
}

// ── 列表 ────────────────────────────────────────────────────
const loading = ref(true);
const list = ref<Api.Management.ProviderAccount[]>([]);

const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({
  provider: '',
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
  provider: { required: true, message: '请选择厂商', trigger: 'change' },
  name: { required: true, message: '请填写账号名称', trigger: 'blur' },
  api_key: { required: true, message: '请填写 API Key', trigger: 'blur' }
};
const editRules: FormRules = {
  name: { required: true, message: '请填写账号名称', trigger: 'blur' }
};

// 当前创建表单选中的厂商条目
const selectedVendor = computed(() => findVendor(formModel.provider));
// 是否显示 base_url 输入:自定义协议或本地/自部署类厂商
const showBaseUrlInput = computed(() => selectedVendor.value?.editable_url ?? true);

function handleVendorChange(value: string) {
  const vendor = findVendor(value);
  // 账号名称未填时自动带出厂商名,方便同一厂商多账号时再补充后缀
  if (!formModel.name) {
    formModel.name = vendor?.name ?? '';
  }
}

function openApiKeyUrl() {
  const url = selectedVendor.value?.api_key_url;
  if (url) {
    window.open(url, '_blank');
  }
}

const columns: DataTableColumns<Api.Management.ProviderAccount> = [
  { title: '名称', key: 'name', width: 160 },
  { title: '厂商', key: 'provider', width: 150, render: row => providerLabel(row.provider) },
  { title: '区域', key: 'region', width: 90, render: row => row.region ?? '-' },
  { title: 'Base URL', key: 'base_url', render: row => row.base_url ?? '(厂商内置)' },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render(row) {
      return h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status) });
    }
  },
  {
    title: '近24h健康',
    key: 'success_rate_24h',
    width: 120,
    render(row) {
      if (row.requests_24h === 0) return '无流量';
      const pct = Math.round((row.success_rate_24h ?? 0) * 100);
      return `${pct}% (${row.requests_24h}次)`;
    }
  },
  { title: '创建时间', key: 'created_at', width: 170, render: row => formatTime(row.created_at) },
  {
    title: '操作',
    key: 'actions',
    width: 240,
    render(row) {
      const actions = [
        h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, { default: () => '编辑' }),
        h(NButton, { size: 'small', quaternary: true, type: 'info', onClick: () => handleTest(row) }, { default: () => '测试' })
      ];
      if (findVendor(row.provider)?.balance_kind) {
        actions.push(h(NButton, { size: 'small', quaternary: true, type: 'warning', onClick: () => handleBalance(row) }, { default: () => '余额' }));
      }
      actions.push(
        h(
          NPopconfirm,
          { onPositiveClick: () => handleDelete(row.id) },
          {
            trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '删除' }),
            default: () => '删除后凭据一并清除,确认删除?'
          }
        )
      );
      return h(NSpace, { size: 4 }, { default: () => actions });
    }
  }
];

// ── 数据加载 ────────────────────────────────────────────────
async function loadCatalog() {
  catalogLoading.value = true;
  try {
    const { data, error } = await fetchProviderCatalog();
    if (!error && data) {
      catalogGroups.value = data;
    }
  } finally {
    catalogLoading.value = false;
  }
}

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
  formModel.provider = '';
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

// 编辑弹窗的 Base URL 是否可改:厂商不可编辑类不显示输入框
const editVendorEditable = computed(() => findVendor(editModel.provider)?.editable_url ?? true);

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchCreateProviderAccount({
      provider: formModel.provider,
      name: formModel.name.trim(),
      region: formModel.region.trim() || undefined,
      base_url: showBaseUrlInput.value ? formModel.base_url.trim() || undefined : undefined,
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
      base_url: editVendorEditable.value ? editModel.base_url.trim() || undefined : undefined
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
      window.$message?.success('连通正常');
    } else {
      window.$message?.error(data.message);
    }
  }
}

// ── 余额查询 ────────────────────────────────────────────────
const showBalance = ref(false);
const balanceLoading = ref(false);
const balanceRows = ref<Api.Management.ProviderBalanceEntry[]>([]);
const balanceTitle = ref('余额查询');

async function handleBalance(row: Api.Management.ProviderAccount) {
  showBalance.value = true;
  balanceLoading.value = true;
  balanceTitle.value = `余额查询 - ${row.name}`;
  try {
    const { data, error } = await fetchProviderBalance(row.id);
    if (!error && data) {
      balanceRows.value = data.balances ?? [];
    } else {
      balanceRows.value = [];
    }
  } finally {
    balanceLoading.value = false;
  }
}

function formatAmount(value: number | null) {
  return value === null ? '-' : value.toLocaleString('zh-CN');
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteProviderAccount(id);
  if (!error) {
    window.$message?.success('Provider 账号已删除');
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
    <NCard title="Provider 账号" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" @click="openCreate">新建账号</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1100" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建 Provider 账号" class="w-560px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem label="厂商" path="provider">
          <NSelect
            v-model:value="formModel.provider"
            :options="vendorOptions"
            :loading="catalogLoading"
            filterable
            placeholder="选择厂商后仅需填写 API Key"
            @update:value="handleVendorChange"
          />
        </NFormItem>
        <NFormItem v-if="selectedVendor?.api_key_url" label=" " :show-feedback="false">
          <NButton text type="primary" @click="openApiKeyUrl">前往厂商控制台获取 API Key ↗</NButton>
        </NFormItem>
        <NFormItem label="账号名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="例如:主账号、备用账号" />
        </NFormItem>
        <NFormItem label="区域" path="region">
          <NInput v-model:value="formModel.region" placeholder="可选,例如 us-east-1" />
        </NFormItem>
        <NFormItem v-if="showBaseUrlInput" label="Base URL" path="base_url">
          <NInput v-model:value="formModel.base_url" placeholder="自部署或代理地址,例如 http://localhost:11434" />
        </NFormItem>
        <NAlert v-else type="info" :bordered="false" class="mb-16px">
          接口地址由系统内置({{ selectedVendor?.base_url }}),无需填写;同一厂商可创建多个账号,多账号自动负载均衡。
        </NAlert>
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

    <NModal v-model:show="showEdit" preset="card" title="编辑 Provider 账号" class="w-560px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="100">
        <NFormItem label="厂商">
          <NTag :bordered="false">{{ providerLabel(editModel.provider) }}</NTag>
        </NFormItem>
        <NFormItem label="账号名称" path="name">
          <NInput v-model:value="editModel.name" placeholder="账号名称" />
        </NFormItem>
        <NFormItem label="区域" path="region">
          <NInput v-model:value="editModel.region" placeholder="可选,例如 us-east-1" />
        </NFormItem>
        <NFormItem v-if="editVendorEditable" label="Base URL" path="base_url">
          <NInput v-model:value="editModel.base_url" placeholder="自部署或代理地址" />
        </NFormItem>
        <NAlert v-else type="info" :bordered="false" class="mb-16px">
          接口地址由系统内置({{ findVendor(editModel.provider)?.base_url }}),无需维护。
        </NAlert>
        <NAlert type="info" :bordered="false">API Key 与厂商类型创建后不可修改;更换密钥请删除后重新创建。</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showEdit = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleUpdate">保存</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showBalance" preset="card" :title="balanceTitle" class="w-420px">
      <NSpin :show="balanceLoading">
        <NList v-if="balanceRows.length" :bordered="false">
          <NListItem v-for="row in balanceRows" :key="row.label">
            <NSpace justify="space-between" class="w-full">
              <span>{{ row.label }}</span>
              <span class="font-bold">
                {{ formatAmount(row.remaining) }} {{ row.unit }}
                <template v-if="row.total !== null">(总额 {{ formatAmount(row.total) }})</template>
              </span>
            </NSpace>
          </NListItem>
        </NList>
        <NEmpty v-else-if="!balanceLoading" description="未获取到余额数据" />
      </NSpin>
    </NModal>
  </div>
</template>

<style scoped></style>
