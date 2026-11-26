<script setup lang="ts">
import { computed, h, reactive, ref, watch } from 'vue';
import { NTag } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { fetchPlaygroundRoutePolicy } from '@/service/api';
import { formatCurrency } from '@/utils/currency';
import { routeStrategyText } from '@/utils/admin-status';
import { $t } from '@/locales';
import { useCapabilityOptions } from './policy-shared';

const props = defineProps<{
  show: boolean;
  policy: Api.Management.RoutePolicy | null;
  modelServices: Api.Management.ModelService[];
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
}>();

const running = ref(false);
const result = ref<Api.Management.PlaygroundRoutePolicyResult | null>(null);
const capabilityOptions = useCapabilityOptions();

const form = reactive<{
  model_service_id: string | null;
  prompt: string;
  stream: boolean;
  required_capabilities: string[];
}>({
  model_service_id: null,
  prompt: '',
  stream: false,
  required_capabilities: []
});

const serviceOptions = computed<SelectOption[]>(() =>
  props.modelServices
    .filter(s => s.status === 'active')
    .map(s => ({ label: s.name, value: s.id }))
);

watch(
  () => [props.show, props.policy],
  () => {
    if (!props.show) return;
    result.value = null;
    if (!form.prompt) {
      form.prompt = $t('business.default-playground-prompt');
    }
    const bound = props.policy?.bound_services?.[0]?.id;
    form.model_service_id = bound ?? serviceOptions.value[0]?.value?.toString() ?? null;
  },
  { immediate: true }
);

const attemptColumns = computed<DataTableColumns<Api.Management.PlaygroundAttemptRecord>>(() => [
  { title: $t('business.provider-c4b9p'), key: 'provider', width: 120 },
  { title: $t('business.upstream-model'), key: 'upstream_model', minWidth: 160 },
  {
    title: $t('business.result'),
    key: 'outcome',
    width: 110,
    render: row =>
      h(
        NTag,
        { size: 'small', type: row.outcome === 'success' ? 'success' : 'error', round: true },
        { default: () => row.outcome }
      )
  },
  { title: $t('business.http-status-code'), key: 'status_code', width: 110, render: row => row.status_code || '-' },
  { title: $t('business.latency-ms'), key: 'latency_ms', width: 100, render: row => `${row.latency_ms} ms` },
  { title: $t('business.error-detail'), key: 'error_message', minWidth: 160, render: row => row.error_message || '-' }
]);

async function handleExecute() {
  if (!props.policy) return;
  if (!form.model_service_id) {
    window.$message?.warning($t('business.select-a-model-service'));
    return;
  }
  if (!form.prompt.trim()) {
    window.$message?.warning($t('business.playground-prompt-required'));
    return;
  }
  running.value = true;
  try {
    const { data, error } = await fetchPlaygroundRoutePolicy(props.policy.id, {
      model_service_id: form.model_service_id,
      prompt: form.prompt.trim(),
      stream: form.stream,
      required_capabilities: form.required_capabilities
    });
    if (!error && data) {
      result.value = data;
    }
  } finally {
    running.value = false;
  }
}
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    :title="`${$t('business.online-playground')} · ${policy?.name ?? ''}`"
    class="w-880px max-w-95vw"
    @update:show="val => emit('update:show', val)"
  >
    <NForm label-placement="top">
      <NGrid :cols="2" :x-gap="12">
        <NFormItemGi :label="$t('business.model-service')">
          <NSelect v-model:value="form.model_service_id" :options="serviceOptions" filterable />
        </NFormItemGi>
        <NFormItemGi :label="$t('business.rule-capabilities')">
          <NSelect v-model:value="form.required_capabilities" :options="capabilityOptions" multiple clearable />
        </NFormItemGi>
      </NGrid>
      <NFormItem :label="$t('business.prompt-input')">
        <NInput v-model:value="form.prompt" type="textarea" :rows="3" />
      </NFormItem>
    </NForm>
    <div class="mb-12px flex justify-end">
      <NButton type="primary" :loading="running" @click="handleExecute">
        {{ $t('business.run-live-test') }}
      </NButton>
    </div>

    <template v-if="result">
      <NDescriptions bordered size="small" :column="3" class="mb-12px">
        <NDescriptionsItem :label="$t('business.served-model')">
          {{ result.served_provider }} / {{ result.served_model }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.sim-effective-strategy')">
          {{ routeStrategyText(result.decision.effective_strategy) }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.latency-ms')">
          {{ result.total_ms }} ms
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.tokens-io')">
          {{ result.input_tokens }} / {{ result.output_tokens }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.cost')">
          {{ formatCurrency(result.cost, result.currency) }}
        </NDescriptionsItem>
        <NDescriptionsItem :label="$t('business.sim-matched-rule')">
          {{ result.decision.matched_rule_name || '-' }}
        </NDescriptionsItem>
      </NDescriptions>
      <NCard size="small" :title="$t('business.model-reply')" class="mb-12px">
        <div class="whitespace-pre-wrap text-13px">{{ result.reply }}</div>
      </NCard>
      <NDataTable :columns="attemptColumns" :data="result.attempts" size="small" :bordered="false" />
    </template>
  </NModal>
</template>
