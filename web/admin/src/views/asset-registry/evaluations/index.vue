<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { NButton, NInput, NPopconfirm, NSelect, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { formatTime } from '@/utils/admin-status';
import { formatCurrency } from '@/utils/currency';
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
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';
import JsonEditor from '../prompt-templates/modules/json-editor.vue';

defineOptions({ name: 'AssetRegistryEvaluations' });

const { has } = usePermission();
const canManage = computed(() => has('evaluation:manage'));

interface CaseDraft {
  id?: string;
  input: Record<string, unknown>;
  inputText: string;
  match_type: 'exact' | 'contains' | 'regex';
  expected: string;
}

const loading = ref(true);
const detailLoading = ref(false);
let selectionGeneration = 0;
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
const form = ref({
  asset_id: '',
  name: '',
  description: '',
  cases: [] as CaseDraft[]
});

const selected = computed(() => overviews.value.find(item => item.id === selectedID.value) ?? null);
const modelOptions = computed(() =>
  models.value.filter(item => item.status === 'active').map(item => ({ label: item.name, value: item.id }))
);
const assetOptions = computed(() =>
  assets.value.filter(item => item.status !== 'archived').map(item => ({ label: item.name, value: item.id }))
);
const matchOptions = computed(() => [
  { label: $t('business.exact-match'), value: 'exact' },
  { label: $t('business.contains'), value: 'contains' },
  { label: $t('business.regex-match'), value: 'regex' }
]);

const overviewColumns = computed<DataTableColumns<Api.Management.EvaluationSetOverview>>(() => [
  { title: $t('business.evaluation-set-name'), key: 'name', ellipsis: { tooltip: true }, minWidth: 180 },
  { title: $t('business.template'), key: 'asset_name', ellipsis: { tooltip: true }, minWidth: 180 },
  { title: $t('business.cases'), key: 'case_count', width: 90 },
  { title: $t('business.runs'), key: 'run_count', width: 90 },
  {
    title: $t('business.historical-match-rate'),
    key: 'accuracy',
    width: 130,
    render: row =>
      h(
        NTag,
        {
          size: 'small',
          type: row.accuracy === null ? 'default' : row.accuracy >= 0.8 ? 'success' : 'warning'
        },
        { default: () => accuracyText(row.accuracy) }
      )
  },
  {
    title: $t('business.last-run'),
    key: 'last_run_at',
    width: 180,
    render: row => (row.last_run_at ? formatTime(row.last_run_at) : $t('business.no-runs'))
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 165,
    render: row =>
      h(
        NSpace,
        { size: 4 },
        {
          default: () => [
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                type: 'primary',
                onClick: () => selectSet(row.id)
              },
              { default: () => $t('business.manage') }
            ),
            ...(canManage.value
              ? [
                  h(
                    NPopconfirm,
                    { onPositiveClick: () => deleteOverview(row) },
                    {
                      trigger: () =>
                        h(
                          NButton,
                          { size: 'small', quaternary: true, type: 'error' },
                          { default: () => $t('business.delete') }
                        ),
                      default: () => $t('business.deleting-this-evaluation-set-does')
                    }
                  )
                ]
              : [])
          ]
        }
      )
  }
]);

const caseColumns = computed<DataTableColumns<CaseDraft>>(() => [
  {
    title: $t('business.input-variables'),
    key: 'input',
    minWidth: 300,
    render: row =>
      h(JsonEditor, {
        value: row.inputText,
        height: 140,
        required: true,
        disabled: !editing.value,
        'onUpdate:value': value => (row.inputText = value)
      })
  },
  {
    title: $t('business.match-rule'),
    key: 'match_type',
    width: 140,
    render: row =>
      editing.value
        ? h(NSelect, {
            value: row.match_type,
            options: matchOptions.value,
            'onUpdate:value': value => (row.match_type = value)
          })
        : h(
            NTag,
            { size: 'small', bordered: false },
            {
              default: () => matchOptions.value.find(option => option.value === row.match_type)?.label ?? row.match_type
            }
          )
  },
  {
    title: $t('business.expected-content'),
    key: 'expected',
    minWidth: 220,
    render: row =>
      h(NInput, {
        value: row.expected,
        type: 'textarea',
        rows: 5,
        readonly: !editing.value,
        'onUpdate:value': value => (row.expected = value)
      })
  },
  {
    title: '',
    key: 'actions',
    width: 65,
    render: row =>
      h(
        NButton,
        {
          size: 'small',
          quaternary: true,
          type: 'error',
          disabled: !editing.value,
          onClick: () => (form.value.cases = form.value.cases.filter(item => item !== row))
        },
        { default: () => $t('business.remove') }
      )
  }
]);

const runColumns = computed<DataTableColumns<Api.Management.PromptRun>>(() => [
  {
    title: $t('business.model-service'),
    key: 'model_service_name',
    minWidth: 150
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 90,
    render: row =>
      h(
        NTag,
        { size: 'small', type: row.status === 'success' ? 'success' : 'error' },
        {
          default: () => (row.status === 'success' ? $t('business.success') : $t('business.failed'))
        }
      )
  },
  {
    title: $t('business.match'),
    key: 'matched',
    width: 90,
    render: row => (row.matched === null ? '-' : row.matched ? $t('business.passed') : $t('business.failed-fta53'))
  },
  {
    title: $t('business.tokens'),
    key: 'tokens',
    width: 110,
    render: row => `${row.input_tokens} / ${row.output_tokens}`
  },
  {
    title: $t('business.cost'),
    key: 'cost',
    width: 120,
    render: row => formatCurrency(row.cost, row.cost_currency)
  },
  {
    title: $t('business.latency'),
    key: 'total_ms',
    width: 100,
    render: row => `${row.total_ms} ms`
  }
]);

onMounted(async () => {
  loading.value = true;
  try {
    const [setRes, assetRes, modelRes] = await Promise.all([
      fetchAllPromptEvaluationSets(),
      has('prompt:read') ? fetchAssets('prompt_template') : Promise.resolve({ data: null, error: null }),
      has('model_service:read') ? fetchModelServices() : Promise.resolve({ data: null, error: null })
    ]);
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
  const generation = ++selectionGeneration;
  pageMode.value = 'detail';
  selectedID.value = id;
  selectedDetail.value = null;
  versionOptions.value = [];
  selectedVersionID.value = null;
  detailLoading.value = true;
  editing.value = false;
  creating.value = false;
  runs.value = [];
  summary.value = null;
  try {
    const current = overviews.value.find(item => item.id === id);
    if (!current) return;
    const [detailRes, assetRes] = await Promise.all([
      fetchPromptEvaluationSets(current.asset_id),
      fetchAssetDetail(current.asset_id)
    ]);
    if (generation !== selectionGeneration || selectedID.value !== id) return;
    if (!detailRes.error && detailRes.data) {
      const detail = detailRes.data.find(item => item.id === id);
      if (detail) selectedDetail.value = detail;
      if (detail) form.value = toForm(detail, current.asset_id);
    }
    if (!assetRes.error && assetRes.data) {
      versionOptions.value = assetRes.data.versions.map(version => ({
        label: $t('business.versionWithStatus', {
          version: version.version,
          status: version.current ? $t('business.current') : ''
        }),
        value: version.id
      }));
      selectedVersionID.value =
        assetRes.data.current_version_id ??
        assetRes.data.versions.find(item => item.version === assetRes.data.latest_version)?.id ??
        null;
    }
  } finally {
    if (generation === selectionGeneration) detailLoading.value = false;
  }
}

function toForm(detail: Api.Management.EvaluationSet, assetID: string) {
  return {
    asset_id: assetID,
    name: detail.name,
    description: detail.description,
    cases: detail.cases.map(item => ({
      id: item.id,
      input: item.input,
      inputText: JSON.stringify(item.input, null, 2),
      match_type: item.match_type,
      expected: item.expected
    }))
  };
}

function newCase(): CaseDraft {
  return { input: {}, inputText: '{}', match_type: 'contains', expected: '' };
}

function startCreate() {
  selectionGeneration++;
  detailLoading.value = false;
  pageMode.value = 'edit';
  selectedID.value = null;
  selectedDetail.value = null;
  creating.value = true;
  editing.value = true;
  runs.value = [];
  summary.value = null;
  versionOptions.value = [];
  selectedVersionID.value = null;
  form.value = {
    asset_id: assetOptions.value[0]?.value ?? '',
    name: '',
    description: '',
    cases: [newCase()]
  };
}

function startEdit() {
  if (detailLoading.value || !selectedDetail.value) return;
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
    window.$message?.success($t('business.evaluation-set-deleted'));
  }
}

async function save() {
  if (saving.value) return;
  if (!form.value.asset_id || !form.value.name.trim()) {
    window.$message?.warning($t('business.select-a-template-and-enter'));
    return;
  }
  const cases: Api.Management.EvaluationCase[] = [];
  for (const item of form.value.cases) {
    try {
      const input = JSON.parse(item.inputText);
      if (!input || typeof input !== 'object' || Array.isArray(input)) throw new Error('object required');
      item.input = input;
    } catch {
      window.$message?.warning($t('business.the-input-of-every-case'));
      return;
    }
    if (!item.expected) {
      window.$message?.warning($t('business.enter-expected-content-for-every'));
      return;
    }
    cases.push({
      id: item.id,
      input: item.input,
      match_type: item.match_type,
      expected: item.expected
    });
  }
  if (!cases.length) {
    window.$message?.warning($t('business.keep-at-least-one-test'));
    return;
  }
  const generation = selectionGeneration;
  const assetId = form.value.asset_id;
  const selectedId = selectedID.value;
  const wasCreating = creating.value;
  saving.value = true;
  try {
    const params = {
      name: form.value.name.trim(),
      description: form.value.description.trim(),
      cases
    };
    if (wasCreating) {
      const { data, error } = await fetchCreatePromptEvaluationSet(assetId, params);
      if (error || !data || generation !== selectionGeneration) return;
      await reloadList();
      if (generation !== selectionGeneration) return;
      await selectSet(data.id);
    } else if (selectedId) {
      const { error } = await fetchUpdatePromptEvaluationSet(assetId, selectedId, params);
      if (error || generation !== selectionGeneration) return;
      await reloadList();
      if (generation !== selectionGeneration) return;
      await selectSet(selectedId);
    }
    editing.value = false;
    creating.value = false;
    window.$message?.success($t('business.evaluation-set-saved'));
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
  window.$message?.success($t('business.evaluation-set-deleted'));
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
      window.$message?.success($t('business.evaluation-completed'));
    }
  } finally {
    running.value = false;
  }
}

function accuracyText(value: number | null) {
  return value === null ? $t('business.no-runs') : `${(value * 100).toFixed(1)}%`;
}
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard v-if="pageMode === 'list'" :title="$t('business.evaluation-sets')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="reloadList">
            {{ $t('business.refresh') }}
          </NButton>
          <NButton v-if="canManage" type="primary" @click="startCreate">{{ $t('business.create-evaluation-set') }}</NButton>
        </NSpace>
      </template>
      <NDataTable
        :columns="overviewColumns"
        :data="overviews"
        :loading="loading"
        :bordered="false"
        :scroll-x="1100"
        :pagination="{ pageSize: 15 }"
      />
    </NCard>

    <NCard
      v-else
      :title="
        creating
          ? $t('business.create-evaluation-set')
          : pageMode === 'edit'
            ? $t('business.edit-evaluation-set')
            : selected?.name
      "
      :bordered="false"
      class="card-wrapper"
    >
      <template #header-extra>
        <NSpace>
          <NButton @click="pageMode === 'edit' ? cancelEdit() : (pageMode = 'list')">{{ $t('business.back') }}</NButton>
          <NButton v-if="canManage && pageMode === 'detail'" type="primary" @click="startEdit">
            {{ $t('business.edit-evaluation-set') }}
          </NButton>
          <NPopconfirm v-if="canManage && pageMode === 'detail'" @positive-click="removeSet">
            <template #trigger>
              <NButton>{{ $t('business.delete-evaluation-set') }}</NButton>
            </template>
            {{ $t('business.deleting-this-evaluation-set-does') }}
          </NPopconfirm>
        </NSpace>
      </template>
      <NSpin :show="detailLoading">
        <NDescriptions v-if="pageMode === 'detail' && selected" :column="3" label-placement="left" bordered>
          <NDescriptionsItem :label="$t('business.template')">{{ selected.asset_name }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.cases')">{{ selected.case_count }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.runs')">{{ selected.run_count }}</NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.historical-match-rate')">
            {{ accuracyText(selected.accuracy) }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.last-run')">
            {{ selected.last_run_at ? formatTime(selected.last_run_at) : $t('business.no-runs') }}
          </NDescriptionsItem>
          <NDescriptionsItem :label="$t('business.usage-description')">
            {{ selected.description || $t('business.noDescription') }}
          </NDescriptionsItem>
        </NDescriptions>
        <NForm v-else label-placement="top">
          <NGrid cols="1 700:2" :x-gap="16">
            <NFormItemGi :label="$t('business.template')">
              <NInput v-if="!creating" :value="selected?.asset_name" readonly />
              <NSelect v-else v-model:value="form.asset_id" :options="assetOptions" />
            </NFormItemGi>
            <NFormItemGi :label="$t('business.evaluation-set-name')"><NInput v-model:value="form.name" /></NFormItemGi>
          </NGrid>
          <NFormItem :label="$t('business.usage-description')"><NInput v-model:value="form.description" /></NFormItem>
        </NForm>

        <NDivider title-placement="left">{{ $t('business.test-case-count', { count: form.cases.length, max: 50 }) }}</NDivider>
        <NDataTable
          :columns="caseColumns"
          :data="form.cases"
          size="small"
          :bordered="false"
          :scroll-x="900"
          :max-height="460"
        />
        <NSpace v-if="pageMode === 'edit'" class="my-12px">
          <NButton :disabled="form.cases.length >= 50" @click="form.cases.push(newCase())">
            {{ $t('business.add-case') }}
          </NButton>
          <NButton v-if="canManage" type="primary" :loading="saving" @click="save">{{ $t('business.save-evaluation-set') }}</NButton>
        </NSpace>

        <template v-if="pageMode === 'detail'">
          <!-- 运行评估会调用模型并产生运行记录,只读角色只能查看已有结果。 -->
          <template v-if="canManage">
            <NDivider title-placement="left">{{ $t('business.run-evaluation') }}</NDivider>
            <NGrid cols="1 700:2" :x-gap="16">
              <NFormItemGi :label="$t('business.template-version')" :show-feedback="false">
                <NSelect v-model:value="selectedVersionID" :options="versionOptions" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.model-service')" :show-feedback="false">
                <NSelect
                  v-model:value="selectedModelIDs"
                  multiple
                  :max="4"
                  :options="modelOptions"
                  :placeholder="$t('business.use-the-template-s-bound')"
                />
              </NFormItemGi>
            </NGrid>
            <NButton
              type="primary"
              class="my-12px"
              :loading="running"
              :disabled="!selectedVersionID"
              @click="runEvaluation"
            >
              {{ $t('business.run-evaluation') }}
            </NButton>
          </template>
          <NCard v-if="summary" size="small" class="mb-12px">
            <NSpace size="large">
              <NStatistic :label="$t('business.runs')" :value="summary.total" />
              <NStatistic :label="$t('business.match-rate')" :value="`${(summary.accuracy * 100).toFixed(1)}%`" />
              <NStatistic :label="$t('business.average-latency')" :value="`${summary.average_latency_ms} ms`" />
              <NStatistic
                v-for="item in runCosts"
                :key="item.currency"
                :label="$t('business.totalCostCurrency', { currency: item.currency })"
                :value="formatCurrency(item.total, item.currency)"
              />
            </NSpace>
          </NCard>
          <NDataTable
            v-if="runs.length"
            :columns="runColumns"
            :data="runs"
            size="small"
            :bordered="false"
            :scroll-x="800"
          />
        </template>
      </NSpin>
    </NCard>
  </div>
</template>

<style scoped></style>
