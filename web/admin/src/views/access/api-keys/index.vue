<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';
import {
  fetchApiKeys,
  fetchApplications,
  fetchCreateApiKey,
  fetchDeleteApiKey,
  fetchProjects,
  fetchUpdateApiKey
} from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';

defineOptions({
  name: 'AccessApiKeys'
});

const { canManage } = usePermission();

const loading = ref(true);
const list = ref<Api.Management.ApiKey[]>([]);
const projects = ref<Api.Management.Project[]>([]);
const applications = ref<Api.Management.Application[]>([]);

const showCreate = ref(false);
const creating = ref(false);
const formRef = ref<FormInst | null>(null);
const formModel = reactive({
  name: '',
  project_id: null as string | null,
  application_id: null as string | null,
  expires_at: null as number | null,
  rate_limit_per_min: null as number | null
});

const showRateLimit = ref(false);
const submittingRateLimit = ref(false);
const rateLimitFormRef = ref<FormInst | null>(null);
const rateLimitModel = reactive({
  id: '',
  name: '',
  rate_limit_per_min: null as number | null
});

const formRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-an-api-key-name'),
    trigger: 'blur'
  },
  project_id: {
    required: true,
    message: $t('business.select-a-project'),
    trigger: 'change'
  },
  application_id: {
    required: true,
    message: $t('business.select-an-application'),
    trigger: 'change'
  }
}));

const createdKey = ref('');

const projectOptions = computed(() => projects.value.map(item => ({ label: item.name, value: item.id })));
const applicationOptions = computed(() =>
  applications.value
    .filter(item => item.project_id === formModel.project_id)
    .map(item => ({ label: item.name, value: item.id }))
);

function projectName(id: string) {
  return projects.value.find(item => item.id === id)?.name ?? '-';
}

function applicationName(id: string) {
  return applications.value.find(item => item.id === id)?.name ?? '-';
}

const columns = computed<DataTableColumns<Api.Management.ApiKey>>(() => [
  { title: $t('business.name'), key: 'name', width: 160 },
  { title: $t('business.prefix'), key: 'key_prefix', width: 120 },
  {
    title: $t('business.project'),
    key: 'project_id',
    width: 140,
    render: row => projectName(row.project_id)
  },
  {
    title: $t('business.application'),
    key: 'application_id',
    width: 140,
    render: row => applicationName(row.application_id)
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 100,
    render(row) {
      return h(
        NTag,
        { type: statusTagType(row.status), size: 'small', round: true },
        { default: () => statusText(row.status, 'api_key') }
      );
    }
  },
  {
    title: $t('business.last-used'),
    key: 'last_used_at',
    width: 180,
    render: row => formatTime(row.last_used_at)
  },
  {
    title: $t('business.created-at'),
    key: 'created_at',
    width: 180,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 150,
    render(row) {
      if (!canManage.value) return [];
      const buttons: ReturnType<typeof h>[] = [];
      buttons.push(
        h(
          NButton,
          {
            size: 'small',
            quaternary: true,
            type: 'primary',
            onClick: () => openRateLimit(row)
          },
          { default: () => $t('business.rate-limit') }
        )
      );
      if (row.status === 'active') {
        buttons.push(
          h(
            NPopconfirm,
            { onPositiveClick: () => handleToggleStatus(row, 'disabled') },
            {
              trigger: () =>
                h(
                  NButton,
                  { size: 'small', type: 'warning', quaternary: true },
                  { default: () => $t('business.disable') }
                ),
              default: () => $t('business.authentication-stops-immediately-after-disabling')
            }
          )
        );
      } else {
        buttons.push(
          h(
            NButton,
            {
              size: 'small',
              type: 'primary',
              quaternary: true,
              onClick: () => handleToggleStatus(row, 'active')
            },
            { default: () => $t('business.enable') }
          )
        );
      }
      buttons.push(
        h(
          NPopconfirm,
          { onPositiveClick: () => handleDelete(row.id) },
          {
            trigger: () =>
              h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => $t('business.delete') }),
            default: () => $t('business.after-deletion-this-key-becomes')
          }
        )
      );
      return h(NSpace, { size: 4 }, { default: () => buttons });
    }
  }
]);

async function loadData() {
  loading.value = true;
  try {
    const [keysRes, projectsRes, applicationsRes] = await Promise.all([
      fetchApiKeys(),
      fetchProjects(),
      fetchApplications()
    ]);
    if (!keysRes.error && keysRes.data) {
      list.value = keysRes.data;
    }
    if (!projectsRes.error && projectsRes.data) {
      projects.value = projectsRes.data;
    }
    if (!applicationsRes.error && applicationsRes.data) {
      applications.value = applicationsRes.data;
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  formModel.name = '';
  formModel.project_id = null;
  formModel.application_id = null;
  formModel.rate_limit_per_min = null;
  showCreate.value = true;
}

function openRateLimit(row: Api.Management.ApiKey) {
  rateLimitModel.id = row.id;
  rateLimitModel.name = row.name;
  rateLimitModel.rate_limit_per_min = row.rate_limit_per_min;
  showRateLimit.value = true;
}

async function handleSaveRateLimit() {
  await rateLimitFormRef.value?.validate();
  submittingRateLimit.value = true;
  try {
    const { error } = await fetchUpdateApiKey(rateLimitModel.id, {
      rate_limit_per_min: rateLimitModel.rate_limit_per_min ?? undefined
    });
    if (!error) {
      window.$message?.success($t('business.rate-limit-threshold-updated'));
      showRateLimit.value = false;
      await loadData();
    }
  } finally {
    submittingRateLimit.value = false;
  }
}

function handleProjectChange() {
  const stillValid = applicationOptions.value.some(item => item.value === formModel.application_id);
  if (!stillValid) {
    formModel.application_id = null;
  }
}

async function handleCreate() {
  await formRef.value?.validate();
  creating.value = true;
  try {
    const { data, error } = await fetchCreateApiKey({
      name: formModel.name.trim(),
      project_id: formModel.project_id!,
      application_id: formModel.application_id!,
      expires_at: formModel.expires_at ? new Date(formModel.expires_at).toISOString() : undefined,
      rate_limit_per_min: formModel.rate_limit_per_min ?? undefined
    });
    if (!error && data) {
      createdKey.value = data.key;
      showCreate.value = false;
      await loadData();
    }
  } finally {
    creating.value = false;
  }
}

async function handleCopyKey() {
  await navigator.clipboard.writeText(createdKey.value);
  window.$message?.success($t('business.copied-to-clipboard'));
}

async function handleToggleStatus(row: Api.Management.ApiKey, status: 'active' | 'disabled') {
  const { error } = await fetchUpdateApiKey(row.id, { status });
  if (!error) {
    window.$message?.success(status === 'active' ? $t('business.api-key-enabled') : $t('business.api-key-disabled'));
    await loadData();
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteApiKey(id);
  if (!error) {
    window.$message?.success($t('business.api-key-deleted'));
    await loadData();
  }
}

onMounted(() => {
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard title="API Key" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton
            v-if="canManage"
            type="primary"
            :disabled="projectOptions.length === 0 || applications.length === 0"
            @click="openCreate"
          >
            {{
              projectOptions.length === 0 || applications.length === 0
                ? $t('business.projectsAndApplicationsRequired')
                : $t('business.create-api-key')
            }}
          </NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1100" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-api-key')" class="w-520px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem :label="$t('business.name')" path="name">
          <NInput v-model:value="formModel.name" :placeholder="$t('business.for-example-order-service')" />
        </NFormItem>
        <NFormItem :label="$t('business.project')" path="project_id">
          <NSelect
            v-model:value="formModel.project_id"
            :options="projectOptions"
            :placeholder="$t('business.select-project')"
            @update:value="handleProjectChange"
          />
        </NFormItem>
        <NFormItem :label="$t('business.application')" path="application_id">
          <NSelect
            v-model:value="formModel.application_id"
            :options="applicationOptions"
            :disabled="!formModel.project_id"
            :placeholder="$t('business.select-a-project-first')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.expires-at')" path="expires_at">
          <NDatePicker
            v-model:value="formModel.expires_at"
            type="datetime"
            clearable
            :placeholder="$t('business.optional-leave-blank-for-no')"
            class="w-full"
          />
        </NFormItem>
        <NFormItem :label="$t('business.rate-limit-minute')" path="rate_limit_per_min">
          <NInputNumber
            v-model:value="formModel.rate_limit_per_min"
            :min="1"
            clearable
            class="w-full"
            :placeholder="$t('business.optional-leave-blank-to-use')"
          />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="creating" @click="handleCreate">{{ $t('business.create') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      :show="createdKey !== ''"
      preset="card"
      :title="$t('business.api-key-created')"
      class="w-560px"
      :mask-closable="false"
    >
      <NAlert type="warning" :bordered="false" class="mb-12px">{{ $t('business.save-it-now-this-plaintext') }}</NAlert>
      <NInput :value="createdKey" readonly type="textarea" :autosize="{ minRows: 3 }" />
      <template #footer>
        <NSpace justify="end">
          <NButton @click="handleCopyKey">{{ $t('business.copy') }}</NButton>
          <NButton type="primary" @click="createdKey = ''">{{ $t('business.i-have-saved-it') }}</NButton>
        </NSpace>
      </template>
    </NModal>
    <NModal v-model:show="showRateLimit" preset="card" :title="$t('business.adjust-rate-limit')" class="w-440px">
      <NForm ref="rateLimitFormRef" :model="rateLimitModel" label-placement="left" label-width="100">
        <NFormItem :label="$t('business.name')">
          <NInput :value="rateLimitModel.name" disabled />
        </NFormItem>
        <NFormItem :label="$t('business.rate-limit-minute')" path="rate_limit_per_min">
          <NInputNumber
            v-model:value="rateLimitModel.rate_limit_per_min"
            :min="1"
            clearable
            class="w-full"
            :placeholder="$t('business.leave-blank-to-use-the')"
          />
        </NFormItem>
        <NAlert type="info" :bordered="false">{{ $t('business.the-threshold-takes-effect-immediately') }}</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showRateLimit = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submittingRateLimit" @click="handleSaveRateLimit">
            {{ $t('business.save') }}
          </NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>
