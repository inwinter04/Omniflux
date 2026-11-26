<script setup lang="ts">
import { computed, h, onMounted, reactive, ref, watch } from 'vue';
import { NButton, NDynamicTags, NInputNumber, NSelect, NSpace } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { settlementCurrencyOptions } from '@/utils/currency';
import { fetchProviderModels } from '@/service/api';
import { $t } from '@/locales';

interface CandidateDraft {
  provider_account_id: string | null;
  upstream_model: string;
  priority: number;
  weight: number;
  input_cost_per_million: number;
  output_cost_per_million: number;
  quality_score: number;
  cost_currency: string;
  context_window: number | null;
  capabilities: string[];
  tags: string[];
  status: 'active' | 'disabled';
}

const props = defineProps<{
  candidates: CandidateDraft[];
  accountOptions: { label: string; value: string; currency?: string }[];
}>();

const emit = defineEmits<{
  'update:candidates': [value: CandidateDraft[]];
}>();

const modelCache = reactive<Record<string, Api.Management.ProviderModel[]>>({});
const modelLoading = reactive<Record<string, boolean>>({});
const modelErrors = reactive<Record<string, string>>({});
const refreshing = ref(false);
const hasNonUSD = computed(() => props.candidates.some(item => item.cost_currency !== 'USD'));

const statusOptions = computed(() => [
  { label: $t('business.enable'), value: 'active' },
  { label: $t('business.disabled'), value: 'disabled' }
]);

const capabilityOptions = computed(() => [
  { label: $t('business.cap-vision'), value: 'vision' },
  { label: $t('business.cap-tools'), value: 'tools' },
  { label: $t('business.cap-reasoning'), value: 'reasoning' },
  { label: $t('business.cap-json-mode'), value: 'json_mode' },
  { label: $t('business.cap-streaming'), value: 'streaming' }
]);

const columns = computed<DataTableColumns<CandidateDraft>>(() => [
  {
    title: $t('business.provider-account'),
    key: 'provider_account_id',
    width: 190,
    render: row =>
      h(NSelect, {
        value: row.provider_account_id,
        options: props.accountOptions,
        filterable: true,
        size: 'small',
        placeholder: $t('business.select-account'),
        'onUpdate:value': (value: string) => {
          row.provider_account_id = value;
          row.upstream_model = '';
          row.input_cost_per_million = 0;
          row.output_cost_per_million = 0;
          row.cost_currency = props.accountOptions.find(item => item.value === value)?.currency ?? 'USD';
          void loadModels(value);
        }
      })
  },
  {
    title: $t('business.upstream-model'),
    key: 'upstream_model',
    width: 200,
    render: row =>
      h(NSelect, {
        value: row.upstream_model || null,
        options: (modelCache[row.provider_account_id ?? ''] ?? []).map(model => ({ label: model.id, value: model.id })),
        loading: modelLoading[row.provider_account_id ?? ''],
        filterable: true,
        tag: true,
        size: 'small',
        placeholder: modelLoading[row.provider_account_id ?? '']
          ? $t('business.loading-models')
          : $t('business.select-or-enter-a-model'),
        'onUpdate:value': (value: string) => selectModel(row, value)
      })
  },
  {
    title: $t('business.priority'),
    key: 'priority',
    width: 95,
    render: row =>
      h(NInputNumber, {
        value: row.priority,
        min: 1,
        size: 'small',
        'onUpdate:value': value => (row.priority = value ?? 100)
      })
  },
  {
    title: $t('business.weight'),
    key: 'weight',
    width: 90,
    render: row =>
      h(NInputNumber, {
        value: row.weight,
        min: 0,
        size: 'small',
        'onUpdate:value': value => (row.weight = value ?? 1)
      })
  },
  {
    title: $t('business.input-price-million'),
    key: 'input_cost_per_million',
    width: 130,
    render: row =>
      h(NInputNumber, {
        value: row.input_cost_per_million,
        min: 0,
        size: 'small',
        'onUpdate:value': value => (row.input_cost_per_million = value ?? 0)
      })
  },
  {
    title: $t('business.output-price-million'),
    key: 'output_cost_per_million',
    width: 130,
    render: row =>
      h(NInputNumber, {
        value: row.output_cost_per_million,
        min: 0,
        size: 'small',
        'onUpdate:value': value => (row.output_cost_per_million = value ?? 0)
      })
  },
  {
    title: $t('business.quality-score'),
    key: 'quality_score',
    width: 100,
    render: row =>
      h(NInputNumber, {
        value: row.quality_score,
        min: 0,
        max: 1,
        step: 0.05,
        size: 'small',
        'onUpdate:value': value => (row.quality_score = value ?? 0.5)
      })
  },
  {
    title: $t('business.settlement-currency'),
    key: 'cost_currency',
    width: 150,
    render: row =>
      h(NSelect, {
        value: row.cost_currency,
        options: settlementCurrencyOptions(),
        size: 'small',
        disabled: true
      })
  },
  {
    title: $t('business.context-window'),
    key: 'context_window',
    width: 120,
    render: row =>
      h(NInputNumber, {
        value: row.context_window,
        min: 1,
        step: 4096,
        clearable: true,
        size: 'small',
        placeholder: '128000',
        'onUpdate:value': value => (row.context_window = value ?? null)
      })
  },
  {
    title: $t('business.capabilities'),
    key: 'capabilities',
    width: 210,
    render: row =>
      h(NSelect, {
        value: row.capabilities,
        options: capabilityOptions.value,
        multiple: true,
        clearable: true,
        maxTagCount: 2,
        size: 'small',
        placeholder: $t('business.select-capabilities'),
        'onUpdate:value': (value: string[]) => (row.capabilities = value ?? [])
      })
  },
  {
    title: $t('business.tags'),
    key: 'tags',
    width: 190,
    render: row =>
      h(NDynamicTags, {
        value: row.tags,
        size: 'small',
        'onUpdate:value': (value: string[]) => (row.tags = value ?? [])
      })
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 110,
    render: row =>
      h(NSelect, {
        value: row.status,
        options: statusOptions.value,
        size: 'small',
        'onUpdate:value': value => (row.status = value)
      })
  },
  {
    title: '',
    key: 'actions',
    width: 84,
    // 窄屏下表格需要横向滚动时,删除按钮固定在右侧,保证始终可见。
    fixed: 'right',
    render: row =>
      h(
        NButton,
        {
          size: 'small',
          quaternary: true,
          type: 'error',
          disabled: props.candidates.length <= 1,
          onClick: () =>
            emit(
              'update:candidates',
              props.candidates.filter(item => item !== row)
            )
        },
        { default: () => $t('business.delete') }
      )
  }
]);

onMounted(refreshAll);
watch(
  () => props.candidates.map(item => item.provider_account_id),
  () => {
    void refreshAll();
  }
);

function newCandidate(): CandidateDraft {
  return {
    provider_account_id: props.accountOptions[0]?.value ?? null,
    upstream_model: '',
    priority: 100,
    weight: 1,
    input_cost_per_million: 0,
    output_cost_per_million: 0,
    quality_score: 0.5,
    cost_currency: props.accountOptions[0]?.currency ?? 'USD',
    context_window: null,
    capabilities: [],
    tags: [],
    status: 'active'
  };
}

function addCandidate() {
  if (props.candidates.length >= 50) {
    window.$message?.warning($t('business.a-model-service-can-have'));
    return;
  }
  const candidate = newCandidate();
  emit('update:candidates', [...props.candidates, candidate]);
  if (candidate.provider_account_id) void loadModels(candidate.provider_account_id);
}

async function refreshAll() {
  refreshing.value = true;
  try {
    const ids = [
      ...new Set(props.candidates.map(item => item.provider_account_id).filter((id): id is string => Boolean(id)))
    ];
    await Promise.all(ids.map(id => loadModels(id)));
  } finally {
    refreshing.value = false;
  }
}

async function loadModels(accountID: string, force = false) {
  if (!force && (modelCache[accountID] || modelLoading[accountID])) return;
  modelLoading[accountID] = true;
  delete modelErrors[accountID];
  try {
    const { data, error } = await fetchProviderModels(accountID);
    if (error || !data) {
      modelErrors[accountID] = $t('business.models-could-not-be-fetched');
      return;
    }
    modelCache[accountID] = data.models;
  } finally {
    modelLoading[accountID] = false;
  }
}

function selectModel(row: CandidateDraft, modelID: string) {
  row.upstream_model = modelID;
  row.input_cost_per_million = 0;
  row.output_cost_per_million = 0;
  const model = (modelCache[row.provider_account_id ?? ''] ?? []).find(item => item.id === modelID);
  if (
    row.cost_currency === 'USD' &&
    model?.input_cost_per_million !== null &&
    model?.input_cost_per_million !== undefined
  ) {
    row.input_cost_per_million = model.input_cost_per_million;
  }
  if (
    row.cost_currency === 'USD' &&
    model?.output_cost_per_million !== null &&
    model?.output_cost_per_million !== undefined
  ) {
    row.output_cost_per_million = model.output_cost_per_million;
  }
}
</script>

<template>
  <NSpace justify="space-between" class="mb-12px">
    <NText depth="2">{{ $t('business.after-selecting-an-account-models') }}</NText>
    <NSpace>
      <NButton size="small" :loading="refreshing" @click="refreshAll">{{ $t('business.refresh-models') }}</NButton>
      <NButton size="small" type="primary" secondary :disabled="props.candidates.length >= 50" @click="addCandidate">
        {{ $t('business.add-candidate') }}
      </NButton>
    </NSpace>
  </NSpace>
  <NAlert v-if="hasNonUSD" type="info" :bordered="false" class="mb-12px">
    {{ $t('business.the-public-price-catalog-provides') }}
  </NAlert>
  <NAlert v-if="Object.keys(modelErrors).length" type="warning" :bordered="false" class="mb-12px">
    {{ $t('business.models-could-not-be-fetched-144q1u7') }}
  </NAlert>
  <NDataTable
    :columns="columns"
    :data="props.candidates"
    size="small"
    :bordered="false"
    :scroll-x="1720"
    :max-height="420"
    :row-key="row => `${row.provider_account_id}-${row.upstream_model}-${props.candidates.indexOf(row)}`"
  />
</template>
