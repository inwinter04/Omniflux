<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import {
  fetchAssetDetail,
  fetchAssets,
  fetchCreateAsset,
  fetchModelServices,
  fetchUpdateAsset
} from '@/service/api';

const props = defineProps<{
  show: boolean;
  assetId: string | null;
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
  saved: [];
}>();

const loading = ref(false);
const saving = ref(false);
const prompts = ref<Api.Management.Asset[]>([]);
const models = ref<Api.Management.ModelService[]>([]);
const promptDetail = ref<Api.Management.AssetDetail | null>(null);
const latestVersion = ref(0);
const form = reactive({
  name: '',
  description: '',
  prompt_template_id: null as string | null,
  prompt_version_id: null as string | null,
  model_service_id: null as string | null,
  temperature: 0.7 as number | null,
  top_p: 1 as number | null,
  max_tokens: 2048,
  max_history_messages: 20
});

const promptOptions = computed(() => prompts.value.filter(item => item.status !== 'archived').map(item => ({ label: item.name, value: item.id })));
const modelOptions = computed(() => models.value.filter(item => item.status === 'active').map(item => ({ label: item.name, value: item.id })));
const versionOptions = computed(() => (promptDetail.value?.versions ?? []).map(item => ({
  label: `版本 ${item.version}${item.current ? '（当前使用）' : ''}`,
  value: item.id
})));
const historyOptions = [10, 20, 40, 60, 100].map(value => ({ label: `${value} 条消息`, value }));

watch(() => props.show, visible => {
  if (visible) void initialize();
});

async function initialize() {
  loading.value = true;
  promptDetail.value = null;
  try {
    const [promptRes, modelRes] = await Promise.all([fetchAssets('prompt_template'), fetchModelServices()]);
    if (!promptRes.error && promptRes.data) prompts.value = promptRes.data;
    if (!modelRes.error && modelRes.data) models.value = modelRes.data;
    if (props.assetId) {
      const { data, error } = await fetchAssetDetail<Api.Management.AgentSpec>(props.assetId);
      if (error || !data) return;
      latestVersion.value = data.latest_version;
      Object.assign(form, {
        name: data.name,
        description: data.description,
        prompt_template_id: data.spec.prompt_template_id,
        prompt_version_id: data.spec.prompt_version_id,
        model_service_id: data.spec.model_service_id,
        temperature: data.spec.temperature ?? null,
        top_p: data.spec.top_p ?? null,
        max_tokens: data.spec.max_tokens || 2048,
        max_history_messages: data.spec.max_history_messages
      });
      await loadPromptVersions(data.spec.prompt_template_id, false);
    } else {
      latestVersion.value = 0;
      Object.assign(form, {
        name: '', description: '', prompt_template_id: null, prompt_version_id: null,
        model_service_id: null, temperature: 0.7, top_p: 1, max_tokens: 2048, max_history_messages: 20
      });
    }
  } finally {
    loading.value = false;
  }
}

async function loadPromptVersions(id: string | null, reset = true) {
  form.prompt_template_id = id;
  promptDetail.value = null;
  if (reset) {
    form.prompt_version_id = null;
    form.model_service_id = null;
  }
  if (!id) return;
  const { data, error } = await fetchAssetDetail(id);
  if (error || !data) return;
  promptDetail.value = data;
  if (reset) {
    form.prompt_version_id = data.versions.find(item => item.version === data.latest_version)?.id ?? null;
    if (!form.model_service_id) form.model_service_id = data.spec.model_service_id || null;
  }
}

async function save() {
  if (!form.name.trim() || !form.prompt_template_id || !form.prompt_version_id || !form.model_service_id) {
    window.$message?.warning('请填写名称，并选择提示词版本和模型服务');
    return;
  }
  saving.value = true;
  try {
    const spec: Api.Management.AgentSpec = {
      prompt_template_id: form.prompt_template_id,
      prompt_version_id: form.prompt_version_id,
      model_service_id: form.model_service_id,
      max_tokens: form.max_tokens,
      max_history_messages: form.max_history_messages
    };
    if (form.temperature !== null) spec.temperature = form.temperature;
    if (form.top_p !== null) spec.top_p = form.top_p;
    const params = { asset_type: 'agent' as const, name: form.name.trim(), description: form.description.trim(), spec };
    const { error } = props.assetId
      ? await fetchUpdateAsset(props.assetId, { ...params, expected_version: latestVersion.value })
      : await fetchCreateAsset(params);
    if (!error) {
      window.$message?.success(props.assetId ? '智能体配置已保存为新版本' : '智能体已创建');
      emit('update:show', false);
      emit('saved');
    }
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <NModal :show="props.show" preset="card" :title="props.assetId ? '编辑智能体' : '新建智能体'" class="w-760px" @update:show="value => emit('update:show', value)">
    <NSpin :show="loading">
      <NForm label-placement="top">
        <NGrid cols="1 640:2" :x-gap="16">
          <NFormItemGi label="名称" required><NInput v-model:value="form.name" placeholder="例如：客户助手" /></NFormItemGi>
          <NFormItemGi label="模型服务" required><NSelect v-model:value="form.model_service_id" filterable :options="modelOptions" placeholder="选择模型服务" /></NFormItemGi>
        </NGrid>
        <NFormItem label="用途说明"><NInput v-model:value="form.description" placeholder="说明这个 智能体的使用场景" /></NFormItem>
        <NGrid cols="1 640:2" :x-gap="16">
          <NFormItemGi label="提示词模板" required>
            <NSelect :value="form.prompt_template_id" filterable :options="promptOptions" placeholder="选择提示词模板" @update:value="loadPromptVersions" />
          </NFormItemGi>
          <NFormItemGi label="提示词版本" required>
            <NSelect v-model:value="form.prompt_version_id" :options="versionOptions" placeholder="选择固定版本" />
          </NFormItemGi>
        </NGrid>
        <NAlert type="info" :bordered="false" class="mb-16px">智能体会固定使用所选提示词版本，已有对话不会因后续发布而改变。</NAlert>
        <NCollapse>
          <NCollapseItem title="生成与上下文设置" name="advanced">
            <NGrid cols="1 640:2" :x-gap="16">
              <NFormItemGi label="温度"><NInputNumber v-model:value="form.temperature" :min="0" :max="2" :step="0.1" class="w-full" /></NFormItemGi>
              <NFormItemGi label="Top P"><NInputNumber v-model:value="form.top_p" :min="0.01" :max="1" :step="0.05" class="w-full" /></NFormItemGi>
              <NFormItemGi label="最大输出 令牌"><NInputNumber v-model:value="form.max_tokens" :min="1" :max="32768" class="w-full" /></NFormItemGi>
              <NFormItemGi label="携带历史消息"><NSelect v-model:value="form.max_history_messages" :options="historyOptions" /></NFormItemGi>
            </NGrid>
          </NCollapseItem>
        </NCollapse>
      </NForm>
    </NSpin>
    <template #footer>
      <NSpace justify="end">
        <NButton @click="emit('update:show', false)">取消</NButton>
        <NButton type="primary" :loading="saving" @click="save">{{ props.assetId ? '保存为新版本' : '创建智能体' }}</NButton>
      </NSpace>
    </template>
  </NModal>
</template>
