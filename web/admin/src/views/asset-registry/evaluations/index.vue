<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { NButton, NInput, NPopconfirm, NSelect, NSpace, NTag } from 'naive-ui';
import { useRoute } from 'vue-router';
import type { DataTableColumns } from 'naive-ui';
import {
  fetchAllPromptEvaluationSets,
  fetchAssetDetail,
  fetchAssets,
  fetchCreatePromptEvaluationSet,
  fetchDeletePromptEvaluationSet,
  fetchEvaluatePrompt,
  fetchModelServices,
  fetchPromptEvaluationSets,
  fetchUpdatePromptEvaluationSet
} from '@/service/api';
import { formatTime } from '@/utils/admin-status';
import { formatCurrency } from '@/utils/currency';
import JsonEditor from '../prompt-templates/modules/json-editor.vue';

defineOptions({ name: 'AssetRegistryEvaluations' });

interface CaseDraft {
  id?: string;
  input: Record<string, unknown>;
  inputText: string;
  match_type: 'exact' | 'contains' | 'regex';
  expected: string;
}

const loading = ref(true);
const detailLoading = ref(false);
const pageMode = ref<'list' | 'detail' | 'edit'>('list');
const route = useRoute();
const saving = ref(false);
const running = ref(false);
const editing = ref(false);
const creating = ref(false);
const overviews = ref<Api.Management.EvaluationSetOverview[]>([]);
const assets = ref<Api.Management.Asset[]>([]);
const models = ref<Api.Management.ModelService[]>([]);
const selectedID = ref<string | null>(null);
const selectedDetail = ref<Api.Management.EvaluationSet | null>(null);
const selectedVersionID = ref<string | null>(null);
const selectedModelIDs = ref<string[]>([]);
const versionOptions = ref<{ label: string; value: string }[]>([]);
const runs = ref<Api.Management.PromptRun[]>([]);
const summary = ref<Api.Management.EvaluationSummary | null>(null);
const runCosts = computed(() => {
  const totals = new Map<string, number>();
  for (const run of runs.value) totals.set(run.cost_currency, (totals.get(run.cost_currency) ?? 0) + run.cost);
  return Array.from(totals, ([currency, total]) => ({ currency, total }));
});
const form = ref({ asset_id: '', name: '', description: '', cases: [] as CaseDraft[] });

const selected = computed(() => overviews.value.find(item => item.id === selectedID.value) ?? null);
const modelOptions = computed(() => models.value.filter(item => item.status === 'active').map(item => ({ label: item.name, value: item.id })));
const assetOptions = computed(() => assets.value.filter(item => item.status !== 'archived').map(item => ({ label: item.name, value: item.id })));
const matchOptions = [
  { label: '精确匹配', value: 'exact' },
  { label: '包含内容', value: 'contains' },
  { label: '正则匹配', value: 'regex' }
];

const overviewColumns: DataTableColumns<Api.Management.EvaluationSetOverview> = [
  { title: '测试集名称', key: 'name', minWidth: 180 },
  { title: '所属模板', key: 'asset_name', minWidth: 180 },
  { title: '用例数', key: 'case_count', width: 90 },
  { title: '运行数', key: 'run_count', width: 90 },
  {
    title: '历史匹配率', key: 'accuracy', width: 120,
    render: row => h(NTag, { size: 'small', type: row.accuracy === null ? 'default' : row.accuracy >= 0.8 ? 'success' : 'warning' }, { default: () => accuracyText(row.accuracy) })
  },
  { title: '最近运行', key: 'last_run_at', width: 180, render: row => row.last_run_at ? formatTime(row.last_run_at) : '暂无运行' },
  {
    title: '操作', key: 'actions', width: 140,
    render: row => h(NSpace, { size: 4 }, {
      default: () => [
        h(NButton, { size: 'small', quaternary: true, type: 'primary', onClick: () => selectSet(row.id) }, { default: () => '管理' }),
        h(NPopconfirm, { onPositiveClick: () => deleteOverview(row) }, {
          trigger: () => h(NButton, { size: 'small', quaternary: true, type: 'error' }, { default: () => '删除' }),
          default: () => '删除测试集不会删除已有运行记录，确认继续？'
        })
      ]
    })
  }
];

const caseColumns: DataTableColumns<CaseDraft> = [
  {
    title: '输入变量',
    key: 'input',
    minWidth: 300,
    render: row => h(JsonEditor, {
      value: row.inputText,
      height: 140,
      required: true,
      disabled: !editing.value,
      'onUpdate:value': value => row.inputText = value
    })
  },
  {
    title: '匹配规则',
    key: 'match_type',
    width: 140,
    render: row => editing.value ? h(NSelect, {
      value: row.match_type,
      options: matchOptions,
      'onUpdate:value': value => row.match_type = value
    }) : h(NTag, { size: 'small', bordered: false }, { default: () => matchOptions.find(option => option.value === row.match_type)?.label ?? row.match_type })
  },
  {
    title: '期望内容',
    key: 'expected',
    minWidth: 220,
    render: row => h(NInput, {
      value: row.expected,
      type: 'textarea',
      rows: 5,
      readonly: !editing.value,
      'onUpdate:value': value => row.expected = value
    })
  },
  {
    title: '',
    key: 'actions',
    width: 65,
    render: row => h(NButton, {
      size: 'small', quaternary: true, type: 'error', disabled: !editing.value,
      onClick: () => form.value.cases = form.value.cases.filter(item => item !== row)
    }, { default: () => '移除' })
  }
];

const runColumns: DataTableColumns<Api.Management.PromptRun> = [
  { title: '模型服务', key: 'model_service_name', minWidth: 150 },
  { title: '状态', key: 'status', width: 90, render: row => h(NTag, { size: 'small', type: row.status === 'success' ? 'success' : 'error' }, { default: () => row.status === 'success' ? '成功' : '失败' }) },
  { title: '匹配', key: 'matched', width: 90, render: row => row.matched === null ? '-' : row.matched ? '通过' : '未通过' },
  { title: '令牌', key: 'tokens', width: 110, render: row => `${row.input_tokens} / ${row.output_tokens}` },
  { title: '成本', key: 'cost', width: 120, render: row => formatCurrency(row.cost, row.cost_currency) },
  { title: '耗时', key: 'total_ms', width: 100, render: row => `${row.total_ms} ms` }
];

onMounted(async () => {
  loading.value = true;
  try {
    const [setRes, assetRes, modelRes] = await Promise.all([fetchAllPromptEvaluationSets(), fetchAssets(), fetchModelServices()]);
    if (!setRes.error && setRes.data) overviews.value = setRes.data;
    if (!assetRes.error && assetRes.data) assets.value = assetRes.data;
    if (!modelRes.error && modelRes.data) models.value = modelRes.data;
    const preferred = overviews.value.find(item => item.asset_id === route.query.asset);
    if (preferred) await selectSet(preferred.id);
  } finally {
    loading.value = false;
  }
});

async function selectSet(id: string) {
  pageMode.value = 'detail';
  selectedID.value = id;
  selectedDetail.value = null;
  detailLoading.value = true;
  editing.value = false;
  creating.value = false;
  runs.value = [];
  summary.value = null;
  try {
    const current = overviews.value.find(item => item.id === id);
    if (!current) return;
    const [detailRes, assetRes] = await Promise.all([fetchPromptEvaluationSets(current.asset_id), fetchAssetDetail(current.asset_id)]);
    if (!detailRes.error && detailRes.data) {
      const detail = detailRes.data.find(item => item.id === id);
      if (detail) selectedDetail.value = detail;
      if (detail) form.value = toForm(detail, current.asset_id);
    }
    if (!assetRes.error && assetRes.data) {
      versionOptions.value = assetRes.data.versions.map(version => ({
        label: `版本 ${version.version}${version.current ? '（当前使用）' : ''}`,
        value: version.id
      }));
      selectedVersionID.value = assetRes.data.current_version_id ?? assetRes.data.versions.find(item => item.version === assetRes.data.latest_version)?.id ?? null;
    }
  } finally {
    detailLoading.value = false;
  }
}

function toForm(detail: Api.Management.EvaluationSet, assetID: string) {
  return {
    asset_id: assetID,
    name: detail.name,
    description: detail.description,
    cases: detail.cases.map(item => ({
      id: item.id, input: item.input, inputText: JSON.stringify(item.input, null, 2),
      match_type: item.match_type, expected: item.expected
    }))
  };
}

function newCase(): CaseDraft {
  return { input: {}, inputText: '{}', match_type: 'contains', expected: '' };
}

function startCreate() {
  pageMode.value = 'edit';
  selectedID.value = null;
  selectedDetail.value = null;
  creating.value = true;
  editing.value = true;
  runs.value = [];
  summary.value = null;
  versionOptions.value = [];
  selectedVersionID.value = null;
  form.value = { asset_id: assetOptions.value[0]?.value ?? '', name: '', description: '', cases: [newCase()] };
}

function startEdit() {
  creating.value = false;
  editing.value = true;
  pageMode.value = 'edit';
}

function cancelEdit() {
  if (creating.value) {
    pageMode.value = 'list';
    creating.value = false;
    editing.value = false;
  } else if (selectedID.value) {
    void selectSet(selectedID.value);
  }
}

async function deleteOverview(row: Api.Management.EvaluationSetOverview) {
  const { error } = await fetchDeletePromptEvaluationSet(row.asset_id, row.id);
  if (!error) {
    await reloadList();
    window.$message?.success('测试集已删除');
  }
}

async function save() {
  if (!form.value.asset_id || !form.value.name.trim()) {
    window.$message?.warning('请选择所属模板并填写测试集名称');
    return;
  }
  const cases: Api.Management.EvaluationCase[] = [];
  for (const item of form.value.cases) {
    try {
      const input = JSON.parse(item.inputText);
      if (!input || typeof input !== 'object' || Array.isArray(input)) throw new Error('object required');
      item.input = input;
    } catch {
      window.$message?.warning('每个用例的输入都必须是合法 JSON 对象');
      return;
    }
    if (!item.expected) {
      window.$message?.warning('请填写每个用例的期望内容');
      return;
    }
    cases.push({ id: item.id, input: item.input, match_type: item.match_type, expected: item.expected });
  }
  if (!cases.length) {
    window.$message?.warning('至少保留一个测试用例');
    return;
  }
  saving.value = true;
  try {
    const params = { name: form.value.name.trim(), description: form.value.description.trim(), cases };
    if (creating.value) {
      const { data, error } = await fetchCreatePromptEvaluationSet(form.value.asset_id, params);
      if (error || !data) return;
      await reloadList();
      await selectSet(data.id);
    } else if (selectedID.value) {
      const { error } = await fetchUpdatePromptEvaluationSet(form.value.asset_id, selectedID.value, params);
      if (error) return;
      await reloadList();
      await selectSet(selectedID.value);
    }
    editing.value = false;
    creating.value = false;
    window.$message?.success('测试集已保存');
  } finally {
    saving.value = false;
  }
}

async function reloadList() {
  const { data, error } = await fetchAllPromptEvaluationSets();
  if (!error && data) overviews.value = data;
}

async function removeSet() {
  if (!selected.value) return;
  const { error } = await fetchDeletePromptEvaluationSet(selected.value.asset_id, selected.value.id);
  if (error) return;
  selectedID.value = null;
  selectedDetail.value = null;
  await reloadList();
  pageMode.value = 'list';
  window.$message?.success('测试集已删除');
}

async function runEvaluation() {
  if (!selected.value || !selectedVersionID.value) return;
  running.value = true;
  try {
    const { data, error } = await fetchEvaluatePrompt(selected.value.asset_id, {
      version_id: selectedVersionID.value,
      evaluation_set_id: selected.value.id,
      model_service_ids: selectedModelIDs.value.length ? selectedModelIDs.value : undefined
    });
    if (!error && data) {
      runs.value = data.runs;
      summary.value = data.summary;
      await reloadList();
      window.$message?.success('评估已完成');
    }
  } finally {
    running.value = false;
  }
}

function accuracyText(value: number | null) {
  return value === null ? '暂无运行' : `${(value * 100).toFixed(1)}%`;
}
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard v-if="pageMode === 'list'" title="测试集与评估" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="reloadList">刷新</NButton>
          <NButton type="primary" @click="startCreate">新建测试集</NButton>
        </NSpace>
      </template>
      <NDataTable :columns="overviewColumns" :data="overviews" :loading="loading" :bordered="false" :scroll-x="1100" :pagination="{ pageSize: 15 }" />
    </NCard>

    <NCard
      v-else
      :title="creating ? '新建测试集' : pageMode === 'edit' ? '编辑测试集' : selected?.name"
      :bordered="false"
      class="card-wrapper"
    >
      <template #header-extra>
        <NSpace>
          <NButton @click="pageMode === 'edit' ? cancelEdit() : pageMode = 'list'">返回</NButton>
          <NButton v-if="pageMode === 'detail'" type="primary" @click="startEdit">编辑测试集</NButton>
          <NPopconfirm v-if="pageMode === 'detail'" @positive-click="removeSet">
            <template #trigger><NButton>删除测试集</NButton></template>
            删除测试集不会删除已有运行记录，确认继续？
          </NPopconfirm>
        </NSpace>
      </template>
      <NSpin :show="detailLoading">
        <NDescriptions v-if="pageMode === 'detail' && selected" :column="3" label-placement="left" bordered>
          <NDescriptionsItem label="所属模板">{{ selected.asset_name }}</NDescriptionsItem>
          <NDescriptionsItem label="用例数">{{ selected.case_count }}</NDescriptionsItem>
          <NDescriptionsItem label="运行数">{{ selected.run_count }}</NDescriptionsItem>
          <NDescriptionsItem label="历史匹配率">{{ accuracyText(selected.accuracy) }}</NDescriptionsItem>
          <NDescriptionsItem label="最近运行">{{ selected.last_run_at ? formatTime(selected.last_run_at) : '暂无运行' }}</NDescriptionsItem>
          <NDescriptionsItem label="用途说明">{{ selected.description || '暂无说明' }}</NDescriptionsItem>
        </NDescriptions>
        <NForm v-else label-placement="left" label-width="90">
          <NGrid cols="1 700:2" :x-gap="16">
            <NFormItemGi label="所属模板"><NInput v-if="!creating" :value="selected?.asset_name" readonly /><NSelect v-else v-model:value="form.asset_id" :options="assetOptions" /></NFormItemGi>
            <NFormItemGi label="测试集名称"><NInput v-model:value="form.name" /></NFormItemGi>
          </NGrid>
          <NFormItem label="用途说明"><NInput v-model:value="form.description" /></NFormItem>
        </NForm>

        <NDivider title-placement="left">测试用例（{{ form.cases.length }} / 50）</NDivider>
        <NDataTable :columns="caseColumns" :data="form.cases" size="small" :bordered="false" :scroll-x="900" :max-height="460" />
        <NSpace v-if="pageMode === 'edit'" class="my-12px">
          <NButton :disabled="form.cases.length >= 50" @click="form.cases.push(newCase())">添加用例</NButton>
          <NButton type="primary" :loading="saving" @click="save">保存测试集</NButton>
        </NSpace>

        <template v-if="pageMode === 'detail'">
          <NDivider title-placement="left">运行评估</NDivider>
          <NGrid cols="1 700:2" :x-gap="16">
            <NFormItemGi label="模板版本" :show-feedback="false"><NSelect v-model:value="selectedVersionID" :options="versionOptions" /></NFormItemGi>
            <NFormItemGi label="模型服务" :show-feedback="false"><NSelect v-model:value="selectedModelIDs" multiple :max="4" :options="modelOptions" placeholder="默认使用模板绑定模型" /></NFormItemGi>
          </NGrid>
          <NButton type="primary" class="my-12px" :loading="running" :disabled="!selectedVersionID" @click="runEvaluation">运行评估</NButton>
          <NCard v-if="summary" size="small" class="mb-12px">
            <NSpace size="large">
              <NStatistic label="运行数" :value="summary.total" />
              <NStatistic label="匹配率" :value="`${(summary.accuracy * 100).toFixed(1)}%`" />
              <NStatistic label="平均耗时" :value="`${summary.average_latency_ms} ms`" />
              <NStatistic v-for="item in runCosts" :key="item.currency" :label="`总成本（${item.currency}）`" :value="formatCurrency(item.total, item.currency)" />
            </NSpace>
          </NCard>
          <NDataTable v-if="runs.length" :columns="runColumns" :data="runs" size="small" :bordered="false" :scroll-x="800" />
        </template>
      </NSpin>
    </NCard>
  </div>
</template>

<style scoped></style>
