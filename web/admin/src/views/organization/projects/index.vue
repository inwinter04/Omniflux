<script setup lang="ts">
import { h, onMounted, reactive, ref, computed } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';
import {
  fetchCreateProject,
  fetchDeleteProject,
  fetchDepartments,
  fetchProjects,
  fetchUpdateProject,
  fetchUpdateProjectStatus
} from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';

defineOptions({
  name: 'OrganizationProjects'
});

const { canManage } = usePermission();

const loading = ref(true);
const list = ref<Api.Management.Project[]>([]);
const departments = ref<Api.Management.Department[]>([]);

const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({
  name: '',
  department_id: null as string | null
});
const editModel = reactive({
  id: '',
  name: ''
});

const formRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-a-project-name'),
    trigger: 'blur'
  },
  department_id: {
    required: true,
    message: $t('business.select-a-department'),
    trigger: 'change'
  }
}));
const editRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-a-project-name'),
    trigger: 'blur'
  }
}));

const departmentOptions = ref<{ label: string; value: string }[]>([]);

function departmentName(id: string) {
  return departments.value.find(item => item.id === id)?.name ?? '-';
}

const columns = computed<DataTableColumns<Api.Management.Project>>(() => [
  { title: $t('business.name'), key: 'name' },
  {
    title: $t('business.department'),
    key: 'department_id',
    render: row => departmentName(row.department_id)
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
                default: () => $t('business.deletion-is-blocked-when-the-wzm80d')
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
    const [projectsRes, departmentsRes] = await Promise.all([fetchProjects(), fetchDepartments()]);
    if (!projectsRes.error && projectsRes.data) {
      list.value = projectsRes.data;
    }
    if (!departmentsRes.error && departmentsRes.data) {
      departments.value = departmentsRes.data;
      departmentOptions.value = departmentsRes.data.map(item => ({
        label: item.name,
        value: item.id
      }));
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  formModel.name = '';
  formModel.department_id = departments.value[0]?.id ?? null;
  showCreate.value = true;
}

function openEdit(row: Api.Management.Project) {
  editModel.id = row.id;
  editModel.name = row.name;
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchCreateProject({
      name: formModel.name.trim(),
      department_id: formModel.department_id!
    });
    if (!error) {
      window.$message?.success($t('business.project-created'));
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
    const { error } = await fetchUpdateProject(editModel.id, {
      name: editModel.name.trim()
    });
    if (!error) {
      window.$message?.success($t('business.project-updated'));
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(row: Api.Management.Project) {
  const status = row.status === 'active' ? 'disabled' : 'active';
  const { error } = await fetchUpdateProjectStatus(row.id, status);
  if (!error) {
    window.$message?.success(status === 'active' ? $t('business.project-enabled') : $t('business.project-disabled'));
    await loadData();
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteProject(id);
  if (!error) {
    window.$message?.success($t('business.project-deleted'));
    await loadData();
  }
}

onMounted(() => {
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :title="$t('business.project')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton v-if="canManage" type="primary" :disabled="departmentOptions.length === 0" @click="openCreate">
            {{ departmentOptions.length === 0 ? $t('business.noAvailableDepartments') : $t('business.create-project') }}
          </NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-project')" class="w-480px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="80">
        <NFormItem :label="$t('business.project-name')" path="name">
          <NInput v-model:value="formModel.name" :placeholder="$t('business.for-example-order-center')" />
        </NFormItem>
        <NFormItem :label="$t('business.department')" path="department_id">
          <NSelect
            v-model:value="formModel.department_id"
            :options="departmentOptions"
            :placeholder="$t('business.select-owning-department')"
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

    <NModal v-model:show="showEdit" preset="card" :title="$t('business.edit-project')" class="w-480px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="80">
        <NFormItem :label="$t('business.project-name')" path="name">
          <NInput v-model:value="editModel.name" :placeholder="$t('business.project-name')" />
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
