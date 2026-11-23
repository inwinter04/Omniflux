<script setup lang="ts">
import { h, onMounted, ref, computed } from 'vue';
import { useRouter } from 'vue-router';
import { NButton, NDropdown, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';
import { fetchAssets, fetchUpdateAssetStatus } from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';
import TemplateEditModal from './modules/template-edit-modal.vue';
import TemplateFormModal from './modules/template-form-modal.vue';
import TemplateGrantsModal from './modules/template-grants-modal.vue';
import TemplateTestModal from './modules/template-test-modal.vue';
import TemplateVersionsModal from './modules/template-versions-modal.vue';

defineOptions({ name: 'AssetRegistryPromptTemplates' });

const { canManage } = usePermission();

const loading = ref(true);
const list = ref<Api.Management.Asset[]>([]);
const createVisible = ref(false);
const editVisible = ref(false);
const testVisible = ref(false);
const testPreferLatest = ref(false);
const versionsVisible = ref(false);
const grantsVisible = ref(false);
const editingID = ref<string | null>(null);
const activeAsset = ref<Api.Management.Asset | null>(null);
const archiveTarget = ref<Api.Management.Asset | null>(null);
const router = useRouter();

const columns = computed<DataTableColumns<Api.Management.Asset>>(() => [
  { title: $t('business.template-name'), key: 'name', minWidth: 160 },
  {
    title: $t('business.usage-description'),
    key: 'description',
    minWidth: 220,
    ellipsis: { tooltip: true }
  },
  {
    title: $t('business.current-version'),
    key: 'current_version',
    width: 150,
    render: row =>
      h(
        NSpace,
        { size: 6 },
        {
          default: () => [
            h(
              'span',
              row.current_version
                ? $t('business.versionValue', { version: row.current_version })
                : $t('business.not-enabled')
            ),
            row.has_draft
              ? h(
                  NTag,
                  { size: 'small', type: 'info', bordered: false },
                  { default: () => $t('business.has-unsaved-changes') }
                )
              : null
          ]
        }
      )
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 90,
    render: row =>
      h(
        NTag,
        { type: statusTagType(row.status), size: 'small', round: true },
        { default: () => statusText(row.status) }
      )
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
    width: 370,
    render: row => {
      if (!canManage.value) {
        return h(NSpace, { size: 4 }, {
          default: () => [
            h(
              NButton,
              { size: 'small', quaternary: true, type: 'primary', onClick: () => openAction('versions', row) },
              { default: () => $t('business.version') }
            ),
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'primary',
                onClick: () => router.push({ name: 'asset-registry_evaluations', query: { asset: row.id } })
              },
              { default: () => $t('business.evaluate') }
            )
          ]
        });
      }
      return h(
        NSpace,
        { size: 4 },
        {
          default: () => [
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'primary',
                onClick: () => openForm(row.id)
              },
              { default: () => $t('business.edit') }
            ),
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'primary',
                onClick: () => openAction('test', row)
              },
              { default: () => $t('business.test') }
            ),
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'primary',
                onClick: () => openAction('versions', row)
              },
              { default: () => $t('business.version') }
            ),
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'primary',
                onClick: () => openAction('grants', row)
              },
              { default: () => $t('business.grants') }
            ),
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'primary',
                onClick: () =>
                  router.push({
                    name: 'asset-registry_evaluations',
                    query: { asset: row.id }
                  })
              },
              { default: () => $t('business.evaluate') }
            ),
            h(
              NDropdown,
              {
                options: lifecycleOptions(row),
                onSelect: key => handleLifecycle(key, row)
              },
              {
                default: () => h(NButton, { size: 'small', quaternary: true }, { default: () => $t('business.more') })
              }
            )
          ]
        }
      );
    }
  }
]);

async function loadData() {
  loading.value = true;
  try {
    const { data, error } = await fetchAssets();
    if (!error && data) list.value = data;
  } finally {
    loading.value = false;
  }
}

function lifecycleOptions(asset: Api.Management.Asset) {
  const options: { label: string; key: string }[] = [];
  if (asset.status === 'published') options.push({ label: $t('business.pause'), key: 'disabled' });
  if (asset.status === 'disabled') options.push({ label: $t('business.resume'), key: 'published' });
  if (asset.status !== 'archived') options.push({ label: $t('business.archive-template'), key: 'archived' });
  return options;
}

async function handleLifecycle(key: string, asset: Api.Management.Asset) {
  if (key === 'archived') {
    archiveTarget.value = asset;
    return;
  }
  const { error } = await fetchUpdateAssetStatus(asset.id, key as Api.Management.AssetStatus);
  if (!error) {
    window.$message?.success(key === 'disabled' ? $t('business.template-paused') : $t('business.template-resumed'));
    await loadData();
  }
}

async function archiveAsset() {
  if (!archiveTarget.value) return;
  const { error } = await fetchUpdateAssetStatus(archiveTarget.value.id, 'archived');
  if (!error) {
    archiveTarget.value = null;
    window.$message?.success($t('business.template-archived'));
    await loadData();
  }
}

function openForm(id: string | null) {
  if (id) {
    editingID.value = id;
    editVisible.value = true;
  } else {
    createVisible.value = true;
  }
}

function openAction(action: 'test' | 'versions' | 'grants', asset: Api.Management.Asset, preferLatest = false) {
  activeAsset.value = asset;
  if (action === 'test') {
    testPreferLatest.value = preferLatest;
    testVisible.value = true;
  }
  if (action === 'versions') versionsVisible.value = true;
  if (action === 'grants') grantsVisible.value = true;
}

function openTestFor(id: string) {
  editVisible.value = false;
  const asset = list.value.find(item => item.id === id);
  if (asset) openAction('test', asset, true);
  else
    void loadData().then(() => {
      const created = list.value.find(item => item.id === id);
      if (created) openAction('test', created, true);
    });
}

onMounted(loadData);
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :title="$t('business.prompt-template')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton v-if="canManage" type="primary" @click="openForm(null)">{{ $t('business.create-template') }}</NButton>
        </NSpace>
      </template>
      <NDataTable
        :columns="columns"
        :data="list"
        :loading="loading"
        :bordered="false"
        :scroll-x="1050"
        :pagination="{ pageSize: 15 }"
      />
    </NCard>

    <TemplateFormModal v-model:show="createVisible" :asset-id="null" @saved="loadData" @test="openTestFor" />
    <TemplateEditModal v-model:show="editVisible" :asset-id="editingID" @saved="loadData" @test="openTestFor" />
    <TemplateTestModal v-model:show="testVisible" :asset="activeAsset" :prefer-latest="testPreferLatest" />
    <TemplateVersionsModal v-model:show="versionsVisible" :asset="activeAsset" @saved="loadData" />
    <TemplateGrantsModal v-model:show="grantsVisible" :asset="activeAsset" @saved="loadData" />
    <NModal
      :show="Boolean(archiveTarget)"
      preset="dialog"
      type="info"
      :title="$t('business.archive-this-template')"
      :positive-text="$t('business.confirm-archive')"
      :negative-text="$t('business.cancel')"
      @positive-click="archiveAsset"
      @update:show="
        value => {
          if (!value) archiveTarget = null;
        }
      "
    >
      {{ $t('business.after-archiving-the-item-cannot') }}
    </NModal>
  </div>
</template>
