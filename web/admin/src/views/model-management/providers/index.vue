<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NDropdown, NSpace, NTag } from 'naive-ui';
import { useRouter } from 'vue-router';
import type { DataTableColumns, FormInst, FormRules, SelectGroupOption } from 'naive-ui';
import {
  fetchCreateProviderAccount,
  fetchDeleteProviderAccount,
  fetchProviderAccounts,
  fetchProviderBalance,
  fetchProviderCatalog,
  fetchProviderModels,
  fetchTestProviderAccount,
  fetchUpdateProviderAccount,
  fetchUpdateProviderAccountStatus
} from '@/service/api';
import { formatTime, healthStatusText, healthSummary, healthTagType, statusTagType, statusText } from '@/utils/admin-status';
import { formatCostUSD, formatResetTime, formatUnitAmount, settlementCurrencyOptions } from '@/utils/currency';

defineOptions({ name: 'ModelManagementProviders' });

const router = useRouter();
const catalogGroups = ref<Api.Management.ProviderCatalogGroup[]>([]);
const catalogLoading = ref(true);
const catalogEntries = computed(() => catalogGroups.value.flatMap(group => group.entries));
const vendorOptions = computed<SelectGroupOption[]>(() => catalogGroups.value.map(group => {
  const families = new Map<string, string>();
  for (const entry of group.entries) families.set(entry.family_id || entry.id, entry.family_name || entry.name);
  return {
    type: 'group', label: group.label, key: group.group,
    children: Array.from(families, ([value, label]) => ({ label, value }))
  };
}));

const loading = ref(true);
const list = ref<Api.Management.ProviderAccount[]>([]);
const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const selectedFamilyID = ref('');
const formModel = reactive({ provider: '', name: '', region: '', base_url: '', billing_currency: 'USD', api_key: '' });
const editModel = reactive({ id: '', provider: '', name: '', region: '', base_url: '', billing_currency: 'USD' });
const formRules: FormRules = {
  provider: { required: true, message: '请选择厂商', trigger: 'change' },
  name: { required: true, message: '请填写账号名称', trigger: 'blur' },
  api_key: { required: true, message: '请填写 API Key', trigger: 'blur' }
};
const editRules: FormRules = { name: { required: true, message: '请填写账号名称', trigger: 'blur' } };

const selectedVendor = computed(() => findVendor(formModel.provider));
const editVendor = computed(() => findVendor(editModel.provider));
const versionOptions = computed(() => catalogEntries.value
  .filter(entry => (entry.family_id || entry.id) === selectedFamilyID.value)
  .map(entry => ({ label: entry.version_label || entry.name, value: entry.id })));

function findVendor(id: string) {
  return catalogEntries.value.find(entry => entry.id === id) ?? null;
}

function providerLabel(value: string) {
  const vendor = findVendor(value);
  return vendor?.family_name || vendor?.name || value;
}

function applyVendor(value: string, resetName = false) {
  const vendor = findVendor(value);
  if (!vendor) return;
  formModel.provider = value;
  selectedFamilyID.value = vendor.family_id || vendor.id;
  formModel.base_url = vendor.editable_url ? vendor.base_url : '';
  if (resetName || !formModel.name) formModel.name = vendor.family_name || vendor.name;
}

function handleFamilyChange(value: string) {
  const vendor = catalogEntries.value.find(entry => (entry.family_id || entry.id) === value);
  if (vendor) applyVendor(vendor.id, true);
}

function openWebsite(vendor: Api.Management.ProviderCatalogEntry | null) {
  if (vendor?.website_url) window.open(vendor.website_url, '_blank', 'noopener,noreferrer');
}

function openApiKeyUrl() {
  if (selectedVendor.value?.api_key_url) window.open(selectedVendor.value.api_key_url, '_blank', 'noopener,noreferrer');
}

const columns: DataTableColumns<Api.Management.ProviderAccount> = [
  { title: '名称', key: 'name', minWidth: 150 },
  { title: '厂商', key: 'provider', width: 140, render: row => providerLabel(row.provider) },
  { title: '服务版本', key: 'version', width: 160, render: row => findVendor(row.provider)?.version_label ?? '自定义' },
  { title: '结算币种', key: 'billing_currency', width: 100 },
  { title: '区域', key: 'region', width: 100, render: row => row.region ?? '-' },
  { title: '接口地址', key: 'base_url', minWidth: 240, ellipsis: { tooltip: true }, render: row => row.base_url ?? findVendor(row.provider)?.api_base_url ?? '-' },
  { title: '状态', key: 'status', width: 90, render: row => h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status, 'account') }) },
  {
    title: '健康', key: 'health_status', width: 160,
    render: row => h('span', { title: healthSummary(row) }, [
      h(NTag, { type: healthTagType(row.health_status), size: 'small', round: true }, { default: () => healthStatusText(row.health_status) }),
      row.requests_24h > 0 ? h('span', { class: 'ml-6px text-12px text-#6b7280' }, `${Math.round((row.success_rate_24h ?? 0) * 100)}%`) : null
    ])
  },
  { title: '创建时间', key: 'created_at', width: 170, render: row => formatTime(row.created_at) },
  {
    title: '操作', key: 'actions', width: 310,
    render(row) {
      const moreOptions = [{ label: '删除账号', key: 'delete' }];
      if (findVendor(row.provider)?.balance_kind) moreOptions.unshift({ label: '查询余额', key: 'balance' });
      return h(NSpace, { size: 4 }, {
        default: () => [
          h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, { default: () => '编辑' }),
          h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openModels(row) }, { default: () => '模型' }),
          h(NButton, { size: 'small', quaternary: true, type: 'info', onClick: () => handleTest(row) }, { default: () => '测试' }),
          h(NButton, { size: 'small', quaternary: true, type: row.status === 'active' ? 'warning' : 'success', onClick: () => toggleStatus(row) }, { default: () => row.status === 'active' ? '停用' : '启用' }),
          h(NDropdown, { options: moreOptions, onSelect: key => key === 'delete' ? handleDelete(row.id) : handleBalance(row) }, {
            default: () => h(NButton, { size: 'small', quaternary: true }, { default: () => '更多' })
          })
        ]
      });
    }
  }
];

async function loadCatalog() {
  catalogLoading.value = true;
  try {
    const { data, error } = await fetchProviderCatalog();
    if (!error && data) catalogGroups.value = data;
  } finally {
    catalogLoading.value = false;
  }
}

async function loadData() {
  loading.value = true;
  try {
    const { data, error } = await fetchProviderAccounts();
    if (!error && data) list.value = data;
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  selectedFamilyID.value = '';
  Object.assign(formModel, { provider: '', name: '', region: '', base_url: '', billing_currency: 'USD', api_key: '' });
  showCreate.value = true;
}

function openEdit(row: Api.Management.ProviderAccount) {
  Object.assign(editModel, {
    id: row.id, provider: row.provider, name: row.name,
    region: row.region ?? '', base_url: row.base_url ?? findVendor(row.provider)?.base_url ?? '', billing_currency: row.billing_currency
  });
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { data, error } = await fetchCreateProviderAccount({
      provider: formModel.provider,
      name: formModel.name.trim(),
      region: formModel.region.trim() || undefined,
      base_url: selectedVendor.value?.editable_url ? formModel.base_url.trim() || undefined : undefined,
      billing_currency: formModel.billing_currency,
      api_key: formModel.api_key
    });
    if (!error && data) {
      window.$message?.success('账号已创建，正在获取可用模型');
      showCreate.value = false;
      await loadData();
      await openModels({ id: data.id, name: formModel.name.trim() });
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
      base_url: editVendor.value?.editable_url ? editModel.base_url.trim() || undefined : undefined,
      billing_currency: editModel.billing_currency
    });
    if (!error) {
      window.$message?.success('账号已更新');
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(row: Api.Management.ProviderAccount) {
  const status = row.status === 'active' ? 'disabled' : 'active';
  const { error } = await fetchUpdateProviderAccountStatus(row.id, status);
  if (!error) {
    window.$message?.success(status === 'active' ? '账号已启用' : '账号已停用');
    await loadData();
  }
}

async function handleTest(row: Api.Management.ProviderAccount) {
  const { data, error } = await fetchTestProviderAccount(row.id);
  if (!error && data) {
    if (data.ok) window.$message?.success('连接正常');
    else window.$message?.error(data.message);
  }
}

const showModels = ref(false);
const modelLoading = ref(false);
const modelRows = ref<Api.Management.ProviderModel[]>([]);
const modelTitle = ref('可用模型');
const modelAccountID = ref('');
const selectedModelKeys = ref<string[]>([]);
const modelColumns: DataTableColumns<Api.Management.ProviderModel> = [
  { type: 'selection' },
  { title: '模型标识', key: 'id', minWidth: 220 },
  { title: '输入价格（美元/百万令牌）', key: 'input_cost_per_million', width: 210, render: row => row.input_cost_per_million === null ? '暂无数据' : formatCostUSD(row.input_cost_per_million) },
  { title: '输出价格（美元/百万令牌）', key: 'output_cost_per_million', width: 210, render: row => row.output_cost_per_million === null ? '暂无数据' : formatCostUSD(row.output_cost_per_million) }
];

async function openModels(row: Pick<Api.Management.ProviderAccount, 'id' | 'name'>) {
  showModels.value = true;
  modelLoading.value = true;
  modelRows.value = [];
  selectedModelKeys.value = [];
  modelAccountID.value = row.id;
  modelTitle.value = `${row.name} · 可用模型`;
  try {
    const { data, error } = await fetchProviderModels(row.id);
    if (!error && data) modelRows.value = data.models;
  } finally {
    modelLoading.value = false;
  }
}

async function addModelsToService() {
  if (!modelAccountID.value || !selectedModelKeys.value.length) return;
  await router.push({
    name: 'model-management_model-services',
    query: { account: modelAccountID.value, models: selectedModelKeys.value.join(',') }
  });
}

const showBalance = ref(false);
const balanceLoading = ref(false);
const balanceRows = ref<Api.Management.ProviderBalanceEntry[]>([]);
const balanceTitle = ref('余额与限额');
const balanceEntries = computed(() => balanceRows.value.filter(item => item.kind !== 'quota'));
const quotaEntries = computed(() => balanceRows.value.filter(item => item.kind === 'quota'));
const balanceColumns: DataTableColumns<Api.Management.ProviderBalanceEntry> = [
  { title: '账户', key: 'label', minWidth: 130 },
  { title: '可用', key: 'remaining', width: 140, render: row => formatUnitAmount(row.remaining, row.unit) },
  { title: '总额', key: 'total', width: 140, render: row => formatUnitAmount(row.total, row.unit) },
  { title: '已用', key: 'used', width: 140, render: row => formatUnitAmount(row.used, row.unit) }
];

async function handleBalance(row: Api.Management.ProviderAccount) {
  showBalance.value = true;
  balanceLoading.value = true;
  balanceTitle.value = `余额与限额 - ${row.name}`;
  try {
    const { data, error } = await fetchProviderBalance(row.id);
    balanceRows.value = !error && data ? data.balances ?? [] : [];
  } finally {
    balanceLoading.value = false;
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteProviderAccount(id);
  if (!error) {
    window.$message?.success('账号已删除');
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
    <NCard title="供应商账号" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" @click="openCreate">新建账号</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1450" :row-key="row => row.id" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建供应商账号" class="w-640px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem label="厂商" path="provider">
          <NSelect :value="selectedFamilyID" :options="vendorOptions" :loading="catalogLoading" filterable placeholder="选择模型厂商或服务平台" @update:value="handleFamilyChange" />
        </NFormItem>
        <NFormItem v-if="versionOptions.length > 1" label="服务版本">
          <NSelect :value="formModel.provider" :options="versionOptions" @update:value="value => applyVendor(value)" />
        </NFormItem>
        <NFormItem v-if="selectedVendor?.website_url" label="官网">
          <NButton text type="primary" @click="openWebsite(selectedVendor)">{{ selectedVendor.website_url }} ↗</NButton>
        </NFormItem>
        <NFormItem v-if="selectedVendor" label="接口地址" path="base_url">
          <NInput v-if="selectedVendor.editable_url" v-model:value="formModel.base_url" placeholder="填写服务接口地址" />
          <NInput v-else :value="selectedVendor.api_base_url" readonly />
        </NFormItem>
        <NFormItem label="账号名称" path="name"><NInput v-model:value="formModel.name" placeholder="例如：主账号、备用账号" /></NFormItem>
        <NFormItem label="区域" path="region"><NInput v-model:value="formModel.region" placeholder="可选，例如：华北、新加坡" /></NFormItem>
        <NFormItem label="结算币种">
          <NSelect v-model:value="formModel.billing_currency" :options="settlementCurrencyOptions" />
          <NText depth="3" class="mt-4px block text-12px">用于模型成本、路由比较和预算统计；不改变账户余额接口的原币种。</NText>
        </NFormItem>
        <NFormItem v-if="selectedVendor?.api_key_url" label=" " :show-feedback="false">
          <NButton text type="primary" @click="openApiKeyUrl">前往厂商控制台获取 API Key ↗</NButton>
        </NFormItem>
        <NFormItem label="API Key" path="api_key"><NInput v-model:value="formModel.api_key" type="password" show-password-on="click" placeholder="服务端加密保存，不会回显" /></NFormItem>
        <NAlert type="info" :bordered="false">创建后会自动获取可用模型；在模型服务中可继续调整价格、优先级和路由参数。</NAlert>
      </NForm>
      <template #footer><NSpace justify="end"><NButton @click="showCreate = false">取消</NButton><NButton type="primary" :loading="submitting" @click="handleCreate">创建并获取模型</NButton></NSpace></template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" title="编辑供应商账号" class="w-600px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="100">
        <NFormItem label="厂商"><NTag :bordered="false">{{ providerLabel(editModel.provider) }}</NTag><NText depth="2">{{ editVendor?.version_label }}</NText></NFormItem>
        <NFormItem v-if="editVendor?.website_url" label="官网"><NButton text type="primary" @click="openWebsite(editVendor)">{{ editVendor.website_url }} ↗</NButton></NFormItem>
        <NFormItem label="账号名称" path="name"><NInput v-model:value="editModel.name" /></NFormItem>
        <NFormItem label="区域"><NInput v-model:value="editModel.region" placeholder="可选" /></NFormItem>
        <NFormItem label="结算币种"><NSelect v-model:value="editModel.billing_currency" :options="settlementCurrencyOptions" /></NFormItem>
        <NFormItem label="接口地址"><NInput v-model:value="editModel.base_url" :readonly="!editVendor?.editable_url" /></NFormItem>
        <NAlert type="info" :bordered="false">API Key 与厂商类型创建后不可修改；更换密钥请重新创建账号。</NAlert>
      </NForm>
      <template #footer><NSpace justify="end"><NButton @click="showEdit = false">取消</NButton><NButton type="primary" :loading="submitting" @click="handleUpdate">保存</NButton></NSpace></template>
    </NModal>

    <NModal v-model:show="showModels" preset="card" :title="modelTitle" class="w-720px">
      <NAlert type="info" :bordered="false" class="mb-12px">模型列表来自账号实际上游；价格由公开模型目录补全，未匹配时可手动填写。</NAlert>
      <NDataTable
        v-model:checked-row-keys="selectedModelKeys"
        :columns="modelColumns"
        :data="modelRows"
        :loading="modelLoading"
        :row-key="row => row.id"
        size="small"
        :bordered="false"
        :max-height="420"
        :scroll-x="650"
      />
      <template #footer>
        <NSpace justify="space-between">
          <NText depth="2">已选择 {{ selectedModelKeys.length }} 个模型</NText>
          <NButton type="primary" :disabled="selectedModelKeys.length === 0" @click="addModelsToService">添加到模型服务</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showBalance" preset="card" :title="balanceTitle" class="w-680px">
      <NSpin :show="balanceLoading">
        <div v-if="quotaEntries.length" class="mb-16px">
          <NCard v-for="row in quotaEntries" :key="row.label" size="small" class="mb-8px" embedded>
            <NSpace justify="space-between" align="center" class="mb-8px">
              <NText strong>{{ row.label }}</NText>
              <NText depth="2">已用 {{ formatUnitAmount(row.used, row.unit) }} · 剩余 {{ formatUnitAmount(row.remaining, row.unit) }}</NText>
            </NSpace>
            <NProgress type="line" :percentage="row.used ?? 0" :status="(row.used ?? 0) >= 90 ? 'error' : (row.used ?? 0) >= 70 ? 'warning' : 'success'" />
            <NText v-if="row.next_reset_time" depth="3" class="mt-8px block text-12px">下次重置：{{ formatResetTime(row.next_reset_time) }}</NText>
          </NCard>
        </div>
        <NDataTable v-if="balanceEntries.length" :columns="balanceColumns" :data="balanceEntries" size="small" :bordered="false" />
        <NEmpty v-if="!balanceLoading && !balanceRows.length" description="未获取到余额或限额数据" />
      </NSpin>
    </NModal>
  </div>
</template>
