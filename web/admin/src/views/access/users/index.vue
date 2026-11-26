<script setup lang="ts">
import { computed, h, onMounted, reactive, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { NButton, NDropdown, NModal, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, DropdownOption, FormInst, FormRules, SelectGroupOption } from 'naive-ui';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';
import { generateInitialPassword, validatePasswordPolicy } from '@/utils/password';
import {
  fetchCreatePermissionGroup,
  fetchCreateUser,
  fetchInviteOptions,
  fetchInviteUser,
  fetchOrganizations,
  fetchPermissionCatalog,
  fetchPermissionGroups,
  fetchResendUserInvitation,
  fetchRevokeUserInvitation,
  fetchUpdateUser,
  fetchUpdateUserPermissions,
  fetchUserPermissions,
  fetchUsers
} from '@/service/api';
import { $t } from '@/locales';
import { useAuthStore } from '@/store/modules/auth';
import { usePermission } from '@/hooks/common/permission';
import PermissionMatrix from '../components/permission-matrix.vue';

defineOptions({
  name: 'AccessUsers'
});

const roleText = computed<Record<string, string>>(() => ({
  platform_admin: $t('business.platform-admin'),
  org_owner: $t('business.organization-owner'),
  org_admin: $t('business.organization-admin'),
  project_admin: $t('business.project-admin'),
  developer: $t('business.developer'),
  viewer: $t('business.viewer'),
  pending: $t('business.pending-role')
}));

const authStore = useAuthStore();
const route = useRoute();
const { has, isPlatformAdmin } = usePermission();
const canManage = computed(() => has('user:manage'));
const canManageGroups = computed(() => has('permission_group:manage'));
const currentUserId = computed(() => authStore.userInfo.userId);

const orgOptions = ref<{ label: string; value: string }[]>([]);
const filterOrgId = ref<string | null>(null);
const emailEnabled = ref(false);

const catalogModules = ref<Api.Management.PermissionModule[]>([]);
const systemPresets = ref<Api.Management.SystemPreset[]>([]);
const permissionGroups = ref<Api.Management.PermissionGroup[]>([]);
const createOrgGroups = ref<Api.Management.PermissionGroup[]>([]);

const allPermissionCodes = computed(() => catalogModules.value.flatMap(m => m.permissions));
const disabledPermissions = computed(() => allPermissionCodes.value.filter(code => !has(code)));

function withinMyPermissions(perms: string[]) {
  return perms.every(p => has(p));
}

function buildIdentityOptions(groups: Api.Management.PermissionGroup[]): SelectGroupOption[] {
  const presetChildren = systemPresets.value.map(preset => ({
    label: roleText.value[preset.role] ?? preset.role,
    value: `preset:${preset.role}`,
    disabled: !withinMyPermissions(preset.permissions)
  }));
  const options: SelectGroupOption[] = [
    {
      type: 'group',
      label: $t('business.system-presets'),
      key: 'system-presets',
      children: presetChildren
    }
  ];
  if (groups.length > 0) {
    options.push({
      type: 'group',
      label: $t('business.custom-permission-groups'),
      key: 'custom-groups',
      children: groups.map(group => ({
        label: group.name,
        value: `group:${group.id}`,
        disabled: !withinMyPermissions(group.permissions)
      }))
    });
  }
  return options;
}

const createIdentityOptions = computed(() =>
  buildIdentityOptions(formModel.organization_id ? createOrgGroups.value : permissionGroups.value)
);
const drawerIdentityOptions = computed(() => buildIdentityOptions(permTargetGroups.value));

const loading = ref(true);
const list = ref<Api.Management.User[]>([]);

const showCreate = ref(false);
const showDetail = ref(false);
const detailUser = ref<Api.Management.User | null>(null);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({
  email: '',
  password: '',
  create_mode: 'password' as 'password' | 'invite',
  identity: 'preset:developer',
  organization_id: null as string | null
});
const showMove = ref(false);
const submittingMove = ref(false);
const moveFormRef = ref<FormInst | null>(null);
const moveModel = reactive({
  id: '',
  email: '',
  organization_id: null as string | null
});
const editModel = reactive({
  id: '',
  email: '',
  password: ''
});

const createdEmail = ref('');
const createdPassword = ref('');
const loginUrl = computed(() => `${window.location.origin}/admin/login`);

// 成员权限配置抽屉状态
const showPermDrawer = ref(false);
const permLoading = ref(false);
const permSaving = ref(false);
const permUser = ref<Api.Management.User | null>(null);
const permIdentity = ref('preset:viewer');
const permBase = ref<string[]>([]);
const permChecked = ref<string[]>([]);
const permTargetGroups = ref<Api.Management.PermissionGroup[]>([]);

// 另存为权限组弹窗
const showSaveGroupModal = ref(false);
const savingGroup = ref(false);
const saveGroupFormRef = ref<FormInst | null>(null);
const saveGroupModel = reactive({
  name: '',
  description: ''
});

const saveGroupRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.permission-group-name'),
    trigger: 'blur'
  }
}));

const formRules = computed<FormRules>(() => ({
  email: [
    {
      required: true,
      message: $t('business.enter-an-email-address'),
      trigger: 'blur'
    },
    {
      validator: (_rule, value: string) => !value || /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim()),
      message: $t('business.enter-a-valid-email-address'),
      trigger: 'blur'
    }
  ],
  password: {
    required: formModel.create_mode === 'password',
    validator: (_rule, value: string) => formModel.create_mode !== 'password' || (Boolean(value) && validatePasswordPolicy(value)),
    message: $t('business.password-must-be-at-least'),
    trigger: 'blur'
  },
  identity: {
    required: true,
    message: $t('business.select-identity-preset'),
    trigger: 'change'
  }
}));

const editRules = computed<FormRules>(() => ({
  password: [
    {
      required: true,
      message: $t('business.enter-a-new-password'),
      trigger: 'blur'
    },
    {
      validator: (_rule, value: string) => !value || validatePasswordPolicy(value),
      message: $t('business.password-must-be-at-least'),
      trigger: 'blur'
    }
  ]
}));

function rowMoreOptions(row: Api.Management.User): DropdownOption[] {
  const protectedAccount = row.role === 'org_owner' || row.role === 'platform_admin';
  const canModifyRow = canManage.value && !(protectedAccount && !isPlatformAdmin.value);
  const options: DropdownOption[] = [];
  if (canModifyRow) {
    options.push({ label: $t('business.reset-password'), key: 'reset-password' });
    if (row.status === 'pending_email_verification') {
      options.push(
        { label: $t('business.resend-invitation'), key: 'resend-invitation' },
        { label: $t('business.revoke-invitation'), key: 'revoke-invitation' }
      );
    }
  }
  if (isPlatformAdmin.value && !protectedAccount) {
    options.push({ label: $t('business.move-organization'), key: 'move-organization' });
  }
  if (canModifyRow && row.id !== currentUserId.value) {
    if (options.length > 0) {
      options.push({ type: 'divider', key: 'divider-lifecycle' });
    }
    options.push({
      label: row.status === 'active' ? $t('business.disable') : $t('business.enable'),
      key: 'toggle-status',
      props: row.status === 'active' ? { style: { color: '#d03050' } } : undefined
    });
  }
  return options;
}

function handleRowMoreAction(key: string | number, row: Api.Management.User) {
  if (key === 'reset-password') {
    openEdit(row);
    return;
  }
  if (key === 'resend-invitation') {
    void handleResendInvitation(row);
    return;
  }
  if (key === 'revoke-invitation') {
    window.$dialog?.warning({
      title: $t('business.revoke-invitation'),
      content: $t('business.revoke-invitation-confirm'),
      positiveText: $t('common.confirm'),
      negativeText: $t('common.cancel'),
      onPositiveClick: () => handleRevokeInvitation(row)
    });
    return;
  }
  if (key === 'move-organization') {
    openMove(row);
    return;
  }
  if (key === 'toggle-status') {
    if (row.status === 'active') {
      window.$dialog?.warning({
        title: $t('business.disable'),
        content: $t('business.all-sessions-of-this-user'),
        positiveText: $t('common.confirm'),
        negativeText: $t('common.cancel'),
        onPositiveClick: () => handleToggleStatus(row, 'disabled')
      });
    } else {
      void handleToggleStatus(row, 'active');
    }
  }
}

const columns = computed<DataTableColumns<Api.Management.User>>(() => [
  { title: $t('business.email'), key: 'email', ellipsis: { tooltip: true }, width: 200 },
  {
    title: $t('business.identity-preset-or-group'),
    key: 'role',
    minWidth: 190,
    render(row) {
      const tags = [
        row.permission_group_name
          ? h(NTag, { type: 'info', size: 'small', round: true }, { default: () => row.permission_group_name })
          : h(
              NTag,
              {
                type: row.role === 'pending' ? 'warning' : 'default',
                size: 'small',
                round: true
              },
              { default: () => roleText.value[row.role] ?? row.role }
            )
      ];
      if ((row.override_count ?? 0) > 0) {
        tags.push(
          h(
            NTag,
            { type: 'primary', size: 'tiny', bordered: false },
            { default: () => $t('business.customized-badge') }
          )
        );
      }
      return h(NSpace, { size: 4, align: 'center' }, { default: () => tags });
    }
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 115,
    render(row) {
      return h(
        NTag,
        { type: statusTagType(row.status), size: 'small', round: true },
        { default: () => statusText(row.status) }
      );
    }
  },
  {
    title: $t('business.last-login'),
    key: 'last_login_at',
    width: 165,
    render: row => formatTime(row.last_login_at)
  },
  {
    title: $t('business.created-at'),
    key: 'created_at',
    width: 165,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 220,
    fixed: 'right',
    render(row) {
      const protectedAccount = row.role === 'org_owner' || row.role === 'platform_admin';
      const actions = [
        h(
          NButton,
          {
            size: 'small',
            quaternary: true,
            type: 'primary',
            onClick: () => openDetail(row)
          },
          { default: () => $t('business.details') }
        )
      ];
      if (canManage.value && !protectedAccount && row.id !== currentUserId.value) {
        actions.push(
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'primary',
              onClick: () => openPermDrawer(row)
            },
            { default: () => $t('business.configure-permissions') }
          )
        );
      }
      const moreOptions = rowMoreOptions(row);
      if (moreOptions.length > 0) {
        actions.push(
          h(
            NDropdown,
            {
              trigger: 'click',
              options: moreOptions,
              onSelect: key => handleRowMoreAction(key, row)
            },
            {
              default: () => h(NButton, { size: 'small', quaternary: true }, { default: () => $t('business.more') })
            }
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
    const [usersRes, groupsRes] = await Promise.all([
      fetchUsers(filterOrgId.value ?? undefined),
      fetchPermissionGroups(filterOrgId.value ?? undefined)
    ]);
    if (!usersRes.error && usersRes.data) {
      list.value = usersRes.data;
    }
    if (!groupsRes.error && groupsRes.data) {
      permissionGroups.value = groupsRes.data;
    }
  } finally {
    loading.value = false;
  }
}

async function loadCatalog() {
  const { data, error } = await fetchPermissionCatalog();
  if (!error && data) {
    catalogModules.value = data.modules;
    systemPresets.value = data.presets;
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

async function loadEmailSettings() {
  const { data, error } = await fetchInviteOptions();
  emailEnabled.value = !error && data.email_enabled;
}

function handleFilterOrg() {
  void loadData();
}

watch(
  () => formModel.organization_id,
  async orgId => {
    if (!showCreate.value) return;
    if (!orgId) {
      createOrgGroups.value = [];
      return;
    }
    const { data, error } = await fetchPermissionGroups(orgId);
    createOrgGroups.value = !error && data ? data : [];
    if (formModel.identity.startsWith('group:')) {
      formModel.identity = defaultAllowedPresetIdentity();
    }
  }
);

function defaultAllowedPresetIdentity(): string {
  for (const role of ['developer', 'viewer', 'pending']) {
    const preset = systemPresets.value.find(p => p.role === role);
    if (!preset || withinMyPermissions(preset.permissions)) {
      return `preset:${role}`;
    }
  }
  return 'preset:pending';
}

function parseIdentity(value: string): { role?: string; permission_group_id?: string } {
  if (value.startsWith('group:')) {
    return { permission_group_id: value.slice('group:'.length) };
  }
  return { role: value.replace(/^preset:/, '') };
}

function openCreate() {
  formModel.email = '';
  formModel.create_mode = 'password';
  formModel.password = '';
  formModel.identity = defaultAllowedPresetIdentity();
  formModel.organization_id = filterOrgId.value;
  showCreate.value = true;
}

function fillRandomCreatePassword() {
  formModel.password = generateInitialPassword(16);
}

function fillRandomEditPassword() {
  editModel.password = generateInitialPassword(16);
}

function openMove(row: Api.Management.User) {
  moveModel.id = row.id;
  moveModel.email = row.email;
  moveModel.organization_id = null;
  showMove.value = true;
}

async function handleMove() {
  await moveFormRef.value?.validate();
  submittingMove.value = true;
  try {
    const { error } = await fetchUpdateUser(moveModel.id, {
      organization_id: moveModel.organization_id!
    });
    if (!error) {
      window.$message?.success($t('business.user-organization-updated'));
      showMove.value = false;
      await loadData();
    }
  } finally {
    submittingMove.value = false;
  }
}

function openDetail(row: Api.Management.User) {
  detailUser.value = row;
  showDetail.value = true;
}

function openEdit(row: Api.Management.User) {
  editModel.id = row.id;
  editModel.email = row.email;
  editModel.password = '';
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  const email = formModel.email.trim();
  const identity = parseIdentity(formModel.identity);
  submitting.value = true;
  try {
    if (formModel.create_mode === 'invite') {
      const { error } = await fetchInviteUser({
        email,
        role: identity.role,
        permission_group_id: identity.permission_group_id,
        organization_id: formModel.organization_id ?? undefined
      });
      if (!error) {
        window.$message?.success($t('business.invitation-email-sent'));
        showCreate.value = false;
        await loadData();
      }
    } else {
      const { error } = await fetchCreateUser({
        email,
        password: formModel.password,
        generate_password: false,
        role: identity.role,
        permission_group_id: identity.permission_group_id,
        organization_id: formModel.organization_id ?? undefined
      });
      if (!error) {
        createdEmail.value = email;
        createdPassword.value = formModel.password;
        showCreate.value = false;
        await loadData();
      }
    }
  } finally {
    submitting.value = false;
  }
}

async function copyText(text: string) {
  await navigator.clipboard.writeText(text);
  window.$message?.success($t('business.copied-to-clipboard'));
}

async function copyAllCredentials() {
  const payload = $t('business.credentials-copy-template', {
    url: loginUrl.value,
    email: createdEmail.value,
    password: createdPassword.value
  });
  await copyText(payload);
}

function handleCloseCreatedPassword() {
  createdEmail.value = '';
  createdPassword.value = '';
}

async function handleUpdate() {
  await editFormRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchUpdateUser(editModel.id, {
      password: editModel.password
    });
    if (!error) {
      window.$message?.success($t('business.password-reset'));
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function handleResendInvitation(row: Api.Management.User) {
  const { error } = await fetchResendUserInvitation(row.id);
  if (!error) {
    window.$message?.success($t('business.invitation-email-sent'));
  }
}

async function handleRevokeInvitation(row: Api.Management.User) {
  const { error } = await fetchRevokeUserInvitation(row.id);
  if (!error) {
    window.$message?.success($t('business.invitation-revoked'));
    await loadData();
  }
}

async function handleToggleStatus(row: Api.Management.User, status: 'active' | 'disabled') {
  const { error } = await fetchUpdateUser(row.id, { status });
  if (!error) {
    window.$message?.success(status === 'active' ? $t('business.user-enabled') : $t('business.user-disabled'));
    await loadData();
  }
}

function resolveIdentityPermissions(identity: string): string[] {
  if (identity.startsWith('group:')) {
    const gid = identity.slice('group:'.length);
    return [...(permTargetGroups.value.find(g => g.id === gid)?.permissions ?? [])];
  }
  const role = identity.replace(/^preset:/, '');
  return [...(systemPresets.value.find(p => p.role === role)?.permissions ?? [])];
}

async function openPermDrawer(row: Api.Management.User) {
  permUser.value = row;
  permTargetGroups.value = permissionGroups.value;
  showPermDrawer.value = true;
  permLoading.value = true;
  try {
    const { data, error } = await fetchUserPermissions(row.id);
    if (!error && data) {
      permIdentity.value = data.permission_group_id ? `group:${data.permission_group_id}` : `preset:${data.role}`;
      permBase.value = [...data.base_permissions];
      permChecked.value = [...data.effective_permissions];
    }
  } finally {
    permLoading.value = false;
  }
}

function handlePermIdentityChange(value: string) {
  permIdentity.value = value;
  const base = resolveIdentityPermissions(value);
  permBase.value = base;
  permChecked.value = base.filter(code => has(code));
}

function resetToPresetBase() {
  permChecked.value = permBase.value.filter(code => has(code));
}

function openSaveAsGroupModal() {
  saveGroupModel.name = '';
  saveGroupModel.description = '';
  showSaveGroupModal.value = true;
}

async function handleSaveAsGroup() {
  await saveGroupFormRef.value?.validate();
  savingGroup.value = true;
  try {
    const { data, error } = await fetchCreatePermissionGroup({
      name: saveGroupModel.name.trim(),
      description: saveGroupModel.description.trim(),
      permissions: permChecked.value,
      organization_id: filterOrgId.value ?? undefined
    });
    if (!error && data) {
      window.$message?.success($t('business.permission-group-created'));
      showSaveGroupModal.value = false;
      const groupsRes = await fetchPermissionGroups(filterOrgId.value ?? undefined);
      if (!groupsRes.error && groupsRes.data) {
        permissionGroups.value = groupsRes.data;
        permTargetGroups.value = groupsRes.data;
      }
      permIdentity.value = `group:${data.id}`;
      permBase.value = [...permChecked.value];
    }
  } finally {
    savingGroup.value = false;
  }
}

async function handleSaveUserPermissions() {
  if (!permUser.value) return;
  const baseSet = new Set(permBase.value);
  const checkedSet = new Set(permChecked.value);
  const allow = permChecked.value.filter(code => !baseSet.has(code));
  const deny = permBase.value.filter(code => !checkedSet.has(code));
  const parsed = parseIdentity(permIdentity.value);
  permSaving.value = true;
  try {
    const { error } = await fetchUpdateUserPermissions(permUser.value.id, {
      role: parsed.role,
      permission_group_id: parsed.permission_group_id ?? null,
      allow,
      deny
    });
    if (!error) {
      window.$message?.success($t('business.user-permissions-updated'));
      showPermDrawer.value = false;
      await loadData();
    }
  } finally {
    permSaving.value = false;
  }
}

onMounted(async () => {
  await Promise.all([loadCatalog(), loadData(), loadOrganizations(), loadEmailSettings()]);
  const userID = typeof route.query.user === 'string' ? route.query.user : '';
  if (userID) {
    const user = list.value.find(item => item.id === userID);
    if (user) openDetail(user);
  }
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :title="$t('business.user')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NSelect
            v-if="isPlatformAdmin"
            v-model:value="filterOrgId"
            :options="orgOptions"
            clearable
            :placeholder="$t('business.filter-by-organization')"
            class="w-220px"
            @update:value="handleFilterOrg"
          />
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton v-if="canManage" type="primary" @click="openCreate">{{ $t('business.create-user') }}</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1130" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-user')" class="w-560px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="top">
        <NFormItem :label="$t('business.email')" path="email">
          <NInput v-model:value="formModel.email" placeholder="user@example.com" />
        </NFormItem>
        <NFormItem :label="$t('business.account-delivery')" path="password">
          <div class="w-full">
            <NRadioGroup v-model:value="formModel.create_mode" class="w-full">
              <NRadio value="password">{{ $t('business.direct-password') }}</NRadio>
              <NRadio value="invite" :disabled="!emailEnabled">
                {{ $t('business.email-invitation') }}
              </NRadio>
            </NRadioGroup>
            <template v-if="formModel.create_mode === 'password'">
              <NInputGroup class="mt-8px">
                <NInput
                  v-model:value="formModel.password"
                  type="password"
                  show-password-on="click"
                  :input-props="{ autocomplete: 'new-password' }"
                  :placeholder="$t('business.at-least-16-characters')"
                />
                <NButton @click="fillRandomCreatePassword">{{ $t('business.generate-random-btn') }}</NButton>
              </NInputGroup>
              <PasswordStrength :password="formModel.password" />
            </template>
            <NAlert v-if="formModel.create_mode === 'invite'" type="info" :show-icon="true" class="mt-8px">
              {{ $t('business.invitation-description') }}
            </NAlert>
          </div>
        </NFormItem>
        <NFormItem :label="$t('business.identity-preset-or-group')" path="identity">
          <NSelect
            v-model:value="formModel.identity"
            :options="createIdentityOptions"
            :placeholder="$t('business.select-identity-preset')"
          />
        </NFormItem>
        <NFormItem v-if="isPlatformAdmin" :label="$t('business.owning-organization')" path="organization_id">
          <NSelect
            v-model:value="formModel.organization_id"
            :options="orgOptions"
            clearable
            :placeholder="$t('business.current-organization-by-default')"
          />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">{{ $t('business.create') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      :show="createdPassword !== ''"
      preset="card"
      :title="$t('business.user-created')"
      class="w-600px"
      :mask-closable="false"
      @update:show="val => { if (!val) handleCloseCreatedPassword(); }"
    >
      <NAlert type="warning" :bordered="false" class="mb-12px">
        {{ $t('business.user-created-hint') }}
      </NAlert>
      <NDescriptions :column="1" bordered size="small" label-placement="left">
        <NDescriptionsItem :label="$t('business.login-url')">
          <code class="text-12px font-mono break-all">{{ loginUrl }}</code>
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.login-account')">
          <div class="flex items-center justify-between gap-8px">
            <code class="text-13px font-mono select-all">{{ createdEmail }}</code>
            <NButton size="tiny" quaternary type="primary" @click="copyText(createdEmail)">
              {{ $t('business.copy-account') }}
            </NButton>
          </div>
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.initial-password')">
          <div class="flex items-center justify-between gap-8px">
            <code class="text-13px font-mono select-all break-all">{{ createdPassword }}</code>
            <NButton size="tiny" quaternary type="primary" @click="copyText(createdPassword)">
              {{ $t('business.copy-password') }}
            </NButton>
          </div>
        </NDescriptionsItem>
      </NDescriptions>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="copyAllCredentials">{{ $t('business.copy-all-credentials') }}</NButton>
          <NButton type="primary" @click="handleCloseCreatedPassword">{{ $t('business.i-have-sent-it') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" :title="$t('business.reset-password')" class="w-520px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="top">
        <NFormItem :label="$t('business.email')">
          <NInput :value="editModel.email" disabled />
        </NFormItem>
        <NFormItem :label="$t('business.new-password')" path="password">
          <div class="w-full">
            <NInputGroup>
              <NInput
                v-model:value="editModel.password"
                type="password"
                show-password-on="click"
                :input-props="{ autocomplete: 'new-password' }"
                :placeholder="$t('business.at-least-16-characters')"
              />
              <NButton @click="fillRandomEditPassword">{{ $t('business.generate-random-btn') }}</NButton>
            </NInputGroup>
            <PasswordStrength :password="editModel.password" />
          </div>
        </NFormItem>
        <NAlert type="info" :bordered="false">{{ $t('business.use-the-action-buttons-in') }}</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="showEdit = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleUpdate">
            {{ $t('business.reset-password') }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showMove" preset="card" :title="$t('business.move-organization')" class="w-440px">
      <NForm ref="moveFormRef" :model="moveModel" label-placement="top">
        <NFormItem :label="$t('business.email')">
          <NInput :value="moveModel.email" disabled />
        </NFormItem>
        <NFormItem :label="$t('business.target-organization')" path="organization_id">
          <NSelect
            v-model:value="moveModel.organization_id"
            :options="orgOptions"
            :placeholder="$t('business.select-the-user-s-owning')"
          />
        </NFormItem>
        <NAlert type="info" :bordered="false">{{ $t('business.after-the-move-the-user') }}</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showMove = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submittingMove" @click="handleMove">{{ $t('business.move') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NDrawer v-model:show="showDetail" :width="520" placement="right">
      <NDrawerContent :title="$t('business.userDetail')" closable>
        <NDescriptions v-if="detailUser" :column="1" bordered size="small" label-placement="left">
          <NDescriptionsItem :label="$t('business.email')">{{ detailUser.email }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.identity-preset-or-group')">
            {{ detailUser.permission_group_name || roleText[detailUser.role] || detailUser.role }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.status')">
            <NTag :type="statusTagType(detailUser.status)" size="small" round>
              {{ statusText(detailUser.status) }}
            </NTag>
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.last-login')">
            {{ formatTime(detailUser.last_login_at) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.created-at')">
            {{ formatTime(detailUser.created_at) }}
          </NDescriptionsItem>
        </NDescriptions>
        <template #footer>
          <NSpace justify="end">
            <NButton @click="showDetail = false">{{ $t('business.close') }}</NButton>
            <NButton
              v-if="canManage && detailUser && !( (detailUser.role === 'org_owner' || detailUser.role === 'platform_admin') && !isPlatformAdmin )"
              type="primary"
              @click="detailUser && openEdit(detailUser)"
            >
              {{ $t('business.reset-password') }}
            </NButton>
            <NButton
              v-if="canManage && detailUser && detailUser.role !== 'org_owner' && detailUser.role !== 'platform_admin' && detailUser.id !== currentUserId"
              type="primary"
              secondary
              @click="detailUser && openPermDrawer(detailUser)"
            >
              {{ $t('business.configure-permissions') }}
            </NButton>
            <NButton
              v-if="isPlatformAdmin && detailUser && detailUser.role !== 'org_owner' && detailUser.role !== 'platform_admin'"
              type="warning"
              @click="detailUser && openMove(detailUser)"
            >
              {{ $t('business.move-organization') }}
            </NButton>
          </NSpace>
        </template>
      </NDrawerContent>
    </NDrawer>

    <NDrawer v-model:show="showPermDrawer" :width="720" placement="right">
      <NDrawerContent :title="`${$t('business.configure-permissions')} · ${permUser?.email ?? ''}`" closable>
        <NSpin :show="permLoading">
          <div class="mb-16px flex flex-col gap-8px">
            <div class="text-13px font-medium">{{ $t('business.identity-preset-or-group') }}</div>
            <NSelect
              :value="permIdentity"
              :options="drawerIdentityOptions"
              :placeholder="$t('business.select-identity-preset')"
              @update:value="handlePermIdentityChange"
            />
            <div class="text-12px text-#6b7280">{{ $t('business.preset-base-hint') }}</div>
          </div>

          <PermissionMatrix
            v-model="permChecked"
            :modules="catalogModules"
            :base-permissions="permBase"
            :disabled-permissions="disabledPermissions"
          />
        </NSpin>

        <template #footer>
          <div class="w-full flex items-center justify-between gap-8px">
            <NSpace>
              <NButton :disabled="permLoading || permSaving" @click="resetToPresetBase">
                {{ $t('business.reset-to-preset') }}
              </NButton>
              <NButton v-if="canManageGroups" :disabled="permLoading || permSaving" @click="openSaveAsGroupModal">
                {{ $t('business.save-as-permission-group') }}
              </NButton>
            </NSpace>
            <NSpace>
              <NButton :disabled="permSaving" @click="showPermDrawer = false">{{ $t('business.cancel') }}</NButton>
              <NButton type="primary" :loading="permSaving" @click="handleSaveUserPermissions">
                {{ $t('business.save-permissions') }}
              </NButton>
            </NSpace>
          </div>
        </template>
      </NDrawerContent>
    </NDrawer>

    <NModal v-model:show="showSaveGroupModal" preset="card" :title="$t('business.save-as-permission-group')" class="w-460px">
      <NForm ref="saveGroupFormRef" :model="saveGroupModel" :rules="saveGroupRules" label-placement="top">
        <NFormItem :label="$t('business.permission-group-name')" path="name">
          <NInput v-model:value="saveGroupModel.name" />
        </NFormItem>
        <NFormItem :label="$t('business.permission-group-desc')" path="description">
          <NInput v-model:value="saveGroupModel.description" type="textarea" :autosize="{ minRows: 2, maxRows: 3 }" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="savingGroup" @click="showSaveGroupModal = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="savingGroup" @click="handleSaveAsGroup">{{ $t('business.save') }}</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>
