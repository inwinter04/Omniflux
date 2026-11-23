<script setup lang="ts">
import { computed, h, reactive, ref, watch } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import {
  fetchDepartments,
  fetchModelServiceGrants,
  fetchOrganizations,
  fetchProjects,
  fetchRevokeModelServiceGrant,
  fetchUpsertModelServiceGrant
} from '@/service/api';

const props = defineProps<{ show: boolean; service: Api.Management.ModelService | null }>();
const emit = defineEmits<{ 'update:show': [value: boolean] }>();

const loading = ref(false);
const saving = ref(false);
const grants = ref<Api.Management.ModelServiceGrant[]>([]);
const organizations = ref<Api.Management.Organization[]>([]);
const departments = ref<Api.Management.Department[]>([]);
const projects = ref<Api.Management.Project[]>([]);
const form = reactive({
  subject_type: 'project' as 'organization' | 'department' | 'project',
  subject_id: null as string | null,
  actions: ['read', 'invoke'] as ('read' | 'invoke')[]
});

const subjectOptions = computed(() => {
  if (form.subject_type === 'organization') return organizations.value.map(item => ({ label: item.name, value: item.id }));
  if (form.subject_type === 'department') return departments.value.map(item => ({ label: item.name, value: item.id }));
  return projects.value.map(item => ({ label: item.name, value: item.id }));
});
const subjectTypeOptions = [
  { label: '整个组织', value: 'organization' },
  { label: '指定部门', value: 'department' },
  { label: '指定项目', value: 'project' }
];
const columns: DataTableColumns<Api.Management.ModelServiceGrant> = [
  { title: '访问范围', key: 'subject_name', minWidth: 170 },
  {
    title: '允许的操作', key: 'actions', width: 190,
    render: row => h(NSpace, { size: 4 }, {
      default: () => row.actions.map(action => h(NTag, { size: 'small', type: 'info', bordered: false }, { default: () => action === 'invoke' ? '调用模型' : '查看模型' }))
    })
  },
  {
    title: '操作', key: 'actions', width: 75,
    render: row => h(NPopconfirm, { onPositiveClick: () => revoke(row.id) }, {
      trigger: () => h(NButton, { size: 'small', quaternary: true, type: 'error' }, { default: () => '移除' }),
      default: () => '移除后，该范围将立即失去对应访问权限。'
    })
  }
];

watch(() => props.show, visible => { if (visible) void initialize(); });

async function initialize() {
  if (!props.service) return;
  form.subject_type = 'project';
  form.subject_id = null;
  form.actions = ['read', 'invoke'];
  loading.value = true;
  try {
    const [grantRes, orgRes, departmentRes, projectRes] = await Promise.all([
      fetchModelServiceGrants(props.service.id), fetchOrganizations(), fetchDepartments(), fetchProjects()
    ]);
    if (!grantRes.error && grantRes.data) grants.value = grantRes.data;
    if (!orgRes.error && orgRes.data) organizations.value = orgRes.data;
    if (!departmentRes.error && departmentRes.data) departments.value = departmentRes.data;
    if (!projectRes.error && projectRes.data) projects.value = projectRes.data;
  } finally {
    loading.value = false;
  }
}

async function reloadGrants() {
  if (!props.service) return;
  const { data, error } = await fetchModelServiceGrants(props.service.id);
  if (!error && data) grants.value = data;
}

async function save() {
  if (!props.service || !form.subject_id) {
    window.$message?.warning('请选择访问范围');
    return;
  }
  if (!form.actions.length) {
    window.$message?.warning('请至少选择一项允许的操作');
    return;
  }
  saving.value = true;
  try {
    const { error } = await fetchUpsertModelServiceGrant(props.service.id, {
      subject_type: form.subject_type, subject_id: form.subject_id, actions: [...form.actions]
    });
    if (!error) {
      window.$message?.success('模型访问权限已保存');
      form.subject_id = null;
      await reloadGrants();
    }
  } finally {
    saving.value = false;
  }
}

async function revoke(grantID: string) {
  if (!props.service) return;
  const { error } = await fetchRevokeModelServiceGrant(props.service.id, grantID);
  if (!error) {
    window.$message?.success('访问权限已移除');
    await reloadGrants();
  }
}
</script>

<template>
  <NModal :show="props.show" preset="card" :title="`${props.service?.name ?? '模型服务'} · 访问授权`" class="w-800px" @update:show="value => emit('update:show', value)">
    <NSpin :show="loading">
      <NAlert type="info" :bordered="false" class="mb-16px">
        业务系统按照 API Key 所属项目检查权限。“查看模型”控制模型列表，“调用模型”控制聊天、向量化和智能体执行。停用组织级授权后，可只开放给指定部门或项目。
      </NAlert>
      <NForm label-placement="left" label-width="90">
        <NGrid cols="1 640:2" :x-gap="16">
          <NFormItemGi label="范围类型"><NSelect v-model:value="form.subject_type" :options="subjectTypeOptions" @update:value="form.subject_id = null" /></NFormItemGi>
          <NFormItemGi label="访问范围"><NSelect v-model:value="form.subject_id" filterable :options="subjectOptions" placeholder="请选择" /></NFormItemGi>
        </NGrid>
        <NFormItem label="允许的操作">
          <NCheckboxGroup v-model:value="form.actions"><NSpace><NCheckbox value="read">查看模型</NCheckbox><NCheckbox value="invoke">调用模型</NCheckbox></NSpace></NCheckboxGroup>
        </NFormItem>
        <NButton type="primary" secondary :loading="saving" @click="save">保存授权</NButton>
      </NForm>
      <NDivider title-placement="left">已授权范围</NDivider>
      <NDataTable :columns="columns" :data="grants" :bordered="false" size="small" :max-height="300" />
    </NSpin>
    <template #footer><NSpace justify="end"><NButton @click="emit('update:show', false)">关闭</NButton></NSpace></template>
  </NModal>
</template>
