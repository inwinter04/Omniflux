<script setup lang="ts">
import { computed, h, reactive, ref, watch } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import {
  fetchAssetGrants,
  fetchDepartments,
  fetchOrganizations,
  fetchProjects,
  fetchRevokeAssetGrant,
  fetchUpsertAssetGrant
} from '@/service/api';

const props = defineProps<{
  show: boolean;
  asset: Api.Management.Asset | null;
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
  saved: [];
}>();

const loading = ref(false);
const saving = ref(false);
const grants = ref<Api.Management.AssetGrant[]>([]);
const organizations = ref<Api.Management.Organization[]>([]);
const departments = ref<Api.Management.Department[]>([]);
const projects = ref<Api.Management.Project[]>([]);
const isArchived = computed(() => props.asset?.status === 'archived');
const isAgent = computed(() => props.asset?.asset_type === 'agent');
const assetNoun = computed(() => isAgent.value ? '智能体' : '模板');

const form = reactive({
  subject_type: 'project' as 'organization' | 'department' | 'project',
  subject_id: null as string | null,
  actions: ['read', 'invoke'] as ('read' | 'invoke')[]
});

const subjectTypeOptions = [
  { label: '整个组织', value: 'organization' },
  { label: '指定部门', value: 'department' },
  { label: '指定项目', value: 'project' }
];

const subjectOptions = computed(() => {
  if (form.subject_type === 'organization') return organizations.value.map(item => ({ label: item.name, value: item.id }));
  if (form.subject_type === 'department') return departments.value.map(item => ({ label: item.name, value: item.id }));
  return projects.value.map(item => ({ label: item.name, value: item.id }));
});

const columns: DataTableColumns<Api.Management.AssetGrant> = [
  { title: '访问范围', key: 'subject_name', minWidth: 160 },
  {
    title: '允许的操作',
    key: 'actions',
    width: 200,
    render: row => h(NSpace, { size: 4 }, {
      default: () => row.actions.map(action => h(NTag, { size: 'small', bordered: false, type: 'info' }, { default: () => action === 'invoke' ? `使用${assetNoun.value}` : '查看内容' }))
    })
  },
  {
    title: '操作',
    key: 'actions',
    width: 80,
    render: row => h(NPopconfirm, { onPositiveClick: () => revoke(row.id) }, {
      trigger: () => h(NButton, { size: 'small', quaternary: true, type: 'error', disabled: isArchived.value }, { default: () => '移除' }),
      default: () => '移除后，该范围将立即失去访问权限。'
    })
  }
];

watch(() => props.show, visible => {
  if (visible) void initialize();
});

async function initialize() {
  if (!props.asset) return;
  form.subject_type = 'project';
  form.subject_id = null;
  form.actions = ['read', 'invoke'];
  loading.value = true;
  try {
    const [grantRes, orgRes, departmentRes, projectRes] = await Promise.all([
      fetchAssetGrants(props.asset.id), fetchOrganizations(), fetchDepartments(), fetchProjects()
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
  if (!props.asset) return;
  const { data, error } = await fetchAssetGrants(props.asset.id);
  if (!error && data) grants.value = data;
}

async function save() {
  if (!props.asset || !form.subject_id) {
    window.$message?.warning('请选择访问范围');
    return;
  }
  if (form.actions.length === 0) {
    window.$message?.warning('请至少选择一项允许的操作');
    return;
  }
  saving.value = true;
  try {
    const { error } = await fetchUpsertAssetGrant(props.asset.id, {
      subject_type: form.subject_type,
      subject_id: form.subject_id,
      actions: [...form.actions]
    });
    if (!error) {
      window.$message?.success('访问权限已保存');
      form.subject_id = null;
      await reloadGrants();
    }
  } finally {
    saving.value = false;
  }
}

async function revoke(grantID: string) {
  if (!props.asset) return;
  const { error } = await fetchRevokeAssetGrant(props.asset.id, grantID);
  if (!error) {
    window.$message?.success('访问权限已移除');
    await reloadGrants();
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    title="访问授权"
    class="w-800px"
    @update:show="value => emit('update:show', value)"
  >
    <NSpin :show="loading">
      <NAlert type="info" :bordered="false" class="mb-16px">
        选择哪些组织、部门或项目可以查看和使用这个{{ assetNoun }}。业务系统会按照 API Key 所属项目检查访问范围。
      </NAlert>
      <NForm label-placement="left" label-width="90">
        <NGrid cols="1 640:2" :x-gap="16">
          <NFormItemGi label="范围类型">
            <NSelect v-model:value="form.subject_type" :options="subjectTypeOptions" :disabled="isArchived" @update:value="form.subject_id = null" />
          </NFormItemGi>
          <NFormItemGi label="访问范围">
            <NSelect v-model:value="form.subject_id" filterable :options="subjectOptions" :disabled="isArchived" placeholder="请选择" />
          </NFormItemGi>
        </NGrid>
        <NFormItem label="允许的操作">
          <NCheckboxGroup v-model:value="form.actions" :disabled="isArchived">
            <NSpace><NCheckbox value="read">查看{{ assetNoun }}内容</NCheckbox><NCheckbox value="invoke">使用{{ assetNoun }}{{ isAgent ? '进行对话' : '生成内容' }}</NCheckbox></NSpace>
          </NCheckboxGroup>
        </NFormItem>
        <NButton type="primary" secondary :loading="saving" :disabled="isArchived" @click="save">保存授权</NButton>
      </NForm>

      <NDivider title-placement="left">已授权范围</NDivider>
      <NDataTable :columns="columns" :data="grants" :bordered="false" size="small" :max-height="280" />

    </NSpin>
    <template #footer>
      <NSpace justify="end"><NButton @click="emit('update:show', false)">关闭</NButton></NSpace>
    </template>
  </NModal>
</template>
