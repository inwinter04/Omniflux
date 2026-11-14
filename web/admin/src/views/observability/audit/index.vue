<script setup lang="ts">
import { h, onMounted, ref } from 'vue';
import { NButton, NPagination, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { fetchAuditEvents } from '@/service/api';
import {
  actorTypeText,
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
  </div>
</template>

<style scoped></style>
