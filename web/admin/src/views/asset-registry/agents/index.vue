<script setup lang="ts">
import { h, onMounted, ref, computed } from 'vue';
import { NButton, NDropdown, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';
import { fetchAssets, fetchUpdateAssetStatus } from '@/service/api';
import { $t } from '@/locales';
import TemplateGrantsModal from '../prompt-templates/modules/template-grants-modal.vue';
import TemplateVersionsModal from '../prompt-templates/modules/template-versions-modal.vue';
import AgentChatWorkspace from './modules/agent-chat-workspace.vue';
import AgentFormModal from './modules/agent-form-modal.vue';

defineOptions({ name: 'AssetRegistryAgents' });

const loading = ref(true);
const list = ref<Api.Management.Asset[]>([]);
const formVisible = ref(false);
const versionsVisible = ref(false);
const grantsVisible = ref(false);
const editingID = ref<string | null>(null);
const activeAsset = ref<Api.Management.Asset | null>(null);
const chatAsset = ref<Api.Management.Asset | null>(null);
const archiveTarget = ref<Api.Management.Asset | null>(null);

const columns = computed<DataTableColumns<Api.Management.Asset>>(() => [
  { title: $t('business.agent-name'), key: 'name', minWidth: 170 },
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
    width: 330,
    render: row =>
      h(
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
                onClick: () => openChat(row)
              },
              { default: () => $t('business.chat') }
            ),
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'primary',
                onClick: () => openForm(row.id)
              },
              { default: () => $t('business.configuration') }
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
      )
  }
]);

onMounted(loadData);

async function loadData() {
  loading.value = true;
  try {
    const { data, error } = await fetchAssets('agent');
    if (!error && data) list.value = data;
  } finally {
    loading.value = false;
  }
}

function openForm(id: string | null) {
  editingID.value = id;
  formVisible.value = true;
}

function openAction(action: 'versions' | 'grants', asset: Api.Management.Asset) {
  activeAsset.value = asset;
  if (action === 'versions') versionsVisible.value = true;
  if (action === 'grants') grantsVisible.value = true;
}

function openChat(asset: Api.Management.Asset) {
  chatAsset.value = asset;
}

function lifecycleOptions(asset: Api.Management.Asset) {
  const options: { label: string; key: string }[] = [];
  if (asset.status === 'published') options.push({ label: $t('business.pause'), key: 'disabled' });
  if (asset.status === 'disabled') options.push({ label: $t('business.resume'), key: 'published' });
  if (asset.status !== 'archived') options.push({ label: $t('business.archive-agent'), key: 'archived' });
  return options;
}

async function handleLifecycle(key: string, asset: Api.Management.Asset) {
  if (key === 'archived') {
    archiveTarget.value = asset;
    return;
  }
  const { error } = await fetchUpdateAssetStatus(asset.id, key as Api.Management.AssetStatus);
  if (!error) {
    window.$message?.success(key === 'disabled' ? $t('business.agent-paused') : $t('business.agent-resumed'));
    await loadData();
  }
}

async function archiveAsset() {
  if (!archiveTarget.value) return;
  const { error } = await fetchUpdateAssetStatus(archiveTarget.value.id, 'archived');
  if (!error) {
    archiveTarget.value = null;
    window.$message?.success($t('business.agent-archived'));
    await loadData();
  }
}
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <AgentChatWorkspace v-if="chatAsset" :asset="chatAsset" @back="chatAsset = null" />
    <NCard v-else :title="$t('business.agent-management')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton type="primary" @click="openForm(null)">{{ $t('business.create-agent') }}</NButton>
        </NSpace>
      </template>
      <NDataTable
        :columns="columns"
        :data="list"
        :loading="loading"
        :bordered="false"
        :scroll-x="1080"
        :pagination="{ pageSize: 15 }"
      />
    </NCard>

    <AgentFormModal v-model:show="formVisible" :asset-id="editingID" @saved="loadData" />
    <TemplateVersionsModal v-model:show="versionsVisible" :asset="activeAsset" @saved="loadData" />
    <TemplateGrantsModal v-model:show="grantsVisible" :asset="activeAsset" @saved="loadData" />
    <NModal
      :show="Boolean(archiveTarget)"
      preset="dialog"
      type="info"
      :title="$t('business.archive-this-agent')"
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
