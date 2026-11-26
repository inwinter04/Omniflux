<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import {
  NAlert,
  NBadge,
  NButton,
  NCard,
  NDataTable,
  NDrawer,
  NDrawerContent,
  NEmpty,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NPopconfirm,
  NRadioButton,
  NRadioGroup,
  NSelect,
  NSpace,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  NText
} from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import AvailabilityBar from '@/components/common/availability-bar.vue';
import {
  failoverReasonText,
  failoverTriggerText,
  formatTime,
  healthStatusText,
  healthTagType,
  probeReasonText
} from '@/utils/admin-status';
import {
  fetchCreatePlatformProxy,
  fetchDeletePlatformProxy,
  fetchPlatformProxies,
  fetchProbeProviderProxies,
  fetchProxyFailoverEvents,
  fetchSwitchProviderProxy,
  fetchTestPlatformProxy,
  fetchUpdatePlatformProxy,
  fetchUpdateProviderProxyOrder,
  fetchUpdateProxyGovernanceSettings
} from '@/service/api';
import { $t } from '@/locales';

defineOptions({ name: 'SystemProxies' });

const loading = ref(true);
const submitting = ref(false);
const activeTab = ref<'pools' | 'nodes'>('pools');
const list = ref<Api.Management.PlatformProxy[]>([]);
const pools = ref<Api.Management.ProviderProxyPool[]>([]);
const providerOptions = ref<SelectOption[]>([]);
const searchKeyword = ref('');

const showCreate = ref(false);
const showEdit = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);

const showSettingsModal = ref(false);
const savingSettings = ref(false);
const settingsForm = reactive<Api.Management.ProxyGovernanceSettings>({
  enabled: true,
  probe_interval_seconds: 60,
  fast_probe_interval_seconds: 10,
  probe_timeout_seconds: 8,
  fail_threshold: 3,
  recover_threshold: 3,
  switch_cooldown_seconds: 300,
  failback_mode: 'sticky',
  failback_soak_seconds: 1800
});

const showEventsDrawer = ref(false);
const eventsLoading = ref(false);
const eventProviderFilter = ref<string | null>(null);
const failoverEvents = ref<Api.Management.ProxyFailoverEvent[]>([]);

const showProbeModal = ref(false);
const probeResults = ref<Api.Management.ProxyTargetProbeResult[]>([]);
const probeTitle = ref('');
const probingProxyId = ref('');
const probingPoolKey = ref('');

const formModel = reactive({
  provider_keys: [] as string[],
  bindings: [] as Api.Management.PlatformProxyBindingInput[],
  name: '',
  proxy_type: 'http' as Api.Management.PlatformProxyType,
  proxy_url: '',
  enabled: true,
  probe_url: ''
});

const editModel = reactive({
  id: '',
  provider_keys: [] as string[],
  bindings: [] as Api.Management.PlatformProxyBindingInput[],
  name: '',
  proxy_type: 'http' as Api.Management.PlatformProxyType,
  original_proxy_type: 'http' as Api.Management.PlatformProxyType,
  proxy_url: '',
  enabled: true,
  probe_url: ''
});

function syncBindingsWithProviderKeys(
  target: { provider_keys: string[]; bindings: Api.Management.PlatformProxyBindingInput[] },
  keys: string[]
) {
  target.provider_keys = keys;
  const existing = new Map(target.bindings.map(b => [b.provider_key, b]));
  target.bindings = keys.map(key => {
    const prev = existing.get(key);
    return prev ?? { provider_key: key, priority: 100, enabled: true };
  });
}

function providerOptionLabel(key: string) {
  const matched = providerOptions.value.find(o => o.value === key);
  return matched ? String(matched.label) : key;
}

const filteredList = computed(() => {
  const keyword = searchKeyword.value.trim().toLowerCase();
  if (!keyword) return list.value;
  return list.value.filter(
    item =>
      item.name.toLowerCase().includes(keyword) ||
      item.provider_keys.some(provider => provider.toLowerCase().includes(keyword)) ||
      item.provider_labels.some(label => label.toLowerCase().includes(keyword))
  );
});

const filteredPools = computed(() => {
  const keyword = searchKeyword.value.trim().toLowerCase();
  if (!keyword) return pools.value;
  return pools.value.filter(
    pool =>
      pool.provider_key.toLowerCase().includes(keyword) ||
      pool.provider_label.toLowerCase().includes(keyword) ||
      pool.members.some(m => m.proxy_name.toLowerCase().includes(keyword))
  );
});

const formRules = computed<FormRules>(() => ({
  provider_keys: {
    validator: (_rule, value: string[]) => (value.length > 0 ? true : new Error($t('business.select-proxy-provider'))),
    trigger: 'change'
  },
  name: { required: true, message: $t('business.enter-proxy-name'), trigger: 'blur' },
  proxy_url: {
    required: true,
    validator: (_rule, value: string) =>
      validProxyURL(formModel.proxy_type, value.trim()) ? true : new Error($t('business.proxy-url-invalid')),
    trigger: 'blur'
  }
}));

const editRules = computed<FormRules>(() => ({
  provider_keys: {
    validator: (_rule, value: string[]) => (value.length > 0 ? true : new Error($t('business.select-proxy-provider'))),
    trigger: 'change'
  },
  name: { required: true, message: $t('business.enter-proxy-name'), trigger: 'blur' },
  proxy_url: {
    validator: (_rule, value: string) => {
      if (!value.trim() && editModel.proxy_type === editModel.original_proxy_type) return true;
      if (!value.trim()) return new Error($t('business.proxy-url-required'));
      return validProxyURL(editModel.proxy_type, value.trim()) ? true : new Error($t('business.proxy-url-invalid'));
    },
    trigger: 'blur'
  }
}));

function proxyTypeText(value: Api.Management.PlatformProxyType) {
  return value === 'http' ? $t('business.proxy-http') : $t('business.proxy-socks5');
}

function validProxyURL(proxyType: Api.Management.PlatformProxyType, value: string) {
  try {
    const parsed = new URL(value);
    const allowedProtocols = proxyType === 'http' ? ['http:', 'https:'] : ['socks5:', 'socks5h:'];
    const portMatch = value.match(/:(\d+)$/);
    const port = portMatch ? Number(portMatch[1]) : Number.NaN;
    return (
      allowedProtocols.includes(parsed.protocol) &&
      parsed.hostname !== '' &&
      Number.isInteger(port) &&
      port > 0 &&
      port <= 65535 &&
      (parsed.pathname === '' || parsed.pathname === '/') &&
      !value.endsWith('/') &&
      parsed.search === '' &&
      parsed.hash === ''
    );
  } catch {
    return false;
  }
}

function formatAvail(val: number | null) {
  if (val === null || val === undefined) return '-';
  return `${Math.round(val * 10) / 10}%`;
}

const columns = computed<DataTableColumns<Api.Management.PlatformProxy>>(() => [
  {
    title: $t('business.proxy-name'),
    key: 'name',
    minWidth: 180,
    render: row =>
      h('div', { class: 'flex items-center gap-6px' }, [
        h('span', { class: 'font-medium' }, row.name),
        !row.enabled
          ? h(NTag, { size: 'tiny', type: 'default', round: true }, { default: () => $t('business.disabled-e5ola') })
          : null
      ])
  },
  {
    title: $t('business.proxy-provider'),
    key: 'bindings',
    minWidth: 240,
    render: row =>
      h(
        'div',
        { class: 'flex flex-wrap gap-4px' },
        (row.bindings?.length
          ? row.bindings
          : row.provider_labels.map((label, i) => ({
              provider_key: row.provider_keys[i],
              provider_label: label,
              priority: 100,
              enabled: true,
              status: 'unknown'
            }))
        ).map(b =>
          h(
            NTag,
            {
              size: 'small',
              round: true,
              type: !b.enabled ? 'default' : healthTagType(b.status)
            },
            { default: () => `${b.provider_label} · P${b.priority}` }
          )
        )
      )
  },
  {
    title: $t('business.status-health'),
    key: 'health_status',
    width: 240,
    render: row =>
      h('div', { class: 'flex flex-col gap-4px' }, [
        h('div', { class: 'flex items-center gap-6px' }, [
          h(
            NTag,
            { size: 'small', round: true, type: healthTagType(row.health_status) },
            { default: () => healthStatusText(row.health_status) }
          ),
          h('span', { class: 'text-12px text-#6b7280' }, formatAvail(row.availability_24h)),
          row.avg_latency_ms_24h !== null
            ? h('span', { class: 'text-12px text-#6b7280' }, $t('business.latencyMs', { value: Math.round(row.avg_latency_ms_24h) }))
            : null
        ]),
        h(AvailabilityBar, { buckets: row.heartbeat_24h, compact: true })
      ])
  },
  {
    title: $t('business.proxy-type'),
    key: 'proxy_type',
    width: 100,
    render: row =>
      h(
        NTag,
        { size: 'small', round: true, type: row.proxy_type === 'http' ? 'info' : 'success' },
        { default: () => proxyTypeText(row.proxy_type) }
      )
  },
  {
    title: $t('business.proxy-url'),
    key: 'proxy_url',
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: row => row.proxy_url || '-'
  },
  { title: $t('business.active-accounts'), key: 'active_accounts', width: 130 },
  {
    title: $t('business.updated-at'),
    key: 'updated_at',
    width: 165,
    render: row => (row.updated_at ? formatTime(row.updated_at) : '-')
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 215,
    fixed: 'right',
    render: row =>
      h(NSpace, { size: 4 }, {
        default: () => [
          h(
            NButton,
            {
              size: 'small',
              quaternary: true,
              type: 'info',
              loading: probingProxyId.value === row.id,
              onClick: () => handleTestProxy(row)
            },
            { default: () => $t('business.test') }
          ),
          h(
            NButton,
            { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) },
            { default: () => $t('business.edit') }
          ),
          h(
            NPopconfirm,
            { onPositiveClick: () => handleDelete(row.id) },
            {
              trigger: () => h(NButton, { size: 'small', quaternary: true, type: 'error' }, { default: () => $t('business.delete') }),
              default: () => $t('business.delete-proxy-confirm')
            }
          )
        ]
      })
  }
]);

const eventColumns = computed<DataTableColumns<Api.Management.ProxyFailoverEvent>>(() => [
  {
    title: $t('business.time'),
    key: 'created_at',
    width: 170,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.provider'),
    key: 'provider_key',
    width: 150,
    render: row => providerOptionLabel(row.provider_key)
  },
  {
    title: `${$t('business.from-proxy')} → ${$t('business.to-proxy')}`,
    key: 'transition',
    minWidth: 210,
    render: row =>
      `${row.from_proxy_name || $t('business.direct-connection')} → ${row.to_proxy_name || $t('business.direct-connection')}`
  },
  {
    title: $t('business.failover-trigger'),
    key: 'trigger',
    width: 130,
    render: row =>
      h(
        NTag,
        {
          size: 'small',
          round: true,
          type: row.trigger === 'auto_failover' ? 'warning' : row.trigger === 'manual' ? 'info' : 'default'
        },
        { default: () => failoverTriggerText(row.trigger) }
      )
  },
  {
    title: $t('business.failover-reason'),
    key: 'reason',
    minWidth: 180,
    render: row => failoverReasonText(row.reason)
  },
  {
    title: $t('business.error-detail'),
    key: 'detail',
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: row => row.detail || '-'
  }
]);

const probeColumns = computed<DataTableColumns<Api.Management.ProxyTargetProbeResult>>(() => [
  {
    title: $t('business.provider'),
    key: 'provider_key',
    width: 150,
    render: row => (row.provider_key ? providerOptionLabel(row.provider_key) : $t('business.custom'))
  },
  { title: $t('business.target-url'), key: 'target_url', minWidth: 200, ellipsis: { tooltip: true } },
  {
    title: $t('business.probe-status'),
    key: 'status',
    width: 100,
    render: row =>
      h(NTag, { size: 'small', round: true, type: healthTagType(row.status) }, { default: () => healthStatusText(row.status) })
  },
  {
    title: $t('business.latency'),
    key: 'latency_ms',
    width: 100,
    render: row => $t('business.latencyMs', { value: row.latency_ms })
  },
  {
    title: $t('business.probe-reason'),
    key: 'reason',
    minWidth: 170,
    render: row => probeReasonText(row.reason)
  },
  {
    title: $t('business.error-detail'),
    key: 'error_detail',
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: row => row.error_detail || '-'
  }
]);

async function loadData() {
  loading.value = true;
  try {
    const { data, error } = await fetchPlatformProxies();
    if (!error && data) {
      list.value = data.proxies;
      pools.value = data.pools ?? [];
      if (data.settings) Object.assign(settingsForm, data.settings);
      providerOptions.value = data.provider_options.map(option => ({ label: option.label, value: option.value }));
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  Object.assign(formModel, {
    provider_keys: [],
    bindings: [],
    name: '',
    proxy_type: 'http',
    proxy_url: '',
    enabled: true,
    probe_url: ''
  });
  showCreate.value = true;
}

function openEdit(row: Api.Management.PlatformProxy) {
  const bindings = row.bindings?.length
    ? row.bindings.map(b => ({ provider_key: b.provider_key, priority: b.priority, enabled: b.enabled }))
    : row.provider_keys.map(k => ({ provider_key: k, priority: 100, enabled: true }));
  Object.assign(editModel, {
    id: row.id,
    provider_keys: [...row.provider_keys],
    bindings,
    name: row.name,
    proxy_type: row.proxy_type,
    original_proxy_type: row.proxy_type,
    proxy_url: '',
    enabled: row.enabled,
    probe_url: row.probe_url ?? ''
  });
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchCreatePlatformProxy({
      provider_keys: formModel.provider_keys,
      bindings: formModel.bindings,
      name: formModel.name.trim(),
      proxy_type: formModel.proxy_type,
      proxy_url: formModel.proxy_url.trim(),
      enabled: formModel.enabled,
      probe_url: formModel.probe_url.trim()
    });
    if (!error) {
      window.$message?.success($t('business.proxy-saved'));
      showCreate.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function handleUpdate() {
  await editFormRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchUpdatePlatformProxy(editModel.id, {
      provider_keys: editModel.provider_keys,
      bindings: editModel.bindings,
      name: editModel.name.trim(),
      proxy_type: editModel.proxy_type,
      proxy_url: editModel.proxy_url.trim() || undefined,
      enabled: editModel.enabled,
      probe_url: editModel.probe_url.trim()
    });
    if (!error) {
      window.$message?.success($t('business.proxy-saved'));
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeletePlatformProxy(id);
  if (!error) {
    window.$message?.success($t('business.proxy-deleted'));
    await loadData();
  }
}

async function handleTestProxy(row: Api.Management.PlatformProxy) {
  probingProxyId.value = row.id;
  try {
    const { data, error } = await fetchTestPlatformProxy(row.id);
    if (!error && data) {
      probeTitle.value = $t('business.probe-results-title', { name: row.name });
      probeResults.value = data.results ?? [];
      showProbeModal.value = true;
      await loadData();
    }
  } finally {
    probingProxyId.value = '';
  }
}

async function handleMoveMember(pool: Api.Management.ProviderProxyPool, index: number, delta: -1 | 1) {
  const nextIndex = index + delta;
  if (nextIndex < 0 || nextIndex >= pool.members.length) return;
  const reordered = [...pool.members];
  const [moved] = reordered.splice(index, 1);
  reordered.splice(nextIndex, 0, moved);
  const items = reordered.map((m, idx) => ({
    proxy_id: m.proxy_id,
    priority: idx + 1,
    enabled: m.binding_enabled
  }));
  const { error } = await fetchUpdateProviderProxyOrder(pool.provider_key, items);
  if (!error) {
    window.$message?.success($t('business.proxy-order-updated'));
    await loadData();
  }
}

async function handleToggleBinding(pool: Api.Management.ProviderProxyPool, member: Api.Management.ProxyPoolMember, enabled: boolean) {
  const items = pool.members.map((m, idx) => ({
    proxy_id: m.proxy_id,
    priority: m.priority > 0 ? m.priority : idx + 1,
    enabled: m.proxy_id === member.proxy_id ? enabled : m.binding_enabled
  }));
  const { error } = await fetchUpdateProviderProxyOrder(pool.provider_key, items);
  if (!error) {
    window.$message?.success($t('business.proxy-order-updated'));
    await loadData();
  }
}

async function handleSwitchProxy(providerKey: string, proxyId: string) {
  const { error } = await fetchSwitchProviderProxy(providerKey, proxyId);
  if (!error) {
    window.$message?.success($t('business.proxy-switched'));
    await loadData();
  }
}

async function handleProbePool(providerKey: string) {
  probingPoolKey.value = providerKey;
  try {
    const { data, error } = await fetchProbeProviderProxies(providerKey);
    if (!error && data) {
      window.$message?.success($t('business.proxy-pool-probed'));
      await loadData();
    }
  } finally {
    probingPoolKey.value = '';
  }
}

async function handleSaveSettings() {
  savingSettings.value = true;
  try {
    const { data, error } = await fetchUpdateProxyGovernanceSettings({ ...settingsForm });
    if (!error && data) {
      Object.assign(settingsForm, data);
      window.$message?.success($t('business.proxy-policy-saved'));
      showSettingsModal.value = false;
    }
  } finally {
    savingSettings.value = false;
  }
}

async function openFailoverEvents(providerKey: string | null = null) {
  eventProviderFilter.value = providerKey;
  showEventsDrawer.value = true;
  await loadFailoverEvents();
}

async function loadFailoverEvents() {
  eventsLoading.value = true;
  try {
    const { data, error } = await fetchProxyFailoverEvents(eventProviderFilter.value ?? undefined, 80);
    if (!error && data) failoverEvents.value = data;
  } finally {
    eventsLoading.value = false;
  }
}

onMounted(loadData);
</script>

<template>
  <div class="flex flex-col gap-16px">
    <NCard :title="$t('business.proxy-governance')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary @click="openFailoverEvents(null)">{{ $t('business.failover-events') }}</NButton>
          <NButton quaternary @click="showSettingsModal = true">{{ $t('business.failover-policy') }}</NButton>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">
            {{ $t('business.refresh') }}
          </NButton>
          <NButton type="primary" @click="openCreate">{{ $t('business.create-proxy') }}</NButton>
        </NSpace>
      </template>

      <NAlert type="info" :show-icon="true" class="mb-16px">
        {{ $t('business.proxy-governance-description') }}
      </NAlert>

      <div class="mb-12px flex items-center justify-between gap-12px">
        <NTag size="small" :type="settingsForm.enabled ? 'success' : 'default'" round>
          {{
            settingsForm.enabled
              ? $t('business.auto-check-active', { seconds: settingsForm.probe_interval_seconds })
              : $t('business.auto-check-paused')
          }}
        </NTag>
        <NInput v-model:value="searchKeyword" clearable :placeholder="$t('business.search-proxy')" class="w-260px" />
      </div>

      <NTabs v-model:value="activeTab" type="line" animated>
        <NTabPane name="pools" :tab="$t('business.provider-proxy-pools')">
          <div v-if="!loading && filteredPools.length === 0" class="py-24px">
            <NEmpty :description="$t('business.no-proxy-pools')" />
          </div>
          <div v-else class="flex flex-col gap-12px">
            <NCard
              v-for="pool in filteredPools"
              :key="pool.provider_key"
              size="small"
              embedded
              :bordered="true"
            >
              <template #header>
                <div class="flex flex-wrap items-center gap-8px">
                  <span class="text-15px font-semibold">{{ pool.provider_label }}</span>
                  <NTag
                    size="small"
                    round
                    :type="pool.healthy_count === pool.total_count && pool.total_count > 0 ? 'success' : pool.healthy_count > 0 ? 'warning' : 'error'"
                  >
                    {{ $t('business.pool-health-count', { healthy: pool.healthy_count, total: pool.total_count }) }}
                  </NTag>
                  <NTag v-if="pool.active_proxy_name" size="small" type="info" round>
                    {{ $t('business.active-in-use') }}: {{ pool.active_proxy_name }}
                  </NTag>
                  <NText v-if="pool.active_since" depth="3" class="text-12px">
                    {{ $t('business.active-since', { time: formatTime(pool.active_since) }) }}
                  </NText>
                </div>
              </template>
              <template #header-extra>
                <NSpace size="small">
                  <NButton
                    size="small"
                    quaternary
                    type="info"
                    :loading="probingPoolKey === pool.provider_key"
                    @click="handleProbePool(pool.provider_key)"
                  >
                    {{ $t('business.probe-now') }}
                  </NButton>
                  <NButton size="small" quaternary @click="openFailoverEvents(pool.provider_key)">
                    {{ $t('business.failover-events') }}
                  </NButton>
                </NSpace>
              </template>

              <div class="flex flex-col gap-8px">
                <div
                  v-for="(member, idx) in pool.members"
                  :key="member.proxy_id"
                  class="flex flex-wrap items-center justify-between gap-12px rounded-6px bg-#ffffff px-12px py-10px dark:bg-#18181c"
                >
                  <div class="flex min-w-260px flex-1 items-center gap-10px">
                    <NBadge :value="`P${idx + 1}`" :type="member.is_active ? 'success' : 'info'" />
                    <div class="flex flex-col gap-2px">
                      <div class="flex items-center gap-6px">
                        <span class="font-medium">{{ member.proxy_name }}</span>
                        <NTag v-if="member.is_active" size="tiny" type="success" round>
                          {{ $t('business.active-in-use') }}
                        </NTag>
                        <NTag v-else size="tiny" round>{{ $t('business.standby') }}</NTag>
                        <NTag size="tiny" round :type="member.proxy_type === 'http' ? 'info' : 'success'">
                          {{ proxyTypeText(member.proxy_type) }}
                        </NTag>
                        <NTag v-if="!member.proxy_enabled" size="tiny" type="default" round>
                          {{ $t('business.disabled-e5ola') }}
                        </NTag>
                      </div>
                      <div class="flex flex-wrap items-center gap-8px text-12px text-#6b7280">
                        <span>{{ member.proxy_url }}</span>
                        <span v-if="member.last_reason && member.last_reason !== 'ok'">
                          · {{ probeReasonText(member.last_reason) }}
                        </span>
                        <span v-if="member.cooldown_until" class="text-#d03050">
                          · {{ $t('business.cooldown-until', { time: formatTime(member.cooldown_until) }) }}
                        </span>
                      </div>
                    </div>
                  </div>

                  <div class="flex flex-wrap items-center gap-12px">
                    <div class="flex items-center gap-8px">
                      <NTag size="small" round :type="healthTagType(member.status)">
                        {{ healthStatusText(member.status) }}
                      </NTag>
                      <AvailabilityBar :buckets="member.heartbeat_24h" compact />
                      <span class="w-48px text-right text-12px text-#6b7280">
                        {{ formatAvail(member.availability_24h) }}
                      </span>
                      <span class="w-60px text-right text-12px text-#6b7280">
                        {{ member.last_latency_ms !== null ? $t('business.latencyMs', { value: member.last_latency_ms }) : '-' }}
                      </span>
                    </div>

                    <NSwitch
                      size="small"
                      :value="member.binding_enabled"
                      :disabled="!member.proxy_enabled"
                      @update:value="val => handleToggleBinding(pool, member, val)"
                    />

                    <NSpace size="small">
                      <NButton
                        size="tiny"
                        quaternary
                        :disabled="idx === 0"
                        @click="handleMoveMember(pool, idx, -1)"
                      >
                        {{ $t('business.move-up') }}
                      </NButton>
                      <NButton
                        size="tiny"
                        quaternary
                        :disabled="idx === pool.members.length - 1"
                        @click="handleMoveMember(pool, idx, 1)"
                      >
                        {{ $t('business.move-down') }}
                      </NButton>
                      <NPopconfirm
                        v-if="!member.is_active && member.proxy_enabled && member.binding_enabled"
                        @positive-click="handleSwitchProxy(pool.provider_key, member.proxy_id)"
                      >
                        <template #trigger>
                          <NButton size="tiny" type="primary" secondary>
                            {{ $t('business.switch-to-proxy') }}
                          </NButton>
                        </template>
                        {{ $t('business.switch-proxy-confirm') }}
                      </NPopconfirm>
                    </NSpace>
                  </div>
                </div>
              </div>
            </NCard>
          </div>
        </NTabPane>

        <NTabPane name="nodes" :tab="$t('business.proxy-nodes')">
          <NDataTable
            :columns="columns"
            :data="filteredList"
            :loading="loading"
            :row-key="row => row.id"
            size="small"
            :bordered="false"
            :scroll-x="1360"
          />
        </NTabPane>
      </NTabs>
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-proxy')" class="w-680px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="top">
        <NFormItem :label="$t('business.proxy-provider')" path="provider_keys">
          <NSelect
            :value="formModel.provider_keys"
            multiple
            filterable
            :options="providerOptions"
            :placeholder="$t('business.select-proxy-provider')"
            @update:value="keys => syncBindingsWithProviderKeys(formModel, keys)"
          />
        </NFormItem>
        <NFormItem v-if="formModel.bindings.length > 0" :label="$t('business.proxy-bindings')">
          <div class="w-full">
            <div class="flex flex-col gap-6px">
              <div
                v-for="b in formModel.bindings"
                :key="b.provider_key"
                class="flex items-center justify-between gap-8px rounded-4px bg-#f6f7f9 px-10px py-6px dark:bg-#1f1f24"
              >
                <span class="text-13px font-medium">{{ providerOptionLabel(b.provider_key) }}</span>
                <NSpace align="center" size="small">
                  <NText depth="3" class="text-12px">{{ $t('business.priority') }}</NText>
                  <NInputNumber v-model:value="b.priority" :min="1" :max="9999" size="small" class="w-90px" />
                  <NSwitch v-model:value="b.enabled" size="small" />
                </NSpace>
              </div>
            </div>
            <NText depth="3" class="mt-4px block text-12px">{{ $t('business.proxy-priority-hint') }}</NText>
          </div>
        </NFormItem>
        <NFormItem :label="$t('business.proxy-name')" path="name">
          <NInput v-model:value="formModel.name" :placeholder="$t('business.enter-proxy-name')" />
        </NFormItem>
        <NFormItem :label="$t('business.proxy-url')" path="proxy_url">
          <NInput
            v-model:value="formModel.proxy_url"
            type="password"
            show-password-on="click"
            :placeholder="$t('business.enter-proxy-url')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.proxy-type')" path="proxy_type">
          <NRadioGroup v-model:value="formModel.proxy_type">
            <NRadioButton value="http">{{ $t('business.proxy-http') }}</NRadioButton>
            <NRadioButton value="socks5">{{ $t('business.proxy-socks5') }}</NRadioButton>
          </NRadioGroup>
        </NFormItem>
        <NFormItem :label="$t('business.proxy-probe-url')">
          <div class="w-full">
            <NInput v-model:value="formModel.probe_url" :placeholder="$t('business.proxy-probe-url-placeholder')" />
            <NText depth="3" class="mt-4px block text-12px">{{ $t('business.proxy-probe-url-hint') }}</NText>
          </div>
        </NFormItem>
        <NFormItem :label="$t('business.proxy-enabled')">
          <NSwitch v-model:value="formModel.enabled" />
        </NFormItem>
        <NText depth="3" class="mb-12px block text-12px">{{ $t('business.proxy-encrypted-and-used-by') }}</NText>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">{{ $t('business.create-proxy') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" :title="$t('business.edit-proxy')" class="w-680px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="top">
        <NFormItem :label="$t('business.proxy-provider')" path="provider_keys">
          <NSelect
            :value="editModel.provider_keys"
            multiple
            filterable
            :options="providerOptions"
            :placeholder="$t('business.select-proxy-provider')"
            @update:value="keys => syncBindingsWithProviderKeys(editModel, keys)"
          />
        </NFormItem>
        <NFormItem v-if="editModel.bindings.length > 0" :label="$t('business.proxy-bindings')">
          <div class="w-full">
            <div class="flex flex-col gap-6px">
              <div
                v-for="b in editModel.bindings"
                :key="b.provider_key"
                class="flex items-center justify-between gap-8px rounded-4px bg-#f6f7f9 px-10px py-6px dark:bg-#1f1f24"
              >
                <span class="text-13px font-medium">{{ providerOptionLabel(b.provider_key) }}</span>
                <NSpace align="center" size="small">
                  <NText depth="3" class="text-12px">{{ $t('business.priority') }}</NText>
                  <NInputNumber v-model:value="b.priority" :min="1" :max="9999" size="small" class="w-90px" />
                  <NSwitch v-model:value="b.enabled" size="small" />
                </NSpace>
              </div>
            </div>
            <NText depth="3" class="mt-4px block text-12px">{{ $t('business.proxy-priority-hint') }}</NText>
          </div>
        </NFormItem>
        <NFormItem :label="$t('business.proxy-name')" path="name">
          <NInput v-model:value="editModel.name" />
        </NFormItem>
        <NFormItem :label="$t('business.proxy-url')" path="proxy_url">
          <NInput
            v-model:value="editModel.proxy_url"
            type="password"
            show-password-on="click"
            :placeholder="editModel.proxy_type === editModel.original_proxy_type ? $t('business.leave-blank-to-keep-proxy-url') : $t('business.enter-proxy-url')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.proxy-type')" path="proxy_type">
          <NRadioGroup v-model:value="editModel.proxy_type">
            <NRadioButton value="http">{{ $t('business.proxy-http') }}</NRadioButton>
            <NRadioButton value="socks5">{{ $t('business.proxy-socks5') }}</NRadioButton>
          </NRadioGroup>
        </NFormItem>
        <NFormItem :label="$t('business.proxy-probe-url')">
          <div class="w-full">
            <NInput v-model:value="editModel.probe_url" :placeholder="$t('business.proxy-probe-url-placeholder')" />
            <NText depth="3" class="mt-4px block text-12px">{{ $t('business.proxy-probe-url-hint') }}</NText>
          </div>
        </NFormItem>
        <NFormItem :label="$t('business.proxy-enabled')">
          <NSwitch v-model:value="editModel.enabled" />
        </NFormItem>
        <NText depth="3" class="mb-12px block text-12px">{{ $t('business.proxy-encrypted-and-used-by') }}</NText>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showEdit = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleUpdate">{{ $t('business.save') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showSettingsModal" preset="card" :title="$t('business.failover-policy')" class="w-640px">
      <NAlert type="info" :bordered="false" class="mb-16px">
        {{ $t('business.proxy-policy-hint') }}
      </NAlert>
      <NForm :model="settingsForm" label-placement="top">
        <NFormItem :label="$t('business.enabled')">
          <NSwitch v-model:value="settingsForm.enabled" />
        </NFormItem>
        <NGrid :cols="2" :x-gap="16">
          <NFormItemGi :label="$t('business.probe-interval-seconds')">
            <NInputNumber v-model:value="settingsForm.probe_interval_seconds" :min="15" :max="3600" class="w-full" />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.fast-probe-interval-seconds')">
            <NInputNumber v-model:value="settingsForm.fast_probe_interval_seconds" :min="3" :max="120" class="w-full" />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.probe-timeout-seconds')">
            <NInputNumber v-model:value="settingsForm.probe_timeout_seconds" :min="2" :max="60" class="w-full" />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.fail-threshold')">
            <NInputNumber v-model:value="settingsForm.fail_threshold" :min="1" :max="20" class="w-full" />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.recover-threshold')">
            <NInputNumber v-model:value="settingsForm.recover_threshold" :min="1" :max="20" class="w-full" />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.switch-cooldown-seconds')">
            <NInputNumber v-model:value="settingsForm.switch_cooldown_seconds" :min="0" :max="86400" class="w-full" />
          </NFormItemGi>
        </NGrid>
        <NFormItem :label="$t('business.failback-mode')">
          <NRadioGroup v-model:value="settingsForm.failback_mode">
            <div class="flex flex-col gap-6px">
              <NRadioButton value="sticky">{{ $t('business.failback-sticky') }}</NRadioButton>
              <NRadioButton value="auto_after_soak">{{ $t('business.failback-auto-soak') }}</NRadioButton>
            </div>
          </NRadioGroup>
        </NFormItem>
        <NFormItem v-if="settingsForm.failback_mode === 'auto_after_soak'" :label="$t('business.failback-soak-seconds')">
          <NInputNumber v-model:value="settingsForm.failback_soak_seconds" :min="60" :max="86400" class="w-full" />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showSettingsModal = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="savingSettings" @click="handleSaveSettings">{{ $t('business.save') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showProbeModal" preset="card" :title="probeTitle" class="w-860px">
      <NDataTable :columns="probeColumns" :data="probeResults" size="small" :bordered="false" :scroll-x="760" />
    </NModal>

    <NDrawer v-model:show="showEventsDrawer" :width="860">
      <NDrawerContent :title="$t('business.failover-events')" closable>
        <div class="mb-12px flex items-center justify-between gap-8px">
          <NSelect
            v-model:value="eventProviderFilter"
            clearable
            filterable
            :options="providerOptions"
            :placeholder="$t('business.all')"
            class="w-260px"
            @update:value="loadFailoverEvents"
          />
          <NButton quaternary type="primary" :loading="eventsLoading" @click="loadFailoverEvents">
            {{ $t('business.refresh') }}
          </NButton>
        </div>
        <NDataTable
          :columns="eventColumns"
          :data="failoverEvents"
          :loading="eventsLoading"
          :row-key="row => row.id"
          size="small"
          :bordered="false"
          :scroll-x="800"
        />
      </NDrawerContent>
    </NDrawer>
  </div>
</template>

<style scoped></style>
