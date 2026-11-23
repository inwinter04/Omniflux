<script setup lang="ts">
import { formatCurrency } from '@/utils/currency';
import { $t } from '@/locales';
import MarkdownPreview from './markdown-preview.vue';

const props = defineProps<{
  run: Api.Management.PromptRun;
}>();

function runErrorText(error: string) {
  return error === 'prompt_run_finalize_failed' ? $t('backendError.promptRunFinalizeFailed') : error;
}

function statusText(status: string) {
  switch (status) {
    case 'success':
      return $t('business.success');
    case 'failed':
      return $t('business.failed');
    case 'interrupted':
      return $t('business.interrupted');
    default:
      return $t('business.running');
  }
}

function statusType(status: string): 'success' | 'error' | 'warning' | 'info' {
  if (status === 'success') return 'success';
  if (status === 'failed') return 'error';
  if (status === 'interrupted') return 'warning';
  return 'info';
}
</script>

<template>
  <NCard size="small" :title="props.run.model_service_name || props.run.model_service_id" class="mb-12px">
    <template #header-extra>
      <NSpace>
        <NTag v-if="props.run.schema_valid !== null" size="small" :type="props.run.schema_valid ? 'success' : 'error'">
          {{ props.run.schema_valid ? $t('business.schemaValidationPassed') : $t('business.schemaValidationFailed') }}
        </NTag>
        <NTag size="small" :type="statusType(props.run.status)">{{ statusText(props.run.status) }}</NTag>
      </NSpace>
    </template>
    <NSpace size="large" class="mb-12px">
      <NText depth="2">{{ $t('business.input') }} {{ props.run.input_tokens }} {{ $t('business.tokens') }}</NText>
      <NText depth="2">{{ $t('business.output') }} {{ props.run.output_tokens }} {{ $t('business.tokens') }}</NText>
      <NText depth="2">{{ $t('business.cost') }} {{ formatCurrency(props.run.cost, props.run.cost_currency) }}</NText>
      <NText v-if="props.run.first_token_ms !== null" depth="2">
        {{ $t('business.prompt-tokens') }} {{ props.run.first_token_ms }} ms
      </NText>
      <NText depth="2">{{ $t('business.total-latency') }} {{ props.run.total_ms }} ms</NText>
    </NSpace>
    <NAlert v-if="props.run.error" type="error" :bordered="false" class="mb-12px">
      {{ runErrorText(props.run.error) }}
    </NAlert>
    <MarkdownPreview v-if="props.run.response" :source="props.run.response" />
    <NCollapse class="mt-12px">
      <NCollapseItem :title="$t('business.view-actual-input')" name="prompt">
        <NInput :value="props.run.rendered_prompt" type="textarea" readonly :rows="5" />
      </NCollapseItem>
    </NCollapse>
  </NCard>
</template>
