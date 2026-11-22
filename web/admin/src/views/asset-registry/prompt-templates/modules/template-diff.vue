<script setup lang="ts">
import { computed } from 'vue';
import DOMPurify from 'dompurify';
import { html } from 'diff2html';
import 'diff2html/bundles/css/diff2html.min.css';

const props = defineProps<{
  diff: string;
}>();

const content = computed(() => DOMPurify.sanitize(html(props.diff, {
  drawFileList: false,
  matching: 'words',
  outputFormat: 'side-by-side'
}), {
  USE_PROFILES: { html: true }
}));
</script>

<template>
  <div class="prompt-diff" v-html="content" />
</template>

<style scoped>
.prompt-diff {
  max-height: 480px;
  overflow: auto;
  text-align: left;
}

.prompt-diff :deep(.d2h-file-wrapper) {
  margin-bottom: 0;
  border-color: #e0e0e6;
  border-radius: 6px;
}

.prompt-diff :deep(.d2h-file-header) {
  display: none;
}

.prompt-diff :deep(.d2h-diff-table) {
  width: 100%;
  table-layout: fixed;
}

.prompt-diff :deep(.d2h-code-line),
.prompt-diff :deep(.d2h-code-side-line) {
  display: block;
  box-sizing: border-box;
  overflow: visible;
  width: max-content;
  min-width: 100%;
  height: 24px;
  min-height: 0;
  padding: 0 8px;
  font-family: 'Cascadia Code', monospace;
  font-size: 12px;
  line-height: 24px;
  text-overflow: clip;
  vertical-align: top;
  white-space: nowrap;
  word-break: normal;
}

.prompt-diff :deep(.d2h-code-line-ctn) {
  display: inline;
  width: auto;
  line-height: 24px;
  white-space: pre;
  word-break: normal;
}

.prompt-diff :deep(.d2h-code-linenumber),
.prompt-diff :deep(.d2h-code-side-linenumber) {
  position: static;
  display: table-cell;
  box-sizing: border-box;
  width: 54px;
  min-width: 54px;
  height: 24px;
  padding: 0 8px;
  line-height: 24px;
  text-align: right;
  vertical-align: top;
  background: #f7f8fa;
}

.prompt-diff :deep(.d2h-code-line del),
.prompt-diff :deep(.d2h-code-line ins),
.prompt-diff :deep(.d2h-code-side-line del),
.prompt-diff :deep(.d2h-code-side-line ins) {
  display: inline;
}

.prompt-diff :deep(.d2h-file-side-diff) {
  overflow-x: auto;
  overflow-y: hidden;
  vertical-align: top;
}

:global(.dark) .prompt-diff {
  color-scheme: dark;
}

:global(.dark) .prompt-diff :deep(.d2h-file-wrapper) {
  border-color: #3a3a40;
}

:global(.dark) .prompt-diff :deep(.d2h-file-header),
:global(.dark) .prompt-diff :deep(.d2h-code-linenumber) {
  color: #d5dbe7;
  background: #1f1f23;
}
</style>
