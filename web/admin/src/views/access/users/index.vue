<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { useRoute } from 'vue-router';
import { NButton, NModal, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';
import {
  fetchCreateUser,
  fetchInviteOptions,
  fetchInviteUser,
  fetchOrganizations,
  fetchResendUserInvitation,
  fetchRevokeUserInvitation,
  fetchUpdateUser,
  fetchUsers
} from '@/service/api';
import { $t } from '@/locales';
import { useAuthStore } from '@/store/modules/auth';

defineOptions({
  name: 'AccessUsers'
});

const roleText = computed<Record<string, string>>(() => ({
  platform_admin: $t('business.platform-admin'),
  org_owner: $t('business.organization-owner'),
  org_admin: $t('business.organization-admin'),
  project_admin: $t('business.project-admin'),
  developer: $t('business.developer'),
  viewer: $t('business.viewer')
}));

const authStore = useAuthStore();
const route = useRoute();
const isPlatformAdmin = computed(() => authStore.userInfo.roles.includes('platform_admin'));
const orgOptions = ref<{ label: string; value: string }[]>([]);
const filterOrgId = ref<string | null>(null);
const emailEnabled = ref(false);

const roleOptions = computed(() => [
  { label: $t('business.organization-admin'), value: 'org_admin' },
  { label: $t('business.project-admin'), value: 'project_admin' },
  { label: $t('business.developer'), value: 'developer' },
  { label: $t('business.viewer'), value: 'viewer' }
]);

const loading = ref(true);
const list = ref<Api.Management.User[]>([]);
const currentUserId = ref('');

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
  create_mode: 'random',
  role: 'developer',
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
  role: null as string | null,
  status: null as string | null,
  password: ''
});

const createdEmail = ref('');
const createdPassword = ref('');

const formRules = computed<FormRules>(() => ({
  email: {
    required: true,
    message: $t('business.enter-an-email-address'),
    trigger: 'blur'
  },
  password: {
    required: formModel.create_mode === 'manual',
    validator: (_rule, value: string) => formModel.create_mode !== 'manual' || !value || value.length >= 16,
    message: $t('business.password-must-be-at-least'),
    trigger: 'blur'
  },
  role: {
    required: true,
    message: $t('business.select-a-role'),
    trigger: 'change'
  }
}));
const editRules = computed<FormRules>(() => ({
  password: {
    required: true,
    message: $t('business.enter-a-new-password'),
    trigger: 'blur'
  }
}));

const columns = computed<DataTableColumns<Api.Management.User>>(() => [
  { title: $t('business.email'), key: 'email', ellipsis: { tooltip: true }, width: 220 },
  {
    title: $t('business.role'),
    key: 'role',
    width: 120,
    render: row => roleText.value[row.role] ?? row.role
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 100,
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
    width: 180,
    render: row => formatTime(row.last_login_at)
  },
  {
    title: $t('business.created-at'),
    key: 'created_at',
    width: 180,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 330,
    // 操作列固定在右侧:列多于一屏宽度时,行内操作不用横向滚动也能点到。
    fixed: 'right',
    render(row) {
      if (row.role === 'org_owner' && !isPlatformAdmin.value) return '-';
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
        ),
        h(
          NButton,
          {
            size: 'small',
            quaternary: true,
            type: 'primary',
            onClick: () => openEdit(row)
          },
          { default: () => $t('business.edit') }
        )
      ];
      if (row.status === 'pending_email_verification') {
        actions.push(
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'primary',
              onClick: () => handleResendInvitation(row)
            },
            { default: () => $t('business.resend-invitation') }
          ),
          h(
            NPopconfirm,
            { onPositiveClick: () => handleRevokeInvitation(row) },
            {
              trigger: () =>
                h(
                  NButton,
                  { size: 'small', type: 'warning', quaternary: true },
                  { default: () => $t('business.revoke-invitation') }
                ),
              default: () => $t('business.revoke-invitation-confirm')
            }
          )
        );
      }
      if (isPlatformAdmin.value) {
        actions.push(
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'warning',
              onClick: () => openMove(row)
            },
            { default: () => $t('business.move-organization') }
          )
        );
      }
      if (row.status === 'active') {
        actions.push(
          h(
            NPopconfirm,
            { onPositiveClick: () => handleToggleStatus(row, 'disabled') },
            {
              trigger: () =>
                h(
                  NButton,
                  { size: 'small', type: 'warning', quaternary: true },
                  { default: () => $t('business.disable') }
                ),
              default: () => $t('business.all-sessions-of-this-user')
            }
          )
        );
      } else {
        actions.push(
          h(
            NButton,
            {
              size: 'small',
              type: 'primary',
              quaternary: true,
              onClick: () => handleToggleStatus(row, 'active')
            },
            { default: () => $t('business.enable') }
          )
        );
      }
      return h(
        NSpace,
        { size: 4 },
        {
          default: () => actions
        }
      );
    }
  }
]);

async function loadData() {
  loading.value = true;
  try {
    const { data, error } = await fetchUsers(filterOrgId.value ?? undefined);
    if (!error && data) {
      list.value = data;
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

async function loadEmailSettings() {
  const { data, error } = await fetchInviteOptions();
  emailEnabled.value = !error && data.email_enabled;
}
function handleFilterOrg() {
  void loadData();
}

function openCreate() {
  formModel.email = '';
  formModel.create_mode = 'random';
  formModel.password = '';
  formModel.role = 'developer';
  formModel.organization_id = null;
  showCreate.value = true;
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
  editModel.role = row.role;
  editModel.status = row.status;
  editModel.password = '';
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  const email = formModel.email.trim();
  submitting.value = true;
  try {
    if (formModel.create_mode === 'invite') {
      const { error } = await fetchInviteUser({
        email,
        role: formModel.role,
        organization_id: formModel.organization_id ?? undefined
      });
      if (!error) {
        window.$message?.success($t('business.invitation-email-sent'));
        showCreate.value = false;
        await loadData();
      }
    } else {
      const { data, error } = await fetchCreateUser({
        email,
        password: formModel.create_mode === 'manual' ? formModel.password : undefined,
        generate_password: formModel.create_mode === 'random',
        role: formModel.role,
        organization_id: formModel.organization_id ?? undefined
      });
    if (!error) {
      if (data?.generated_password) {
        createdEmail.value = email;
        createdPassword.value = data.generated_password;
      } else {
        window.$message?.success($t('business.user-created'));
      }
      showCreate.value = false;
      await loadData();
    }
  }
  } finally {
    submitting.value = false;
  }
}

async function handleCopyCreatedPassword() {
  await navigator.clipboard.writeText(createdPassword.value);
  window.$message?.success($t('business.copied-to-clipboard'));
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

onMounted(async () => {
  await Promise.all([loadData(), loadOrganizations(), loadEmailSettings()]);
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
          <NButton type="primary" @click="openCreate">{{ $t('business.create-user') }}</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1000" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-user')" class="w-520px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem :label="$t('business.email')" path="email">
          <NInput v-model:value="formModel.email" placeholder="user@example.com" />
        </NFormItem>
        <NFormItem :label="$t('business.account-delivery')" path="password">
          <div class="w-full">
            <NRadioGroup v-model:value="formModel.create_mode" class="w-full">
              <NRadio value="random">{{ $t('business.generate-random-password') }}</NRadio>
              <NRadio value="manual">{{ $t('business.manual-password') }}</NRadio>
              <NRadio value="invite" :disabled="!emailEnabled">
                {{ $t('business.email-invitation') }}
              </NRadio>
            </NRadioGroup>
            <NInput
              v-if="formModel.create_mode === 'manual'"
              v-model:value="formModel.password"
              type="password"
              show-password-on="click"
              class="mt-8px"
              :placeholder="$t('business.at-least-16-characters')"
            />
            <NAlert v-if="formModel.create_mode === 'invite'" type="info" :show-icon="true" class="mt-8px">
              {{ $t('business.invitation-description') }}
            </NAlert>
          </div>
        </NFormItem>
        <NFormItem :label="$t('business.role')" path="role">
          <NSelect v-model:value="formModel.role" :options="roleOptions" />
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
      class="w-560px"
      :mask-closable="false"
    >
      <NAlert type="warning" :bordered="false" class="mb-12px">
        {{ $t('business.initial-password-shown-only-once') }}
      </NAlert>
      <NDescriptions :column="1" bordered size="small" label-placement="left">
        <NDescriptionsItem :label="$t('business.email')">{{ createdEmail }}</NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.initial-password')">
          <NInput :value="createdPassword" readonly type="textarea" :autosize="{ minRows: 2 }" />
        </NDescriptionsItem>
      </NDescriptions>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="handleCopyCreatedPassword">{{ $t('business.copy-password') }}</NButton>
          <NButton type="primary" @click="handleCloseCreatedPassword">{{ $t('business.i-have-sent-it') }}</NButton>
        </NSpace>
      </template>
    </NModal>
    <NModal v-model:show="showEdit" preset="card" :title="$t('business.reset-password')" class="w-520px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="100">
        <NFormItem :label="$t('business.email')">
          <NInput :value="editModel.email" disabled />
        </NFormItem>
        <NFormItem :label="$t('business.new-password')" path="password">
          <NInput
            v-model:value="editModel.password"
            type="password"
            show-password-on="click"
            :placeholder="$t('business.at-least-16-characters')"
          />
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
      <NForm ref="moveFormRef" :model="moveModel" label-placement="left" label-width="100">
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
          <NDescriptionsItem :label="$t('business.role')">
            {{ roleText[detailUser.role] ?? detailUser.role }}
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
              v-if="detailUser && !(detailUser.role === 'org_owner' && !isPlatformAdmin)"
              type="primary"
              @click="detailUser && openEdit(detailUser)"
            >
              {{ $t('business.reset-password') }}
            </NButton>
            <NButton v-if="isPlatformAdmin && detailUser" type="warning" @click="detailUser && openMove(detailUser)">
              {{ $t('business.move-organization') }}
            </NButton>
          </NSpace>
        </template>
      </NDrawerContent>
    </NDrawer>
  </div>
</template>

<style scoped></style>
