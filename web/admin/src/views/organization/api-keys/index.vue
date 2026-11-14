<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchApiKeys, fetchApplications, fetchCreateApiKey, fetchDeleteApiKey, fetchProjects, fetchUpdateApiKeyStatus } from '@/service/api';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';

defineOptions({
  name: 'OrganizationApiKeys'
});

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
  application_id: null as string | null
});

const formRules: FormRules = {
  name: { required: true, message: '请填写 API Key 名称', trigger: 'blur' },
  project_id: { required: true, message: '请选择项目', trigger: 'change' },
  application_id: { required: true, message: '请选择应用', trigger: 'change' }
};

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

const columns: DataTableColumns<Api.Management.ApiKey> = [
  { title: '名称', key: 'name', width: 160 },
  { title: '前缀', key: 'key_prefix', width: 120 },
  { title: '项目', key: 'project_id', width: 140, render: row => projectName(row.project_id) },
  { title: '应用', key: 'application_id', width: 140, render: row => applicationName(row.application_id) },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render(row) {
      return h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status) });
    }
  },
  { title: '最近使用', key: 'last_used_at', width: 180, render: row => formatTime(row.last_used_at) },
  { title: '创建时间', key: 'created_at', width: 180, render: row => formatTime(row.created_at) },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    render(row) {
      const buttons: ReturnType<typeof h>[] = [];
      if (row.status === 'active') {
        buttons.push(
          h(
            NPopconfirm,
            { onPositiveClick: () => handleToggleStatus(row, 'disabled') },
            {
              trigger: () => h(NButton, { size: 'small', type: 'warning', quaternary: true }, { default: () => '禁用' }),
              default: () => '禁用后立即停止认证,之后可重新启用,确认禁用?'
            }
          )
        );
      } else {
        buttons.push(
          h(NButton, { size: 'small', type: 'primary', quaternary: true, onClick: () => handleToggleStatus(row, 'active') }, { default: () => '启用' })
        );
      }
      buttons.push(
        h(
          NPopconfirm,
          { onPositiveClick: () => handleDelete(row.id) },
          {
            trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '删除' }),
            default: () => '删除后该 Key 彻底失效且不再显示,不可恢复,确认删除?'
          }
        )
      );
      return h(NSpace, { size: 4 }, { default: () => buttons });
    }
  }
];

async function loadData() {
  loading.value = true;
  try {
    const [keysRes, projectsRes, applicationsRes] = await Promise.all([fetchApiKeys(), fetchProjects(), fetchApplications()]);
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
  showCreate.value = true;
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
      application_id: formModel.application_id!
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
  window.$message?.success('已复制到剪贴板');
}

async function handleToggleStatus(row: Api.Management.ApiKey, status: 'active' | 'disabled') {
  const { error } = await fetchUpdateApiKeyStatus(row.id, status);
  if (!error) {
    window.$message?.success(status === 'active' ? 'API Key 已启用' : 'API Key 已禁用');
    await loadData();
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteApiKey(id);
  if (!error) {
    window.$message?.success('API Key 已删除');
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
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton
            type="primary"
            :disabled="projectOptions.length === 0 || applications.length === 0"
            @click="openCreate"
          >
            {{ projectOptions.length === 0 || applications.length === 0 ? '需要先存在项目与应用' : '新建 API Key' }}
          </NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1100" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建 API Key" class="w-520px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem label="名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="例如:订单服务" />
        </NFormItem>
        <NFormItem label="项目" path="project_id">
          <NSelect v-model:value="formModel.project_id" :options="projectOptions" placeholder="选择项目" @update:value="handleProjectChange" />
        </NFormItem>
        <NFormItem label="应用" path="application_id">
          <NSelect
            v-model:value="formModel.application_id"
            :options="applicationOptions"
            :disabled="!formModel.project_id"
            placeholder="先选择项目"
          />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="creating" @click="handleCreate">创建</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal :show="createdKey !== ''" preset="card" title="API Key 已创建" class="w-560px" :mask-closable="false">
      <NAlert type="warning" :bordered="false" class="mb-12px">请立即保存,该明文仅出现这一次。</NAlert>
      <NInput :value="createdKey" readonly type="textarea" :autosize="{ minRows: 3 }" />
      <template #footer>
        <NSpace justify="end">
          <NButton @click="handleCopyKey">复制</NButton>
          <NButton type="primary" @click="createdKey = ''">我已保存</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>
