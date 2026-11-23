<script setup lang="ts">
import { h, onMounted, ref, computed } from 'vue';
import { NButton, NPagination, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import {
  actorTypeText,
  auditActionText as auditActionLabel,
  auditActionText,
  auditResourceText,
  auditResultTagType,
  auditResultText,
  formatTime
} from '@/utils/admin-status';
import { fetchAuditEventDetail } from '@/service/api';
import { fetchAuditEvents } from '@/service/api';
import { $t } from '@/locales';

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

const columns = computed<DataTableColumns<Api.Management.AuditEvent>>(() => [
  {
    title: $t('business.time'),
    key: 'created_at',
    width: 180,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.operator'),
    key: 'actor_name',
    width: 200,
    render: row => row.actor_name || actorTypeText(row.actor_type)
  },
  {
    title: $t('business.identity'),
    key: 'actor_type',
    width: 100,
    render: row => actorTypeText(row.actor_type)
  },
  {
    title: $t('business.action'),
    key: 'action',
    width: 160,
    render: row => auditActionText(row.action)
  },
  {
    title: $t('business.resource-type'),
    key: 'resource_type',
    width: 140,
    render: row => auditResourceText(row.resource_type)
  },
  {
    title: $t('business.result'),
    key: 'result',
    width: 90,
    render(row) {
      return h(
        NTag,
        { type: auditResultTagType(row.result), size: 'small', round: true },
        { default: () => auditResultText(row.result) }
      );
    }
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 80,
    render(row) {
      return h(
        NButton,
        {
          size: 'small',
          quaternary: true,
          type: 'primary',
          onClick: () => openDetail(row)
        },
        { default: () => $t('business.details') }
      );
    }
  }
]);

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
    <NCard :title="$t('business.audit-log')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
      </template>
      <NDataTable :columns="columns" :data="records" :loading="loading" :bordered="false" />
      <div class="mt-12px flex justify-end">
        <NPagination :page="page" :page-size="pageSize" :item-count="total" @update:page="handlePageChange" />
      </div>
    </NCard>

    <NModal v-model:show="showDetail" preset="card" :title="$t('business.audit-details')" class="w-640px">
      <NSpin :show="detailLoading">
        <NDescriptions v-if="detail" :column="2" label-placement="left" bordered size="small">
          <NDescriptionsItem :label="$t('business.action')">{{ detail.action }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.result')">{{ auditResultText(detail.result) }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.operator')">
            {{ detail.actor_name || actorTypeText(detail.actor_type) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.identity')">{{ actorTypeText(detail.actor_type) }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.resource-type')">
            {{ auditResourceText(detail.resource_type) }}
          </NDescriptionsItem>
          <NDescriptionsItem label="IP">{{ detail.ip_address ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.time')" :span="2">
            {{ formatTime(detail.created_at) }}
          </NDescriptionsItem>
          <NDescriptionsItem label="User Agent" :span="2">{{ detail.user_agent ?? '-' }}</NDescriptionsItem>
        </NDescriptions>
        <template v-if="detail">
          <NDivider title-placement="left">{{ $t('business.before-change') }}</NDivider>
          <NInput
            :value="formatJSON(detail.before_state)"
            readonly
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 8 }"
          />
          <NDivider title-placement="left">{{ $t('business.after-change') }}</NDivider>
          <NInput
            :value="formatJSON(detail.after_state)"
            readonly
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 8 }"
          />
        </template>
      </NSpin>
    </NModal>
  </div>
</template>

<style scoped></style>
