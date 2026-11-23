<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { formatTime } from '@/utils/admin-status';
import { fetchCreateOrganization, fetchOrganizations, fetchUpdateOrganization } from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';

defineOptions({
  name: 'OrganizationOrganizations'
});

const { canManage, isPlatformAdmin } = usePermission();

const loading = ref(true);
const list = ref<Api.Management.Organization[]>([]);

const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const formModel = reactive({ name: '', slug: '' });
const editModel = reactive({ id: '', name: '', slug: '' });

const formRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-an-organization-name'),
    trigger: 'blur'
  },
  slug: {
    required: true,
    validator: (_rule, value: string) => {
      if (!/^[a-z0-9][a-z0-9-]{0,62}$/.test(value)) {
        return new Error($t('business.organization-slugs-can-contain-only'));
      }
      return true;
    },
    trigger: 'blur'
  }
}));
const editRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-an-organization-name'),
    trigger: 'blur'
  }
}));

const columns = computed<DataTableColumns<Api.Management.Organization>>(() => [
  { title: $t('business.name'), key: 'name' },
  { title: $t('business.organization-slug'), key: 'slug' },
  {
    title: $t('business.created-at'),
    key: 'created_at',
    width: 180,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 120,
    render(row) {
      if (!canManage.value) return '';
      if (!isPlatformAdmin.value && row.id !== currentOrgId()) return '';
      return h(
        NButton,
        {
          size: 'small',
          quaternary: true,
          type: 'primary',
          onClick: () => openEdit(row)
        },
        { default: () => $t('business.edit') }
      );
    }
  }
]);

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
    const { error } = await fetchCreateOrganization({
      name: formModel.name.trim(),
      slug: formModel.slug.trim()
    });
    if (!error) {
      window.$message?.success($t('business.organization-created'));
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
    const { error } = await fetchUpdateOrganization(editModel.id, {
      name: editModel.name.trim()
    });
    if (!error) {
      window.$message?.success($t('business.organization-updated'));
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
    <NCard :title="$t('business.organization')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NTag v-if="!isPlatformAdmin" type="info" size="small" round>
            {{ $t('business.showing-current-organization-only') }}
          </NTag>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton v-if="isPlatformAdmin" type="primary" @click="openCreate">
            {{ $t('business.create-organization') }}
          </NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-organization')" class="w-440px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="80">
        <NFormItem :label="$t('business.organization-name')" path="name">
          <NInput v-model:value="formModel.name" :placeholder="$t('business.for-example-acme')" />
        </NFormItem>
        <NFormItem :label="$t('business.organization-slug')" path="slug">
          <NInput v-model:value="formModel.slug" :placeholder="$t('business.for-example-acme-114ldh9')" />
        </NFormItem>
        <NAlert type="info" :bordered="false">{{ $t('business.a-new-organization-has-no') }}</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">{{ $t('business.create') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" :title="$t('business.edit-organization')" class="w-440px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="left" label-width="80">
        <NFormItem :label="$t('business.organization-name')" path="name">
          <NInput v-model:value="editModel.name" :placeholder="$t('business.organization-name')" />
        </NFormItem>
        <NFormItem :label="$t('business.organization-slug')">
          <NInput :value="editModel.slug" disabled />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showEdit = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleUpdate">{{ $t('business.save') }}</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>
