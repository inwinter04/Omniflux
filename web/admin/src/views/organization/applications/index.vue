<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { useRoute } from 'vue-router';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { statusTagType, statusText } from '@/utils/admin-status';
import {
  fetchApplications,
  fetchCreateApplication,
  fetchDeleteApplication,
  fetchProjects,
  fetchUpdateApplication,
  fetchUpdateApplicationStatus
} from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';

defineOptions({
  name: 'OrganizationApplications'
});

const { has } = usePermission();
const canManage = computed(() => has('org:manage'));
const route = useRoute();

const loading = ref(true);
const list = ref<Api.Management.Application[]>([]);
const projects = ref<Api.Management.Project[]>([]);

const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({
  name: '',
  project_id: null as string | null
});
const editModel = reactive({
  id: '',
  name: ''
});

const formRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-an-application-name'),
    trigger: 'blur'
  },
  project_id: {
    required: true,
    message: $t('business.select-a-project'),
    trigger: 'change'
  }
}));
const editRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-an-application-name'),
    trigger: 'blur'
  }
}));

const projectOptions = computed(() => projects.value.map(item => ({ label: item.name, value: item.id })));

function projectName(id: string) {
  return projects.value.find(item => item.id === id)?.name ?? '-';
}

const columns = computed<DataTableColumns<Api.Management.Application>>(() => [
  { title: $t('business.name'), key: 'name', ellipsis: { tooltip: true } },
  {
    title: $t('business.project'),
    key: 'project_id',
    ellipsis: { tooltip: true },
    render: row => projectName(row.project_id)
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 100,
    render(row) {
      return h(
        NTag,
        { type: statusTagType(row.status), size: 'small', round: true },
        { default: () => statusText(row.status) }
      );
    }
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 230,
    render(row) {
      if (!canManage.value) return '';
      return h(
        NSpace,
        {},
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
            // 停用会立即生效:名下 API Key 全部停止调用,所以停用前需要确认,启用则直接执行。
            row.status === 'active'
              ? h(
                  NPopconfirm,
                  { onPositiveClick: () => toggleStatus(row) },
                  {
                    trigger: () =>
                      h(NButton, { size: 'small', quaternary: true, type: 'warning' }, { default: () => $t('business.disabled') }),
                    default: () => $t('business.application-disable-confirm')
                  }
                )
              : h(
                  NButton,
                  {
                    size: 'small',
                    quaternary: true,
                    type: 'success',
                    onClick: () => toggleStatus(row)
                  },
                  { default: () => $t('business.enable') }
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
                default: () => $t('business.deletion-is-blocked-when-the-qh8q0y')
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
    const [applicationsRes, projectsRes] = await Promise.all([fetchApplications(), fetchProjects()]);
    if (!applicationsRes.error && applicationsRes.data) {
      list.value = applicationsRes.data;
    }
    if (!projectsRes.error && projectsRes.data) {
      projects.value = projectsRes.data;
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  formModel.name = '';
  formModel.project_id = null;
  showCreate.value = true;
}

function openEdit(row: Api.Management.Application) {
  editModel.id = row.id;
  editModel.name = row.name;
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchCreateApplication({
      name: formModel.name.trim(),
      project_id: formModel.project_id!
    });
    if (!error) {
      window.$message?.success($t('business.application-created'));
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
    const { error } = await fetchUpdateApplication(editModel.id, {
      name: editModel.name.trim()
    });
    if (!error) {
      window.$message?.success($t('business.application-updated'));
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(row: Api.Management.Application) {
  const status = row.status === 'active' ? 'disabled' : 'active';
  const { error } = await fetchUpdateApplicationStatus(row.id, status);
  if (!error) {
    window.$message?.success(
      status === 'active' ? $t('business.application-enabled') : $t('business.application-disabled')
    );
    await loadData();
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteApplication(id);
  if (!error) {
    window.$message?.success($t('business.application-deleted'));
    await loadData();
  }
}

onMounted(async () => {
  await loadData();
  const applicationID = typeof route.query.application === 'string' ? route.query.application : '';
  if (applicationID) {
    const application = list.value.find(item => item.id === applicationID);
    if (application) openEdit(application);
  }
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :title="$t('business.application')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton v-if="canManage" type="primary" :disabled="projectOptions.length === 0" @click="openCreate">
            {{ projectOptions.length === 0 ? $t('business.createProjectFirst') : $t('business.create-application') }}
          </NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-application')" class="w-480px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="80">
        <NFormItem :label="$t('business.application-name')" path="name">
          <NInput v-model:value="formModel.name" :placeholder="$t('business.for-example-order-service')" />
        </NFormItem>
        <NFormItem :label="$t('business.project-csdt0z')" path="project_id">
          <NSelect
            v-model:value="formModel.project_id"
            :options="projectOptions"
            :placeholder="$t('business.select-project')"
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

    <NModal v-model:show="showEdit" preset="card" :title="$t('business.edit-application')" class="w-480px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="80">
        <NFormItem :label="$t('business.application-name')" path="name">
          <NInput v-model:value="editModel.name" :placeholder="$t('business.application-name')" />
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
