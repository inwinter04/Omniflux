<script setup lang="ts">
import { computed, h, reactive, ref, watch } from 'vue';
import { NButton, NCheckbox, NInput, NInputNumber, NSelect, NSwitch } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { fetchCreateAsset, fetchAssetDetail, fetchModelServices, fetchUpdateAsset } from '@/service/api';
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
const templateVariables = computed<Api.Management.PromptVariable[]>(() => form.variables.map(variable => ({
  name: variable.name.trim(),
  type: variable.type,
  description: variable.description.trim() || undefined,
  required: variable.required,
  ...(variable.default !== null ? { default: variable.default } : {})
})));

const variableTypeOptions = [
  { label: '文本', value: 'string' },
  { label: '数字', value: 'number' },
  { label: '整数', value: 'integer' },
  { label: '是或否', value: 'boolean' }
];

const variablePage = ref(1);
const variablePageSize = 5;
const maxVariables = 64;
const variablePageCount = computed(() => Math.max(1, Math.ceil(form.variables.length / variablePageSize)));
const pagedVariables = computed(() => {
  const start = (variablePage.value - 1) * variablePageSize;
  return form.variables.slice(start, start + variablePageSize);
});

const variableColumns: DataTableColumns<VariableDraft> = [
  {
    title: '变量名',
    key: 'name',
    minWidth: 150,
    render: row => h(NInput, { size: 'small', value: row.name, disabled: detail.value?.status === 'archived', placeholder: '例如：customer_name', 'onUpdate:value': value => row.name = value })
  },
  {
    title: '类型',
    key: 'type',
    width: 105,
    render: row => h(NSelect, {
      size: 'small', value: row.type, options: variableTypeOptions, disabled: detail.value?.status === 'archived',
      'onUpdate:value': value => { row.type = value; changeVariableType(row); }
    })
  },
  {
    title: '说明',
    key: 'description',
    minWidth: 160,
    render: row => h(NInput, { size: 'small', value: row.description, disabled: detail.value?.status === 'archived', placeholder: '变量用途', 'onUpdate:value': value => row.description = value })
  },
  {
    title: '必填',
    key: 'required',
    width: 65,
    render: row => h(NCheckbox, { checked: row.required, disabled: detail.value?.status === 'archived', 'onUpdate:checked': value => row.required = value })
  },
  {
    title: '默认值',
    key: 'default',
    width: 145,
    render(row) {
      if (row.type === 'boolean') {
        return h(NSwitch, { size: 'small', value: Boolean(row.default), disabled: detail.value?.status === 'archived', 'onUpdate:value': value => row.default = value });
      }
      if (row.type === 'number' || row.type === 'integer') {
        return h(NInputNumber, {
          size: 'small', value: variableNumber(row.default), precision: row.type === 'integer' ? 0 : undefined,
          disabled: detail.value?.status === 'archived',
          clearable: true, 'onUpdate:value': value => row.default = value
        });
      }
      return h(NInput, { size: 'small', value: String(row.default ?? ''), disabled: detail.value?.status === 'archived', placeholder: '可选', 'onUpdate:value': value => row.default = value });
    }
  },
  {
    title: '',
    key: 'actions',
    width: 60,
    render: row => h(NButton, {
      size: 'small', quaternary: true, type: 'error', disabled: detail.value?.status === 'archived',
      onClick: () => form.variables.splice(form.variables.indexOf(row), 1)
    }, { default: () => '移除' })
  }
];

watch(() => props.show, visible => {
  if (visible) void initialize();
});

watch(form, () => {
  dirty.value = true;
}, { deep: true, flush: 'sync' });

watch(() => form.variables.length, () => {
  if (variablePage.value > variablePageCount.value) variablePage.value = variablePageCount.value;
});

async function initialize() {
  currentStep.value = 1;
  detail.value = null;
  closeConfirm.value = false;
  resetForm();
  loading.value = Boolean(props.assetId);
  try {
    const [modelRes, detailRes] = await Promise.all([
      fetchModelServices(),
      props.assetId ? fetchAssetDetail(props.assetId) : Promise.resolve(null)
    ]);
    if (!modelRes.error && modelRes.data) models.value = modelRes.data;
    if (detailRes && !detailRes.error && detailRes.data) {
      detail.value = detailRes.data;
      applyDetail(detailRes.data);
    }
  } finally {
    loading.value = false;
  }
}

function resetForm() {
  variablePage.value = 1;
  form.name = '';
  form.description = '';
  form.model_service_id = null;
  form.template = '请根据已知信息回答问题。\n\n用户问题：{{ .question }}';
  form.variables.splice(0, form.variables.length, {
    name: 'question', type: 'string', description: '用户提出的问题', required: true, default: null
  });
  dirty.value = false;
}

function applyDetail(value: Api.Management.AssetDetail) {
  variablePage.value = 1;
  form.name = value.name;
  form.description = value.description;
  form.model_service_id = value.spec.model_service_id || null;
  form.template = value.spec.template;
  form.variables.splice(0, form.variables.length, ...value.spec.variables.map(variable => ({
    name: variable.name,
    type: variable.type,
    description: variable.description ?? '',
    required: variable.required,
    default: (variable.default ?? null) as string | number | boolean | null
  })));
  dirty.value = false;
}

function addVariable() {
  if (form.variables.length >= maxVariables) {
    window.$message?.info(`一个模板最多设置 ${maxVariables} 个变量`);
    return;
  }
  form.variables.push({ name: '', type: 'string', description: '', required: false, default: null });
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
    window.$message?.warning('请填写模板名称');
    return false;
  }
  if (step === 2) {
    const names = new Set<string>();
    for (const variable of form.variables) {
      const name = variable.name.trim();
      if (!/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(name)) {
        window.$message?.warning('变量名只能使用字母、数字和下划线，且不能以数字开头');
        return false;
      }
      if (names.has(name)) {
        window.$message?.warning(`变量 ${name} 重复，请修改后再继续`);
        return false;
      }
      names.add(name);
    }
  }
  if (step === 3 && !form.template.trim()) {
    window.$message?.warning('请填写模板内容');
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
  if (!validateStep(1) || !validateStep(2) || !validateStep(3)) return;
  saving.value = true;
  try {
    let id = props.assetId;
    let version = 1;
    if (id && detail.value) {
      const { data, error } = await fetchUpdateAsset(id, { ...buildParams(), expected_version: detail.value.latest_version });
      if (error || !data) return;
      version = data.version;
    } else {
      const { data, error } = await fetchCreateAsset(buildParams());
      if (error || !data) return;
      id = data.id;
    }
    dirty.value = false;
    emit('saved');
    emit('update:show', false);
    window.$message?.success(props.assetId ? `修改已保存为版本 ${version}，可在「版本」中启用` : '模板已创建，可以继续测试或启用');
    if (openTest && id) emit('test', id);
  } finally {
    saving.value = false;
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
    :title="props.assetId ? '编辑模板' : '新建模板'"
    class="w-760px"
    :mask-closable="false"
    @update:show="value => { if (value) emit('update:show', true); else requestClose(); }"
  >
    <NSpin :show="loading">
      <NSteps :current="currentStep" class="mb-24px">
        <NStep title="基本信息" description="名称和用途" />
        <NStep title="变量设置" description="定义可替换的内容" />
        <NStep title="模板内容" description="编写并插入变量" />
      </NSteps>

      <NAlert v-if="detail?.status === 'archived'" type="info" :bordered="false" class="mb-16px">
        已归档模板只能查看，不能继续编辑。
      </NAlert>

      <NForm v-show="currentStep === 1" label-placement="left" label-width="100" :disabled="detail?.status === 'archived'">
        <NFormItem label="模板名称" required>
          <NInput v-model:value="form.name" maxlength="120" placeholder="例如：客服政策问答" />
        </NFormItem>
        <NFormItem label="用途说明">
          <NInput v-model:value="form.description" type="textarea" :rows="3" maxlength="2000" placeholder="说明这个模板用于什么场景" />
        </NFormItem>
        <NFormItem label="模型服务">
          <NSelect v-model:value="form.model_service_id" clearable filterable :options="modelOptions" placeholder="可选，选择默认使用的模型服务" />
        </NFormItem>
      </NForm>

      <div v-show="currentStep === 2">
        <div class="mb-12px flex items-center justify-between">
          <NText depth="2">已添加 {{ form.variables.length }} / {{ maxVariables }} 个变量，没有变量也可以直接继续。</NText>
          <NButton size="small" type="primary" secondary :disabled="detail?.status === 'archived' || form.variables.length >= maxVariables" @click="addVariable">添加变量</NButton>
        </div>
        <NDataTable
          :columns="variableColumns"
          :data="pagedVariables"
          :bordered="false"
          size="small"
          :scroll-x="760"
        />
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
          @update:value="value => form.template = value"
        />
      </div>
    </NSpin>

    <template #footer>
      <div class="flex items-center justify-between">
        <NButton @click="requestClose">取消</NButton>
        <NSpace>
          <NButton v-if="currentStep > 1" @click="currentStep -= 1">上一步</NButton>
          <NButton v-if="currentStep < 3" type="primary" @click="nextStep">下一步</NButton>
          <template v-else-if="detail?.status !== 'archived'">
            <NButton :loading="saving" @click="save(true)">保存并测试</NButton>
            <NButton type="primary" :loading="saving" @click="save(false)">
              {{ props.assetId ? '保存为新版本' : '创建模板' }}
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
    title="关闭编辑？"
    positive-text="放弃修改"
    negative-text="继续编辑"
    @positive-click="discardAndClose"
  >
    当前修改尚未保存，关闭后将丢失。
  </NModal>
</template>
