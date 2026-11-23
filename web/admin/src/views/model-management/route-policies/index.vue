<script setup lang="ts">
import { h, onMounted, reactive, ref, computed } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';
import {
  fetchCreateRoutePolicy,
  fetchDeleteRoutePolicy,
  fetchProviderCatalog,
  fetchRoutePolicies,
  fetchUpdateRoutePolicy,
  fetchUpdateRoutePolicyStatus
} from '@/service/api';
import { $t } from '@/locales';

defineOptions({
  name: 'ModelManagementRoutePolicies'
});

const strategyOptions = computed(() => [
  { label: $t('business.cost-first'), value: 'cost_first' },
  { label: $t('business.quality-first'), value: 'quality_first' },
  { label: $t('business.balanced'), value: 'balanced' }
]);

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
      children: group.entries.map(entry => ({
        label: entry.name,
        value: entry.id
      }))
    });
  }
  options.push({
    type: 'group',
    label: $t('business.custom'),
    key: 'custom',
    children: [
      {
        label: $t('business.custom-openai-compatible'),
        value: 'openai_compatible'
      }
    ]
  });
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

const formRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-a-policy-name'),
    trigger: 'blur'
  }
}));
const editRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-a-policy-name'),
    trigger: 'blur'
  }
}));

function strategyLabel(value: string) {
  return strategyOptions.value.find(item => item.value === value)?.label ?? value;
}

const columns = computed<DataTableColumns<Api.Management.RoutePolicy>>(() => [
  { title: $t('business.name'), key: 'name', width: 160 },
  {
    title: $t('business.strategy'),
    key: 'strategy',
    width: 180,
    render: row => strategyLabel(row.strategy)
  },
  { title: $t('business.version'), key: 'version', width: 70 },
  { title: $t('business.failover-limit'), key: 'fallback_limit', width: 120 },
  {
    title: $t('business.upstream-timeout-ms'),
    key: 'provider_timeout_ms',
    width: 120
  },
  {
    title: $t('business.first-token-timeout-ms'),
    key: 'first_token_timeout_ms',
    width: 140
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 90,
    render(row) {
      return h(
        NTag,
        { type: statusTagType(row.status), size: 'small', round: true },
        { default: () => statusText(row.status, 'policy') }
      );
    }
  },
  {
    title: $t('business.updated-at'),
    key: 'updated_at',
    width: 180,
    render: row => formatTime(row.updated_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 230,
    render(row) {
      return h(
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
                type: row.status === 'active' ? 'warning' : 'success',
                onClick: () => toggleStatus(row)
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
                default: () => $t('business.deletion-is-blocked-when-the')
              }
            )
          ]
        }
      );
    }
  }
]);

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
      window.$message?.success($t('business.route-policy-created'));
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
      window.$message?.success($t('business.route-policy-updated'));
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
    window.$message?.success(
      status === 'active' ? $t('business.route-policy-enabled') : $t('business.route-policy-disabled')
    );
    await loadData();
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteRoutePolicy(id);
  if (!error) {
    window.$message?.success($t('business.route-policy-deleted'));
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
    <NCard :title="$t('business.route-policy')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton type="primary" @click="openCreate">{{ $t('business.create-policy') }}</NButton>
        </NSpace>
      </template>
      <NAlert type="info" :bordered="false" class="mb-12px">
        {{ $t('business.scoring-weights-apply-according-to') }}traints。
      </NAlert>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1200" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-route-policy')" class="w-520px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="140">
        <NFormItem :label="$t('business.policy-name')" path="name">
          <NInput v-model:value="formModel.name" :placeholder="$t('business.for-example-default-balanced-policy')" />
        </NFormItem>
        <NFormItem :label="$t('business.scheduling-strategy')" path="strategy">
          <NSelect v-model:value="formModel.strategy" :options="strategyOptions" />
        </NFormItem>
        <NFormItem :label="$t('business.failover-limit')" path="fallback_limit">
          <NInputNumber v-model:value="formModel.fallback_limit" :min="0" class="w-full" />
        </NFormItem>
        <NFormItem :label="$t('business.upstream-timeout-ms')" path="provider_timeout_ms">
          <NInputNumber v-model:value="formModel.provider_timeout_ms" :min="1" class="w-full" />
        </NFormItem>
        <NFormItem :label="$t('business.first-token-timeout-ms')" path="first_token_timeout_ms">
          <NInputNumber v-model:value="formModel.first_token_timeout_ms" :min="1" class="w-full" />
        </NFormItem>

        <NDivider title-placement="left">{{ $t('business.hard-constraints-optional-settable-only') }}</NDivider>
        <NFormItem :label="$t('business.allowed-providers')" path="allowed_providers">
          <NSelect
            v-model:value="formModel.allowed_providers"
            multiple
            clearable
            tag
            :options="constraintProviderOptions"
            :placeholder="$t('business.leave-blank-for-no-limit')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.blocked-providers')" path="blocked_providers">
          <NSelect
            v-model:value="formModel.blocked_providers"
            multiple
            clearable
            tag
            :options="constraintProviderOptions"
            :placeholder="$t('business.leave-blank-for-no-limit')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.allowed-upstream-models')" path="allowed_upstream_models">
          <NSelect
            v-model:value="formModel.allowed_upstream_models"
            multiple
            clearable
            filterable
            tag
            :placeholder="$t('business.enter-a-model-name-and')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.blocked-upstream-models')" path="blocked_upstream_models">
          <NSelect
            v-model:value="formModel.blocked_upstream_models"
            multiple
            clearable
            filterable
            tag
            :placeholder="$t('business.enter-a-model-name-and')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.blended-unit-price-ceiling')" path="max_cost_per_million">
          <NInputNumber
            v-model:value="formModel.max_cost_per_million"
            :min="0"
            class="w-full"
            :placeholder="$t('business.usd-per-million-leave-blank')"
          />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">{{ $t('business.create') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" :title="$t('business.edit-route-policy')" class="w-520px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="140">
        <NFormItem :label="$t('business.policy-name')" path="name">
          <NInput v-model:value="editModel.name" :placeholder="$t('business.policy-name')" />
        </NFormItem>
        <NFormItem :label="$t('business.scheduling-strategy')" path="strategy">
          <NSelect v-model:value="editModel.strategy" :options="strategyOptions" />
        </NFormItem>
        <NFormItem :label="$t('business.failover-limit')" path="fallback_limit">
          <NInputNumber v-model:value="editModel.fallback_limit" :min="0" class="w-full" />
        </NFormItem>
        <NFormItem :label="$t('business.upstream-timeout-ms')" path="provider_timeout_ms">
          <NInputNumber v-model:value="editModel.provider_timeout_ms" :min="1" class="w-full" />
        </NFormItem>
        <NFormItem :label="$t('business.first-token-timeout-ms')" path="first_token_timeout_ms">
          <NInputNumber v-model:value="editModel.first_token_timeout_ms" :min="1" class="w-full" />
        </NFormItem>
        <NAlert type="info" :bordered="false">{{ $t('business.switching-the-strategy-resets-scoring') }}</NAlert>
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
