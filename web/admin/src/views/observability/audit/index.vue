<script setup lang="ts">
import { h, onMounted, ref } from 'vue';
import { NButton, NPagination, NTag } from 'naive-ui';
import { fetchAuditEventDetail } from '@/service/api';
import type { DataTableColumns } from 'naive-ui';
import { fetchAuditEvents } from '@/service/api';
import {
  actorTypeText,
  auditActionText as auditActionLabel,
  auditActionText,
  auditResourceText,
  auditResultTagType,
  auditResultText,
  formatTime
} from '@/utils/admin-status';

defineOptions({
  name: 'ObservabilityAudit'
});

const loading = ref(true);
const records = ref<Api.Management.AuditEvent[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = 20;

const showDetail = ref(false);
const detailLoading = ref(false);
const detail = ref<Api.Management.AuditEventDetail | null>(null);

async function openDetail(row: Api.Management.AuditEvent) {
  showDetail.value = true;
  detailLoading.value = true;
  detail.value = null;
  try {
    const { data } = await fetchAuditEventDetail(row.id);
    if (data) {
      detail.value = data;
    }
  } finally {
    detailLoading.value = false;
  }
}

function formatJSON(value: unknown) {
  if (value == null) return '-';
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

const columns: DataTableColumns<Api.Management.AuditEvent> = [
  { title: '时间', key: 'created_at', width: 180, render: row => formatTime(row.created_at) },
  { title: '操作人', key: 'actor_name', width: 200 },
  { title: '身份', key: 'actor_type', width: 100, render: row => actorTypeText(row.actor_type) },
  { title: '动作', key: 'action', width: 160, render: row => auditActionText(row.action) },
  { title: '资源类型', key: 'resource_type', width: 140, render: row => auditResourceText(row.resource_type) },
  {
    title: '结果',
    key: 'result',
    width: 90,
    render(row) {
      return h(NTag, { type: auditResultTagType(row.result), size: 'small', round: true }, { default: () => auditResultText(row.result) });
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 80,
    render(row) {
      return h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => openDetail(row) }, { default: () => '详情' });
    }
  }
];

async function loadData() {
  loading.value = true;
  try {
    const { data, error } = await fetchAuditEvents(page.value, pageSize);
    if (!error && data) {
      records.value = data.records;
      total.value = data.total;
    }
  } finally {
    loading.value = false;
  }
}

function handlePageChange(p: number) {
  page.value = p;
  void loadData();
}

onMounted(() => {
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard title="审计日志" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
      </template>
      <NDataTable :columns="columns" :data="records" :loading="loading" :bordered="false" />
      <div class="mt-12px flex justify-end">
        <NPagination :page="page" :page-size="pageSize" :item-count="total" @update:page="handlePageChange" />
      </div>
    </NCard>

    <NModal v-model:show="showDetail" preset="card" title="审计详情" class="w-640px">
      <NSpin :show="detailLoading">
        <NDescriptions v-if="detail" :column="2" label-placement="left" bordered size="small">
          <NDescriptionsItem label="动作">{{ detail.action }}</NDescriptionsItem>
          <NDescriptionsItem label="结果">{{ auditResultText(detail.result) }}</NDescriptionsItem>
          <NDescriptionsItem label="操作人">{{ detail.actor_name }}</NDescriptionsItem>
          <NDescriptionsItem label="身份">{{ actorTypeText(detail.actor_type) }}</NDescriptionsItem>
          <NDescriptionsItem label="资源类型">{{ auditResourceText(detail.resource_type) }}</NDescriptionsItem>
          <NDescriptionsItem label="IP">{{ detail.ip_address ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem label="时间" :span="2">{{ formatTime(detail.created_at) }}</NDescriptionsItem>
          <NDescriptionsItem label="User Agent" :span="2">{{ detail.user_agent ?? '-' }}</NDescriptionsItem>
        </NDescriptions>
        <template v-if="detail">
          <NDivider title-placement="left">变更前</NDivider>
          <NInput :value="formatJSON(detail.before_state)" readonly type="textarea" :autosize="{ minRows: 2, maxRows: 8 }" />
          <NDivider title-placement="left">变更后</NDivider>
          <NInput :value="formatJSON(detail.after_state)" readonly type="textarea" :autosize="{ minRows: 2, maxRows: 8 }" />
        </template>
      </NSpin>
    </NModal>
  </div>
</template>

<style scoped></style>

