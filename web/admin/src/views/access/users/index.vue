<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NModal, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchCreateUser, fetchOrganizations, fetchUpdateUser, fetchUsers } from '@/service/api';
import { useAuthStore } from '@/store/modules/auth';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';

defineOptions({
  name: 'AccessUsers'
});

const roleText: Record<string, string> = {
  platform_admin: '平台管理员',
  org_owner: '组织所有者',
  org_admin: '组织管理员',
  project_admin: '项目管理员',
  developer: '开发者',
  viewer: '查看者'
};

const authStore = useAuthStore();
const isPlatformAdmin = computed(() => authStore.userInfo.roles.includes('platform_admin'));
const orgOptions = ref<{ label: string; value: string }[]>([]);
const filterOrgId = ref<string | null>(null);

const roleOptions = [
  { label: '组织管理员', value: 'org_admin' },
  { label: '项目管理员', value: 'project_admin' },
  { label: '开发者', value: 'developer' },
  { label: '查看者', value: 'viewer' }
];

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

const formRules: FormRules = {
  email: { required: true, message: '请填写邮箱', trigger: 'blur' },
  password: { required: true, message: '请填写密码', trigger: 'blur' },
  role: { required: true, message: '请选择角色', trigger: 'change' }
};
const editRules: FormRules = {
  password: { required: true, message: '请填写新密码', trigger: 'blur' }
};

const columns: DataTableColumns<Api.Management.User> = [
  { title: '邮箱', key: 'email', width: 220 },
  {
    title: '角色',
    key: 'role',
    width: 120,
    render: row => roleText[row.role] ?? row.role
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render(row) {
      return h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status) });
    }
  },
  { title: '最近登录', key: 'last_login_at', width: 180, render: row => formatTime(row.last_login_at) },
  { title: '创建时间', key: 'created_at', width: 180, render: row => formatTime(row.created_at) },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    render(row) {
      if (row.role === 'org_owner' && !isPlatformAdmin.value) return '-';
      const actions = [
        h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, { default: () => '编辑' })
      ];
      if (isPlatformAdmin.value) {
        actions.push(
          h(NButton, { size: 'small', quaternary: true, type: 'warning', onClick: () => openMove(row) }, { default: () => '移动组织' })
        );
      }
      if (row.status === 'active') {
        actions.push(
          h(
            NPopconfirm,
            { onPositiveClick: () => handleToggleStatus(row, 'disabled') },
            {
              trigger: () => h(NButton, { size: 'small', type: 'warning', quaternary: true }, { default: () => '禁用' }),
              default: () => '禁用后该用户所有会话立即失效,确认禁用?'
            }
          )
        );
      } else {
        actions.push(
          h(NButton, { size: 'small', type: 'primary', quaternary: true, onClick: () => handleToggleStatus(row, 'active') }, { default: () => '启用' })
        );
      }
      return h(NSpace, { size: 4 }, {
        default: () => actions
      });
    }
  }
];

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
    orgOptions.value = data.map(item => ({ label: `${item.name} (${item.slug})`, value: item.id }));
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
    const { error } = await fetchUpdateUser(moveModel.id, { organization_id: moveModel.organization_id! });
    if (!error) {
      window.$message?.success('用户归属组织已更新');
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
      window.$message?.success('用户已创建');
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
    const { error } = await fetchUpdateUser(editModel.id, { password: editModel.password });
    if (!error) {
      window.$message?.success('密码已重置');
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
    window.$message?.success(status === 'active' ? '用户已启用' : '用户已禁用');
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
    <NCard title="用户" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NSelect
            v-if="isPlatformAdmin"
            v-model:value="filterOrgId"
            :options="orgOptions"
            clearable
            placeholder="按组织筛选"
            class="w-220px"
            @update:value="handleFilterOrg"
          />
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" @click="openCreate">新建用户</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1000" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建用户" class="w-520px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem label="邮箱" path="email">
          <NInput v-model:value="formModel.email" placeholder="user@example.com" />
        </NFormItem>
        <NFormItem label="初始密码" path="password">
          <NInput v-model:value="formModel.password" type="password" show-password-on="click" placeholder="至少 16 个字符" />
        </NFormItem>
        <NFormItem label="角色" path="role">
          <NSelect v-model:value="formModel.role" :options="roleOptions" />
        </NFormItem>
        <NFormItem v-if="isPlatformAdmin" label="归属组织" path="organization_id">
          <NSelect v-model:value="formModel.organization_id" :options="orgOptions" clearable placeholder="默认当前组织" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">创建</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" title="重置密码" class="w-520px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="100">
        <NFormItem label="邮箱">
          <NInput :value="editModel.email" disabled />
        </NFormItem>
        <NFormItem label="新密码" path="password">
          <NInput v-model:value="editModel.password" type="password" show-password-on="click" placeholder="至少 16 个字符" />
        </NFormItem>
        <NAlert type="info" :bordered="false">角色与状态调整请使用列表中的操作按钮。</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showEdit = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleUpdate">重置密码</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showMove" preset="card" title="移动组织" class="w-440px">
      <NForm ref="moveFormRef" :model="moveModel" label-placement="left" label-width="100">
        <NFormItem label="邮箱">
          <NInput :value="moveModel.email" disabled />
        </NFormItem>
        <NFormItem label="目标组织" path="organization_id">
          <NSelect v-model:value="moveModel.organization_id" :options="orgOptions" placeholder="选择用户归属组织" />
        </NFormItem>
        <NAlert type="info" :bordered="false">移动后该用户的会话仍然有效,下一次请求即按新组织鉴权。</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showMove = false">取消</NButton>
          <NButton type="primary" :loading="submittingMove" @click="handleMove">移动</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>

