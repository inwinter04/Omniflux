<script setup lang="ts">
import { h, onMounted, reactive, ref, computed } from 'vue';
import { useRouter } from 'vue-router';
import { NButton, NPagination, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import {
  actorTypeText,
  auditActionText,
  auditActions,
  auditResourceText,
  auditResourceTypes,
  auditResultTagType,
  auditResultText,
  auditSnapshotRows,
  formatTime
} from '@/utils/admin-status';
import type { AuditSnapshotRow } from '@/utils/admin-status';
import { fetchAssetDetail, fetchAuditEventDetail, fetchModelCandidate, fetchAuditEvents } from '@/service/api';
import { $t } from '@/locales';

defineOptions({
  name: 'ObservabilityAudit'
});

const router = useRouter();
const loading = ref(true);
const records = ref<Api.Management.AuditEvent[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = 20;
const filterRange = ref<[number, number] | null>(null);
// 时间快捷范围:全部时间不带时间条件;今天、近 N 天、自定义下发起止时间。
type AuditQuickRange = 'all' | 'today' | 'last7' | 'last30' | 'custom';

const quickRange = ref<AuditQuickRange>('all');
const quickRangePresets = computed<{ label: string; value: AuditQuickRange }[]>(() => [
  { label: $t('business.allTime'), value: 'all' },
  { label: $t('business.today'), value: 'today' },
  { label: $t('business.lastNDays', { days: 7 }), value: 'last7' },
  { label: $t('business.lastNDays', { days: 30 }), value: 'last30' },
  { label: $t('business.custom'), value: 'custom' }
]);
const filters = reactive<{
  actor_type: string | null;
  action: string | null;
  resource_type: string | null;
  resource_id: string;
  result: string | null;
}>({
  actor_type: null,
  action: null,
  resource_type: null,
  resource_id: '',
  result: null
});
const actorTypeOptions = computed(() => [
  { label: actorTypeText('user'), value: 'user' },
  { label: actorTypeText('api_key'), value: 'api_key' },
  { label: actorTypeText('system'), value: 'system' }
]);
const resultOptions = computed(() => [
  { label: auditResultText('success'), value: 'success' },
  { label: auditResultText('failure'), value: 'failure' }
]);
// 动作 / 资源类型用下拉而不是手输动作码:列表里展示的是文案,用户不需要知道 model_service_grant 这类内部标识。
const actionOptions = computed(() => auditActions.map(action => ({ label: auditActionText(action), value: action })));
const resourceTypeOptions = computed(() =>
  auditResourceTypes.map(resourceType => ({ label: auditResourceText(resourceType), value: resourceType }))
);

const showDetail = ref(false);
const detailLoading = ref(false);
const detail = ref<Api.Management.AuditEventDetail | null>(null);
const detailResourceName = ref('');
const detailResourceExists = ref(true);
// 详情里的“变更内容”:把快照按字段列成变更前 / 变更后,有变化的值高亮。
const changeRows = computed(() => (detail.value ? auditSnapshotRows(detail.value) : []));
const changeColumns = computed<DataTableColumns<AuditSnapshotRow>>(() => [
  { title: $t('business.field'), key: 'label', width: 160 },
  { title: $t('business.before-change'), key: 'before', render: row => row.before || '-' },
  {
    title: $t('business.after-change'),
    key: 'after',
    render: row => (row.changed ? h('span', { class: 'text-primary font-medium' }, row.after || '-') : row.after || '-')
  }
]);

async function openDetail(row: Api.Management.AuditEvent) {
  showDetail.value = true;
  detailLoading.value = true;
  detail.value = null;
  // 详情接口只返回类型与 ID,名称与是否仍存在直接取列表行上解析好的结果。
  detailResourceName.value = row.resource_name;
  detailResourceExists.value = row.resource_exists;
  try {
    const { data } = await fetchAuditEventDetail(row.id);
    if (data) {
      detail.value = data;
    }
  } finally {
    detailLoading.value = false;
  }
}

function resourceRoute(row: Api.Management.AuditEvent | null) {
  if (!row) return null;
  const routeMap: Record<string, string> = {
    model_service_grant: '/model-management/model-services',
    model_candidate: '/model-management/model-services',
    route_policy: '/model-management/route-policies',
    budget: '/observability/budgets',
    project: '/organization/projects',
    application: '/organization/applications',
    api_key: '/access/api-keys',
    user: '/access/users',
    organization: '/organization/organizations',
    department: '/organization/departments',
    asset: '/asset-registry/prompt-templates',
    asset_grant: '/asset-registry/prompt-templates'
  };
  if (row.resource_type === 'model_service') {
    return row.resource_id
      ? `/model-management/model-services?service=${row.resource_id}`
      : '/model-management/model-services';
  }
  if (row.resource_type === 'provider_account') {
    return row.resource_id
      ? `/model-management/providers?provider=${row.resource_id}`
      : '/model-management/providers';
  }
  if (row.resource_type === 'budget') {
    return row.resource_id
      ? `/observability/budgets?budget=${row.resource_id}`
      : '/observability/budgets';
  }
  if (row.resource_type === 'api_key') {
    return row.resource_id
      ? `/access/api-keys?key=${row.resource_id}`
      : '/access/api-keys';
  }
  if (row.resource_type === 'route_policy') {
    return row.resource_id
      ? `/model-management/route-policies?policy=${row.resource_id}`
      : '/model-management/route-policies';
  }
  if (row.resource_type === 'user') {
    return row.resource_id
      ? `/access/users?user=${row.resource_id}`
      : '/access/users';
  }
  if (row.resource_type === 'organization') {
    return row.resource_id
      ? `/organization/organizations?organization=${row.resource_id}`
      : '/organization/organizations';
  }
  if (row.resource_type === 'department') {
    return row.resource_id
      ? `/organization/departments?department=${row.resource_id}`
      : '/organization/departments';
  }
  if (row.resource_type === 'project') {
    return row.resource_id
      ? `/organization/projects?project=${row.resource_id}`
      : '/organization/projects';
  }
  if (row.resource_type === 'application') {
    return row.resource_id
      ? `/organization/applications?application=${row.resource_id}`
      : '/organization/applications';
  }
  return routeMap[row.resource_type] ?? null;
}

/** 授权被撤销后记录已删除,所属资源只能从审计快照(变更后 → 变更前)里取。 */
function snapshotId(row: Api.Management.AuditEventDetail, key: string) {
  for (const state of [row.after_state, row.before_state]) {
    const value = (state as Record<string, unknown> | null)?.[key];
    if (typeof value === 'string' && value) return value;
  }
  return '';
}

/** 资产可能是 Prompt 模板或智能体,要先查类型才知道去哪个页面;查询失败返回 false 由调用方退回列表。 */
async function openAssetPage(assetID: string, grants: boolean) {
  const { data, error } = await fetchAssetDetail(assetID);
  if (error || !data) return false;
  const base = data.asset_type === 'agent' ? '/asset-registry/agents' : '/asset-registry/prompt-templates';
  await router.push(grants ? `${base}?asset=${assetID}&grants=1` : `${base}?asset=${assetID}`);
  return true;
}

/** 授权类事件要知道所属资源,必须有审计快照;处理成功返回 true。 */
async function openGrantResource(row: Api.Management.AuditEventDetail) {
  if (row.resource_type === 'model_service_grant') {
    const serviceID = snapshotId(row, 'model_service_id');
    if (serviceID) {
      await router.push(`/model-management/model-services?service=${serviceID}&grants=1`);
      return true;
    }
  }
  if (row.resource_type === 'asset_grant') {
    const assetID = snapshotId(row, 'asset_id');
    if (assetID && (await openAssetPage(assetID, true))) return true;
  }
  return false;
}

async function openDetailResource(row: Api.Management.AuditEventDetail | null) {
  if (!row) return;
  if (!(await openGrantResource(row))) await openResource(row);
}

/** 列表里的资源名称可直接跳转;授权类事件的所属资源记在审计快照里,列表行不带快照,先取详情再跳转。 */
async function openRowResource(row: Api.Management.AuditEvent) {
  if (row.resource_type === 'model_service_grant' || row.resource_type === 'asset_grant') {
    const { data } = await fetchAuditEventDetail(row.id);
    if (data) await openDetailResource(data);
    return;
  }
  await openResource(row);
}

async function openResource(row: Api.Management.AuditEvent) {
  if (row.resource_type === 'asset' && row.resource_id && (await openAssetPage(row.resource_id, false))) return;
  if (row.resource_type === 'model_candidate' && row.resource_id) {
    const { data, error } = await fetchModelCandidate(row.resource_id);
    if (!error && data) {
      await router.push(`/model-management/model-services?service=${data.model_service_id}`);
      return;
    }
  }
  const path = resourceRoute(row);
  if (path) void router.push(path);
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
    // 英文时间形如 “11/24/2026, 4:03:50 AM”,列窄了会折行。
    width: 200,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.operator'),
    key: 'actor_name',
    width: 170,
    ellipsis: { tooltip: true },
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
    width: 190,
    render: row => auditActionText(row.action)
  },
  {
    title: $t('business.resource-type'),
    key: 'resource_type',
    width: 140,
    render: row => auditResourceText(row.resource_type)
  },
  {
    title: $t('business.resource'),
    key: 'resource_name',
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: row => {
      if (!row.resource_name) return '-';
      // 能直达的资源,名称本身就是入口;其余只展示名称。
      if (!row.resource_id || !resourceRoute(row)) return row.resource_name;
      // 资源已删除时没有可打开的页面,只显示名称并标注,避免点进去什么都没有。
      if (!row.resource_exists) {
        // 标注放在名称前面,名称过长时交给列自带的省略与悬停提示处理。
        return h('span', null, [
          h(
            NTag,
            { size: 'small', type: 'default', bordered: false, class: 'mr-4px align-middle' },
            { default: () => $t('business.resource-deleted') }
          ),
          h('span', { class: 'align-middle' }, row.resource_name)
        ]);
      }
      return h(
        'span',
        { class: 'cursor-pointer text-primary hover:underline', onClick: () => openRowResource(row) },
        row.resource_name
      );
    }
  },
  {
    title: $t('business.result'),
    key: 'result',
    width: 100,
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
    width: 95,
    // 窗口较窄时表格横向滚动,“详情”固定在右侧不被挤出可视区域。
    fixed: 'right',
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
    const params: Api.Management.AuditEventFilters = {};
    if (filters.actor_type) params.actor_type = filters.actor_type;
    if (filters.action) params.action = filters.action;
    if (filters.resource_type) params.resource_type = filters.resource_type;
    if (filters.resource_id.trim()) params.resource_id = filters.resource_id.trim();
    if (filters.result) params.result = filters.result;
    if (filterRange.value) {
      params.start_time = new Date(filterRange.value[0]).toISOString();
      params.end_time = new Date(filterRange.value[1]).toISOString();
    }
    const { data, error } = await fetchAuditEvents(page.value, pageSize, params);
    if (!error && data) {
      records.value = data.records;
      total.value = data.total;
    }
  } finally {
    loading.value = false;
  }
}

function applyFilters() {
  page.value = 1;
  void loadData();
}

// 选中快捷时间范围后立即刷新
function applyQuickRange(kind: AuditQuickRange) {
  const now = Date.now();
  const day = 24 * 60 * 60 * 1000;

  if (kind === 'all') {
    filterRange.value = null;
  } else if (kind === 'today') {
    const today = new Date();
    filterRange.value = [new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime(), now];
  } else if (kind === 'last7' || kind === 'last30') {
    filterRange.value = [now - (kind === 'last7' ? 7 : 30) * day, now];
  } else {
    // 切到自定义时沿用当前的时间范围,没有则带入近 7 天,方便在此基础上微调
    filterRange.value ??= [now - 7 * day, now];
  }

  applyFilters();
}

// 自定义范围被清空时,等同于全部时间
function onCustomRangeChange() {
  if (!filterRange.value) quickRange.value = 'all';
  applyFilters();
}

function resetFilters() {
  filters.actor_type = null;
  filters.action = null;
  filters.resource_type = null;
  filters.resource_id = '';
  filters.result = null;
  quickRange.value = 'all';
  filterRange.value = null;
  applyFilters();
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
      <NForm :model="filters" label-placement="top" :show-feedback="false" class="mb-12px">
        <NGrid cols="1 s:2 m:3 l:5" responsive="screen" item-responsive :x-gap="12" :y-gap="12">
          <NFormItemGi span="1 s:2 m:3 l:5" :label="$t('business.time')">
            <div class="w-full flex flex-wrap items-center justify-between gap-12px">
              <div class="flex flex-wrap items-center gap-12px">
                <NRadioGroup v-model:value="quickRange" @update:value="applyQuickRange">
                  <NRadioButton v-for="preset in quickRangePresets" :key="preset.value" :value="preset.value">
                    {{ preset.label }}
                  </NRadioButton>
                </NRadioGroup>
                <NDatePicker
                  v-if="quickRange === 'custom'"
                  v-model:value="filterRange"
                  type="datetimerange"
                  clearable
                  :start-placeholder="$t('business.start-time')"
                  :end-placeholder="$t('business.end-time')"
                  @update:value="onCustomRangeChange"
                />
              </div>
              <NSpace>
                <NButton type="primary" @click="applyFilters">{{ $t('common.search') }}</NButton>
                <NButton @click="resetFilters">{{ $t('common.reset') }}</NButton>
              </NSpace>
            </div>
          </NFormItemGi>
          <NFormItemGi :label="$t('business.identity')">
            <NSelect
              v-model:value="filters.actor_type"
              :options="actorTypeOptions"
              clearable
              :placeholder="$t('business.all')"
            />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.action')">
            <NSelect
              v-model:value="filters.action"
              :options="actionOptions"
              filterable
              clearable
              :placeholder="$t('business.all')"
            />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.resource-type')">
            <NSelect
              v-model:value="filters.resource_type"
              :options="resourceTypeOptions"
              filterable
              clearable
              :placeholder="$t('business.all')"
            />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.resourceId')">
            <NInput v-model:value="filters.resource_id" clearable />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.result')">
            <NSelect
              v-model:value="filters.result"
              :options="resultOptions"
              clearable
              :placeholder="$t('business.all')"
            />
          </NFormItemGi>
        </NGrid>
      </NForm>
      <NDataTable :columns="columns" :data="records" :loading="loading" :bordered="false" :scroll-x="1150" />
      <div class="mt-12px flex justify-end">
        <NPagination :page="page" :page-size="pageSize" :item-count="total" @update:page="handlePageChange" />
      </div>
    </NCard>

    <NModal v-model:show="showDetail" preset="card" :title="$t('business.audit-details')" class="w-640px">
      <NSpin :show="detailLoading">
        <NDescriptions v-if="detail" :column="2" label-placement="left" bordered size="small">
          <NDescriptionsItem :label="$t('business.action')">{{ auditActionText(detail.action) }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.result')">{{ auditResultText(detail.result) }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.operator')">
            {{ detail.actor_name || actorTypeText(detail.actor_type) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.identity')">{{ actorTypeText(detail.actor_type) }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.resource-type')">
            {{ auditResourceText(detail.resource_type) }}
          </NDescriptionsItem>
          <NDescriptionsItem label="IP">{{ detail.ip_address ?? '-' }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.resource')" :span="2">
            {{ detailResourceName || '-' }}
            <NTag
              v-if="!detailResourceExists && detailResourceName"
              size="small"
              type="default"
              :bordered="false"
              class="ml-6px"
            >
              {{ $t('business.resource-deleted') }}
            </NTag>
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.time')" :span="2">
            {{ formatTime(detail.created_at) }}
          </NDescriptionsItem>
          <NDescriptionsItem label="User Agent" :span="2">{{ detail.user_agent ?? '-' }}</NDescriptionsItem>
        </NDescriptions>
        <template v-if="detail">
          <NDivider title-placement="left">{{ $t('business.changes') }}</NDivider>
          <NEmpty v-if="!changeRows.length" :description="$t('business.no-change-recorded')" class="py-8px" />
          <NDataTable v-else :columns="changeColumns" :data="changeRows" :bordered="false" size="small" />
          <NCollapse class="mt-12px">
            <NCollapseItem :title="$t('business.raw-snapshot')" name="raw">
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
            </NCollapseItem>
          </NCollapse>
        </template>
      </NSpin>
      <template #footer>
        <NSpace justify="end">
          <NButton
            v-if="detail && detailResourceExists && resourceRoute(detail)"
            type="primary"
            @click="openDetailResource(detail)"
          >
            {{ $t('business.viewResource') }}
          </NButton>
          <NButton @click="showDetail = false">{{ $t('business.close') }}</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>
