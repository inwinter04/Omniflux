<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { fetchAssetDetail, fetchModelServices, fetchUpdateAsset } from '@/service/api';
import { $t } from '@/locales';
import JsonEditor from './json-editor.vue';
import { schemaFromExample } from './json-schema';
import TemplateCodeEditor from './template-code-editor.vue';
import type { VariableDraft } from './template-form-types';
import TemplateVariablesTable from './template-variables-table.vue';
import TemplateVersionsModal from './template-versions-modal.vue';

const props = defineProps<{
  show: boolean;
  assetId: string | null;
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
  saved: [];
  test: [id: string];
}>();

const loading = ref(false);
const saving = ref(false);
const dirty = ref(false);
let editGeneration = 0;
const closeConfirm = ref(false);
const showExample = ref(false);
const showVersions = ref(false);
// 示例内容可编辑:每次打开弹窗时用当前语言的示例重置。
const exampleText = ref('');

function openExample() {
  exampleText.value = $t('business.name-john-doe-age-30');
  showExample.value = true;
}
const activeTab = ref('content');
const detail = ref<Api.Management.AssetDetail | null>(null);
const models = ref<Api.Management.ModelService[]>([]);
const form = reactive({
  name: '',
  description: '',
  model_service_id: null as string | null,
  template: '',
  output_schema_text: '',
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

async function initialize() {
  if (!props.assetId) return;
  const generation = ++editGeneration;
  const assetId = props.assetId;
  activeTab.value = 'content';
  detail.value = null;
  loading.value = true;
  try {
    const [detailRes, modelRes] = await Promise.all([fetchAssetDetail(assetId), fetchModelServices()]);
    if (generation !== editGeneration || assetId !== props.assetId) return;
    if (!modelRes.error && modelRes.data) models.value = modelRes.data;
    if (detailRes.error || !detailRes.data) return;
    detail.value = detailRes.data;
    form.name = detailRes.data.name;
    form.description = detailRes.data.description;
    form.model_service_id = detailRes.data.spec.model_service_id || null;
    form.template = detailRes.data.spec.template;
    form.output_schema_text = detailRes.data.spec.output_schema
      ? JSON.stringify(detailRes.data.spec.output_schema, null, 2)
      : '';
    form.variables = detailRes.data.spec.variables.map(variable => ({
      name: variable.name,
      type: variable.type,
      description: variable.description ?? '',
      required: variable.required,
      default: (variable.default ?? null) as string | number | boolean | null
    }));
    dirty.value = false;
  } finally {
    if (generation === editGeneration) loading.value = false;
  }
}

function validate() {
  if (!form.name.trim()) return $t('business.enter-a-template-name');
  if (!form.template.trim()) return $t('business.enter-template-content');
  const names = new Set<string>();
  for (const variable of form.variables) {
    const name = variable.name.trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(name)) return $t('business.variable-names-can-contain-only');
    if (names.has(name)) return $t('business.duplicateVariable', { name });
    names.add(name);
  }
  return null;
}

async function save(openTest: boolean) {
  if (loading.value || saving.value || !props.assetId || !detail.value) return;
  const generation = editGeneration;
  const assetId = props.assetId;
  const expectedVersion = detail.value.latest_version;
  const errorMessage = validate();
  if (errorMessage) {
    window.$message?.warning(errorMessage);
    return;
  }
  saving.value = true;
  let outputSchema: Record<string, unknown> | undefined;
  if (form.output_schema_text.trim()) {
    try {
      outputSchema = JSON.parse(form.output_schema_text);
    } catch {
      saving.value = false;
      window.$message?.warning($t('business.the-structured-output-definition-must'));
      return;
    }
  }
  try {
    const { data, error } = await fetchUpdateAsset(assetId, {
      name: form.name.trim(),
      description: form.description.trim(),
      expected_version: expectedVersion,
      spec: {
        template: form.template,
        model_service_id: form.model_service_id || undefined,
        output_schema: outputSchema,
        variables: templateVariables.value
      }
    });
    if (error || !data || generation !== editGeneration) return;
    dirty.value = false;
    emit('saved');
    emit('update:show', false);
    window.$message?.success($t('business.savedAsVersion', { version: data.version }));
    if (openTest) emit('test', assetId);
  } finally {
    if (generation === editGeneration) saving.value = false;
  }
}

function generateSchema() {
  try {
    const example = JSON.parse(exampleText.value);
    form.output_schema_text = JSON.stringify(schemaFromExample(example), null, 2);
    showExample.value = false;
  } catch {
    window.$message?.warning($t('business.sample-content-must-be-valid'));
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
  <NDrawer
    :show="props.show"
    :width="1200"
    placement="right"
    :mask-closable="false"
    @update:show="
      value => {
        if (value) emit('update:show', true);
        else requestClose();
      }
    "
  >
    <NDrawerContent :title="$t('business.edit-template')" closable>
    <NSpin :show="loading">
      <NAlert v-if="detail?.status === 'archived'" type="info" :bordered="false" class="mb-16px">
        {{ $t('business.archived-templates-are-read-only') }}
      </NAlert>
      <NTabs v-model:value="activeTab" type="line">
        <NTabPane name="content" :tab="$t('business.template-content')">
          <TemplateCodeEditor
            v-if="detail"
            :value="form.template"
            :variables="templateVariables"
            :disabled="detail.status === 'archived'"
            @update:value="value => (form.template = value)"
          />
        </NTabPane>
        <NTabPane name="variables" :tab="$t('business.variable-settings')">
          <TemplateVariablesTable v-model:variables="form.variables" :disabled="detail?.status === 'archived'" />
        </NTabPane>
        <NTabPane name="settings" :tab="$t('business.basic-settings')">
          <NForm label-placement="left" label-width="100" :disabled="detail?.status === 'archived'">
            <NFormItem :label="$t('business.template-name')" required>
              <NInput v-model:value="form.name" maxlength="120" />
            </NFormItem>
            <NFormItem :label="$t('business.usage-description')">
              <NInput v-model:value="form.description" type="textarea" :rows="3" maxlength="2000" />
            </NFormItem>
            <NFormItem :label="$t('business.model-service')">
              <NSelect
                v-model:value="form.model_service_id"
                clearable
                filterable
                :options="modelOptions"
                :placeholder="$t('business.select-a-default-model-service')"
              />
            </NFormItem>
            <NFormItem :label="$t('business.structured-output')">
              <div class="w-full">
                <JsonEditor
                  v-model:value="form.output_schema_text"
                  :height="260"
                  :disabled="detail?.status === 'archived'"
                />
                <NButton
                  size="small"
                  secondary
                  class="mt-8px"
                  :disabled="detail?.status === 'archived'"
                  @click="openExample"
                >
                  {{ $t('business.generate-from-sample') }}
                </NButton>
              </div>
            </NFormItem>
          </NForm>
        </NTabPane>
      </NTabs>
    </NSpin>
      <template #footer>
        <div class="flex items-center justify-between">
          <NText depth="3">
            {{
              dirty
                ? $t('business.unsavedChanges')
                : detail
                  ? $t('business.editingLatestVersion', {
                      version: detail.latest_version
                    })
                  : ''
            }}
          </NText>
          <NSpace>
            <NButton @click="showVersions = true">{{ $t('business.version-management') }}</NButton>
            <NButton @click="requestClose">{{ $t('business.close') }}</NButton>
            <NButton :loading="saving" :disabled="loading || saving || !detail || detail.status === 'archived'" @click="save(true)">
              {{ $t('business.save-and-test') }}
            </NButton>
            <NButton type="primary" :loading="saving" :disabled="loading || saving || !detail || detail.status === 'archived'" @click="save(false)">
              {{ $t('business.save-as-new-version') }}
            </NButton>
          </NSpace>
        </div>
      </template>
    </NDrawerContent>
  </NDrawer>

  <TemplateVersionsModal v-model:show="showVersions" :asset="detail" @saved="initialize" />

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

  <NModal
    v-model:show="showExample"
    preset="card"
    :title="$t('business.generate-structure-from-sample')"
    class="w-720px"
  >
    <NAlert type="info" :bordered="false" class="mb-12px">{{ $t('business.paste-an-expected-json-sample') }}</NAlert>
    <JsonEditor v-model:value="exampleText" :height="260" required />
    <template #footer>
      <NSpace justify="end">
        <NButton @click="showExample = false">{{ $t('business.cancel') }}</NButton>
        <NButton type="primary" @click="generateSchema">{{ $t('business.generate-structure') }}</NButton>
      </NSpace>
    </template>
  </NModal>
</template>
