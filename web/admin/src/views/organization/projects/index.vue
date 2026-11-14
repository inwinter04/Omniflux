<script setup lang="ts">
import { h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import {
  fetchCreateProject,
  fetchDeleteProject,
  fetchDepartments,
  fetchProjects,
  fetchUpdateProject
} from '@/service/api';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';

defineOptions({
  name: 'OrganizationProjects'
});

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

const formRules: FormRules = {
  name: { required: true, message: '请填写项目名称', trigger: 'blur' },
  department_id: { required: true, message: '请选择部门', trigger: 'change' }
};
const editRules: FormRules = {
  name: { required: true, message: '请填写项目名称', trigger: 'blur' }
};

const departmentOptions = ref<{ label: string; value: string }[]>([]);

function departmentName(id: string) {
  return departments.value.find(item => item.id === id)?.name ?? '-';
}

const columns: DataTableColumns<Api.Management.Project> = [
  { title: '名称', key: 'name' },
  { title: '部门', key: 'department_id', render: row => departmentName(row.department_id) },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render(row) {
      return h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status) });
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    render(row) {
      return h(NSpace, {}, {
        default: () => [
          h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, { default: () => '编辑' }),
          h(
            NPopconfirm,
            { onPositiveClick: () => handleDelete(row.id) },
            {
              trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '删除' }),
              default: () => '项目下存在应用或 API Key 时将无法删除,确认删除?'
            }
          )
        ]
      });
    }
  }
];

async function loadData() {
  loading.value = true;
  try {
    const [projectsRes, departmentsRes] = await Promise.all([fetchProjects(), fetchDepartments()]);
    if (!projectsRes.error && projectsRes.data) {
      list.value = projectsRes.data;
    }
    if (!departmentsRes.error && departmentsRes.data) {
      departments.value = departmentsRes.data;
      departmentOptions.value = departmentsRes.data.map(item => ({ label: item.name, value: item.id }));
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
      window.$message?.success('项目已创建');
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
    const { error } = await fetchUpdateProject(editModel.id, { name: editModel.name.trim() });
    if (!error) {
      window.$message?.success('项目已更新');
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteProject(id);
  if (!error) {
    window.$message?.success('项目已删除');
    await loadData();
  }
}

onMounted(() => {
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard title="项目" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" :disabled="departmentOptions.length === 0" @click="openCreate">
            {{ departmentOptions.length === 0 ? '没有可用部门' : '新建项目' }}
          </NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建项目" class="w-480px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="80">
        <NFormItem label="项目名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="例如:订单中心" />
        </NFormItem>
        <NFormItem label="部门" path="department_id">
          <NSelect v-model:value="formModel.department_id" :options="departmentOptions" placeholder="选择所属部门" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">创建</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" title="编辑项目" class="w-480px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="80">
        <NFormItem label="项目名称" path="name">
          <NInput v-model:value="editModel.name" placeholder="项目名称" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showEdit = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleUpdate">保存</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>

