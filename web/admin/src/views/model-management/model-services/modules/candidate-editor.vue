<script setup lang="ts">
import { computed, h, onMounted, reactive, ref, watch } from 'vue';
import { NButton, NInputNumber, NSelect, NSpace } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { fetchProviderModels } from '@/service/api';
import { settlementCurrencyOptions } from '@/utils/currency';

interface CandidateDraft {
  provider_account_id: string | null;
  upstream_model: string;
  priority: number;
  weight: number;
  input_cost_per_million: number;
  output_cost_per_million: number;
  quality_score: number;
  cost_currency: string;
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

const statusOptions = [{ label: '启用', value: 'active' }, { label: '停用', value: 'disabled' }];

const columns: DataTableColumns<CandidateDraft> = [
  {
    title: '供应商账号', key: 'provider_account_id', width: 210,
    render: row => h(NSelect, {
      value: row.provider_account_id, options: props.accountOptions, filterable: true, size: 'small',
      placeholder: '选择账号',
      'onUpdate:value': (value: string) => {
        row.provider_account_id = value;
        row.upstream_model = '';
        row.cost_currency = props.accountOptions.find(item => item.value === value)?.currency ?? 'USD';
        void loadModels(value);
      }
    })
  },
  {
    title: '上游模型', key: 'upstream_model', width: 240,
    render: row => h(NSelect, {
      value: row.upstream_model || null,
      options: (modelCache[row.provider_account_id ?? ''] ?? []).map(model => ({ label: model.id, value: model.id })),
      loading: modelLoading[row.provider_account_id ?? ''], filterable: true, tag: true, size: 'small',
      placeholder: modelLoading[row.provider_account_id ?? ''] ? '正在获取模型' : '选择或输入模型',
      'onUpdate:value': (value: string) => selectModel(row, value)
    })
  },
  { title: '优先级', key: 'priority', width: 100, render: row => h(NInputNumber, { value: row.priority, min: 1, size: 'small', 'onUpdate:value': value => row.priority = value ?? 100 }) },
  { title: '权重', key: 'weight', width: 90, render: row => h(NInputNumber, { value: row.weight, min: 0, size: 'small', 'onUpdate:value': value => row.weight = value ?? 1 }) },
  { title: '输入价格（美元/百万）', key: 'input_cost_per_million', width: 170, render: row => h(NInputNumber, { value: row.input_cost_per_million, min: 0, size: 'small', 'onUpdate:value': value => row.input_cost_per_million = value ?? 0 }) },
  { title: '输出价格（美元/百万）', key: 'output_cost_per_million', width: 170, render: row => h(NInputNumber, { value: row.output_cost_per_million, min: 0, size: 'small', 'onUpdate:value': value => row.output_cost_per_million = value ?? 0 }) },
  { title: '质量分', key: 'quality_score', width: 105, render: row => h(NInputNumber, { value: row.quality_score, min: 0, max: 1, step: 0.05, size: 'small', 'onUpdate:value': value => row.quality_score = value ?? 0.5 }) },
  { title: '结算币种', key: 'cost_currency', width: 140, render: row => h(NSelect, { value: row.cost_currency, options: settlementCurrencyOptions, size: 'small', 'onUpdate:value': value => row.cost_currency = value }) },
  { title: '状态', key: 'status', width: 95, render: row => h(NSelect, { value: row.status, options: statusOptions, size: 'small', 'onUpdate:value': value => row.status = value }) },
  {
    title: '', key: 'actions', width: 65,
    render: row => h(NButton, {
      size: 'small', quaternary: true, type: 'error', disabled: props.candidates.length <= 1,
      onClick: () => emit('update:candidates', props.candidates.filter(item => item !== row))
    }, { default: () => '删除' })
  }
];

onMounted(refreshAll);
watch(() => props.candidates.map(item => item.provider_account_id), () => { void refreshAll(); });

function newCandidate(): CandidateDraft {
  return {
    provider_account_id: props.accountOptions[0]?.value ?? null,
    upstream_model: '', priority: 100, weight: 1,
    input_cost_per_million: 0, output_cost_per_million: 0, quality_score: 0.5,
    cost_currency: props.accountOptions[0]?.currency ?? 'USD', status: 'active'
  };
}

function addCandidate() {
  if (props.candidates.length >= 50) {
    window.$message?.warning('一个模型服务最多添加 50 个候选');
    return;
  }
  const candidate = newCandidate();
  emit('update:candidates', [...props.candidates, candidate]);
  if (candidate.provider_account_id) void loadModels(candidate.provider_account_id);
}

async function refreshAll() {
  refreshing.value = true;
  try {
    const ids = [...new Set(props.candidates.map(item => item.provider_account_id).filter((id): id is string => Boolean(id)))];
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
      modelErrors[accountID] = '部分账号未能自动获取模型，可直接输入模型标识。';
      return;
    }
    modelCache[accountID] = data.models;
  } finally {
    modelLoading[accountID] = false;
  }
}

function selectModel(row: CandidateDraft, modelID: string) {
  row.upstream_model = modelID;
  const model = (modelCache[row.provider_account_id ?? ''] ?? []).find(item => item.id === modelID);
  if (row.cost_currency === 'USD' && model?.input_cost_per_million !== null && model?.input_cost_per_million !== undefined) {
    row.input_cost_per_million = model.input_cost_per_million;
  }
  if (row.cost_currency === 'USD' && model?.output_cost_per_million !== null && model?.output_cost_per_million !== undefined) {
    row.output_cost_per_million = model.output_cost_per_million;
  }
}
</script>

<template>
  <NSpace justify="space-between" class="mb-12px">
    <NText depth="2">选择账号后自动获取模型，已知价格会随模型补全；质量分是路由初始评分，可按实际效果调整。</NText>
    <NSpace>
      <NButton size="small" :loading="refreshing" @click="refreshAll">重新获取模型</NButton>
      <NButton size="small" type="primary" secondary :disabled="props.candidates.length >= 50" @click="addCandidate">添加候选</NButton>
    </NSpace>
  </NSpace>
  <NAlert v-if="hasNonUSD" type="info" :bordered="false" class="mb-12px">公开价格目录仅提供美元参考价，其他结算币种请按厂商实际价格填写，系统不会自动换算。</NAlert>
  <NAlert v-if="Object.keys(modelErrors).length" type="warning" :bordered="false" class="mb-12px">部分账号未能自动获取模型，可直接在模型列输入标识。</NAlert>
  <NDataTable
    :columns="columns"
    :data="props.candidates"
    size="small"
    :bordered="false"
    :scroll-x="1200"
    :max-height="420"
    :row-key="row => `${row.provider_account_id}-${row.upstream_model}-${props.candidates.indexOf(row)}`"
  />
</template>
