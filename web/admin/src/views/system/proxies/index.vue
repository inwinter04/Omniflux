<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { formatTime } from '@/utils/admin-status';
import { fetchCreatePlatformProxy, fetchDeletePlatformProxy, fetchPlatformProxies, fetchUpdatePlatformProxy } from '@/service/api';
import { $t } from '@/locales';

defineOptions({ name: 'SystemProxies' });

const loading = ref(true);
const submitting = ref(false);
const list = ref<Api.Management.PlatformProxy[]>([]);
const providerOptions = ref<SelectOption[]>([]);
const searchKeyword = ref('');
const showCreate = ref(false);
const showEdit = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);

const formModel = reactive({
  provider_keys: [] as string[],
  name: '',
  proxy_type: 'http' as Api.Management.PlatformProxyType,
  proxy_url: ''
});

const editModel = reactive({
  id: '',
  provider_keys: [] as string[],
  name: '',
  proxy_type: 'http' as Api.Management.PlatformProxyType,
  original_proxy_type: 'http' as Api.Management.PlatformProxyType,
  proxy_url: ''
});

const filteredList = computed(() => {
  const keyword = searchKeyword.value.trim().toLowerCase();
  if (!keyword) return list.value;
  return list.value.filter(item =>
    item.name.toLowerCase().includes(keyword) ||
    item.provider_keys.some(provider => provider.toLowerCase().includes(keyword)) ||
    item.provider_labels.some(label => label.toLowerCase().includes(keyword))
  );
});

const formRules = computed<FormRules>(() => ({
  provider_keys: {
    validator: (_rule, value: string[]) => (value.length > 0 ? true : new Error($t('business.select-proxy-provider'))),
    trigger: 'change'
  },
  name: { required: true, message: $t('business.enter-proxy-name'), trigger: 'blur' },
  proxy_url: {
    required: true,
    validator: (_rule, value: string) =>
      validProxyURL(formModel.proxy_type, value.trim()) ? true : new Error($t('business.proxy-url-invalid')),
    trigger: 'blur'
  }
}));

const editRules = computed<FormRules>(() => ({
  provider_keys: {
    validator: (_rule, value: string[]) => (value.length > 0 ? true : new Error($t('business.select-proxy-provider'))),
    trigger: 'change'
  },
  name: { required: true, message: $t('business.enter-proxy-name'), trigger: 'blur' },
  proxy_url: {
    validator: (_rule, value: string) => {
      if (!value.trim() && editModel.proxy_type === editModel.original_proxy_type) return true;
      if (!value.trim()) return new Error($t('business.proxy-url-required'));
      return validProxyURL(editModel.proxy_type, value.trim()) ? true : new Error($t('business.proxy-url-invalid'));
    },
    trigger: 'blur'
  }
}));
const columns = computed<DataTableColumns<Api.Management.PlatformProxy>>(() => [
  { title: $t('business.proxy-name'), key: 'name', minWidth: 160, ellipsis: { tooltip: true } },
  {
    title: $t('business.proxy-provider'),
    key: 'provider_keys',
    minWidth: 220,
    render: row =>
      h(
        'div',
        { class: 'flex flex-wrap gap-4px' },
        row.provider_labels.map(label => h(NTag, { size: 'small', round: true }, { default: () => label }))
      )
  },
  {
    title: $t('business.proxy-type'),
    key: 'proxy_type',
    width: 100,
    render: row =>
      h(NTag, { size: 'small', round: true, type: row.proxy_type === 'http' ? 'info' : 'success' }, {
        default: () => proxyTypeText(row.proxy_type)
      })
  },
  {
    title: $t('business.proxy-url'),
    key: 'proxy_url',
    minWidth: 260,
    ellipsis: { tooltip: true },
    render: row => row.proxy_url || '-'
  },
  { title: $t('business.active-accounts'), key: 'active_accounts', width: 120 },
  {
    title: $t('business.updated-at'),
    key: 'updated_at',
    width: 170,
    render: row => (row.updated_at ? formatTime(row.updated_at) : '-')
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 150,
    render: row => [
      h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, {
        default: () => $t('business.edit')
      }),
      h(NPopconfirm, { onPositiveClick: () => handleDelete(row.id) }, {
        trigger: () => h(NButton, { size: 'small', quaternary: true, type: 'error' }, { default: () => $t('business.delete') }),
        default: () => $t('business.delete-proxy-confirm')
      })
    ]
  }
]);

function proxyTypeText(value: Api.Management.PlatformProxyType) {
  return value === 'http' ? $t('business.proxy-http') : $t('business.proxy-socks5');
}

function validProxyURL(proxyType: Api.Management.PlatformProxyType, value: string) {
  try {
    const parsed = new URL(value);
    const allowedProtocols = proxyType === 'http' ? ['http:', 'https:'] : ['socks5:', 'socks5h:'];
    const portMatch = value.match(/:(\d+)$/);
    const port = portMatch ? Number(portMatch[1]) : Number.NaN;
    return (
      allowedProtocols.includes(parsed.protocol) &&
      parsed.hostname !== '' &&
      Number.isInteger(port) &&
      port > 0 &&
      port <= 65535 &&
      (parsed.pathname === '' || parsed.pathname === '/') &&
      !value.endsWith('/') &&
      parsed.search === '' &&
      parsed.hash === ''
    );
  } catch {
    return false;
  }
}
async function loadData() {
  loading.value = true;
  try {
    const { data, error } = await fetchPlatformProxies();
    if (!error && data) {
      list.value = data.proxies;
      providerOptions.value = data.provider_options.map(option => ({ label: option.label, value: option.value }));
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  Object.assign(formModel, { provider_keys: [], name: '', proxy_type: 'http', proxy_url: '' });
  showCreate.value = true;
}

function openEdit(row: Api.Management.PlatformProxy) {
  Object.assign(editModel, {
    id: row.id,
    provider_keys: row.provider_keys,
    name: row.name,
    proxy_type: row.proxy_type,
    original_proxy_type: row.proxy_type,
    proxy_url: ''
  });
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchCreatePlatformProxy({
      provider_keys: formModel.provider_keys,
      name: formModel.name.trim(),
      proxy_type: formModel.proxy_type,
      proxy_url: formModel.proxy_url.trim()
    });
    if (!error) {
      window.$message?.success($t('business.proxy-saved'));
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
    const { error } = await fetchUpdatePlatformProxy(editModel.id, {
      provider_keys: editModel.provider_keys,
      name: editModel.name.trim(),
      proxy_type: editModel.proxy_type,
      proxy_url: editModel.proxy_url.trim() || undefined
    });
    if (!error) {
      window.$message?.success($t('business.proxy-saved'));
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeletePlatformProxy(id);
  if (!error) {
    window.$message?.success($t('business.proxy-deleted'));
    await loadData();
  }
}

onMounted(loadData);
</script>
<template>
  <div class="flex flex-col gap-16px">
    <NCard :title="$t('business.proxy-governance')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">
            {{ $t('business.refresh') }}
          </NButton>
          <NButton type="primary" @click="openCreate">{{ $t('business.create-proxy') }}</NButton>
        </NSpace>
      </template>

      <NAlert type="info" :show-icon="true" class="mb-16px">
        {{ $t('business.proxy-governance-description') }}
      </NAlert>

      <div class="mb-12px flex justify-end">
        <NInput v-model:value="searchKeyword" clearable :placeholder="$t('business.search-proxy')" class="w-260px" />
      </div>

      <NDataTable
        :columns="columns"
        :data="filteredList"
        :loading="loading"
        :row-key="row => row.id"
        size="small"
        :bordered="false"
        :scroll-x="1200"
      />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-proxy')" class="w-640px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="120">
        <NFormItem :label="$t('business.proxy-provider')" path="provider_keys">
          <NSelect
            v-model:value="formModel.provider_keys"
            multiple
            filterable
            :options="providerOptions"
            :placeholder="$t('business.select-proxy-provider')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.proxy-name')" path="name">
          <NInput v-model:value="formModel.name" :placeholder="$t('business.enter-proxy-name')" />
        </NFormItem>
        <NFormItem :label="$t('business.proxy-url')" path="proxy_url">
          <NInput
            v-model:value="formModel.proxy_url"
            type="password"
            show-password-on="click"
            :placeholder="$t('business.enter-proxy-url')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.proxy-type')" path="proxy_type">
          <NRadioGroup v-model:value="formModel.proxy_type">
            <NRadioButton value="http">{{ $t('business.proxy-http') }}</NRadioButton>
            <NRadioButton value="socks5">{{ $t('business.proxy-socks5') }}</NRadioButton>
          </NRadioGroup>
        </NFormItem>
        <NText depth="3" class="mb-12px block text-12px">{{ $t('business.proxy-encrypted-and-used-by') }}</NText>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">{{ $t('business.create-proxy') }}</NButton>
        </NSpace>
      </template>
    </NModal>
    <NModal v-model:show="showEdit" preset="card" :title="$t('business.edit-proxy')" class="w-640px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="120">
        <NFormItem :label="$t('business.proxy-provider')" path="provider_keys">
          <NSelect
            v-model:value="editModel.provider_keys"
            multiple
            filterable
            :options="providerOptions"
            :placeholder="$t('business.select-proxy-provider')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.proxy-name')" path="name">
          <NInput v-model:value="editModel.name" />
        </NFormItem>
        <NFormItem :label="$t('business.proxy-url')" path="proxy_url">
          <NInput
            v-model:value="editModel.proxy_url"
            type="password"
            show-password-on="click"
            :placeholder="editModel.proxy_type === editModel.original_proxy_type ? $t('business.leave-blank-to-keep-proxy-url') : $t('business.enter-proxy-url')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.proxy-type')" path="proxy_type">
          <NRadioGroup v-model:value="editModel.proxy_type">
            <NRadioButton value="http">{{ $t('business.proxy-http') }}</NRadioButton>
            <NRadioButton value="socks5">{{ $t('business.proxy-socks5') }}</NRadioButton>
          </NRadioGroup>
        </NFormItem>
        <NText depth="3" class="mb-12px block text-12px">{{ $t('business.proxy-encrypted-and-used-by') }}</NText>
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

<style scoped></style>