<script setup lang="ts">
import { h, onMounted, reactive, ref, computed } from 'vue';
import { NButton, NDatePicker, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { formatTime } from '@/utils/admin-status';
import {
  fetchApiKeys,
  fetchApplications,
  fetchBudgets,
  fetchCreateBudget,
  fetchDeleteBudget,
  fetchProjects,
  fetchUpdateBudget
} from '@/service/api';
import { $t } from '@/locales';

defineOptions({
  name: 'ObservabilityBudgets'
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

const formRules = computed<FormRules>(() => ({
  scope_id: {
    required: true,
    message: $t('business.select-a-budget-subject'),
    trigger: 'change'
  },
  window_range: {
    required: true,
    message: $t('business.select-a-time-window'),
    trigger: 'change'
  },
  limit_amount: {
    required: true,
    message: $t('business.enter-a-budget-limit'),
    trigger: 'blur'
  }
}));
const editRules = computed<FormRules>(() => ({
  limit_amount: {
    required: true,
    message: $t('business.enter-a-budget-limit'),
    trigger: 'blur'
  }
}));

const scopeTypeOptions = computed(() => [
  { label: $t('business.organization'), value: 'organization' },
  { label: $t('business.project'), value: 'project' },
  { label: $t('business.application'), value: 'application' },
  { label: 'API Key', value: 'api_key' }
]);

const scopeOptions = ref<{ label: string; value: string }[]>([]);

const currencyOptions = computed(() => [
  { label: $t('business.usd-us-dollar'), value: 'USD' },
  { label: $t('business.cny-chinese-yuan'), value: 'CNY' },
  { label: $t('business.eur-euro'), value: 'EUR' },
  { label: $t('business.gbp-british-pound'), value: 'GBP' },
  { label: $t('business.jpy-japanese-yen'), value: 'JPY' }
]);

function reloadScopeOptions() {
  switch (formModel.scope_type) {
    case 'organization':
      scopeOptions.value = [{ label: $t('business.current-organization'), value: 'organization' }];
      break;
    case 'project':
      scopeOptions.value = projects.value.map(item => ({
        label: item.name,
        value: item.id
      }));
      break;
    case 'application':
      scopeOptions.value = applications.value.map(item => ({
        label: $t('business.budgetSubjectWithProject', {
          name: item.name,
          project: projects.value.find(p => p.id === item.project_id)?.name ?? '-'
        }),
        value: item.id
      }));
      break;
    case 'api_key':
      scopeOptions.value = apiKeys.value.map(item => ({
        label: item.name,
        value: item.id
      }));
      break;
  }
}

function scopeTypeText(t: string) {
  return scopeTypeOptions.value.find(item => item.value === t)?.label ?? t;
}

function windowState(row: Api.Management.Budget) {
  const now = Date.now();
  const start = new Date(row.window_start).getTime();
  const end = new Date(row.window_end).getTime();
  if (now < start) return $t('business.not-started');
  if (now >= end) return $t('business.ended');
  return $t('business.active');
}

function formatAmount(value: string) {
  return value;
}

const columns = computed<DataTableColumns<Api.Management.Budget>>(() => [
  {
    title: $t('business.scope'),
    key: 'scope_type',
    width: 90,
    render: row => scopeTypeText(row.scope_type)
  },
  { title: $t('business.subject'), key: 'scope_name', width: 140 },
  {
    title: $t('business.start-time'),
    key: 'window_start',
    width: 170,
    render: row => formatTime(row.window_start)
  },
  {
    title: $t('business.end-time'),
    key: 'window_end',
    width: 170,
    render: row => formatTime(row.window_end)
  },
  {
    title: $t('business.limit'),
    key: 'limit_amount',
    width: 120,
    render: row => `${row.currency} ${formatAmount(row.limit_amount)}`
  },
  {
    title: $t('business.used'),
    key: 'spent_amount',
    width: 120,
    render(row) {
      const type = row.exceeded ? 'error' : 'default';
      return h(
        NTag,
        { type, size: 'small', round: true },
        { default: () => `${row.currency} ${formatAmount(row.spent_amount)}` }
      );
    }
  },
  {
    title: $t('business.window-status'),
    key: 'window_state',
    width: 90,
    render: row => windowState(row)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 150,
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
              { default: () => $t('business.adjust-limit') }
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
                default: () => $t('business.windows-with-settlement-records-cannot')
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
      window.$message?.success($t('business.budget-created'));
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
      window.$message?.success($t('business.budget-limit-adjusted'));
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
    window.$message?.success($t('business.budget-deleted'));
    await loadData();
  }
}

onMounted(() => {
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :title="$t('business.budget')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton type="primary" @click="openCreate">{{ $t('business.create-budget') }}</NButton>
        </NSpace>
      </template>
      <NAlert type="info" :bordered="false" class="mb-12px">
        {{ $t('business.budgets-set-window-limits-by') }}ceeded)。
      </NAlert>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1100" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-budget')" class="w-560px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem :label="$t('business.scope-type')" path="scope_type">
          <NSelect
            v-model:value="formModel.scope_type"
            :options="scopeTypeOptions"
            @update:value="reloadScopeOptions"
          />
        </NFormItem>
        <NFormItem :label="$t('business.budget-subject')" path="scope_id">
          <NSelect
            v-model:value="formModel.scope_id"
            :options="scopeOptions"
            :placeholder="$t('business.select-subject')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.time-window')" path="window_range">
          <NDatePicker v-model:value="formModel.window_range" type="datetimerange" clearable class="w-full" />
        </NFormItem>
        <NFormItem :label="$t('business.currency')" path="currency">
          <NSelect v-model:value="formModel.currency" :options="currencyOptions" />
        </NFormItem>
        <NFormItem :label="$t('business.limit')" path="limit_amount">
          <NInputNumber
            v-model:value="formModel.limit_amount"
            :min="0"
            class="w-full"
            :placeholder="$t('business.maximum-spend-allowed-in-the')"
          >
            <template #prefix>{{ formModel.currency }}</template>
          </NInputNumber>
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">{{ $t('business.create') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" :title="$t('business.adjust-budget-limit')" class="w-480px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="100">
        <NFormItem :label="$t('business.new-limit')" path="limit_amount">
          <NInputNumber v-model:value="editModel.limit_amount" :min="0" class="w-full" />
        </NFormItem>
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
