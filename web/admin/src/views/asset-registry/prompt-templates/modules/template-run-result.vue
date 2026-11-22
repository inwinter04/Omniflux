<script setup lang="ts">
import MarkdownPreview from './markdown-preview.vue';

const props = defineProps<{
  run: Api.Management.PromptRun;
}>();

function statusText(status: string) {
  switch (status) {
    case 'success': return '成功';
    case 'failed': return '失败';
    case 'interrupted': return '已中断';
    default: return '运行中';
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
          {{ props.run.schema_valid ? '结构校验通过' : '结构校验失败' }}
        </NTag>
        <NTag size="small" :type="statusType(props.run.status)">{{ statusText(props.run.status) }}</NTag>
      </NSpace>
    </template>
    <NSpace size="large" class="mb-12px">
      <NText depth="2">输入 {{ props.run.input_tokens }} Token</NText>
      <NText depth="2">输出 {{ props.run.output_tokens }} Token</NText>
      <NText depth="2">成本 ${{ props.run.cost.toFixed(6) }}</NText>
      <NText v-if="props.run.first_token_ms !== null" depth="2">首 Token {{ props.run.first_token_ms }} ms</NText>
      <NText depth="2">总耗时 {{ props.run.total_ms }} ms</NText>
    </NSpace>
    <NAlert v-if="props.run.error" type="error" :bordered="false" class="mb-12px">{{ props.run.error }}</NAlert>
    <MarkdownPreview v-if="props.run.response" :source="props.run.response" />
    <NCollapse class="mt-12px">
      <NCollapseItem title="查看实际输入" name="prompt">
        <NInput :value="props.run.rendered_prompt" type="textarea" readonly :rows="5" />
      </NCollapseItem>
    </NCollapse>
  </NCard>
</template>
