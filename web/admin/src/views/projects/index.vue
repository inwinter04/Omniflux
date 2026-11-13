<script setup lang="ts">
import { h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchCreateProject, fetchDeleteProject, fetchDepartments, fetchProjects } from '@/service/api';

defineOptions({
  name: 'Projects'
});

const loading = ref(true);
const list = ref<Api.Management.Project[]>([]);
const departments = ref<Api.Management.Department[]>([]);

const showCreate = ref(false);
const creating = ref(false);
const formRef = ref<FormInst | null>(null);
const formModel = reactive({
  name: '',
  department_id: null as string | null
});

const formRules: FormRules = {
  name: { required: true, message: '请填写项目名称', trigger: 'blur' },
  department_id: { required: true, message: '请选择部门', trigger: 'change' }
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
      return h(NTag, { type: row.status === 'active' ? 'success' : 'default', size: 'small', round: true }, { default: () => row.status });
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 100,
    render(row) {
      return h(
        NPopconfirm,
        { onPositiveClick: () => handleDelete(row.id) },
        {
          trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '删除' }),
          default: () => '项目下存在应用或 API Key 时将无法删除,确认删除?'
        }
      );
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

async function handleCreate() {
  await formRef.value?.validate();
  creating.value = true;
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
    creating.value = false;
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
          <NButton type="primary" :loading="creating" @click="handleCreate">创建</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>
