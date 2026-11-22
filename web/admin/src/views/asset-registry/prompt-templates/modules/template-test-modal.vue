<script setup lang="ts">
import { computed, h, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import {
  fetchAssetDetail,
  fetchAssetVersion,
  fetchModelServices,
  fetchPromptRunHistory,
  fetchRenderAssetPreview,
  fetchRunPrompt,
  streamPromptRun
} from '@/service/api';
import { formatTime } from '@/utils/admin-status';
import MarkdownPreview from './markdown-preview.vue';
import TemplateRunResult from './template-run-result.vue';

const props = defineProps<{
  show: boolean;
  asset: Api.Management.Asset | null;
  preferLatest: boolean;
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
}>();

const loading = ref(false);
const testing = ref(false);
const running = ref(false);
const activeTab = ref('debug');
const detail = ref<Api.Management.AssetDetail | null>(null);
const models = ref<Api.Management.ModelService[]>([]);
const selectedVersionID = ref<string | null>(null);
const selectedModelIDs = ref<string[]>([]);
const selectedSpec = ref<Api.Management.PromptSpec | null>(null);
const rendered = ref('');
const resultMode = ref<'rendered' | 'raw'>('rendered');
const runs = ref<Api.Management.PromptRun[]>([]);
const history = ref<Api.Management.PromptRun[]>([]);
const streamingText = ref('');
const values = reactive<Record<string, unknown>>({});
let abortController: AbortController | null = null;

const versionOptions = computed(() => (detail.value?.versions ?? []).map(version => ({
  label: `版本 ${version.version}${version.current ? '（当前使用）' : version.published_at ? '（历史版本）' : '（尚未启用）'}`,
  value: version.id
})));
const modelOptions = computed(() => models.value.filter(item => item.status === 'active').map(item => ({ label: item.name, value: item.id })));

const historyColumns: DataTableColumns<Api.Management.PromptRun> = [
  { title: '时间', key: 'created_at', width: 165, render: row => formatTime(row.created_at) },
  { title: '类型', key: 'kind', width: 80, render: row => row.kind === 'evaluation' ? '评估' : row.kind === 'compare' ? '对比' : '单次' },
  { title: '版本', key: 'version', width: 75, render: row => `版本 ${row.version}` },
  { title: '模型服务', key: 'model_service_name', minWidth: 140 },
  { title: '状态', key: 'status', width: 85, render: row => h(NTag, { size: 'small', type: row.status === 'success' ? 'success' : 'error' }, { default: () => row.status === 'success' ? '成功' : '失败' }) },
  { title: 'Token', key: 'tokens', width: 105, render: row => `${row.input_tokens} / ${row.output_tokens}` },
  { title: '成本', key: 'cost', width: 105, render: row => `$${row.cost.toFixed(6)}` },
  { title: '耗时', key: 'total_ms', width: 90, render: row => `${row.total_ms} ms` }
];

watch(() => props.show, visible => {
  if (visible) void initialize();
  else abortController?.abort();
});

watch(selectedVersionID, versionID => {
  if (versionID) void selectVersion(versionID);
});

watch(activeTab, tab => {
  if (tab === 'history') void loadHistory();
});

async function initialize() {
  if (!props.asset) return;
  loading.value = true;
  rendered.value = '';
  runs.value = [];
  history.value = [];
  streamingText.value = '';
  activeTab.value = 'debug';
  detail.value = null;
  selectedSpec.value = null;
  selectedVersionID.value = null;
  try {
    const [detailRes, modelRes] = await Promise.all([fetchAssetDetail(props.asset.id), fetchModelServices()]);
    if (!modelRes.error && modelRes.data) models.value = modelRes.data;
    if (detailRes.error || !detailRes.data) return;
    detail.value = detailRes.data;
    const latestID = detailRes.data.versions.find(item => item.version === detailRes.data.latest_version)?.id ?? null;
    selectedVersionID.value = props.preferLatest ? latestID : detailRes.data.current_version_id ?? latestID;
  } finally {
    loading.value = false;
  }
}

async function selectVersion(versionID: string) {
  if (!props.asset) return;
  loading.value = true;
  try {
    const { data, error } = await fetchAssetVersion(props.asset.id, versionID);
    if (error || !data || !data.spec) return;
    selectedSpec.value = data.spec;
    selectedModelIDs.value = data.spec.model_service_id ? [data.spec.model_service_id] : [];
    resetValues(data.spec);
    rendered.value = '';
    runs.value = [];
    streamingText.value = '';
  } finally {
    loading.value = false;
  }
}

function resetValues(spec: Api.Management.PromptSpec) {
  for (const key of Object.keys(values)) delete values[key];
  for (const variable of spec.variables) {
    if (variable.default !== undefined && variable.default !== null) values[variable.name] = variable.default;
    else if (variable.type === 'boolean') values[variable.name] = false;
    else if (variable.type === 'string') values[variable.name] = '';
    else values[variable.name] = null;
  }
}

function numberValue(value: unknown) {
  return typeof value === 'number' ? value : null;
}

async function runLocalTest() {
  if (!props.asset || !selectedVersionID.value) return;
  testing.value = true;
  try {
    const { data, error } = await fetchRenderAssetPreview(props.asset.id, selectedVersionID.value, { ...values });
    if (!error && data) {
      rendered.value = data.rendered;
      window.$message?.success('本地渲染完成');
    }
  } finally {
    testing.value = false;
  }
}

function withModelName(run: Api.Management.PromptRun) {
  if (!run.model_service_name) run.model_service_name = models.value.find(item => item.id === run.model_service_id)?.name ?? run.model_service_id;
  return run;
}

async function runModels() {
  if (!props.asset || !selectedVersionID.value || !selectedSpec.value) return;
  running.value = true;
  runs.value = [];
  streamingText.value = '';
  rendered.value = '';
  abortController?.abort();
  abortController = new AbortController();
  const modelIDs = selectedModelIDs.value.length ? selectedModelIDs.value : undefined;
  try {
    if (selectedModelIDs.value.length <= 1) {
      await streamPromptRun(props.asset.id, {
        version_id: selectedVersionID.value, variables: { ...values }, model_service_ids: modelIDs, stream: true
      }, {
        onDelta: delta => streamingText.value += delta,
        onResult: result => {
          runs.value = [withModelName(result)];
          streamingText.value = '';
        },
        onError: result => {
          runs.value = [withModelName(result)];
          streamingText.value = '';
        }
      }, abortController.signal);
    } else {
      const { data, error } = await fetchRunPrompt(props.asset.id, {
        version_id: selectedVersionID.value, variables: { ...values }, model_service_ids: modelIDs
      });
      if (!error && data) runs.value = data.runs.map(withModelName);
    }
    window.$message?.success('模型运行已结束');
    void loadHistory();
  } catch (error) {
    if (!abortController.signal.aborted) window.$message?.error(error instanceof Error ? error.message : '模型运行失败');
  } finally {
    running.value = false;
  }
}

async function loadHistory() {
  if (!props.asset) return;
  const { data, error } = await fetchPromptRunHistory(props.asset.id);
  if (!error && data) history.value = data.map(withModelName);
}

function showHistoryRun(row: Api.Management.PromptRun) {
  runs.value = [row];
  activeTab.value = 'debug';
}

onBeforeUnmount(() => abortController?.abort());
</script>

<template>
  <NModal :show="props.show" preset="card" title="模型调试工作台" class="w-1100px" @update:show="value => emit('update:show', value)">
    <NSpin :show="loading">
      <NTabs v-model:value="activeTab" type="line">
        <NTabPane name="debug" tab="模型调试">
          <NGrid cols="1 700:2" :x-gap="12">
            <NFormItemGi label="测试版本" :show-feedback="false"><NSelect v-model:value="selectedVersionID" :options="versionOptions" /></NFormItemGi>
            <NFormItemGi label="模型服务" :show-feedback="false">
              <NSelect v-model:value="selectedModelIDs" multiple :max="4" :options="modelOptions" placeholder="默认使用模板绑定模型；选择多个模型可进行对比" />
            </NFormItemGi>
          </NGrid>
          <NScrollbar v-if="selectedSpec" style="max-height: 230px" class="mt-12px">
            <NForm label-placement="top">
              <NFormItem v-for="variable in selectedSpec.variables" :key="variable.name" :label="variable.description ? `${variable.name}（${variable.description}）` : variable.name" :required="variable.required">
                <NInput v-if="variable.type === 'string'" :value="String(values[variable.name] ?? '')" type="textarea" :rows="2" placeholder="请输入测试内容" @update:value="value => values[variable.name] = value" />
                <NInputNumber v-else-if="variable.type === 'number' || variable.type === 'integer'" :value="numberValue(values[variable.name])" :precision="variable.type === 'integer' ? 0 : undefined" class="w-full" @update:value="value => values[variable.name] = value" />
                <NSwitch v-else :value="Boolean(values[variable.name])" @update:value="value => values[variable.name] = value" />
              </NFormItem>
              <NEmpty v-if="selectedSpec.variables.length === 0" description="这个模板不需要填写变量" />
            </NForm>
          </NScrollbar>
          <NSpace class="my-12px">
            <NButton :loading="testing" :disabled="!selectedSpec || running" @click="runLocalTest">仅渲染模板</NButton>
            <NButton type="primary" :loading="running" :disabled="!selectedSpec" @click="runModels">运行真实模型</NButton>
            <NButton v-if="running" @click="abortController?.abort()">停止</NButton>
          </NSpace>

          <NCard v-if="streamingText" size="small" title="实时模型输出" class="mb-12px">
            <MarkdownPreview :source="streamingText" />
          </NCard>
          <NScrollbar v-if="runs.length" style="max-height: 500px">
            <TemplateRunResult v-for="run in runs" :key="run.id" :run="run" />
          </NScrollbar>
          <NCard v-if="rendered" size="small" title="本地模板结果">
            <template #header-extra>
              <NRadioGroup v-model:value="resultMode" size="small"><NRadioButton value="rendered">渲染效果</NRadioButton><NRadioButton value="raw">原始文本</NRadioButton></NRadioGroup>
            </template>
            <MarkdownPreview v-if="resultMode === 'rendered'" :source="rendered" />
            <NInput v-else :value="rendered" type="textarea" readonly :rows="9" />
          </NCard>
        </NTabPane>
        <NTabPane name="history" tab="运行历史">
          <NDataTable
            :columns="historyColumns"
            :data="history"
            size="small"
            :bordered="false"
            :max-height="520"
            :pagination="{ pageSize: 15 }"
            :row-props="row => ({ style: 'cursor:pointer', onClick: () => showHistoryRun(row) })"
          />
        </NTabPane>
      </NTabs>
    </NSpin>
    <template #footer><NSpace justify="end"><NButton @click="emit('update:show', false)">关闭</NButton></NSpace></template>
  </NModal>
</template>
