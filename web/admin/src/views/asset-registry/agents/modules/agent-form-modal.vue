<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { fetchAssetDetail, fetchAssets, fetchCreateAsset, fetchModelServices, fetchUpdateAsset } from '@/service/api';
import { $t } from '@/locales';

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
let editGeneration = 0;
let promptGeneration = 0;
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

const promptOptions = computed(() =>
  prompts.value.filter(item => item.status !== 'archived').map(item => ({ label: item.name, value: item.id }))
);
const modelOptions = computed(() =>
  models.value.filter(item => item.status === 'active').map(item => ({ label: item.name, value: item.id }))
);
const versionOptions = computed(() =>
  (promptDetail.value?.versions ?? []).map(item => ({
    label: $t('business.versionWithStatus', {
      version: item.version,
      status: item.current ? $t('business.current') : ''
    }),
    value: item.id
  }))
);
const historyOptions = computed(() =>
  [10, 20, 40, 60, 100].map(value => ({
    label: $t('business.messageCount', { count: value }),
    value
  }))
);

watch(
  [() => props.show, () => props.assetId],
  ([visible]) => {
    editGeneration++;
    promptGeneration++;
    saving.value = false;
    if (visible) void initialize();
    else loading.value = false;
  }
);

async function initialize() {
  const generation = ++editGeneration;
  const assetId = props.assetId;
  loading.value = true;
  promptDetail.value = null;
  latestVersion.value = 0;
  Object.assign(form, {
    name: '', description: '', prompt_template_id: null, prompt_version_id: null, model_service_id: null,
    temperature: 0.7, top_p: 1, max_tokens: 2048, max_history_messages: 20
  });
  try {
    const [promptRes, modelRes] = await Promise.all([fetchAssets('prompt_template'), fetchModelServices()]);
    if (generation !== editGeneration) return;
    if (!promptRes.error && promptRes.data) prompts.value = promptRes.data;
    if (!modelRes.error && modelRes.data) models.value = modelRes.data;
    if (assetId) {
      const { data, error } = await fetchAssetDetail<Api.Management.AgentSpec>(assetId);
      if (generation !== editGeneration || error || !data) return;
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
        name: '',
        description: '',
        prompt_template_id: null,
        prompt_version_id: null,
        model_service_id: null,
        temperature: 0.7,
        top_p: 1,
        max_tokens: 2048,
        max_history_messages: 20
      });
    }
  } finally {
    if (generation === editGeneration) loading.value = false;
  }
}

async function loadPromptVersions(id: string | null, reset = true) {
  const generation = ++promptGeneration;
  const parentGeneration = editGeneration;
  form.prompt_template_id = id;
  promptDetail.value = null;
  if (reset) {
    form.prompt_version_id = null;
    form.model_service_id = null;
  }
  if (!id) return;
  const { data, error } = await fetchAssetDetail(id);
  if (generation !== promptGeneration || parentGeneration !== editGeneration || form.prompt_template_id !== id || error || !data) return;
  promptDetail.value = data;
  if (reset) {
    form.prompt_version_id = data.versions.find(item => item.version === data.latest_version)?.id ?? null;
    if (!form.model_service_id) form.model_service_id = data.spec.model_service_id || null;
  }
}

async function save() {
  if (loading.value || saving.value || (props.assetId && !latestVersion.value)) return;
  if (!form.name.trim() || !form.prompt_template_id || !form.prompt_version_id || !form.model_service_id) {
    window.$message?.warning($t('business.enter-a-name-and-select'));
    return;
  }
  const generation = editGeneration;
  const assetId = props.assetId;
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
    const params = {
      asset_type: 'agent' as const,
      name: form.name.trim(),
      description: form.description.trim(),
      spec
    };
    const { error } = assetId
      ? await fetchUpdateAsset(assetId, {
          ...params,
          expected_version: latestVersion.value
        })
      : await fetchCreateAsset(params);
    if (!error && generation === editGeneration) {
      window.$message?.success(
        assetId ? $t('business.agent-configuration-saved-as-a') : $t('business.agent-created')
      );
      emit('update:show', false);
      emit('saved');
    }
  } finally {
    if (generation === editGeneration) saving.value = false;
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="props.assetId ? $t('business.editAgent') : $t('business.createAgent')"
    class="w-760px"
    @update:show="value => emit('update:show', value)"
  >
    <NSpin :show="loading">
      <NForm label-placement="top">
        <NGrid cols="1 640:2" :x-gap="16">
          <NFormItemGi :label="$t('business.name')" required>
            <NInput v-model:value="form.name" :placeholder="$t('business.for-example-customer-assistant')" />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.model-service')" required>
            <NSelect
              v-model:value="form.model_service_id"
              filterable
              :options="modelOptions"
              :placeholder="$t('business.select-a-model-service')"
            />
          </NFormItemGi>
        </NGrid>
        <NFormItem :label="$t('business.usage-description')">
          <NInput v-model:value="form.description" :placeholder="$t('business.describe-where-this-agent-is')" />
        </NFormItem>
        <NGrid cols="1 640:2" :x-gap="16">
          <NFormItemGi :label="$t('business.prompt-template')" required>
            <NSelect
              :value="form.prompt_template_id"
              filterable
              :options="promptOptions"
              :placeholder="$t('business.select-a-prompt-template')"
              @update:value="loadPromptVersions"
            />
          </NFormItemGi>
          <NFormItemGi :label="$t('business.prompt-version')" required>
            <NSelect
              v-model:value="form.prompt_version_id"
              :options="versionOptions"
              :placeholder="$t('business.select-a-pinned-version')"
            />
          </NFormItemGi>
        </NGrid>
        <NAlert type="info" :bordered="false" class="mb-16px">{{ $t('business.the-agent-always-uses-the') }}</NAlert>
        <NCollapse>
          <NCollapseItem :title="$t('business.generation-and-context-settings')" name="advanced">
            <NGrid cols="1 640:2" :x-gap="16">
              <NFormItemGi :label="$t('business.temperature')">
                <NInputNumber v-model:value="form.temperature" :min="0" :max="2" :step="0.1" class="w-full" />
              </NFormItemGi>
              <NFormItemGi label="Top P">
                <NInputNumber v-model:value="form.top_p" :min="0.01" :max="1" :step="0.05" class="w-full" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.max-output-tokens')">
                <NInputNumber v-model:value="form.max_tokens" :min="1" :max="32768" class="w-full" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.include-history-messages')">
                <NSelect v-model:value="form.max_history_messages" :options="historyOptions" />
              </NFormItemGi>
            </NGrid>
          </NCollapseItem>
        </NCollapse>
      </NForm>
    </NSpin>
    <template #footer>
      <NSpace justify="end">
        <NButton :disabled="saving" @click="emit('update:show', false)">{{ $t('business.cancel') }}</NButton>
        <NButton type="primary" :loading="saving" :disabled="loading" @click="save">
          {{ props.assetId ? $t('business.save-as-new-version') : $t('business.createAgent') }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>
