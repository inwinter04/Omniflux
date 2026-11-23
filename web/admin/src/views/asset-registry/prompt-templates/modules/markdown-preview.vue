<script setup lang="ts">
import { computed } from 'vue';
import MarkdownIt from 'markdown-it';
import DOMPurify from 'dompurify';
import 'github-markdown-css/github-markdown.css';

const props = defineProps<{
  source: string;
  compact?: boolean;
}>();

const parser = new MarkdownIt({
  html: false,
  linkify: true,
  breaks: true
});

parser.renderer.rules.link_open = (tokens, index, options, env, self) => {
  tokens[index].attrSet('target', '_blank');
  tokens[index].attrSet('rel', 'noopener noreferrer nofollow');
  return self.renderToken(tokens, index, options);
};

const content = computed(() => DOMPurify.sanitize(parser.render(props.source), {
  USE_PROFILES: { html: true },
  ADD_ATTR: ['target', 'rel']
}));
</script>

<template>
  <div
    v-if="source.trim()"
    class="markdown-body prompt-markdown-preview"
    :style="props.compact ? { minHeight: '0', maxHeight: 'none', padding: '0' } : undefined"
    v-html="content"
  />
  <NEmpty v-else description="暂无内容" />
</template>

<style scoped>
.prompt-markdown-preview {
  min-height: 180px;
  max-height: 420px;
  overflow: auto;
  padding: 4px 2px;
  color: inherit;
  background: transparent;
  font-size: 14px;
  line-height: 1.75;
}

.prompt-markdown-preview :deep(> :first-child) {
  margin-top: 0;
}

.prompt-markdown-preview :deep(> :last-child) {
  margin-bottom: 0;
}

.prompt-markdown-preview :deep(pre) {
  max-height: 280px;
  overflow: auto;
}

.prompt-markdown-preview :deep(table) {
  display: table;
  width: max-content;
  max-width: 100%;
}

:global(.dark) .prompt-markdown-preview {
  color: #e5e7eb;
  background: transparent;
  color-scheme: dark;
}
</style>
