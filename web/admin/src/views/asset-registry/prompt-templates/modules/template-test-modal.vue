<script setup lang="ts">
import { computed, h, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { formatTime } from '@/utils/admin-status';
import { formatCurrency } from '@/utils/currency';
import {
  fetchAssetDetail,
  fetchAssetVersion,
  fetchModelServices,
  fetchPromptRunHistory,
  fetchRenderAssetPreview,
  fetchRunPrompt,
  streamPromptRun
} from '@/service/api';
import { $t } from '@/locales';
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

const versionOptions = computed(() =>
  (detail.value?.versions ?? []).map(version => ({
    label: $t('business.versionWithStatus', {
      version: version.version,
      status: version.current
        ? $t('business.current')
        : version.published_at
          ? $t('business.historical-version')
          : $t('business.not-enabled-w1mvaw')
    }),
    value: version.id
  }))
);
const modelOptions = computed(() =>
  models.value.filter(item => item.status === 'active').map(item => ({ label: item.name, value: item.id }))
);

const historyColumns = computed<DataTableColumns<Api.Management.PromptRun>>(() => [
  {
    title: $t('business.time'),
    key: 'created_at',
    width: 165,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.type'),
    key: 'kind',
    width: 80,
    render: row =>
      row.kind === 'evaluation'
        ? $t('business.evaluate')
        : row.kind === 'compare'
          ? $t('business.compare')
          : $t('business.single-run')
  },
  {
    title: $t('business.version'),
    key: 'version',
    width: 75,
    render: row => $t('business.versionValue', { version: row.version })
  },
  {
    title: $t('business.model-service'),
    key: 'model_service_name',
    minWidth: 140
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 85,
    render: row =>
      h(
        NTag,
        {
          size: 'small',
          type: row.status === 'success' ? 'success' : 'error'
        },
        {
          default: () => (row.status === 'success' ? $t('business.success') : $t('business.failed'))
        }
      )
  },
  {
    title: $t('business.tokens'),
    key: 'tokens',
    width: 105,
    render: row => `${row.input_tokens} / ${row.output_tokens}`
  },
  {
    title: $t('business.cost'),
    key: 'cost',
    width: 105,
    render: row => formatCurrency(row.cost, row.cost_currency)
  },
  {
    title: $t('business.latency'),
    key: 'total_ms',
    width: 90,
    render: row => `${row.total_ms} ms`
  }
]);

let sessionGeneration = 0;
watch(
  [() => props.show, () => props.asset?.id],
  ([visible]) => {
    sessionGeneration++;
    running.value = false;
    testing.value = false;
    if (visible) void initialize();
    else {
      abortController?.abort();
      detail.value = null;
      selectedSpec.value = null;
      loading.value = false;
    }
  }
);

watch(selectedVersionID, versionID => {
  if (versionID) void selectVersion(versionID);
});

watch(activeTab, tab => {
  if (tab === 'history') void loadHistory();
});

async function initialize() {
  if (!props.asset) return;
  const generation = sessionGeneration;
  const assetId = props.asset.id;
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
    const [detailRes, modelRes] = await Promise.all([fetchAssetDetail(assetId), fetchModelServices()]);
    if (generation !== sessionGeneration || assetId !== props.asset?.id) return;
    if (!modelRes.error && modelRes.data) models.value = modelRes.data;
    if (detailRes.error || !detailRes.data) return;
    detail.value = detailRes.data;
    const latestID = detailRes.data.versions.find(item => item.version === detailRes.data.latest_version)?.id ?? null;
    selectedVersionID.value = props.preferLatest ? latestID : (detailRes.data.current_version_id ?? latestID);
  } finally {
    if (generation === sessionGeneration) loading.value = false;
  }
}

async function selectVersion(versionID: string) {
  if (!props.asset) return;
  const generation = sessionGeneration;
  const assetId = props.asset.id;
  loading.value = true;
  try {
    const { data, error } = await fetchAssetVersion(assetId, versionID);
    if (generation !== sessionGeneration || assetId !== props.asset?.id || selectedVersionID.value !== versionID || error || !data || !data.spec) return;
    selectedSpec.value = data.spec;
    selectedModelIDs.value = data.spec.model_service_id ? [data.spec.model_service_id] : [];
    resetValues(data.spec);
    rendered.value = '';
    runs.value = [];
    streamingText.value = '';
  } finally {
    if (generation === sessionGeneration) loading.value = false;
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
  const generation = sessionGeneration;
  const assetId = props.asset.id;
  const versionId = selectedVersionID.value;
  testing.value = true;
  try {
    const { data, error } = await fetchRenderAssetPreview(assetId, versionId, { ...values });
    if (generation === sessionGeneration && selectedVersionID.value === versionId && !error && data) {
      rendered.value = data.rendered;
      window.$message?.success($t('business.local-rendering-completed'));
    }
  } finally {
    if (generation === sessionGeneration) testing.value = false;
  }
}

function withModelName(run: Api.Management.PromptRun) {
  if (!run.model_service_name)
    run.model_service_name = models.value.find(item => item.id === run.model_service_id)?.name ?? run.model_service_id;
  return run;
}

async function runModels() {
  if (!props.asset || !selectedVersionID.value || !selectedSpec.value || running.value) return;
  const generation = sessionGeneration;
  const assetId = props.asset.id;
  const versionId = selectedVersionID.value;
  running.value = true;
  runs.value = [];
  streamingText.value = '';
  rendered.value = '';
  abortController?.abort();
  const controller = new AbortController();
  abortController = controller;
  const modelIDs = selectedModelIDs.value.length ? selectedModelIDs.value : undefined;
  try {
    if (modelIDs === undefined || modelIDs.length <= 1) {
      await streamPromptRun(
        assetId,
        {
          version_id: versionId,
          variables: { ...values },
          model_service_ids: modelIDs,
          stream: true
        },
        {
          onDelta: delta => {
            if (generation === sessionGeneration) streamingText.value += delta;
          },
          onResult: result => {
            if (generation !== sessionGeneration) return;
            runs.value = [withModelName(result)];
            streamingText.value = '';
          },
          onError: result => {
            if (generation !== sessionGeneration) return;
            runs.value = [withModelName(result)];
            streamingText.value = '';
          }
        },
        controller.signal
      );
    } else {
      const { data, error } = await fetchRunPrompt(assetId, {
        version_id: versionId,
        variables: { ...values },
        model_service_ids: modelIDs
      });
      if (generation === sessionGeneration && !error && data) runs.value = data.runs.map(withModelName);
    }
    if (generation !== sessionGeneration) return;
    window.$message?.success($t('business.model-run-completed'));
    void loadHistory();
  } catch (error) {
    if (generation === sessionGeneration && !controller.signal.aborted)
      window.$message?.error(error instanceof Error ? error.message : $t('business.model-run-failed'));
  } finally {
    if (generation === sessionGeneration) running.value = false;
  }
}

async function loadHistory() {
  if (!props.asset) return;
  const generation = sessionGeneration;
  const assetId = props.asset.id;
  const { data, error } = await fetchPromptRunHistory(assetId);
  if (generation === sessionGeneration && assetId === props.asset?.id && !error && data) history.value = data.map(withModelName);
}

function showHistoryRun(row: Api.Management.PromptRun) {
  runs.value = [row];
  activeTab.value = 'debug';
}

onBeforeUnmount(() => abortController?.abort());
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="$t('business.model-debugging-workspace')"
    class="w-1100px"
    @update:show="value => emit('update:show', value)"
  >
    <NSpin :show="loading">
      <NTabs v-model:value="activeTab" type="line">
        <NTabPane name="debug" :tab="$t('business.model-debugging')">
          <NGrid cols="1 700:2" :x-gap="12">
            <NFormItemGi :label="$t('business.test-version')" :show-feedback="false">
              <NSelect v-model:value="selectedVersionID" :options="versionOptions" />
            </NFormItemGi>
            <NFormItemGi :label="$t('business.model-service')" :show-feedback="false">
              <NSelect
                v-model:value="selectedModelIDs"
                multiple
                :max="4"
                :options="modelOptions"
                :placeholder="$t('business.the-template-s-bound-model')"
              />
            </NFormItemGi>
          </NGrid>
          <NScrollbar v-if="selectedSpec" style="max-height: 230px" class="mt-12px">
            <NForm label-placement="top">
              <NFormItem
                v-for="variable in selectedSpec.variables"
                :key="variable.name"
                :label="variable.description ? `${variable.name}（${variable.description}）` : variable.name"
                :required="variable.required"
              >
                <NInput
                  v-if="variable.type === 'string'"
                  :value="String(values[variable.name] ?? '')"
                  type="textarea"
                  :rows="2"
                  :placeholder="$t('business.enter-test-content')"
                  @update:value="value => (values[variable.name] = value)"
                />
                <NInputNumber
                  v-else-if="variable.type === 'number' || variable.type === 'integer'"
                  :value="numberValue(values[variable.name])"
                  :precision="variable.type === 'integer' ? 0 : undefined"
                  class="w-full"
                  @update:value="value => (values[variable.name] = value)"
                />
                <NSwitch
                  v-else
                  :value="Boolean(values[variable.name])"
                  @update:value="value => (values[variable.name] = value)"
                />
              </NFormItem>
              <NEmpty
                v-if="selectedSpec.variables.length === 0"
                :description="$t('business.this-template-does-not-require')"
              />
            </NForm>
          </NScrollbar>
          <NSpace class="my-12px">
            <NButton :loading="testing" :disabled="!selectedSpec || running" @click="runLocalTest">
              {{ $t('business.render-template-only') }}
            </NButton>
            <NButton type="primary" :loading="running" :disabled="!selectedSpec" @click="runModels">
              {{ $t('business.run-real-model') }}
            </NButton>
            <NButton v-if="running" @click="abortController?.abort()">{{ $t('business.stop') }}</NButton>
          </NSpace>

          <NCard v-if="streamingText" size="small" :title="$t('business.live-model-output')" class="mb-12px">
            <MarkdownPreview :source="streamingText" />
          </NCard>
          <NScrollbar v-if="runs.length" style="max-height: 500px">
            <TemplateRunResult v-for="run in runs" :key="run.id" :run="run" />
          </NScrollbar>
          <NCard v-if="rendered" size="small" :title="$t('business.local-template-result')">
            <template #header-extra>
              <NRadioGroup v-model:value="resultMode" size="small">
                <NRadioButton value="rendered">{{ $t('business.rendered-result') }}</NRadioButton>
                <NRadioButton value="raw">{{ $t('business.raw-text') }}</NRadioButton>
              </NRadioGroup>
            </template>
            <MarkdownPreview v-if="resultMode === 'rendered'" :source="rendered" />
            <NInput v-else :value="rendered" type="textarea" readonly :rows="9" />
          </NCard>
        </NTabPane>
        <NTabPane name="history" :tab="$t('business.run-history-ipd9yg')">
          <NDataTable
            :columns="historyColumns"
            :data="history"
            size="small"
            :bordered="false"
            :max-height="520"
            :pagination="{ pageSize: 15 }"
            :row-props="
              row => ({
                style: 'cursor:pointer',
                onClick: () => showHistoryRun(row)
              })
            "
          />
        </NTabPane>
      </NTabs>
    </NSpin>
    <template #footer>
      <NSpace justify="end">
        <NButton @click="emit('update:show', false)">{{ $t('business.close') }}</NButton>
      </NSpace>
    </template>
  </NModal>
</template>
