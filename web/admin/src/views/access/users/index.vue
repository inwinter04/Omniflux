<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NModal, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';
import { fetchCreateUser, fetchOrganizations, fetchUpdateUser, fetchUsers } from '@/service/api';
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
const isPlatformAdmin = computed(() => authStore.userInfo.roles.includes('platform_admin'));
const orgOptions = ref<{ label: string; value: string }[]>([]);
const filterOrgId = ref<string | null>(null);

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
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({
  email: '',
  password: '',
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

const formRules = computed<FormRules>(() => ({
  email: {
    required: true,
    message: $t('business.enter-an-email-address'),
    trigger: 'blur'
  },
  password: {
    required: true,
    message: $t('business.enter-a-password'),
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
  { title: $t('business.email'), key: 'email', width: 220 },
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
    width: 150,
    render(row) {
      if (row.role === 'org_owner' && !isPlatformAdmin.value) return '-';
      const actions = [
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

function handleFilterOrg() {
  void loadData();
}

function openCreate() {
  formModel.email = '';
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
  submitting.value = true;
  try {
    const { error } = await fetchCreateUser({
      email: formModel.email.trim(),
      password: formModel.password,
      role: formModel.role,
      organization_id: formModel.organization_id ?? undefined
    });
    if (!error) {
      window.$message?.success($t('business.user-created'));
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

async function handleToggleStatus(row: Api.Management.User, status: 'active' | 'disabled') {
  const { error } = await fetchUpdateUser(row.id, { status });
  if (!error) {
    window.$message?.success(status === 'active' ? $t('business.user-enabled') : $t('business.user-disabled'));
    await loadData();
  }
}

onMounted(() => {
  void loadData();
  void loadOrganizations();
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
        <NFormItem :label="$t('business.initial-password')" path="password">
          <NInput
            v-model:value="formModel.password"
            type="password"
            show-password-on="click"
            :placeholder="$t('business.at-least-16-characters')"
          />
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
          <NButton @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">{{ $t('business.create') }}</NButton>
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
          <NButton @click="showEdit = false">{{ $t('business.cancel') }}</NButton>
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
  </div>
</template>

<style scoped></style>
