<script setup lang="ts">
import { h, onMounted, ref, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { NButton, NDropdown, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns, DropdownOption } from 'naive-ui';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';
import { fetchAssets, fetchUpdateAssetStatus } from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';
import TemplateGrantsModal from '../prompt-templates/modules/template-grants-modal.vue';
import TemplateVersionsModal from '../prompt-templates/modules/template-versions-modal.vue';
import AgentChatWorkspace from './modules/agent-chat-workspace.vue';
import AgentFormModal from './modules/agent-form-modal.vue';

defineOptions({ name: 'AssetRegistryAgents' });

const { has } = usePermission();
const canManage = computed(() => has('agent:manage'));
const route = useRoute();
const router = useRouter();

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
  { title: $t('business.agent-name'), key: 'name', ellipsis: { tooltip: true }, minWidth: 170 },
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
    width: 210,
    // 操作列固定在右侧:列多于一屏宽度时,行内操作不用横向滚动也能点到。
    fixed: 'right',
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
              { size: 'small', quaternary: true, type: 'primary', onClick: () => openAction('grants', row) },
              { default: () => $t('business.grants') }
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
              NDropdown,
              {
                trigger: 'click',
                options: rowMoreOptions(row),
                onSelect: key => handleRowMoreAction(key, row)
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

onMounted(async () => {
  await loadData();
  const assetID = typeof route.query.asset === 'string' ? route.query.asset : '';
  if (assetID) {
    const asset = list.value.find(item => item.id === assetID);
    if (asset) {
      // 审计里的授权事件带 grants=1,直接打开授权弹窗,不叠开编辑表单。
      if (route.query.grants === '1') openAction('grants', asset);
      else openForm(asset.id);
    }
  }
  const agentID = typeof route.query.agent === 'string' ? route.query.agent : '';
  if (agentID) {
    const asset = list.value.find(item => item.id === agentID);
    if (asset) chatAsset.value = asset;
  }
});

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
  void router.replace({ query: { ...route.query, agent: asset.id } });
}

function closeChat() {
  chatAsset.value = null;
  const query = { ...route.query };
  delete query.agent;
  void router.replace({ query });
}

function lifecycleOptions(asset: Api.Management.Asset): DropdownOption[] {
  const options: DropdownOption[] = [];
  if (asset.status === 'published') options.push({ label: $t('business.pause'), key: 'disabled' });
  if (asset.status === 'disabled') options.push({ label: $t('business.resume'), key: 'published' });
  if (asset.status !== 'archived') {
    options.push({
      label: $t('business.archive-agent'),
      key: 'archived',
      props: { style: { color: '#d03050' } }
    });
  }
  return options;
}

function rowMoreOptions(asset: Api.Management.Asset): DropdownOption[] {
  const options: DropdownOption[] = [
    { label: $t('business.version'), key: 'versions' },
    { label: $t('business.grants'), key: 'grants' }
  ];
  const lifecycle = lifecycleOptions(asset);
  if (lifecycle.length > 0) {
    options.push({ type: 'divider', key: 'divider-lifecycle' }, ...lifecycle);
  }
  return options;
}

function handleRowMoreAction(key: string | number, asset: Api.Management.Asset) {
  if (key === 'versions' || key === 'grants') {
    openAction(key, asset);
    return;
  }
  void handleLifecycle(String(key), asset);
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
    <AgentChatWorkspace v-if="chatAsset" :asset="chatAsset" @back="closeChat" />
    <NCard v-else :title="$t('business.agent-management')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton v-if="canManage" type="primary" @click="openForm(null)">{{ $t('business.create-agent') }}</NButton>
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
