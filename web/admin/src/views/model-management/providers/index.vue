<script setup lang="ts">
import { computed, h, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NDescriptions,
  NDescriptionsItem,
  NDrawer,
  NDrawerContent,
  NDropdown,
  NEmpty,
  NForm,
  NFormItem,
  NGrid,
  NGi,
  NInput,
  NInputNumber,
  NModal,
  NProgress,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  NText
} from 'naive-ui';
import type { DataTableColumns, DropdownOption, FormInst, FormRules, SelectGroupOption } from 'naive-ui';
import AvailabilityBar from '@/components/common/availability-bar.vue';
import {
  apiProtocolText,
  formatTime,
  healthStatusText,
  healthSummary,
  healthTagType,
  probeReasonText,
  statusTagType,
  statusText
} from '@/utils/admin-status';
import { formatCostUSD, formatResetTime, formatUnitAmount, settlementCurrencyOptions } from '@/utils/currency';
import { providerFamilyLabel, providerGroupLabel, providerVersionLabel } from '@/utils/provider-catalog';
import {
  fetchCreateProviderAccount,
  fetchDeleteProviderAccount,
  fetchProviderAccountHealthHistory,
  fetchProviderAccounts,
  fetchProviderAvailability,
  fetchProviderBalance,
  fetchProviderCatalog,
  fetchProviderModels,
  fetchTestAllProviderAccounts,
  fetchTestProviderAccount,
  fetchUpdateProviderAccount,
  fetchUpdateProviderAccountStatus,
  fetchUpdateProviderHealthSettings
} from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';

defineOptions({ name: 'ModelManagementProviders' });

const { has, isPlatformAdmin } = usePermission();
const canManage = computed(() => has('provider:manage'));

function providerTestMessage(result: Api.Management.ProviderTestResult) {
  if (result.reason === 'network_error' || result.reason === 'proxy_error') {
    return $t('business.providerTestNetworkError', { detail: result.detail ?? probeReasonText(result.reason) });
  }
  if (result.reason === 'connected' || result.reason === 'slow_response') {
    return $t('business.providerTestConnected', { status: result.status_code ?? 0 });
  }
  if (result.reason === 'credentials_rejected') {
    return $t('business.providerTestCredentialsRejected', { status: result.status_code ?? 0 });
  }
  if (result.reason === 'v1_suffix_suspected') return $t('business.providerTestV1SuffixSuspected');
  if (result.reason === 'upstream_http_error') {
    return $t('business.providerTestUpstreamHTTPError', { status: result.status_code ?? 0 });
  }
  return result.reason ? probeReasonText(result.reason) : $t('business.providerTestUnknown');
}

const route = useRoute();
const router = useRouter();
const catalogGroups = ref<Api.Management.ProviderCatalogGroup[]>([]);
const catalogLoading = ref(true);
const catalogEntries = computed(() => catalogGroups.value.flatMap(group => group.entries));
const vendorOptions = computed<SelectGroupOption[]>(() =>
  catalogGroups.value.map(group => {
    const families = new Map<string, string>();
    for (const entry of group.entries) families.set(entry.family_id || entry.id, providerFamilyLabel(entry));
    return {
      type: 'group',
      label: providerGroupLabel(group),
      key: group.group,
      children: Array.from(families, ([value, label]) => ({ label, value }))
    };
  })
);

const loading = ref(true);
const testingAll = ref(false);
const list = ref<Api.Management.ProviderAccount[]>([]);
const availabilityOverview = ref<Api.Management.ProviderAvailabilityOverview | null>(null);
const selectedProviderFilter = ref<string | null>(null);

const showCreate = ref(false);
const showEdit = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const editFormRef = ref<FormInst | null>(null);
const selectedFamilyID = ref('');
const formModel = reactive({
  provider: '',
  api_protocol: 'openai_chat',
  name: '',
  region: '',
  base_url: '',
  billing_currency: 'USD',
  api_key: ''
});
const editModel = reactive({
  id: '',
  provider: '',
  api_protocol: 'openai_chat',
  name: '',
  region: '',
  base_url: '',
  billing_currency: 'USD',
  api_key: ''
});

const showHealthSettings = ref(false);
const savingHealthSettings = ref(false);
const healthSettings = reactive<Api.Management.ProviderHealthSettings>({
  enabled: true,
  interval_seconds: 300,
  timeout_seconds: 15,
  degraded_latency_ms: 5000,
  failure_threshold: 2
});

const showHealthDrawer = ref(false);
const healthHistoryLoading = ref(false);
const healthHistory = ref<Api.Management.ProviderAccountHealthHistory | null>(null);
const healthDrawerAccountID = ref('');

const formRules = computed<FormRules>(() => ({
  provider: {
    required: true,
    message: $t('business.select-a-provider'),
    trigger: 'change'
  },
  name: {
    required: true,
    message: $t('business.enter-an-account-name'),
    trigger: 'blur'
  },
  api_key: {
    required: true,
    message: $t('business.enter-an-api-key'),
    trigger: 'blur'
  }
}));
const editRules = computed<FormRules>(() => ({
  name: {
    required: true,
    message: $t('business.enter-an-account-name'),
    trigger: 'blur'
  }
}));

const filteredAccounts = computed(() => {
  if (!selectedProviderFilter.value) return list.value;
  return list.value.filter(item => item.provider === selectedProviderFilter.value);
});

function proxyTypeText(value: Api.Management.ProviderProxyType) {
  if (value === 'http') return $t('business.proxy-http');
  if (value === 'socks5') return $t('business.proxy-socks5');
  return $t('business.proxy-none');
}

function formatAvail(val: number | null) {
  if (val === null || val === undefined) return '-';
  return `${Math.round(val * 10) / 10}%`;
}

const selectedVendor = computed(() => findVendor(formModel.provider));
const editVendor = computed(() => findVendor(editModel.provider));
const officialProviderProtocols: Record<string, string[]> = {
  openai: ['openai_responses', 'openai_chat'],
  anthropic: ['anthropic_messages'],
  gemini: ['gemini_native'],
  azure_openai: ['azure_openai'],
  bedrock: ['bedrock'],
  vertex: ['vertex'],
  openai_compatible: ['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_native']
};

function allowedProtocolsForProvider(provider: string) {
  if (!provider) return ['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_native'];
  return officialProviderProtocols[provider] ?? ['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_native'];
}

function protocolOptionsForProvider(provider: string) {
  return allowedProtocolsForProvider(provider).map(proto => ({
    label: apiProtocolText(proto),
    value: proto
  }));
}

const createProtocolOptions = computed(() => protocolOptionsForProvider(formModel.provider));
const editProtocolOptions = computed(() => protocolOptionsForProvider(editModel.provider));

const versionOptions = computed(() =>
  catalogEntries.value
    .filter(entry => (entry.family_id || entry.id) === selectedFamilyID.value)
    .map(entry => ({
      label: providerVersionLabel(entry),
      value: entry.id
    }))
);

function findVendor(id: string) {
  return catalogEntries.value.find(entry => entry.id === id) ?? null;
}

function providerLabel(value: string) {
  const vendor = findVendor(value);
  return vendor ? providerFamilyLabel(vendor) : value;
}

function applyVendor(value: string, resetName = false) {
  const vendor = findVendor(value);
  if (!vendor) return;
  formModel.provider = value;
  const allowed = allowedProtocolsForProvider(value);
  formModel.api_protocol =
    vendor.default_protocol && allowed.includes(vendor.default_protocol) ? vendor.default_protocol : (allowed[0] ?? 'openai_chat');
  selectedFamilyID.value = vendor.family_id || vendor.id;
  formModel.base_url = vendor.editable_url ? vendor.base_url : '';
  if (resetName || !formModel.name) formModel.name = providerFamilyLabel(vendor);
}

function handleFamilyChange(value: string) {
  const vendor = catalogEntries.value.find(entry => (entry.family_id || entry.id) === value);
  if (vendor) applyVendor(vendor.id, true);
}

function openWebsite(vendor: Api.Management.ProviderCatalogEntry | null) {
  if (vendor?.website_url) window.open(vendor.website_url, '_blank', 'noopener,noreferrer');
}

function openApiKeyUrl() {
  if (selectedVendor.value?.api_key_url) window.open(selectedVendor.value.api_key_url, '_blank', 'noopener,noreferrer');
}

function toggleProviderFilter(provider: string) {
  selectedProviderFilter.value = selectedProviderFilter.value === provider ? null : provider;
}

const overviewMoreOptions = computed<DropdownOption[]>(() => {
  if (!isPlatformAdmin.value) return [];
  return [
    { label: $t('business.health-check-settings'), key: 'health-settings' },
    { label: $t('business.proxy-governance'), key: 'proxy-governance' }
  ];
});

function handleOverviewMoreAction(key: string | number) {
  if (key === 'health-settings') showHealthSettings.value = true;
  else if (key === 'proxy-governance') void router.push('/system/proxies');
}

function rowMoreOptions(row: Api.Management.ProviderAccount): DropdownOption[] {
  const options: DropdownOption[] = [
    { label: $t('business.test'), key: 'test' },
    { label: $t('business.availability-details'), key: 'availability' }
  ];
  if (findVendor(row.provider)?.balance_kind) {
    options.push({ label: $t('business.query-balance'), key: 'balance' });
  }
  options.push(
    { type: 'divider', key: 'divider-lifecycle' },
    { label: row.status === 'active' ? $t('business.disabled') : $t('business.enable'), key: 'toggle-status' },
    { label: $t('business.delete-account'), key: 'delete', props: { style: { color: '#d03050' } } }
  );
  return options;
}

function handleRowMoreAction(key: string | number, row: Api.Management.ProviderAccount) {
  if (key === 'test') {
    void handleTest(row);
    return;
  }
  if (key === 'availability') {
    void openHealthHistory(row.id);
    return;
  }
  if (key === 'balance') {
    void handleBalance(row);
    return;
  }
  if (key === 'toggle-status') {
    if (row.status === 'active') {
      window.$dialog?.warning({
        title: $t('business.disabled'),
        content: $t('business.provider-account-disable-confirm'),
        positiveText: $t('common.confirm'),
        negativeText: $t('common.cancel'),
        onPositiveClick: () => toggleStatus(row)
      });
    } else {
      void toggleStatus(row);
    }
    return;
  }
  if (key === 'delete') {
    window.$dialog?.warning({
      title: $t('business.delete-account'),
      content: $t('common.confirmDelete'),
      positiveText: $t('common.confirm'),
      negativeText: $t('common.cancel'),
      onPositiveClick: () => handleDelete(row.id)
    });
  }
}

const columns = computed<DataTableColumns<Api.Management.ProviderAccount>>(() => [
  {
    title: $t('business.provider-account-overview'),
    key: 'name',
    minWidth: 200,
    render: row =>
      h('div', { class: 'flex flex-col gap-2px' }, [
        h('span', { class: 'font-medium' }, row.name),
        h('span', { class: 'text-12px text-#6b7280' }, providerLabel(row.provider))
      ])
  },
  {
    title: $t('business.protocol'),
    key: 'api_protocol',
    width: 160,
    render: row => h(NTag, { size: 'small', type: 'info', bordered: false }, { default: () => apiProtocolText(row.api_protocol) })
  },
  {
    title: $t('business.status-health'),
    key: 'status',
    width: 170,
    render: row =>
      h('div', { class: 'flex flex-col items-start gap-4px' }, [
        h(
          NTag,
          { type: statusTagType(row.status), size: 'small', round: true },
          { default: () => statusText(row.status, 'account') }
        ),
        h('div', { class: 'flex items-center gap-4px', title: healthSummary(row) }, [
          h(
            NTag,
            { type: healthTagType(row.health_status), size: 'small', round: true },
            { default: () => healthStatusText(row.health_status) }
          ),
          row.requests_24h > 0
            ? h('span', { class: 'text-12px text-#6b7280' }, `${Math.round((row.success_rate_24h ?? 0) * 100)}%`)
            : null
        ])
      ])
  },
  {
    title: $t('business.availability-24h'),
    key: 'availability_24h',
    width: 240,
    render: row =>
      h(
        'div',
        { class: 'flex cursor-pointer flex-col gap-4px', onClick: () => openHealthHistory(row.id) },
        [
          h('div', { class: 'flex items-center gap-6px text-12px' }, [
            h('span', { class: 'font-medium' }, formatAvail(row.availability_24h)),
            row.active_check_latency_ms !== null && row.active_check_latency_ms !== undefined
              ? h('span', { class: 'text-#6b7280' }, $t('business.latencyMs', { value: row.active_check_latency_ms }))
              : null,
            row.active_check_reason && row.active_check_reason !== 'connected'
              ? h('span', { class: 'text-#d03050 truncate max-w-120px' }, probeReasonText(row.active_check_reason))
              : null
          ]),
          h(AvailabilityBar, { buckets: row.heartbeat_24h, compact: true }),
          h(
            'span',
            { class: 'text-11px text-#9ca3af' },
            row.last_checked_at ? `${$t('business.last-checked-at')}: ${formatTime(row.last_checked_at)}` : $t('business.not-verified')
          )
        ]
      )
  },
  {
    title: $t('business.version-region'),
    key: 'version',
    width: 150,
    render: row =>
      h('div', { class: 'flex flex-col gap-2px' }, [
        h('span', {}, providerVersionLabel(findVendor(row.provider))),
        h('span', { class: 'text-12px text-#6b7280' }, row.region ?? '-')
      ])
  },
  {
    title: $t('business.settlement-currency'),
    key: 'billing_currency',
    width: 105,
    render: row => h('span', {}, row.billing_currency)
  },
  {
    title: $t('business.proxy'),
    key: 'proxy_type',
    width: 180,
    render: row =>
      row.proxy_type === 'none'
        ? h('span', { class: 'text-#6b7280' }, proxyTypeText(row.proxy_type))
        : h('div', { class: 'flex flex-col gap-2px' }, [
            h('div', { class: 'flex items-center gap-4px' }, [
              h(
                NTag,
                { size: 'tiny', round: true, type: healthTagType(row.proxy_status) },
                { default: () => row.proxy_name || proxyTypeText(row.proxy_type) }
              )
            ]),
            h(
              'span',
              { class: 'text-11px text-#6b7280' },
              `${proxyTypeText(row.proxy_type)} · ${$t('business.pool-size-count', { count: row.proxy_pool_size })}`
            )
          ])
  },
  {
    title: $t('business.api-endpoint'),
    key: 'base_url',
    minWidth: 230,
    render: row => h('span', { class: 'break-all' }, row.base_url ?? findVendor(row.provider)?.api_base_url ?? '-')
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 200,
    fixed: 'right',
    render(row) {
      if (!canManage.value) {
        return h(
          NButton,
          {
            size: 'small',
            quaternary: true,
            onClick: () => openHealthHistory(row.id)
          },
          { default: () => $t('business.availability-details') }
        );
      }

      return h(
        NSpace,
        { size: 4 },
        {
          default: () => [
            h(
              NButton,
              { size: 'small', quaternary: true, type: 'primary', onClick: () => openModels(row) },
              { default: () => $t('business.model') }
            ),
            h(
              NButton,
              { size: 'small', quaternary: true, type: 'primary', onClick: () => openEdit(row) },
              { default: () => $t('business.edit') }
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

const checkHistoryColumns = computed<DataTableColumns<Api.Management.ProviderAccountHealthCheckRecord>>(() => [
  {
    title: $t('business.time'),
    key: 'checked_at',
    width: 170,
    render: row => formatTime(row.checked_at)
  },
  {
    title: $t('business.check-trigger'),
    key: 'trigger',
    width: 100,
    render: row =>
      h(
        NTag,
        { size: 'tiny', round: true, type: row.trigger === 'manual' ? 'info' : 'default' },
        { default: () => (row.trigger === 'manual' ? $t('business.trigger-manual') : $t('business.trigger-auto')) }
      )
  },
  {
    title: $t('business.probe-status'),
    key: 'status',
    width: 105,
    render: row =>
      h(NTag, { size: 'small', round: true, type: healthTagType(row.status) }, { default: () => healthStatusText(row.status) })
  },
  {
    title: $t('business.http-status-code'),
    key: 'http_status',
    width: 110,
    render: row => (row.http_status !== null ? String(row.http_status) : '-')
  },
  {
    title: $t('business.latency'),
    key: 'latency_ms',
    width: 95,
    render: row => (row.latency_ms !== null ? $t('business.latencyMs', { value: row.latency_ms }) : '-')
  },
  {
    title: $t('business.proxy-used'),
    key: 'proxy_name',
    width: 130,
    render: row => row.proxy_name || $t('business.direct-connection')
  },
  {
    title: $t('business.probe-reason'),
    key: 'reason',
    minWidth: 160,
    render: row => probeReasonText(row.reason)
  },
  {
    title: $t('business.error-detail'),
    key: 'error_detail',
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: row => row.error_detail || '-'
  }
]);

async function loadCatalog() {
  catalogLoading.value = true;
  try {
    const { data, error } = await fetchProviderCatalog();
    if (!error && data) catalogGroups.value = data;
  } finally {
    catalogLoading.value = false;
  }
}

async function loadData() {
  loading.value = true;
  try {
    const [accountsRes, availRes] = await Promise.all([fetchProviderAccounts(), fetchProviderAvailability()]);
    if (!accountsRes.error && accountsRes.data) list.value = accountsRes.data;
    if (!availRes.error && availRes.data) {
      availabilityOverview.value = availRes.data;
      if (availRes.data.settings) Object.assign(healthSettings, availRes.data.settings);
    }
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  selectedFamilyID.value = '';
  Object.assign(formModel, {
    provider: '',
    api_protocol: 'openai_chat',
    name: '',
    region: '',
    base_url: '',
    billing_currency: 'USD',
    api_key: ''
  });
  showCreate.value = true;
}

function openEdit(row: Api.Management.ProviderAccount) {
  const allowed = allowedProtocolsForProvider(row.provider);
  Object.assign(editModel, {
    id: row.id,
    provider: row.provider,
    api_protocol: row.api_protocol || allowed[0] || 'openai_chat',
    name: row.name,
    region: row.region ?? '',
    base_url: row.base_url ?? findVendor(row.provider)?.base_url ?? '',
    billing_currency: row.billing_currency,
    api_key: ''
  });
  showEdit.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  submitting.value = true;
  try {
    const { data, error } = await fetchCreateProviderAccount({
      provider: formModel.provider,
      api_protocol: formModel.api_protocol,
      name: formModel.name.trim(),
      region: formModel.region.trim() || undefined,
      base_url: selectedVendor.value?.editable_url ? formModel.base_url.trim() || undefined : undefined,
      billing_currency: formModel.billing_currency,
      api_key: formModel.api_key
    });
    if (!error && data) {
      window.$message?.success($t('business.account-created-available-models-are'));
      showCreate.value = false;
      await loadData();
      await openModels({ id: data.id, name: formModel.name.trim() });
    }
  } finally {
    submitting.value = false;
  }
}

async function handleUpdate() {
  await editFormRef.value?.validate();
  submitting.value = true;
  try {
    const { error } = await fetchUpdateProviderAccount(editModel.id, {
      name: editModel.name.trim(),
      api_protocol: editModel.api_protocol,
      region: editModel.region.trim() || undefined,
      base_url: editVendor.value?.editable_url ? editModel.base_url.trim() || undefined : undefined,
      billing_currency: editModel.billing_currency,
      api_key: editModel.api_key.trim() || undefined
    });
    if (!error) {
      window.$message?.success($t('business.account-updated'));
      showEdit.value = false;
      await loadData();
    }
  } finally {
    submitting.value = false;
  }
}

async function toggleStatus(row: Api.Management.ProviderAccount) {
  const status = row.status === 'active' ? 'disabled' : 'active';
  const { error } = await fetchUpdateProviderAccountStatus(row.id, status);
  if (!error) {
    window.$message?.success(status === 'active' ? $t('business.account-enabled') : $t('business.account-disabled'));
    await loadData();
  }
}

async function handleTest(row: Api.Management.ProviderAccount) {
  const { data, error } = await fetchTestProviderAccount(row.id);
  if (!error && data) {
    if (data.ok) window.$message?.success(providerTestMessage(data));
    else window.$message?.error(providerTestMessage(data));
    await loadData();
    if (showHealthDrawer.value && healthDrawerAccountID.value === row.id) {
      await openHealthHistory(row.id);
    }
  }
}

async function handleTestAll() {
  testingAll.value = true;
  try {
    const { data, error } = await fetchTestAllProviderAccounts();
    if (!error && data) {
      window.$message?.success(
        $t('business.batch-test-finished', {
          checked: data.checked,
          healthy: data.healthy,
          degraded: data.degraded,
          unhealthy: data.unhealthy
        })
      );
      await loadData();
    }
  } finally {
    testingAll.value = false;
  }
}

async function openHealthHistory(accountID: string) {
  healthDrawerAccountID.value = accountID;
  showHealthDrawer.value = true;
  healthHistoryLoading.value = true;
  try {
    const { data, error } = await fetchProviderAccountHealthHistory(accountID, 50);
    if (!error && data) healthHistory.value = data;
  } finally {
    healthHistoryLoading.value = false;
  }
}

async function handleSaveHealthSettings() {
  savingHealthSettings.value = true;
  try {
    const { data, error } = await fetchUpdateProviderHealthSettings({ ...healthSettings });
    if (!error && data) {
      Object.assign(healthSettings, data);
      window.$message?.success($t('business.provider-health-settings-saved'));
      showHealthSettings.value = false;
      await loadData();
    }
  } finally {
    savingHealthSettings.value = false;
  }
}

const showModels = ref(false);
const modelLoading = ref(false);
const modelRows = ref<Api.Management.ProviderModel[]>([]);
const modelTitle = ref('');
const modelAccountID = ref('');
let modelRequestGeneration = 0;
watch(showModels, visible => {
  if (!visible) modelRequestGeneration++;
});
const selectedModelKeys = ref<string[]>([]);
const modelColumns = computed<DataTableColumns<Api.Management.ProviderModel>>(() => [
  { type: 'selection' },
  { title: $t('business.model-identifier'), key: 'id', ellipsis: { tooltip: true }, minWidth: 220 },
  {
    title: $t('business.input-price-usd-per-million-1858l67'),
    key: 'input_cost_per_million',
    width: 150,
    render: row =>
      row.input_cost_per_million === null ? $t('business.no-data') : formatCostUSD(row.input_cost_per_million)
  },
  {
    title: $t('business.output-price-usd-per-million-72p7y2'),
    key: 'output_cost_per_million',
    width: 150,
    render: row =>
      row.output_cost_per_million === null ? $t('business.no-data') : formatCostUSD(row.output_cost_per_million)
  }
]);

async function openModels(row: Pick<Api.Management.ProviderAccount, 'id' | 'name'>) {
  const generation = ++modelRequestGeneration;
  showModels.value = true;
  modelLoading.value = true;
  modelRows.value = [];
  selectedModelKeys.value = [];
  modelAccountID.value = row.id;
  modelTitle.value = $t('business.accountAvailableModels', { name: row.name });
  try {
    const { data, error } = await fetchProviderModels(row.id);
    if (generation === modelRequestGeneration && !error && data) modelRows.value = data.models;
  } finally {
    if (generation === modelRequestGeneration) modelLoading.value = false;
  }
}

async function addModelsToService() {
  if (!modelAccountID.value || !selectedModelKeys.value.length) return;
  await router.push({
    name: 'model-management_model-services',
    query: {
      account: modelAccountID.value,
      models: selectedModelKeys.value.join(',')
    }
  });
}

const showBalance = ref(false);
const balanceLoading = ref(false);
const balanceRows = ref<Api.Management.ProviderBalanceEntry[]>([]);
const balanceTitle = ref('');
const balanceLabelKeys: Record<string, App.I18n.I18nKey> = {
  wallet_cny: 'business.balanceWalletCNY',
  wallet_usd: 'business.balanceWalletUSD',
  provider_zhipu: 'business.balanceProviderZhipu',
  provider_zai: 'business.balanceProviderZAI',
  provider_stepfun: 'business.balanceProviderStepFun',
  provider_stepfun_global: 'business.balanceProviderStepFunGlobal',
  provider_siliconflow: 'business.balanceProviderSiliconFlow',
  provider_siliconflow_global: 'business.balanceProviderSiliconFlowGlobal',
  provider_openrouter: 'business.balanceProviderOpenRouter',
  provider_novita: 'business.balanceProviderNovita',
  quota_5h: 'business.balanceQuota5h',
  quota_weekly: 'business.balanceQuotaWeekly'
};

function balanceLabel(row: Api.Management.ProviderBalanceEntry) {
  const key = balanceLabelKeys[row.label_key];
  return key ? $t(key) : row.label_key;
}

const balanceEntries = computed(() => balanceRows.value.filter(item => item.kind !== 'quota'));
const quotaEntries = computed(() => balanceRows.value.filter(item => item.kind === 'quota'));
const balanceColumns = computed<DataTableColumns<Api.Management.ProviderBalanceEntry>>(() => [
  {
    title: $t('business.account'),
    key: 'label_key',
    minWidth: 130,
    render: row => balanceLabel(row)
  },
  {
    title: $t('business.available'),
    key: 'remaining',
    width: 140,
    render: row => formatUnitAmount(row.remaining, row.unit)
  },
  {
    title: $t('business.total'),
    key: 'total',
    width: 140,
    render: row => formatUnitAmount(row.total, row.unit)
  },
  {
    title: $t('business.used'),
    key: 'used',
    width: 140,
    render: row => formatUnitAmount(row.used, row.unit)
  }
]);

async function handleBalance(row: Api.Management.ProviderAccount) {
  showBalance.value = true;
  balanceLoading.value = true;
  balanceTitle.value = $t('business.accountBalanceTitle', { name: row.name });
  try {
    const { data, error } = await fetchProviderBalance(row.id);
    balanceRows.value = !error && data ? (data.balances ?? []) : [];
  } finally {
    balanceLoading.value = false;
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteProviderAccount(id);
  if (!error) {
    window.$message?.success($t('business.account-deleted'));
    await loadData();
  }
}

onMounted(async () => {
  await Promise.all([loadCatalog(), loadData()]);
  const providerID = typeof route.query.provider === 'string' ? route.query.provider : '';
  if (providerID) {
    const account = list.value.find(item => item.id === providerID);
    if (account) openEdit(account);
  }
});
</script>

<template>
  <div class="flex flex-col gap-16px bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard :title="$t('business.provider-availability-overview')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace align="center">
          <NTag size="small" :type="healthSettings.enabled ? 'success' : 'default'" round>
            {{
              healthSettings.enabled
                ? $t('business.auto-check-active', { seconds: healthSettings.interval_seconds })
                : $t('business.auto-check-paused')
            }}
          </NTag>
          <NButton v-if="canManage" secondary type="info" size="small" :loading="testingAll" @click="handleTestAll">
            {{ $t('business.test-all-accounts') }}
          </NButton>
          <NDropdown
            v-if="overviewMoreOptions.length"
            trigger="click"
            :options="overviewMoreOptions"
            @select="handleOverviewMoreAction"
          >
            <NButton quaternary size="small">{{ $t('business.more') }}</NButton>
          </NDropdown>
        </NSpace>
      </template>

      <div v-if="availabilityOverview" class="mb-12px flex flex-wrap items-center justify-between gap-8px">
        <NSpace align="center" size="small">
          <NTag type="info" round size="small">
            {{ $t('business.overall-availability-24h') }}:
            {{ formatAvail(availabilityOverview.summary.overall_availability_24h) }}
          </NTag>
          <NTag type="success" round size="small">
            {{ $t('business.healthy') }}: {{ availabilityOverview.summary.healthy_accounts }}
          </NTag>
          <NTag v-if="availabilityOverview.summary.degraded_accounts > 0" type="warning" round size="small">
            {{ $t('business.degraded') }}: {{ availabilityOverview.summary.degraded_accounts }}
          </NTag>
          <NTag v-if="availabilityOverview.summary.unhealthy_accounts > 0" type="error" round size="small">
            {{ $t('business.unhealthy') }}: {{ availabilityOverview.summary.unhealthy_accounts }}
          </NTag>
          <NText depth="3" class="text-12px">{{ $t('business.filter-by-provider-hint') }}</NText>
        </NSpace>
        <NButton
          v-if="selectedProviderFilter"
          size="tiny"
          type="primary"
          secondary
          @click="selectedProviderFilter = null"
        >
          {{ $t('business.clear-provider-filter', { provider: providerLabel(selectedProviderFilter) }) }}
        </NButton>
      </div>

      <NEmpty v-if="!loading && !(availabilityOverview?.providers?.length)" :description="$t('business.no-data')" />

      <NGrid v-else cols="1 s:2 m:3 l:4" responsive="screen" :x-gap="12" :y-gap="12">
        <NGi v-for="item in availabilityOverview?.providers ?? []" :key="item.provider">
          <NCard
            size="small"
            embedded
            class="cursor-pointer transition-all"
            :class="selectedProviderFilter === item.provider ? 'ring-2 ring-#18a058' : ''"
            @click="toggleProviderFilter(item.provider)"
          >
            <div class="flex items-center justify-between gap-8px">
              <span class="truncate font-semibold">{{ providerLabel(item.provider) }}</span>
              <NTag size="tiny" round :type="healthTagType(item.health_status)">
                {{ healthStatusText(item.health_status) }}
              </NTag>
            </div>

            <div class="mt-8px flex items-baseline justify-between gap-8px">
              <span class="text-20px font-semibold">{{ formatAvail(item.availability_24h) }}</span>
              <span class="text-12px text-#6b7280">
                {{
                  $t('business.accounts-healthy-ratio', {
                    healthy: item.healthy_accounts,
                    total: item.total_accounts
                  })
                }}
              </span>
            </div>

            <div class="mt-6px">
              <AvailabilityBar :buckets="item.heartbeat_24h" compact />
            </div>

            <div class="mt-8px flex flex-wrap items-center justify-between gap-4px text-12px text-#6b7280">
              <span>
                {{
                  item.avg_latency_ms_24h !== null
                    ? $t('business.latencyMs', { value: Math.round(item.avg_latency_ms_24h) })
                    : '-'
                }}
              </span>
              <span>
                {{
                  item.active_proxy_name
                    ? `${item.active_proxy_name} (${item.proxy_pool_size})`
                    : $t('business.direct-connection')
                }}
              </span>
            </div>
          </NCard>
        </NGi>
      </NGrid>
    </NCard>

    <NCard :title="$t('business.provider-account')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">{{ $t('business.refresh') }}</NButton>
          <NButton v-if="canManage" type="primary" @click="openCreate">{{ $t('business.create-account') }}</NButton>
        </NSpace>
      </template>
      <NDataTable
        :columns="columns"
        :data="filteredAccounts"
        :loading="loading"
        :bordered="false"
        :scroll-x="1560"
        :row-key="row => row.id"
      />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" :title="$t('business.create-provider-account')" class="w-640px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="top">
        <NFormItem :label="$t('business.provider')" path="provider">
          <NSelect
            :value="selectedFamilyID"
            :options="vendorOptions"
            :loading="catalogLoading"
            filterable
            :placeholder="$t('business.select-a-model-provider-or')"
            @update:value="handleFamilyChange"
          />
        </NFormItem>
        <NFormItem v-if="versionOptions.length > 1" :label="$t('business.service-version')">
          <NSelect :value="formModel.provider" :options="versionOptions" @update:value="value => applyVendor(value)" />
        </NFormItem>
        <NFormItem v-if="selectedVendor" :label="$t('business.api-protocol')">
          <div class="w-full">
            <NSelect
              v-model:value="formModel.api_protocol"
              :options="createProtocolOptions"
              :disabled="createProtocolOptions.length <= 1"
            />
            <NText depth="3" class="mt-4px block text-12px">{{ $t('business.api-protocol-hint') }}</NText>
          </div>
        </NFormItem>
        <NFormItem v-if="selectedVendor?.website_url" :label="$t('business.website')">
          <NButton text type="primary" @click="openWebsite(selectedVendor)">{{ selectedVendor.website_url }} ↗</NButton>
        </NFormItem>
        <NFormItem v-if="selectedVendor" :label="$t('business.api-endpoint')" path="base_url">
          <NInput
            v-if="selectedVendor.editable_url"
            v-model:value="formModel.base_url"
            :placeholder="$t('business.enter-the-service-api-endpoint')"
          />
          <NInput v-else :value="selectedVendor.api_base_url" readonly />
        </NFormItem>
        <NFormItem :label="$t('business.account-name')" path="name">
          <NInput v-model:value="formModel.name" :placeholder="$t('business.for-example-primary-account-or')" />
        </NFormItem>
        <NFormItem :label="$t('business.region')" path="region">
          <NInput v-model:value="formModel.region" :placeholder="$t('business.optional-for-example-north-china')" />
        </NFormItem>
        <NFormItem :label="$t('business.settlement-currency')">
          <div class="w-full">
            <NSelect v-model:value="formModel.billing_currency" :options="settlementCurrencyOptions()" />
            <NText depth="3" class="mt-4px block text-12px">{{ $t('business.used-for-model-cost-route') }}</NText>
          </div>
        </NFormItem>
        <div v-if="selectedVendor?.api_key_url" class="mb-12px">
          <NButton text type="primary" @click="openApiKeyUrl">
            {{ $t('business.open-provider-console-to-get') }}
          </NButton>
        </div>
        <NFormItem label="API Key" path="api_key">
          <NInput
            v-model:value="formModel.api_key"
            type="password"
            show-password-on="click"
            :placeholder="$t('business.encrypted-and-stored-server-side')"
          />
        </NFormItem>
        <NAlert type="info" :bordered="false">{{ $t('business.after-creation-available-models-are') }}</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleCreate">
            {{ $t('business.create-and-fetch-models') }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showEdit" preset="card" :title="$t('business.edit-provider-account')" class="w-600px">
      <NForm ref="editFormRef" :model="editModel" :rules="editRules" label-placement="top">
        <NFormItem :label="$t('business.provider')">
          <NSpace align="center" :size="8">
            <NTag :bordered="false">{{ providerLabel(editModel.provider) }}</NTag>
            <NText depth="2">{{ editVendor ? providerVersionLabel(editVendor) : '' }}</NText>
          </NSpace>
        </NFormItem>
        <NFormItem v-if="editVendor?.website_url" :label="$t('business.website')">
          <NButton text type="primary" @click="openWebsite(editVendor)">{{ editVendor.website_url }} ↗</NButton>
        </NFormItem>
        <NFormItem :label="$t('business.account-name')" path="name">
          <NInput v-model:value="editModel.name" />
        </NFormItem>
        <NFormItem :label="$t('business.api-protocol')">
          <div class="w-full">
            <NSelect
              v-model:value="editModel.api_protocol"
              :options="editProtocolOptions"
              :disabled="editProtocolOptions.length <= 1"
            />
            <NText depth="3" class="mt-4px block text-12px">{{ $t('business.api-protocol-hint') }}</NText>
          </div>
        </NFormItem>
        <NFormItem :label="$t('business.region')">
          <NInput v-model:value="editModel.region" :placeholder="$t('business.optional')" />
        </NFormItem>
        <NFormItem :label="$t('business.settlement-currency')">
          <NSelect v-model:value="editModel.billing_currency" :options="settlementCurrencyOptions()" />
        </NFormItem>
        <NFormItem :label="$t('business.api-endpoint')">
          <NInput v-model:value="editModel.base_url" :readonly="!editVendor?.editable_url" />
        </NFormItem>
        <NFormItem :label="$t('business.newApiKey')" path="api_key">
          <NInput
            v-model:value="editModel.api_key"
            type="password"
            show-password-on="click"
            :placeholder="$t('business.leaveBlankToKeepApiKey')"
          />
        </NFormItem>
        <NAlert type="info" :bordered="false">{{ $t('business.the-api-key-and-provider') }}</NAlert>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showEdit = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="submitting" @click="handleUpdate">{{ $t('business.save') }}</NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showModels" preset="card" :title="modelTitle" class="w-720px">
      <NAlert type="info" :bordered="false" class="mb-12px">{{ $t('business.the-model-list-comes-from') }}</NAlert>
      <NDataTable
        v-model:checked-row-keys="selectedModelKeys"
        :columns="modelColumns"
        :data="modelRows"
        :loading="modelLoading"
        :row-key="row => row.id"
        size="small"
        :bordered="false"
        :max-height="420"
        :scroll-x="650"
      />
      <template #footer>
        <NSpace justify="space-between">
          <NText depth="2">
            {{ $t('business.selected') }} {{ selectedModelKeys.length }}
            {{ $t('business.models') }}
          </NText>
          <NButton type="primary" :disabled="selectedModelKeys.length === 0" @click="addModelsToService">
            {{ $t('business.add-to-model-service') }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal v-model:show="showBalance" preset="card" :title="balanceTitle" class="w-680px">
      <NSpin :show="balanceLoading">
        <div v-if="quotaEntries.length" class="mb-16px">
          <NCard v-for="row in quotaEntries" :key="row.label_key" size="small" class="mb-8px" embedded>
            <NSpace justify="space-between" align="center" class="mb-8px">
              <NText strong>{{ balanceLabel(row) }}</NText>
              <NText depth="2">
                {{ $t('business.used') }}
                {{ formatUnitAmount(row.used, row.unit) }}
                {{ $t('business.remaining') }}
                {{ formatUnitAmount(row.remaining, row.unit) }}
              </NText>
            </NSpace>
            <NProgress
              type="line"
              :percentage="row.used ?? 0"
              :status="(row.used ?? 0) >= 90 ? 'error' : (row.used ?? 0) >= 70 ? 'warning' : 'success'"
            />
            <NText v-if="row.next_reset_time" depth="3" class="mt-8px block text-12px">
              {{ $t('business.next-reset') }}{{ formatResetTime(row.next_reset_time) }}
            </NText>
          </NCard>
        </div>
        <NDataTable
          v-if="balanceEntries.length"
          :columns="balanceColumns"
          :data="balanceEntries"
          size="small"
          :bordered="false"
        />
        <NEmpty v-if="!balanceLoading && !balanceRows.length" :description="$t('business.no-balance-or-limit-data')" />
      </NSpin>
    </NModal>

    <NModal v-model:show="showHealthSettings" preset="card" :title="$t('business.health-check-settings')" class="w-600px">
      <NAlert type="info" :bordered="false" class="mb-16px">
        {{ $t('business.provider-health-settings-hint') }}
      </NAlert>
      <NForm :model="healthSettings" label-placement="top">
        <NFormItem :label="$t('business.enable-auto-health-check')">
          <NSwitch v-model:value="healthSettings.enabled" />
        </NFormItem>
        <NGrid :cols="2" :x-gap="16">
          <NFormItemGi :label="$t('business.check-interval-seconds')">
            <NInputNumber v-model:value="healthSettings.interval_seconds" :min="60" :max="86400" class="w-full" />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.check-timeout-seconds')">
            <NInputNumber v-model:value="healthSettings.timeout_seconds" :min="3" :max="60" class="w-full" />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.degraded-latency-ms')">
            <NInputNumber v-model:value="healthSettings.degraded_latency_ms" :min="500" :max="60000" class="w-full" />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.failure-threshold')">
            <NInputNumber v-model:value="healthSettings.failure_threshold" :min="1" :max="10" class="w-full" />
          </NFormItemGi>
        </NGrid>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showHealthSettings = false">{{ $t('business.cancel') }}</NButton>
          <NButton type="primary" :loading="savingHealthSettings" @click="handleSaveHealthSettings">
            {{ $t('business.save') }}
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NDrawer v-model:show="showHealthDrawer" :width="880">
      <NDrawerContent
        :title="healthHistory ? $t('business.availability-diagnostics-title', { name: healthHistory.name }) : $t('business.availability-details')"
        closable
      >
        <NSpin :show="healthHistoryLoading">
          <template v-if="healthHistory">
            <div class="mb-12px flex flex-wrap items-center justify-between gap-8px">
              <AvailabilityBar :buckets="healthHistory.heartbeat_24h" />
              <NSpace>
                <NButton
                  v-if="canManage"
                  size="small"
                  type="info"
                  @click="handleTest({ id: healthHistory.account_id } as Api.Management.ProviderAccount)"
                >
                  {{ $t('business.test-now') }}
                </NButton>
                <NButton size="small" quaternary type="primary" @click="openHealthHistory(healthHistory.account_id)">
                  {{ $t('business.refresh') }}
                </NButton>
              </NSpace>
            </div>

            <NAlert v-if="healthHistory.current_error_detail" type="warning" :bordered="false" class="mb-12px">
              {{ probeReasonText(healthHistory.current_reason) }}: {{ healthHistory.current_error_detail }}
            </NAlert>

            <NDescriptions :column="3" size="small" bordered class="mb-16px">
              <NDescriptionsItem :label="$t('business.probe-status')">
                <NTag size="small" round :type="healthTagType(healthHistory.current_status)">
                  {{ healthStatusText(healthHistory.current_status) }}
                </NTag>
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.availability-24h')">
                {{ formatAvail(healthHistory.availability_24h) }}
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.availability-7d')">
                {{ formatAvail(healthHistory.availability_7d) }}
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.avgLatency24h')">
                {{
                  healthHistory.avg_latency_ms_24h !== null
                    ? $t('business.latencyMs', { value: Math.round(healthHistory.avg_latency_ms_24h) })
                    : '-'
                }}
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.p95-latency-24h')">
                {{
                  healthHistory.p95_latency_ms_24h !== null
                    ? $t('business.latencyMs', { value: Math.round(healthHistory.p95_latency_ms_24h) })
                    : '-'
                }}
              </NDescriptionsItem>
              <NDescriptionsItem :label="$t('business.proxy-used')">
                {{ healthHistory.current_proxy_name || $t('business.direct-connection') }}
              </NDescriptionsItem>
            </NDescriptions>

            <div class="mb-8px font-medium">{{ $t('business.check-history') }}</div>
            <NDataTable
              :columns="checkHistoryColumns"
              :data="healthHistory.checks"
              :row-key="row => row.id"
              size="small"
              :bordered="false"
              :scroll-x="820"
            />
          </template>
        </NSpin>
      </NDrawerContent>
    </NDrawer>
  </div>
</template>
