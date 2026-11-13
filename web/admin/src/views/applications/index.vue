<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchApplications, fetchCreateApplication, fetchDeleteApplication, fetchProjects } from '@/service/api';

defineOptions({
  name: 'Applications'
});

const loading = ref(true);
const list = ref<Api.Management.Application[]>([]);
const projects = ref<Api.Management.Project[]>([]);

const showCreate = ref(false);
const creating = ref(false);
const formRef = ref<FormInst | null>(null);
const formModel = reactive({
  name: '',
  project_id: null as string | null
});

const formRules: FormRules = {
  name: { required: true, message: '请填写应用名称', trigger: 'blur' },
  project_id: { required: true, message: '请选择项目', trigger: 'change' }
};

const projectOptions = computed(() => projects.value.map(item => ({ label: item.name, value: item.id })));

function projectName(id: string) {
  return projects.value.find(item => item.id === id)?.name ?? '-';
}

const columns: DataTableColumns<Api.Management.Application> = [
  { title: '名称', key: 'name' },
  { title: '项目', key: 'project_id', render: row => projectName(row.project_id) },
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
          default: () => '应用下存在 API Key 时将无法删除,确认删除?'
        }
      );
    }
  }
];

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

async function handleCreate() {
  await formRef.value?.validate();
  creating.value = true;
  try {
    const { error } = await fetchCreateApplication({
      name: formModel.name.trim(),
      project_id: formModel.project_id!
    });
    if (!error) {
      window.$message?.success('应用已创建');
      showCreate.value = false;
      await loadData();
    }
  } finally {
    creating.value = false;
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteApplication(id);
  if (!error) {
    window.$message?.success('应用已删除');
    await loadData();
  }
}

onMounted(() => {
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard title="应用" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" :disabled="projectOptions.length === 0" @click="openCreate">
            {{ projectOptions.length === 0 ? '请先创建项目' : '新建应用' }}
          </NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建应用" class="w-480px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="80">
        <NFormItem label="应用名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="例如:订单服务" />
        </NFormItem>
        <NFormItem label="所属项目" path="project_id">
          <NSelect v-model:value="formModel.project_id" :options="projectOptions" placeholder="选择项目" />
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
