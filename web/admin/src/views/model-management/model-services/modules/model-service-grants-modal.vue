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
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';

const props = defineProps<{
  show: boolean;
  service: Api.Management.ModelService | null;
}>();
const emit = defineEmits<{ 'update:show': [value: boolean] }>();
// 只读角色也能查看授权范围(审计直达会打开本弹窗),但不提供保存与移除入口;后端同样要求 project_admin。
const { has } = usePermission();
const canManage = computed(() => has('model_service:manage'));

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
const subjectTypeOptions = computed(() => [
  { label: $t('business.entire-organization'), value: 'organization' },
  { label: $t('business.selected-departments'), value: 'department' },
  { label: $t('business.selected-projects'), value: 'project' }
]);
const baseColumns = computed<DataTableColumns<Api.Management.ModelServiceGrant>>(() => [
  {
    title: $t('business.access-scope'),
    key: 'subject_name',
    minWidth: 170,
    render: row => row.subject_name || $t('business.unknownSubject')
  },
  {
    title: $t('business.allowed-actions'),
    key: 'actions',
    width: 190,
    render: row =>
      h(
        NSpace,
        { size: 4 },
        {
          default: () =>
            row.actions.map(action =>
              h(
                NTag,
                { size: 'small', type: 'info', bordered: false },
                {
                  default: () => (action === 'invoke' ? $t('business.invoke-model') : $t('business.view-model'))
                }
              )
            )
        }
      )
  },
]);

// 移除入口只给可管理角色;只读角色只能查看已授权范围。
const manageColumns = computed<DataTableColumns<Api.Management.ModelServiceGrant>>(() => [
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 75,
    render: row =>
      h(
        NPopconfirm,
        { onPositiveClick: () => revoke(row.id) },
        {
          trigger: () =>
            h(NButton, { size: 'small', quaternary: true, type: 'error' }, { default: () => $t('business.remove') }),
          default: () => $t('business.after-removal-this-scope-immediately-1kx5ywy')
        }
      )
  }
]);

const columns = computed<DataTableColumns<Api.Management.ModelServiceGrant>>(() =>
  canManage.value ? [...baseColumns.value, ...manageColumns.value] : baseColumns.value
);

let sessionGeneration = 0;
let grantRequestGeneration = 0;
watch(
  [() => props.show, () => props.service?.id],
  ([visible]) => {
    sessionGeneration++;
    grantRequestGeneration++;
    saving.value = false;
    if (visible) void initialize();
    else loading.value = false;
  }
);

async function initialize() {
  if (!props.service) return;
  const generation = sessionGeneration;
  const serviceId = props.service.id;
  form.subject_type = 'project';
  form.subject_id = null;
  form.actions = ['read', 'invoke'];
  loading.value = true;
  try {
    const [grantRes, orgRes, departmentRes, projectRes] = await Promise.all([
      fetchModelServiceGrants(serviceId),
      has('org:read') ? fetchOrganizations() : Promise.resolve({ data: null, error: null }),
      has('org:read') ? fetchDepartments() : Promise.resolve({ data: null, error: null }),
      has('org:read') ? fetchProjects() : Promise.resolve({ data: null, error: null })
    ]);
    if (generation !== sessionGeneration || serviceId !== props.service?.id) return;
    if (!grantRes.error && grantRes.data) grants.value = grantRes.data;
    if (!orgRes.error && orgRes.data) organizations.value = orgRes.data;
    if (!departmentRes.error && departmentRes.data) departments.value = departmentRes.data;
    if (!projectRes.error && projectRes.data) projects.value = projectRes.data;
  } finally {
    if (generation === sessionGeneration) loading.value = false;
  }
}

async function reloadGrants() {
  if (!props.service) return;
  const generation = sessionGeneration;
  const requestGeneration = ++grantRequestGeneration;
  const serviceId = props.service.id;
  const { data, error } = await fetchModelServiceGrants(serviceId);
  if (generation === sessionGeneration && requestGeneration === grantRequestGeneration && serviceId === props.service?.id && !error && data) grants.value = data;
}

async function save() {
  if (!props.service || !form.subject_id) {
    window.$message?.warning($t('business.select-an-access-scope'));
    return;
  }
  if (!form.actions.length) {
    window.$message?.warning($t('business.select-at-least-one-allowed'));
    return;
  }
  const generation = sessionGeneration;
  const serviceId = props.service.id;
  saving.value = true;
  try {
    const { error } = await fetchUpsertModelServiceGrant(serviceId, {
      subject_type: form.subject_type,
      subject_id: form.subject_id,
      actions: [...form.actions]
    });
    if (!error && generation === sessionGeneration && serviceId === props.service?.id) {
      window.$message?.success($t('business.model-access-permissions-saved'));
      form.subject_id = null;
      await reloadGrants();
    }
  } finally {
    if (generation === sessionGeneration) saving.value = false;
  }
}

async function revoke(grantID: string) {
  if (!props.service) return;
  const { error } = await fetchRevokeModelServiceGrant(props.service.id, grantID);
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
    :title="
      $t('business.modelAccessTitle', {
        name: props.service?.name ?? $t('business.model-service')
      })
    "
    class="w-800px"
    @update:show="value => emit('update:show', value)"
  >
    <NSpin :show="loading">
      <NAlert type="info" :bordered="false" class="mb-16px">
        {{ $t('business.modelGrantNotice') }}
      </NAlert>
      <NForm v-if="canManage" label-placement="left" label-width="auto">
        <NGrid cols="1 640:2" :x-gap="16">
          <NFormItemGi :label="$t('business.scope-type')">
            <NSelect
              v-model:value="form.subject_type"
              :options="subjectTypeOptions"
              @update:value="form.subject_id = null"
            />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.access-scope')">
            <NSelect
              v-model:value="form.subject_id"
              filterable
              :options="subjectOptions"
              :placeholder="$t('business.please-select')"
            />
          </NFormItemGi>
        </NGrid>
        <NFormItem :label="$t('business.allowed-actions')">
          <NCheckboxGroup v-model:value="form.actions">
            <NSpace>
              <NCheckbox value="read">{{ $t('business.view-model') }}</NCheckbox>
              <NCheckbox value="invoke">{{ $t('business.invoke-model') }}</NCheckbox>
            </NSpace>
          </NCheckboxGroup>
        </NFormItem>
        <NButton type="primary" secondary :loading="saving" @click="save">{{ $t('business.save-grant') }}</NButton>
      </NForm>
      <NDivider title-placement="left">{{ $t('business.granted-scopes') }}</NDivider>
      <NDataTable :columns="columns" :data="grants" :bordered="false" size="small" :max-height="300" />
    </NSpin>
    <template #footer>
      <NSpace justify="end">
        <NButton @click="emit('update:show', false)">{{ $t('business.close') }}</NButton>
      </NSpace>
    </template>
  </NModal>
</template>
