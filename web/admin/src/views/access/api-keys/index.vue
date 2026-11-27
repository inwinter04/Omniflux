<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { useRoute } from 'vue-router';
import { NButton, NDropdown, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, DropdownOption, FormInst, FormRules } from 'naive-ui';
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
import ClientSetupModal from './modules/client-setup-modal.vue';

defineOptions({
  name: 'AccessApiKeys'
});

const { has } = usePermission();
const canManage = computed(() => has('api_key:manage'));
const route = useRoute();

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
const createdKeyName = ref('');
const showQuickConnect = ref(false);
const quickConnectKey = ref('');
const quickConnectKeyName = ref('');

function openQuickConnect(plaintext = '', keyName = '') {
  quickConnectKey.value = plaintext;
  quickConnectKeyName.value = keyName;
  showQuickConnect.value = true;
}

function handleQuickConnectShowChange(visible: boolean) {
  showQuickConnect.value = visible;
  if (!visible) {
    quickConnectKey.value = '';
    quickConnectKeyName.value = '';
  }
}

function closeCreatedKeyModal() {
  createdKey.value = '';
  createdKeyName.value = '';
}

function openQuickConnectFromCreatedKey() {
  const key = createdKey.value;
  const keyName = createdKeyName.value;
  closeCreatedKeyModal();
  openQuickConnect(key, keyName);
}

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

// Key 是否可用不只取决于自身状态:已过期,或所属项目 / 应用被停用时,数据面都会拒绝。
function keyStatus(row: Api.Management.ApiKey): {
  type: ReturnType<typeof statusTagType>;
  text: string;
} {
  if (row.expired) return { type: 'warning', text: $t('business.key-expired') };
  if (row.status === 'active') {
    if (projects.value.find(item => item.id === row.project_id)?.status === 'disabled') {
      return { type: 'warning', text: $t('business.project-disabled') };
    }
    if (applications.value.find(item => item.id === row.application_id)?.status === 'disabled') {
      return { type: 'warning', text: $t('business.application-disabled') };
    }
  }
  return { type: statusTagType(row.status), text: statusText(row.status, 'api_key') };
}

function keyStatusTag(row: Api.Management.ApiKey) {
  const status = keyStatus(row);
  return h(NTag, { type: status.type, size: 'small', round: true }, { default: () => status.text });
}

const showDetail = ref(false);
const detailKey = ref<Api.Management.ApiKey | null>(null);

function openDetail(row: Api.Management.ApiKey) {
  detailKey.value = row;
  showDetail.value = true;
}

function adjustRateLimitFromDetail() {
  if (!detailKey.value) return;
  const key = detailKey.value;
  showDetail.value = false;
  openRateLimit(key);
}

function rowMoreOptions(row: Api.Management.ApiKey): DropdownOption[] {
  return [
    {
      label: row.status === 'active' ? $t('business.disable') : $t('business.enable'),
      key: 'toggle-status'
    },
    { type: 'divider', key: 'divider-danger' },
    {
      label: $t('business.delete'),
      key: 'delete',
      props: { style: { color: '#d03050' } }
    }
  ];
}

function handleRowMoreAction(key: string | number, row: Api.Management.ApiKey) {
  if (key === 'toggle-status') {
    if (row.status === 'active') {
      window.$dialog?.warning({
        title: $t('business.disable'),
        content: $t('business.authentication-stops-immediately-after-disabling'),
        positiveText: $t('common.confirm'),
        negativeText: $t('common.cancel'),
        onPositiveClick: () => handleToggleStatus(row, 'disabled')
      });
    } else {
      void handleToggleStatus(row, 'active');
    }
    return;
  }
  if (key === 'delete') {
    window.$dialog?.warning({
      title: $t('business.delete'),
      content: $t('business.after-deletion-this-key-becomes'),
      positiveText: $t('common.confirm'),
      negativeText: $t('common.cancel'),
      onPositiveClick: () => handleDelete(row.id)
    });
  }
}

// 列顺序把状态、健康等关键信息排在前面:列多于一屏宽度时,这些列在常见窗口下仍然可见,其余列横向滚动查看。
const columns = computed<DataTableColumns<Api.Management.ApiKey>>(() => [
  { title: $t('business.name'), key: 'name', ellipsis: { tooltip: true }, width: 160 },
  {
    title: $t('business.project'),
    key: 'project_id',
    ellipsis: { tooltip: true },
    width: 140,
    render: row => projectName(row.project_id)
  },
  {
    title: $t('business.application'),
    key: 'application_id',
    ellipsis: { tooltip: true },
    width: 140,
    render: row => applicationName(row.application_id)
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 110,
    render: row => keyStatusTag(row)
  },
  {
    title: $t('business.expires-at'),
    key: 'expires_at',
    width: 170,
    render: row => (row.expires_at ? formatTime(row.expires_at) : '-')
  },
  {
    title: $t('business.last-used'),
    key: 'last_used_at',
    width: 180,
    render: row => formatTime(row.last_used_at)
  },
  {
    title: $t('business.rate-limit-minute'),
    key: 'rate_limit_per_min',
    width: 135,
    render: row => (row.rate_limit_per_min != null ? String(row.rate_limit_per_min) : '-')
  },
  { title: $t('business.prefix'), key: 'key_prefix', ellipsis: { tooltip: true }, width: 120 },
  {
    title: $t('business.created-at'),
    key: 'created_at',
    width: 180,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 290,
    // 操作列固定在右侧:列多于一屏宽度时,行内操作不用横向滚动也能点到。
    fixed: 'right',
    render(row) {
      // 详情与接入配置对所有角色开放;限流、启停、删除只给可管理角色。
      const buttons: ReturnType<typeof h>[] = [
        h(
          NButton,
          { size: 'small', quaternary: true, type: 'primary', onClick: () => openQuickConnect('', row.name) },
          { default: () => $t('business.quick-connect') }
        ),
        h(
          NButton,
          { size: 'small', quaternary: true, type: 'primary', onClick: () => openDetail(row) },
          { default: () => $t('business.details') }
        )
      ];
      if (!canManage.value) return h(NSpace, { size: 4 }, { default: () => buttons });
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
        ),
        h(
          NDropdown,
          {
            trigger: 'click',
            options: rowMoreOptions(row),
            onSelect: key => handleRowMoreAction(key, row)
          },
          {
            default: () => h(NButton, { size: 'small', quaternary: true }, { default: () => $t('business.more') })
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
      has('org:read') ? fetchProjects() : Promise.resolve({ data: null, error: null }),
      has('org:read') ? fetchApplications() : Promise.resolve({ data: null, error: null })
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
  formModel.expires_at = null;
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
      rate_limit_per_min: rateLimitModel.rate_limit_per_min ?? null
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
      createdKeyName.value = formModel.name.trim();
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

onMounted(async () => {
  await loadData();
  const keyID = typeof route.query.key === 'string' ? route.query.key : '';
  if (keyID) {
    const key = list.value.find(item => item.id === keyID);
    if (key) openDetail(key);
  }
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
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1200" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-api-key')" class="w-520px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="top">
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
          <NButton secondary type="primary" @click="openQuickConnectFromCreatedKey">
            {{ $t('business.quick-connect') }}
          </NButton>
          <NButton @click="handleCopyKey">{{ $t('business.copy') }}</NButton>
          <NButton type="primary" @click="closeCreatedKeyModal">{{ $t('business.i-have-saved-it') }}</NButton>
        </NSpace>
      </template>
    </NModal>
    <ClientSetupModal
      :show="showQuickConnect"
      :api-key="quickConnectKey"
      :key-name="quickConnectKeyName"
      @update:show="handleQuickConnectShowChange"
    />
    <NModal v-model:show="showRateLimit" preset="card" :title="$t('business.adjust-rate-limit')" class="w-440px">
      <NForm ref="rateLimitFormRef" :model="rateLimitModel" label-placement="top">
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

    <NDrawer v-model:show="showDetail" :width="520" placement="right">
      <NDrawerContent :title="$t('business.api-key-detail')" closable>
        <NDescriptions v-if="detailKey" :column="1" bordered size="small" label-placement="left">
          <NDescriptionsItem :label="$t('business.name')">{{ detailKey.name }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.prefix')">{{ detailKey.key_prefix }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.project')">
            {{ projectName(detailKey.project_id) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.application')">
            {{ applicationName(detailKey.application_id) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.status')">
            <NTag :type="keyStatus(detailKey).type" size="small" round>{{ keyStatus(detailKey).text }}</NTag>
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.expires-at')">
            {{ detailKey.expires_at ? formatTime(detailKey.expires_at) : $t('business.never-expires') }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.rate-limit-minute')">
            {{ detailKey.rate_limit_per_min != null ? detailKey.rate_limit_per_min : $t('business.use-global-default') }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.last-used')">{{ formatTime(detailKey.last_used_at) }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.created-at')">{{ formatTime(detailKey.created_at) }}</NDescriptionsItem>
        </NDescriptions>
        <template #footer>
          <NSpace justify="end">
            <NButton @click="showDetail = false">{{ $t('business.close') }}</NButton>
            <NButton v-if="canManage && detailKey" type="primary" @click="adjustRateLimitFromDetail">
              {{ $t('business.adjust-rate-limit') }}
            </NButton>
          </NSpace>
        </template>
      </NDrawerContent>
    </NDrawer>
  </div>
</template>

<style scoped></style>
