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
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';

const props = defineProps<{
  show: boolean;
  asset: Api.Management.Asset | null;
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
  saved: [];
}>();
// 只读角色也能查看授权范围(审计直达会打开本弹窗),但不提供保存与移除入口;后端同样要求 project_admin。
const { canManage } = usePermission();

const loading = ref(false);
const saving = ref(false);
const grants = ref<Api.Management.AssetGrant[]>([]);
const organizations = ref<Api.Management.Organization[]>([]);
const departments = ref<Api.Management.Department[]>([]);
const projects = ref<Api.Management.Project[]>([]);
const isArchived = computed(() => props.asset?.status === 'archived');
const isAgent = computed(() => props.asset?.asset_type === 'agent');
const assetNoun = computed(() => (isAgent.value ? $t('business.agent') : $t('business.template-imbi')));

const form = reactive({
  subject_type: 'project' as 'organization' | 'department' | 'project',
  subject_id: null as string | null,
  actions: ['read', 'invoke'] as ('read' | 'invoke')[]
});

const subjectTypeOptions = computed(() => [
  { label: $t('business.entire-organization'), value: 'organization' },
  { label: $t('business.selected-departments'), value: 'department' },
  { label: $t('business.selected-projects'), value: 'project' }
]);

const subjectOptions = computed(() => {
  if (form.subject_type === 'organization')
    return organizations.value.map(item => ({
      label: item.name,
      value: item.id
    }));
  if (form.subject_type === 'department')
    return departments.value.map(item => ({
      label: item.name,
      value: item.id
    }));
  return projects.value.map(item => ({ label: item.name, value: item.id }));
});

const baseColumns = computed<DataTableColumns<Api.Management.AssetGrant>>(() => [
  {
    title: $t('business.access-scope'),
    key: 'subject_name',
    minWidth: 160,
    render: row => row.subject_name || $t('business.unknownSubject')
  },
  {
    title: $t('business.allowed-actions'),
    key: 'actions',
    width: 200,
    render: row =>
      h(
        NSpace,
        { size: 4 },
        {
          default: () =>
            row.actions.map(action =>
              h(
                NTag,
                { size: 'small', bordered: false, type: 'info' },
                {
                  default: () =>
                    action === 'invoke'
                      ? $t('business.useAsset', { asset: assetNoun.value })
                      : $t('business.view-content')
                }
              )
            )
        }
      )
  },
]);

// 移除入口只给可管理角色;只读角色只能查看已授权范围。
const manageColumns = computed<DataTableColumns<Api.Management.AssetGrant>>(() => [
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 80,
    render: row =>
      h(
        NPopconfirm,
        { onPositiveClick: () => revoke(row.id) },
        {
          trigger: () =>
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'error',
                disabled: isArchived.value
              },
              { default: () => $t('business.remove') }
            ),
          default: () => $t('business.after-removal-this-scope-immediately')
        }
      )
  }
]);

const columns = computed<DataTableColumns<Api.Management.AssetGrant>>(() =>
  canManage.value ? [...baseColumns.value, ...manageColumns.value] : baseColumns.value
);

watch(
  () => props.show,
  visible => {
    if (visible) void initialize();
  }
);

async function initialize() {
  if (!props.asset) return;
  form.subject_type = 'project';
  form.subject_id = null;
  form.actions = ['read', 'invoke'];
  loading.value = true;
  try {
    const [grantRes, orgRes, departmentRes, projectRes] = await Promise.all([
      fetchAssetGrants(props.asset.id),
      fetchOrganizations(),
      fetchDepartments(),
      fetchProjects()
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
    window.$message?.warning($t('business.select-an-access-scope'));
    return;
  }
  if (form.actions.length === 0) {
    window.$message?.warning($t('business.select-at-least-one-allowed'));
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
      window.$message?.success($t('business.access-permissions-saved'));
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
    window.$message?.success($t('business.access-permissions-removed'));
    await reloadGrants();
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="$t('business.asset-access-title', { name: props.asset?.name ?? $t('business.access-grants') })"
    class="w-800px"
    @update:show="value => emit('update:show', value)"
  >
    <NSpin :show="loading">
      <NAlert type="info" :bordered="false" class="mb-16px">
        {{ $t('business.grantIntroduction', { asset: assetNoun }) }}
      </NAlert>
      <NForm v-if="canManage" label-placement="left" label-width="auto">
        <NGrid cols="1 640:2" :x-gap="16">
          <NFormItemGi :label="$t('business.scope-type')">
            <NSelect
              v-model:value="form.subject_type"
              :options="subjectTypeOptions"
              :disabled="isArchived"
              @update:value="form.subject_id = null"
            />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.access-scope')">
            <NSelect
              v-model:value="form.subject_id"
              filterable
              :options="subjectOptions"
              :disabled="isArchived"
              :placeholder="$t('business.please-select')"
            />
          </NFormItemGi>
        </NGrid>
        <NFormItem :label="$t('business.allowed-actions')">
          <NCheckboxGroup v-model:value="form.actions" :disabled="isArchived">
            <NSpace>
              <NCheckbox value="read">{{ $t('business.grantViewAction', { asset: assetNoun }) }}</NCheckbox>
              <NCheckbox value="invoke">
                {{
                  isAgent
                    ? $t('business.chatWithAsset', { asset: assetNoun })
                    : $t('business.generateAssetContent', { asset: assetNoun })
                }}
              </NCheckbox>
            </NSpace>
          </NCheckboxGroup>
        </NFormItem>
        <NButton type="primary" secondary :loading="saving" :disabled="isArchived" @click="save">
          {{ $t('business.save-grant') }}
        </NButton>
      </NForm>

      <NDivider title-placement="left">{{ $t('business.granted-scopes') }}</NDivider>
      <NDataTable :columns="columns" :data="grants" :bordered="false" size="small" :max-height="280" />
    </NSpin>
    <template #footer>
      <NSpace justify="end">
        <NButton @click="emit('update:show', false)">{{ $t('business.close') }}</NButton>
      </NSpace>
    </template>
  </NModal>
</template>
