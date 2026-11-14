<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchCreateOrganization, fetchOrganizations, fetchUpdateOrganization } from '@/service/api';
import { useAuthStore } from '@/store/modules/auth';
import { formatTime } from '@/utils/admin-status';

defineOptions({
  name: 'OrganizationOrganizations'
});

const authStore = useAuthStore();
const isPlatformAdmin = computed(() => authStore.userInfo.roles.includes('platform_admin'));

const loading = ref(true);
const list = ref<Api.Management.Organization[]>([]);

const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({ name: '', slug: '' });
const editModel = reactive({ id: '', name: '', slug: '' });

const formRules: FormRules = {
  name: { required: true, message: '请填写组织名称', trigger: 'blur' },
  slug: {
    required: true,
    validator: (_rule, value: string) => {
      if (!/^[a-z0-9][a-z0-9-]{0,62}$/.test(value)) {
        return new Error('slug 仅支持小写字母、数字和连字符,长度 1-63,且以字母或数字开头');
      }
      return true;
    },
    trigger: 'blur'
  }
};
const editRules: FormRules = {
  name: { required: true, message: '请填写组织名称', trigger: 'blur' }
};

const columns: DataTableColumns<Api.Management.Organization> = [
  { title: '名称', key: 'name' },
  { title: 'Slug', key: 'slug' },
  { title: '创建时间', key: 'created_at', width: 180, render: row => formatTime(row.created_at) },
  {
    title: '操作',
    key: 'actions',
    width: 120,
    render(row) {
      if (!isPlatformAdmin.value && row.id !== currentOrgId()) return '';
      return h(
        NButton,
        { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) },
        { default: () => '编辑' }
      );
    }
  }
];

function currentOrgId() {
  // 组织列表对非平台管理员只含自己的组织,直接取第一行即可。
  return list.value[0]?.id ?? '';
}

async function loadData() {
  loading.value = true;
  try {
    const { data, error } = await fetchOrganizations();
    if (!error && data) {
      list.value = data;
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  formModel.name = '';
  formModel.slug = '';
  showCreate.value = true;
}

function openEdit(row: Api.Management.Organization) {
  editModel.id = row.id;
  editModel.name = row.name;
  editModel.slug = row.slug;
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchCreateOrganization({ name: formModel.name.trim(), slug: formModel.slug.trim() });
    if (!error) {
      window.$message?.success('组织已创建');
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
    const { error } = await fetchUpdateOrganization(editModel.id, { name: editModel.name.trim() });
    if (!error) {
      window.$message?.success('组织已更新');
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

onMounted(() => {
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard title="组织" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NTag v-if="!isPlatformAdmin" type="info" size="small" round>仅显示当前组织</NTag>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton v-if="isPlatformAdmin" type="primary" @click="openCreate">新建组织</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建组织" class="w-440px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="80">
        <NFormItem label="组织名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="例如:Acme" />
        </NFormItem>
        <NFormItem label="Slug" path="slug">
          <NInput v-model:value="formModel.slug" placeholder="例如:acme" />
        </NFormItem>
        <NAlert type="info" :bordered="false">新组织初始没有任何成员,请到「用户」页面将用户归属到该组织。</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">创建</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" title="编辑组织" class="w-440px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="80">
        <NFormItem label="组织名称" path="name">
          <NInput v-model:value="editModel.name" placeholder="组织名称" />
        </NFormItem>
        <NFormItem label="Slug">
          <NInput :value="editModel.slug" disabled />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showEdit = false">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="handleUpdate">保存</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>