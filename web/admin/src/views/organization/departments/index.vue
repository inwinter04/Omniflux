<script setup lang="ts">
import { h, onMounted, reactive, ref, computed } from 'vue';
import { useRoute } from 'vue-router';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchCreateDepartment, fetchDeleteDepartment, fetchDepartments, fetchUpdateDepartment } from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';

defineOptions({
  name: 'OrganizationDepartments'
});

const { has } = usePermission();
const canManage = computed(() => has('org:manage'));
const route = useRoute();

const loading = ref(true);
const list = ref<Api.Management.Department[]>([]);

const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({ name: '' });
const editModel = reactive({ id: '', name: '' });

const formRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-a-department-name'),
    trigger: 'blur'
  }
}));

const columns = computed<DataTableColumns<Api.Management.Department>>(() => [
  { title: $t('business.name'), key: 'name', ellipsis: { tooltip: true } },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 150,
    render(row) {
      if (!canManage.value) return '';
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
              NPopconfirm,
              { onPositiveClick: () => handleDelete(row.id) },
              {
                trigger: () =>
                  h(
                    NButton,
                    { size: 'small', type: 'error', quaternary: true },
                    { default: () => $t('business.delete') }
                  ),
                default: () => $t('business.deletion-is-blocked-when-the-33v85u')
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
    const { data, error } = await fetchDepartments();
    if (!error && data) {
      list.value = data;
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  formModel.name = '';
  showCreate.value = true;
}

function openEdit(row: Api.Management.Department) {
  editModel.id = row.id;
  editModel.name = row.name;
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchCreateDepartment({
      name: formModel.name.trim()
    });
    if (!error) {
      window.$message?.success($t('business.department-created'));
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
    const { error } = await fetchUpdateDepartment(editModel.id, {
      name: editModel.name.trim()
    });
    if (!error) {
      window.$message?.success($t('business.department-updated'));
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteDepartment(id);
  if (!error) {
    window.$message?.success($t('business.department-deleted'));
    await loadData();
  }
}

onMounted(async () => {
  await loadData();
  const departmentID = typeof route.query.department === 'string' ? route.query.department : '';
  if (departmentID) {
    const department = list.value.find(item => item.id === departmentID);
    if (department) openEdit(department);
  }
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :title="$t('business.department')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton v-if="canManage" type="primary" @click="openCreate">{{ $t('business.create-department') }}</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-department')" class="w-440px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="80">
        <NFormItem :label="$t('business.department-name')" path="name">
          <NInput v-model:value="formModel.name" :placeholder="$t('business.for-example-platform-department')" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">{{ $t('business.create') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" :title="$t('business.edit-department')" class="w-440px">
      <NForm ref="editFormRef" :model="editModel" :rules="formRules" label-placement="left" label-width="80">
        <NFormItem :label="$t('business.department-name')" path="name">
          <NInput v-model:value="editModel.name" :placeholder="$t('business.department-name')" />
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
