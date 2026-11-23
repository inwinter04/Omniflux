<script setup lang="ts">
import { computed, h, ref, watch } from 'vue';
import { NButton, NPopconfirm, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { formatTime } from '@/utils/admin-status';
import { buildAssetDiff } from '@/utils/asset-diff';
import { fetchAssetDetail, fetchAssetDiff, fetchPublishAsset, fetchRollbackAsset } from '@/service/api';
import { $t } from '@/locales';
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

const latestDraft = computed(
  () => detail.value?.versions.find(item => item.version === detail.value?.latest_version && !item.published_at) ?? null
);
const versionOptions = computed(() =>
  (detail.value?.versions ?? []).map(version => ({
    label: $t('business.versionWithStatus', {
      version: version.version,
      status: version.current ? $t('business.current') : ''
    }),
    value: version.id
  }))
);

const columns = computed<DataTableColumns<Api.Management.AssetVersion>>(() => [
  {
    title: $t('business.version'),
    key: 'version',
    width: 90,
    render: row => $t('business.versionValue', { version: row.version })
  },
  {
    title: $t('business.status'),
    key: 'state',
    width: 110,
    render: row =>
      h(
        NTag,
        {
          size: 'small',
          type: row.current ? 'success' : row.published_at ? 'default' : 'info'
        },
        {
          default: () =>
            row.current
              ? $t('business.current-cda1r7')
              : row.published_at
                ? $t('business.historical-version-aw3ksw')
                : $t('business.not-enabled')
        }
      )
  },
  { title: $t('business.saved-by'), key: 'created_by_email', minWidth: 180 },
  {
    title: $t('business.saved-at'),
    key: 'created_at',
    width: 175,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 90,
    render(row) {
      if (latestDraft.value?.id === row.id) {
        return h(
          NPopconfirm,
          { onPositiveClick: () => publish(row.id) },
          {
            trigger: () =>
              h(
                NButton,
                { size: 'small', quaternary: true, type: 'primary' },
                { default: () => $t('business.enable') }
              ),
            default: () => $t('business.after-enabling-business-calls-use')
          }
        );
      }
      if (row.published_at && !row.current) {
        return h(
          NPopconfirm,
          { onPositiveClick: () => rollback(row.id) },
          {
            trigger: () =>
              h(
                NButton,
                { size: 'small', quaternary: true, type: 'primary' },
                { default: () => $t('business.restore') }
              ),
            default: () =>
              $t('business.restoreVersionDescription', {
                version: row.version
              })
          }
        );
      }
      return null;
    }
  }
]);

watch(
  () => props.show,
  visible => {
    if (visible) void loadData();
  }
);

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
    window.$message?.success($t('business.new-version-enabled'));
    emit('saved');
    await loadData();
  }
}

async function rollback(versionID: string) {
  if (!props.asset || !detail.value) return;
  const { data, error } = await fetchRollbackAsset(props.asset.id, versionID, detail.value.latest_version);
  if (!error && data) {
    window.$message?.success($t('business.restoredAsVersion', { version: data.version }));
    emit('saved');
    await loadData();
  }
}

async function compare() {
  if (!props.asset || !diffFrom.value || !diffTo.value) return;
  diffLoading.value = true;
  try {
    const { data, error } = await fetchAssetDiff(props.asset.id, diffFrom.value, diffTo.value);
    if (!error && data) diff.value = buildAssetDiff(data);
  } finally {
    diffLoading.value = false;
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="$t('business.version-management')"
    class="w-900px"
    @update:show="value => emit('update:show', value)"
  >
    <NSpin :show="loading">
      <NAlert type="info" :bordered="false" class="mb-16px">
        {{ $t('business.after-a-new-version-is') }}
      </NAlert>
      <NDataTable
        :columns="columns"
        :data="detail?.versions ?? []"
        :bordered="false"
        size="small"
        :scroll-x="720"
        :max-height="300"
      />
      <NDivider title-placement="left">{{ $t('business.compare-versions') }}</NDivider>
      <NSpace align="center" class="mb-12px">
        <NSelect v-model:value="diffFrom" :options="versionOptions" class="w-180px" />
        <span>{{ $t('business.with') }}</span>
        <NSelect v-model:value="diffTo" :options="versionOptions" class="w-180px" />
        <NButton type="primary" secondary :loading="diffLoading" @click="compare">
          {{ $t('business.compare-j4mn') }}
        </NButton>
      </NSpace>
      <TemplateDiff v-if="diff" :diff="diff" />
    </NSpin>
    <template #footer>
      <NSpace justify="end">
        <NButton @click="emit('update:show', false)">{{ $t('business.close') }}</NButton>
      </NSpace>
    </template>
  </NModal>
</template>
