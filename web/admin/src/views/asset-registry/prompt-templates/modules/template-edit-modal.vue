<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { fetchAssetDetail, fetchModelServices, fetchUpdateAsset } from '@/service/api';
import TemplateCodeEditor from './template-code-editor.vue';
import TemplateVariablesTable from './template-variables-table.vue';
import JsonEditor from './json-editor.vue';
import { schemaFromExample } from './json-schema';
import type { VariableDraft } from './template-form-types';

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
const closeConfirm = ref(false);
const showExample = ref(false);
const exampleText = ref('{\n  "name": "张三",\n  "age": 30,\n  "tags": ["重要客户"]\n}');
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
const templateVariables = computed<Api.Management.PromptVariable[]>(() => form.variables.map(variable => ({
  name: variable.name.trim(),
  type: variable.type,
  description: variable.description.trim() || undefined,
  required: variable.required,
  ...(variable.default !== null ? { default: variable.default } : {})
})));

watch(() => props.show, visible => {
  if (visible) void initialize();
});

watch(form, () => {
  dirty.value = true;
}, { deep: true, flush: 'sync' });

async function initialize() {
  if (!props.assetId) return;
  activeTab.value = 'content';
  detail.value = null;
  loading.value = true;
  try {
    const [detailRes, modelRes] = await Promise.all([fetchAssetDetail(props.assetId), fetchModelServices()]);
    if (!modelRes.error && modelRes.data) models.value = modelRes.data;
    if (detailRes.error || !detailRes.data) return;
    detail.value = detailRes.data;
    form.name = detailRes.data.name;
    form.description = detailRes.data.description;
    form.model_service_id = detailRes.data.spec.model_service_id || null;
    form.template = detailRes.data.spec.template;
    form.output_schema_text = detailRes.data.spec.output_schema ? JSON.stringify(detailRes.data.spec.output_schema, null, 2) : '';
    form.variables = detailRes.data.spec.variables.map(variable => ({
      name: variable.name,
      type: variable.type,
      description: variable.description ?? '',
      required: variable.required,
      default: (variable.default ?? null) as string | number | boolean | null
    }));
    dirty.value = false;
  } finally {
    loading.value = false;
  }
}

function validate() {
  if (!form.name.trim()) return '请填写模板名称';
  if (!form.template.trim()) return '请填写模板内容';
  const names = new Set<string>();
  for (const variable of form.variables) {
    const name = variable.name.trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(name)) return '变量名只能使用字母、数字和下划线，且不能以数字开头';
    if (names.has(name)) return `变量 ${name} 重复`;
    names.add(name);
  }
  return null;
}

async function save(openTest: boolean) {
  if (!props.assetId || !detail.value) return;
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
      window.$message?.warning('结构化输出定义必须是合法 JSON 对象');
      return;
    }
  }
  try {
    const { data, error } = await fetchUpdateAsset(props.assetId, {
      name: form.name.trim(),
      description: form.description.trim(),
      expected_version: detail.value.latest_version,
      spec: {
        template: form.template,
        model_service_id: form.model_service_id || undefined,
        output_schema: outputSchema,
        variables: templateVariables.value
      }
    });
    if (error || !data) return;
    dirty.value = false;
    emit('saved');
    emit('update:show', false);
    window.$message?.success(`修改已保存为版本 ${data.version}`);
    if (openTest) emit('test', props.assetId);
  } finally {
    saving.value = false;
  }
}

function generateSchema() {
  try {
    const example = JSON.parse(exampleText.value);
    form.output_schema_text = JSON.stringify(schemaFromExample(example), null, 2);
    showExample.value = false;
  } catch {
    window.$message?.warning('示例内容必须是合法 JSON');
  }
}

function requestClose() {
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
    title="编辑模板"
    class="w-1000px"
    :mask-closable="false"
    @update:show="value => { if (value) emit('update:show', true); else requestClose(); }"
  >
    <NSpin :show="loading">
      <NAlert v-if="detail?.status === 'archived'" type="info" :bordered="false" class="mb-16px">已归档模板只能查看，不能继续编辑。</NAlert>
      <NTabs v-model:value="activeTab" type="line">
        <NTabPane name="content" tab="模板内容">
          <TemplateCodeEditor
            v-if="detail"
            :value="form.template"
            :variables="templateVariables"
            :disabled="detail.status === 'archived'"
            @update:value="value => form.template = value"
          />
        </NTabPane>
        <NTabPane name="variables" tab="变量设置">
          <TemplateVariablesTable v-model:variables="form.variables" :disabled="detail?.status === 'archived'" />
        </NTabPane>
        <NTabPane name="settings" tab="基本设置">
          <NForm label-placement="left" label-width="100" :disabled="detail?.status === 'archived'">
            <NFormItem label="模板名称" required><NInput v-model:value="form.name" maxlength="120" /></NFormItem>
            <NFormItem label="用途说明"><NInput v-model:value="form.description" type="textarea" :rows="3" maxlength="2000" /></NFormItem>
            <NFormItem label="模型服务"><NSelect v-model:value="form.model_service_id" clearable filterable :options="modelOptions" placeholder="选择默认模型服务" /></NFormItem>
            <NFormItem label="结构化输出">
              <div class="w-full">
                <JsonEditor v-model:value="form.output_schema_text" :height="260" :disabled="detail?.status === 'archived'" />
                <NButton size="small" secondary class="mt-8px" :disabled="detail?.status === 'archived'" @click="showExample = true">从示例生成</NButton>
              </div>
            </NFormItem>
          </NForm>
        </NTabPane>
      </NTabs>
    </NSpin>
    <template #footer>
      <div class="flex items-center justify-between">
        <NText depth="3">{{ dirty ? '当前修改尚未保存' : detail ? `正在编辑最新版本 ${detail.latest_version}` : '' }}</NText>
        <NSpace>
          <NButton @click="requestClose">关闭</NButton>
          <NButton :loading="saving" :disabled="detail?.status === 'archived'" @click="save(true)">保存并测试</NButton>
          <NButton type="primary" :loading="saving" :disabled="detail?.status === 'archived'" @click="save(false)">保存为新版本</NButton>
        </NSpace>
      </div>
    </template>
  </NModal>

  <NModal v-model:show="closeConfirm" preset="dialog" type="info" title="关闭编辑？" positive-text="放弃修改" negative-text="继续编辑" @positive-click="discardAndClose">
    当前修改尚未保存，关闭后将丢失。
  </NModal>

  <NModal v-model:show="showExample" preset="card" title="从示例生成结构" class="w-720px">
    <NAlert type="info" :bordered="false" class="mb-12px">粘贴一份期望的 JSON 示例，系统会生成字段类型、必填项和嵌套结构。</NAlert>
    <JsonEditor v-model:value="exampleText" :height="260" required />
    <template #footer><NSpace justify="end"><NButton @click="showExample = false">取消</NButton><NButton type="primary" @click="generateSchema">生成结构</NButton></NSpace></template>
  </NModal>
</template>
