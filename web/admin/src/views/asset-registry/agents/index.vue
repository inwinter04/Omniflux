<script setup lang="ts">
import { h, onMounted, ref } from 'vue';
import { NButton, NDropdown, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { fetchAssets, fetchUpdateAssetStatus } from '@/service/api';
import { formatTime, statusTagType, statusText } from '@/utils/admin-status';
import TemplateVersionsModal from '../prompt-templates/modules/template-versions-modal.vue';
import TemplateGrantsModal from '../prompt-templates/modules/template-grants-modal.vue';
import AgentFormModal from './modules/agent-form-modal.vue';
import AgentChatWorkspace from './modules/agent-chat-workspace.vue';

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

const columns: DataTableColumns<Api.Management.Asset> = [
  { title: '智能体名称', key: 'name', minWidth: 170 },
  { title: '用途说明', key: 'description', minWidth: 220, ellipsis: { tooltip: true } },
  {
    title: '当前版本', key: 'current_version', width: 150,
    render: row => h(NSpace, { size: 6 }, {
      default: () => [
        h('span', row.current_version ? `版本 ${row.current_version}` : '尚未启用'),
        row.has_draft ? h(NTag, { size: 'small', type: 'info', bordered: false }, { default: () => '有新修改' }) : null
      ]
    })
  },
  { title: '状态', key: 'status', width: 90, render: row => h(NTag, { type: statusTagType(row.status), size: 'small', round: true }, { default: () => statusText(row.status) }) },
  { title: '更新时间', key: 'updated_at', width: 180, render: row => formatTime(row.updated_at) },
  {
    title: '操作', key: 'actions', width: 330,
    render: row => h(NSpace, { size: 4 }, {
      default: () => [
        h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openChat(row) }, { default: () => '对话' }),
        h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openForm(row.id) }, { default: () => '配置' }),
        h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openAction('versions', row) }, { default: () => '版本' }),
        h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openAction('grants', row) }, { default: () => '授权' }),
        h(NDropdown, { options: lifecycleOptions(row), onSelect: key => handleLifecycle(key, row) }, {
          default: () => h(NButton, { size: 'small', quaternary: true }, { default: () => '更多' })
        })
      ]
    })
  }
];

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
  if (asset.status === 'published') options.push({ label: '暂停使用', key: 'disabled' });
  if (asset.status === 'disabled') options.push({ label: '恢复使用', key: 'published' });
  if (asset.status !== 'archived') options.push({ label: '归档智能体', key: 'archived' });
  return options;
}

async function handleLifecycle(key: string, asset: Api.Management.Asset) {
  if (key === 'archived') {
    archiveTarget.value = asset;
    return;
  }
  const { error } = await fetchUpdateAssetStatus(asset.id, key as Api.Management.AssetStatus);
  if (!error) {
    window.$message?.success(key === 'disabled' ? '智能体已暂停使用' : '智能体已恢复使用');
    await loadData();
  }
}

async function archiveAsset() {
  if (!archiveTarget.value) return;
  const { error } = await fetchUpdateAssetStatus(archiveTarget.value.id, 'archived');
  if (!error) {
    archiveTarget.value = null;
    window.$message?.success('智能体已归档');
    await loadData();
  }
}
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <AgentChatWorkspace v-if="chatAsset" :asset="chatAsset" @back="chatAsset = null" />
    <NCard v-else title="智能体管理" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" @click="openForm(null)">新建智能体</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="1080" :pagination="{ pageSize: 15 }" />
    </NCard>

    <AgentFormModal v-model:show="formVisible" :asset-id="editingID" @saved="loadData" />
    <TemplateVersionsModal v-model:show="versionsVisible" :asset="activeAsset" @saved="loadData" />
    <TemplateGrantsModal v-model:show="grantsVisible" :asset="activeAsset" @saved="loadData" />
    <NModal :show="Boolean(archiveTarget)" preset="dialog" type="info" title="归档智能体？" positive-text="确认归档" negative-text="取消" @positive-click="archiveAsset" @update:show="value => { if (!value) archiveTarget = null; }">
      归档后不能继续编辑或恢复使用，请确认。
    </NModal>
  </div>
</template>
