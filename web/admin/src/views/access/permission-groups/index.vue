<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { formatTime } from '@/utils/admin-status';
import {
  fetchCreatePermissionGroup,
  fetchDeletePermissionGroup,
  fetchOrganizations,
  fetchPermissionCatalog,
  fetchPermissionGroups,
  fetchUpdatePermissionGroup
} from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';
import PermissionMatrix from '../components/permission-matrix.vue';

defineOptions({
  name: 'AccessPermissionGroups'
});

const { has, isPlatformAdmin } = usePermission();
const canManage = computed(() => has('permission_group:manage'));

const roleText = computed<Record<string, string>>(() => ({
  pending: $t('business.pending-role'),
  viewer: $t('business.viewer'),
  developer: $t('business.developer'),
  project_admin: $t('business.project-admin'),
  org_admin: $t('business.organization-admin')
}));

const loading = ref(true);
const modules = ref<Api.Management.PermissionModule[]>([]);
const presets = ref<Api.Management.SystemPreset[]>([]);
const groups = ref<Api.Management.PermissionGroup[]>([]);
const orgOptions = ref<{ label: string; value: string }[]>([]);
const filterOrgId = ref<string | null>(null);

const showDrawer = ref(false);
const drawerMode = ref<'create' | 'edit' | 'view'>('create');
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const drawerModel = reactive({
  id: '',
  name: '',
  description: '',
  permissions: [] as string[],
  organization_id: null as string | null
});

const allPermissionCodes = computed(() => modules.value.flatMap(m => m.permissions));
const disabledPermissions = computed(() => allPermissionCodes.value.filter(code => !has(code)));

const formRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.permission-group-name'),
    trigger: 'blur'
  }
}));

const drawerTitle = computed(() => {
  if (drawerMode.value === 'view') return drawerModel.name || $t('business.view-permissions');
  if (drawerMode.value === 'edit') return $t('business.edit-permission-group');
  return $t('business.create-permission-group');
});

function withinMyPermissions(perms: string[]) {
  return perms.every(p => has(p));
}

const groupColumns = computed<DataTableColumns<Api.Management.PermissionGroup>>(() => [
  { title: $t('business.permission-group-name'), key: 'name', minWidth: 160, ellipsis: { tooltip: true } },
  {
    title: $t('business.permission-group-desc'),
    key: 'description',
    minWidth: 220,
    ellipsis: { tooltip: true },
    render: row => row.description || '-'
  },
  {
    title: $t('business.permission-count'),
    key: 'permissions',
    width: 100,
    render: row => h(NTag, { size: 'small', bordered: false }, { default: () => String(row.permissions.length) })
  },
  {
    title: $t('business.member-count'),
    key: 'member_count',
    width: 100,
    render: row => String(row.member_count)
  },
  {
    title: $t('business.updated-at'),
    key: 'updated_at',
    width: 180,
    render: row => formatTime(row.updated_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 220,
    fixed: 'right',
    render(row) {
      const actions = [
        h(
          NButton,
          {
            size: 'small',
            quaternary: true,
            type: 'primary',
            onClick: () => openViewGroup(row)
          },
          { default: () => $t('business.view-permissions') }
        )
      ];
      if (canManage.value && withinMyPermissions(row.permissions)) {
        actions.push(
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'primary',
              onClick: () => openEditGroup(row)
            },
            { default: () => $t('business.edit') }
          ),
          h(
            NPopconfirm,
            { onPositiveClick: () => handleDeleteGroup(row) },
            {
              trigger: () =>
                h(
                  NButton,
                  { size: 'small', quaternary: true, type: 'error', disabled: row.member_count > 0 },
                  { default: () => $t('common.delete') }
                ),
              default: () => $t('business.delete-permission-group-confirm')
            }
          )
        );
      }
      return h(NSpace, { size: 4 }, { default: () => actions });
    }
  }
]);

const presetColumns = computed<DataTableColumns<Api.Management.SystemPreset>>(() => [
  {
    title: $t('business.role'),
    key: 'role',
    width: 145,
    render: row =>
      h(
        NTag,
        { size: 'small', round: true, type: row.role === 'pending' ? 'warning' : 'info' },
        { default: () => roleText.value[row.role] ?? row.role }
      )
  },
  {
    title: $t('business.permission-group-desc'),
    key: 'description',
    minWidth: 200,
    render: row => $t(`business.preset-desc-${row.role}` as App.I18n.I18nKey)
  },
  {
    title: $t('business.permission-count'),
    key: 'permissions',
    width: 85,
    render: row => h(NTag, { size: 'small', bordered: false }, { default: () => String(row.permissions.length) })
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 210,
    fixed: 'right',
    render(row) {
      const actions = [
        h(
          NButton,
          {
            size: 'small',
            quaternary: true,
            type: 'primary',
            onClick: () => openViewPreset(row)
          },
          { default: () => $t('business.view-permissions') }
        )
      ];
      if (canManage.value && withinMyPermissions(row.permissions)) {
        actions.push(
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'primary',
              onClick: () => openDuplicatePreset(row)
            },
            { default: () => $t('business.copy-as-group') }
          )
        );
      }
      return h(NSpace, { size: 4 }, { default: () => actions });
    }
  }
]);

async function loadData() {
  loading.value = true;
  try {
    const [catalogRes, groupRes] = await Promise.all([
      fetchPermissionCatalog(),
      fetchPermissionGroups(filterOrgId.value ?? undefined)
    ]);
    if (!catalogRes.error && catalogRes.data) {
      modules.value = catalogRes.data.modules;
      presets.value = catalogRes.data.presets;
    }
    if (!groupRes.error && groupRes.data) {
      groups.value = groupRes.data;
    }
  } finally {
    loading.value = false;
  }
}

async function loadOrganizations() {
  if (!isPlatformAdmin.value) return;
  const { data, error } = await fetchOrganizations();
  if (!error && data) {
    orgOptions.value = data.map(item => ({
      label: `${item.name} (${item.slug})`,
      value: item.id
    }));
  }
}

function openCreateGroup() {
  drawerMode.value = 'create';
  drawerModel.id = '';
  drawerModel.name = '';
  drawerModel.description = '';
  drawerModel.permissions = [];
  drawerModel.organization_id = filterOrgId.value;
  showDrawer.value = true;
}

function openEditGroup(row: Api.Management.PermissionGroup) {
  drawerMode.value = 'edit';
  drawerModel.id = row.id;
  drawerModel.name = row.name;
  drawerModel.description = row.description;
  drawerModel.permissions = [...row.permissions];
  drawerModel.organization_id = row.organization_id;
  showDrawer.value = true;
}

function openViewGroup(row: Api.Management.PermissionGroup) {
  drawerMode.value = 'view';
  drawerModel.id = row.id;
  drawerModel.name = row.name;
  drawerModel.description = row.description;
  drawerModel.permissions = [...row.permissions];
  showDrawer.value = true;
}

function openViewPreset(preset: Api.Management.SystemPreset) {
  drawerMode.value = 'view';
  drawerModel.id = '';
  drawerModel.name = roleText.value[preset.role] ?? preset.role;
  drawerModel.description = $t(`business.preset-desc-${preset.role}` as App.I18n.I18nKey);
  drawerModel.permissions = [...preset.permissions];
  showDrawer.value = true;
}

function openDuplicatePreset(preset: Api.Management.SystemPreset) {
  drawerMode.value = 'create';
  drawerModel.id = '';
  drawerModel.name = `${roleText.value[preset.role] ?? preset.role} - Custom`;
  drawerModel.description = $t(`business.preset-desc-${preset.role}` as App.I18n.I18nKey);
  drawerModel.permissions = [...preset.permissions];
  drawerModel.organization_id = filterOrgId.value;
  showDrawer.value = true;
}

async function handleSaveGroup() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    if (drawerMode.value === 'create') {
      const { error } = await fetchCreatePermissionGroup({
        name: drawerModel.name.trim(),
        description: drawerModel.description.trim(),
        permissions: drawerModel.permissions,
        organization_id: drawerModel.organization_id ?? undefined
      });
      if (!error) {
        window.$message?.success($t('business.permission-group-created'));
        showDrawer.value = false;
        await loadData();
      }
    } else if (drawerMode.value === 'edit') {
      const { error } = await fetchUpdatePermissionGroup(drawerModel.id, {
        name: drawerModel.name.trim(),
        description: drawerModel.description.trim(),
        permissions: drawerModel.permissions
      });
      if (!error) {
        window.$message?.success($t('business.permission-group-updated'));
        showDrawer.value = false;
        await loadData();
      }
    }
  } finally {
    submitting.value = false;
  }
}

async function handleDeleteGroup(row: Api.Management.PermissionGroup) {
  const { error } = await fetchDeletePermissionGroup(row.id);
  if (!error) {
    window.$message?.success($t('business.permission-group-deleted'));
    await loadData();
  }
}

onMounted(async () => {
  await Promise.all([loadData(), loadOrganizations()]);
});
</script>

<template>
  <div class="flex flex-col gap-16px p-4px">
    <NCard :title="$t('business.custom-permission-groups')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NSelect
            v-if="isPlatformAdmin"
            v-model:value="filterOrgId"
            :options="orgOptions"
            clearable
            :placeholder="$t('business.filter-by-organization')"
            class="w-220px"
            @update:value="loadData"
          />
          <NButton quaternary type="primary" :loading="loading" @click="loadData">
            {{ $t('business.refresh') }}
          </NButton>
          <NButton v-if="canManage" type="primary" @click="openCreateGroup">
            {{ $t('business.create-permission-group') }}
          </NButton>
        </NSpace>
      </template>
      <NDataTable :columns="groupColumns" :data="groups" :loading="loading" :bordered="false" :scroll-x="860" />
    </NCard>

    <NCard :title="$t('business.system-presets')" :bordered="false" class="card-wrapper">
      <NDataTable :columns="presetColumns" :data="presets" :loading="loading" :bordered="false" :scroll-x="640" />
    </NCard>

    <NDrawer v-model:show="showDrawer" :width="720" placement="right">
      <NDrawerContent :title="drawerTitle" closable>
        <NForm
          v-if="drawerMode !== 'view'"
          ref="formRef"
          :model="drawerModel"
          :rules="formRules"
          label-placement="left"
          label-width="110"
          class="mb-16px"
        >
          <NFormItem :label="$t('business.permission-group-name')" path="name">
            <NInput v-model:value="drawerModel.name" />
          </NFormItem>
          <NFormItem :label="$t('business.permission-group-desc')" path="description">
            <NInput v-model:value="drawerModel.description" type="textarea" :autosize="{ minRows: 2, maxRows: 3 }" />
          </NFormItem>
          <NFormItem v-if="isPlatformAdmin && drawerMode === 'create'" :label="$t('business.owning-organization')">
            <NSelect
              v-model:value="drawerModel.organization_id"
              :options="orgOptions"
              clearable
              :placeholder="$t('business.current-organization-by-default')"
            />
          </NFormItem>
        </NForm>
        <p v-else-if="drawerModel.description" class="mb-16px text-13px text-#6b7280">
          {{ drawerModel.description }}
        </p>

        <PermissionMatrix
          v-model="drawerModel.permissions"
          :modules="modules"
          :disabled-permissions="disabledPermissions"
          :readonly="drawerMode === 'view'"
        />

        <template #footer>
          <NSpace justify="end">
            <NButton @click="showDrawer = false">
              {{ drawerMode === 'view' ? $t('business.close') : $t('business.cancel') }}
            </NButton>
            <NButton v-if="drawerMode !== 'view'" type="primary" :loading="submitting" @click="handleSaveGroup">
              {{ $t('business.save') }}
            </NButton>
          </NSpace>
        </template>
      </NDrawerContent>
    </NDrawer>
  </div>
</template>
