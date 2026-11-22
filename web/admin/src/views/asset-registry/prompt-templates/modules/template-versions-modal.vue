<script setup lang="ts">
import { computed, h, ref, watch } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { fetchAssetDetail, fetchAssetDiff, fetchPublishAsset, fetchRollbackAsset } from '@/service/api';
import { formatTime } from '@/utils/admin-status';
import TemplateDiff from './template-diff.vue';

const props = defineProps<{
  show: boolean;
  asset: Api.Management.Asset | null;
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
  saved: [];
}>();

const loading = ref(false);
const diffLoading = ref(false);
const detail = ref<Api.Management.AssetDetail | null>(null);
const diffFrom = ref<string | null>(null);
const diffTo = ref<string | null>(null);
const diff = ref('');

const latestDraft = computed(() => detail.value?.versions.find(item => item.version === detail.value?.latest_version && !item.published_at) ?? null);
const versionOptions = computed(() => (detail.value?.versions ?? []).map(version => ({
  label: `版本 ${version.version}${version.current ? '（当前使用）' : ''}`,
  value: version.id
})));

const columns: DataTableColumns<Api.Management.AssetVersion> = [
  { title: '版本', key: 'version', width: 90, render: row => `版本 ${row.version}` },
  {
    title: '状态',
    key: 'state',
    width: 110,
    render: row => h(NTag, { size: 'small', type: row.current ? 'success' : row.published_at ? 'default' : 'info' }, {
      default: () => row.current ? '当前使用' : row.published_at ? '历史版本' : '尚未启用'
    })
  },
  { title: '保存人', key: 'created_by_email', minWidth: 180 },
  { title: '保存时间', key: 'created_at', width: 175, render: row => formatTime(row.created_at) },
  {
    title: '操作',
    key: 'actions',
    width: 90,
    render(row) {
      if (latestDraft.value?.id === row.id) {
        return h(NPopconfirm, { onPositiveClick: () => publish(row.id) }, {
          trigger: () => h(NButton, { size: 'small', quaternary: true, type: 'primary' }, { default: () => '启用' }),
          default: () => '启用后，业务调用将使用这个版本。'
        });
      }
      if (row.published_at && !row.current) {
        return h(NPopconfirm, { onPositiveClick: () => rollback(row.id) }, {
          trigger: () => h(NButton, { size: 'small', quaternary: true, type: 'primary' }, { default: () => '恢复' }),
          default: () => `将版本 ${row.version} 的内容保存为一个新版本并启用，原有记录会保留。`
        });
      }
      return null;
    }
  }
];

watch(() => props.show, visible => {
  if (visible) void loadData();
});

async function loadData() {
  if (!props.asset) return;
  loading.value = true;
  diff.value = '';
  try {
    const { data, error } = await fetchAssetDetail(props.asset.id);
    if (error || !data) return;
    detail.value = data;
    diffFrom.value = data.current_version_id ?? data.versions.at(-1)?.id ?? null;
    diffTo.value = data.versions.find(item => item.version === data.latest_version)?.id ?? null;
  } finally {
    loading.value = false;
  }
}

async function publish(versionID: string) {
  if (!props.asset) return;
  const { error } = await fetchPublishAsset(props.asset.id, versionID);
  if (!error) {
    window.$message?.success('新版本已启用');
    emit('saved');
    await loadData();
  }
}

async function rollback(versionID: string) {
  if (!props.asset || !detail.value) return;
  const { data, error } = await fetchRollbackAsset(props.asset.id, versionID, detail.value.latest_version);
  if (!error && data) {
    window.$message?.success(`已恢复内容并启用为版本 ${data.version}`);
    emit('saved');
    await loadData();
  }
}

async function compare() {
  if (!props.asset || !diffFrom.value || !diffTo.value) return;
  diffLoading.value = true;
  try {
    const { data, error } = await fetchAssetDiff(props.asset.id, diffFrom.value, diffTo.value);
    if (!error && data) diff.value = data.diff;
  } finally {
    diffLoading.value = false;
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    title="版本管理"
    class="w-900px"
    @update:show="value => emit('update:show', value)"
  >
    <NSpin :show="loading">
      <NAlert type="info" :bordered="false" class="mb-16px">
        启用新版本后，业务调用会使用该版本。恢复历史内容也会生成新版本，不会删除原有记录。
      </NAlert>
      <NDataTable :columns="columns" :data="detail?.versions ?? []" :bordered="false" size="small" :scroll-x="720" :max-height="300" />
      <NDivider title-placement="left">比较版本</NDivider>
      <NSpace align="center" class="mb-12px">
        <NSelect v-model:value="diffFrom" :options="versionOptions" class="w-180px" />
        <span>与</span>
        <NSelect v-model:value="diffTo" :options="versionOptions" class="w-180px" />
        <NButton type="primary" secondary :loading="diffLoading" @click="compare">比较</NButton>
      </NSpace>
      <TemplateDiff v-if="diff" :diff="diff" />
    </NSpin>
    <template #footer>
      <NSpace justify="end"><NButton @click="emit('update:show', false)">关闭</NButton></NSpace>
    </template>
  </NModal>
</template>
