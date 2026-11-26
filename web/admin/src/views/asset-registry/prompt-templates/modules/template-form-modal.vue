<script setup lang="ts">
import { computed, h, reactive, ref, watch } from 'vue';
import { NButton, NCheckbox, NInput, NInputNumber, NSelect, NSwitch } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { fetchCreateAsset, fetchAssetDetail, fetchModelServices, fetchUpdateAsset } from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';

const { has } = usePermission();
import TemplateCodeEditor from './template-code-editor.vue';

interface VariableDraft {
  name: string;
  type: 'string' | 'number' | 'integer' | 'boolean';
  description: string;
  required: boolean;
  default: string | number | boolean | null;
}

const props = defineProps<{
  show: boolean;
  assetId: string | null;
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
  saved: [];
  test: [id: string];
}>();

const currentStep = ref(1);
const loading = ref(false);
const saving = ref(false);
const dirty = ref(false);
let editGeneration = 0;
const closeConfirm = ref(false);
const detail = ref<Api.Management.AssetDetail | null>(null);
const models = ref<Api.Management.ModelService[]>([]);

const form = reactive({
  name: '',
  description: '',
  model_service_id: null as string | null,
  template: '',
  variables: [] as VariableDraft[]
});

const modelOptions = computed(() => models.value.map(item => ({ label: item.name, value: item.id })));
const templateVariables = computed<Api.Management.PromptVariable[]>(() =>
  form.variables.map(variable => ({
    name: variable.name.trim(),
    type: variable.type,
    description: variable.description.trim() || undefined,
    required: variable.required,
    ...(variable.default !== null ? { default: variable.default } : {})
  }))
);

const variableTypeOptions = computed(() => [
  { label: $t('business.text'), value: 'string' },
  { label: $t('business.number'), value: 'number' },
  { label: $t('business.integer'), value: 'integer' },
  { label: $t('business.boolean'), value: 'boolean' }
]);

const variablePage = ref(1);
const variablePageSize = 5;
const maxVariables = 64;
const variablePageCount = computed(() => Math.max(1, Math.ceil(form.variables.length / variablePageSize)));
const pagedVariables = computed(() => {
  const start = (variablePage.value - 1) * variablePageSize;
  return form.variables.slice(start, start + variablePageSize);
});

const variableColumns = computed<DataTableColumns<VariableDraft>>(() => [
  {
    title: $t('business.variable-name'),
    key: 'name',
    minWidth: 150,
    render: row =>
      h(NInput, {
        size: 'small',
        value: row.name,
        disabled: detail.value?.status === 'archived',
        placeholder: $t('business.for-example-customer-name'),
        'onUpdate:value': value => (row.name = value)
      })
  },
  {
    title: $t('business.type'),
    key: 'type',
    width: 105,
    render: row =>
      h(NSelect, {
        size: 'small',
        value: row.type,
        options: variableTypeOptions.value,
        disabled: detail.value?.status === 'archived',
        'onUpdate:value': value => {
          row.type = value;
          changeVariableType(row);
        }
      })
  },
  {
    title: $t('business.description'),
    key: 'description',
    minWidth: 160,
    render: row =>
      h(NInput, {
        size: 'small',
        value: row.description,
        disabled: detail.value?.status === 'archived',
        placeholder: $t('business.variable-purpose'),
        'onUpdate:value': value => (row.description = value)
      })
  },
  {
    title: $t('business.required'),
    key: 'required',
    width: 65,
    render: row =>
      h(NCheckbox, {
        checked: row.required,
        disabled: detail.value?.status === 'archived',
        'onUpdate:checked': value => (row.required = value)
      })
  },
  {
    title: $t('business.default-value'),
    key: 'default',
    width: 145,
    render(row) {
      if (row.type === 'boolean') {
        return h(NSwitch, {
          size: 'small',
          value: Boolean(row.default),
          disabled: detail.value?.status === 'archived',
          'onUpdate:value': value => (row.default = value)
        });
      }
      if (row.type === 'number' || row.type === 'integer') {
        return h(NInputNumber, {
          size: 'small',
          value: variableNumber(row.default),
          precision: row.type === 'integer' ? 0 : undefined,
          disabled: detail.value?.status === 'archived',
          clearable: true,
          'onUpdate:value': value => (row.default = value)
        });
      }
      return h(NInput, {
        size: 'small',
        value: String(row.default ?? ''),
        disabled: detail.value?.status === 'archived',
        placeholder: $t('business.optional'),
        'onUpdate:value': value => (row.default = value)
      });
    }
  },
  {
    title: '',
    key: 'actions',
    width: 60,
    render: row =>
      h(
        NButton,
        {
          size: 'small',
          quaternary: true,
          type: 'error',
          disabled: detail.value?.status === 'archived',
          onClick: () => form.variables.splice(form.variables.indexOf(row), 1)
        },
        { default: () => $t('business.remove') }
      )
  }
]);

watch(
  [() => props.show, () => props.assetId],
  ([visible]) => {
    editGeneration++;
    saving.value = false;
    if (visible) void initialize();
    else loading.value = false;
  }
);

watch(
  form,
  () => {
    dirty.value = true;
  },
  { deep: true, flush: 'sync' }
);

watch(
  () => form.variables.length,
  () => {
    if (variablePage.value > variablePageCount.value) variablePage.value = variablePageCount.value;
  }
);

async function initialize() {
  const generation = ++editGeneration;
  const assetId = props.assetId;
  currentStep.value = 1;
  detail.value = null;
  closeConfirm.value = false;
  resetForm();
  loading.value = Boolean(assetId);
  try {
    const [modelRes, detailRes] = await Promise.all([
      has('model_service:read') ? fetchModelServices() : Promise.resolve({ data: null, error: null }),
      assetId ? fetchAssetDetail(assetId) : Promise.resolve(null)
    ]);
    if (generation !== editGeneration || assetId !== props.assetId) return;
    if (!modelRes.error && modelRes.data) models.value = modelRes.data;
    if (detailRes && !detailRes.error && detailRes.data) {
      detail.value = detailRes.data;
      applyDetail(detailRes.data);
    }
  } finally {
    if (generation === editGeneration) loading.value = false;
  }
}

function resetForm() {
  variablePage.value = 1;
  form.name = '';
  form.description = '';
  form.model_service_id = null;
  form.template = $t('business.answer-the-question-using-the');
  form.variables.splice(0, form.variables.length, {
    name: 'question',
    type: 'string',
    description: $t('business.question-asked-by-the-user'),
    required: true,
    default: null
  });
  dirty.value = false;
}

function applyDetail(value: Api.Management.AssetDetail) {
  variablePage.value = 1;
  form.name = value.name;
  form.description = value.description;
  form.model_service_id = value.spec.model_service_id || null;
  form.template = value.spec.template;
  form.variables.splice(
    0,
    form.variables.length,
    ...value.spec.variables.map(variable => ({
      name: variable.name,
      type: variable.type,
      description: variable.description ?? '',
      required: variable.required,
      default: (variable.default ?? null) as string | number | boolean | null
    }))
  );
  dirty.value = false;
}

function addVariable() {
  if (form.variables.length >= maxVariables) {
    window.$message?.info($t('business.maxVariables', { count: maxVariables }));
    return;
  }
  form.variables.push({
    name: '',
    type: 'string',
    description: '',
    required: false,
    default: null
  });
  variablePage.value = variablePageCount.value;
}

function changeVariableType(variable: VariableDraft) {
  variable.default = variable.type === 'boolean' ? false : null;
}

function variableNumber(value: unknown) {
  return typeof value === 'number' ? value : null;
}

function validateStep(step: number) {
  if (step === 1 && !form.name.trim()) {
    window.$message?.warning($t('business.enter-a-template-name'));
    return false;
  }
  if (step === 2) {
    const names = new Set<string>();
    for (const variable of form.variables) {
      const name = variable.name.trim();
      if (!/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(name)) {
        window.$message?.warning($t('business.variable-names-can-contain-only'));
        return false;
      }
      if (names.has(name)) {
        window.$message?.warning($t('business.duplicateVariablePleaseFix', { name }));
        return false;
      }
      names.add(name);
    }
  }
  if (step === 3 && !form.template.trim()) {
    window.$message?.warning($t('business.enter-template-content'));
    return false;
  }
  return true;
}

function nextStep() {
  if (validateStep(currentStep.value)) currentStep.value += 1;
}

function buildParams(): Api.Management.CreateAssetParams {
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    spec: {
      template: form.template,
      model_service_id: form.model_service_id || undefined,
      variables: templateVariables.value
    }
  };
}

async function save(openTest: boolean) {
  if (saving.value || loading.value || (props.assetId && !detail.value)) return;
  if (!validateStep(1) || !validateStep(2) || !validateStep(3)) return;
  const generation = editGeneration;
  const assetId = props.assetId;
  const expectedVersion = detail.value?.latest_version;
  const params = buildParams();
  saving.value = true;
  try {
    let id = assetId;
    let version = 1;
    if (id && expectedVersion !== undefined) {
      const { data, error } = await fetchUpdateAsset(id, {
        ...params,
        expected_version: expectedVersion
      });
      if (error || !data) return;
      version = data.version;
    } else {
      const { data, error } = await fetchCreateAsset(params);
      if (error || !data) return;
      id = data.id;
    }
    if (generation !== editGeneration) return;
    dirty.value = false;
    emit('saved');
    emit('update:show', false);
    window.$message?.success(
      assetId ? $t('business.savedVersionEnable', { version }) : $t('business.template-created-you-can-continue')
    );
    if (openTest && id) emit('test', id);
  } finally {
    if (generation === editGeneration) saving.value = false;
  }
}

function requestClose() {
  if (saving.value) return;
  if (dirty.value) closeConfirm.value = true;
  else emit('update:show', false);
}

function discardAndClose() {
  dirty.value = false;
  closeConfirm.value = false;
  emit('update:show', false);
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="props.assetId ? $t('business.edit-template') : $t('business.create-template')"
    class="w-760px"
    :mask-closable="false"
    @update:show="
      value => {
        if (value) emit('update:show', true);
        else requestClose();
      }
    "
  >
    <NSpin :show="loading">
      <NSteps :current="currentStep" class="mb-24px">
        <NStep :title="$t('business.basic-information')" :description="$t('business.name-and-purpose')" />
        <NStep :title="$t('business.variable-settings')" :description="$t('business.define-replaceable-content')" />
        <NStep
          :title="$t('business.template-content')"
          :description="$t('business.write-content-and-insert-variables')"
        />
      </NSteps>

      <NAlert v-if="detail?.status === 'archived'" type="info" :bordered="false" class="mb-16px">
        {{ $t('business.archived-templates-are-read-only') }}
      </NAlert>

      <NForm
        v-show="currentStep === 1"
        label-placement="top"
        :disabled="detail?.status === 'archived'"
      >
        <NFormItem :label="$t('business.template-name')" required>
          <NInput
            v-model:value="form.name"
            maxlength="120"
            :placeholder="$t('business.for-example-support-policy-q')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.usage-description')">
          <NInput
            v-model:value="form.description"
            type="textarea"
            :rows="3"
            maxlength="2000"
            :placeholder="$t('business.describe-where-this-template-is')"
          />
        </NFormItem>
        <NFormItem :label="$t('business.model-service')">
          <NSelect
            v-model:value="form.model_service_id"
            clearable
            filterable
            :options="modelOptions"
            :placeholder="$t('business.optional-select-the-default-model')"
          />
        </NFormItem>
      </NForm>

      <div v-show="currentStep === 2">
        <div class="mb-12px flex items-center justify-between">
          <NText depth="2">
            {{ $t('business.added') }} {{ form.variables.length }} /
            {{ maxVariables }}
            {{ $t('business.variables-you-can-also-continue') }}
          </NText>
          <NButton
            size="small"
            type="primary"
            secondary
            :disabled="detail?.status === 'archived' || form.variables.length >= maxVariables"
            @click="addVariable"
          >
            {{ $t('business.add-variable') }}
          </NButton>
        </div>
        <NDataTable :columns="variableColumns" :data="pagedVariables" :bordered="false" size="small" :scroll-x="760" />
        <NSpace v-if="form.variables.length > variablePageSize" justify="end" class="mt-12px">
          <NPagination v-model:page="variablePage" size="small" :page-count="variablePageCount" />
        </NSpace>
      </div>

      <div v-show="currentStep === 3">
        <TemplateCodeEditor
          v-if="currentStep === 3"
          :value="form.template"
          :variables="templateVariables"
          :disabled="detail?.status === 'archived'"
          @update:value="value => (form.template = value)"
        />
      </div>
    </NSpin>

    <template #footer>
      <div class="flex items-center justify-between">
        <NButton @click="requestClose">{{ $t('business.cancel') }}</NButton>
        <NSpace>
          <NButton v-if="currentStep > 1" @click="currentStep -= 1">{{ $t('business.previous') }}</NButton>
          <NButton v-if="currentStep < 3" type="primary" @click="nextStep">{{ $t('business.next') }}</NButton>
          <template v-else-if="detail?.status !== 'archived'">
            <NButton :loading="saving" :disabled="loading || saving || Boolean(props.assetId && !detail)" @click="save(true)">{{ $t('business.save-and-test') }}</NButton>
            <NButton type="primary" :loading="saving" :disabled="loading || saving || Boolean(props.assetId && !detail)" @click="save(false)">
              {{ props.assetId ? $t('business.save-as-new-version') : $t('business.createTemplate') }}
            </NButton>
          </template>
        </NSpace>
      </div>
    </template>
  </NModal>

  <NModal
    v-model:show="closeConfirm"
    preset="dialog"
    type="info"
    :title="$t('business.close-editing')"
    :positive-text="$t('business.discard-changes')"
    :negative-text="$t('business.continue-editing')"
    @positive-click="discardAndClose"
  >
    {{ $t('business.the-current-changes-are-unsaved') }}
  </NModal>
</template>
