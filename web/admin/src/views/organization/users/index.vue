<script setup lang="ts">
import { h, onMounted, reactive, ref } from 'vue';
import { NButton, NModal, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchCreateUser, fetchUpdateUser, fetchUsers } from '@/service/api';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';

defineOptions({
  name: 'OrganizationUsers'
});

const roleText: Record<string, string> = {
  org_owner: '组织所有者',
  org_admin: '组织管理员',
  project_admin: '项目管理员',
  developer: '开发者',
  viewer: '查看者'
};

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
  role: 'developer'
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
      if (row.role === 'org_owner') return '-';
      return h(NSpace, { size: 4 }, {
        default: () => [
          h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) }, { default: () => '编辑' }),
          row.status === 'active'
            ? h(
                NPopconfirm,
                { onPositiveClick: () => handleToggleStatus(row, 'disabled') },
                {
                  trigger: () => h(NButton, { size: 'small', type: 'warning', quaternary: true }, { default: () => '禁用' }),
                  default: () => '禁用后该用户所有会话立即失效,确认禁用?'
                }
              )
            : h(NButton, { size: 'small', type: 'primary', quaternary: true, onClick: () => handleToggleStatus(row, 'active') }, { default: () => '启用' })
        ]
      });
    }
  }
];

async function loadData() {
  loading.value = true;
  try {
    const { data, error } = await fetchUsers();
    if (!error && data) {
      list.value = data;
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  formModel.email = '';
  formModel.password = '';
  formModel.role = 'developer';
  showCreate.value = true;
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
      role: formModel.role
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
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard title="用户" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
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
  </div>
</template>

<style scoped></style>
