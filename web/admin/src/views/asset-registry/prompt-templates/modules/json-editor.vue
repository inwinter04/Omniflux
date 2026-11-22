<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { json } from '@codemirror/lang-json';
import { minimalSetup } from 'codemirror';
import { EditorState } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { applyEdits, format, parse, printParseErrorCode } from 'jsonc-parser';
import type { ParseError } from 'jsonc-parser';
import type { TreeOption } from 'naive-ui';

const props = defineProps<{
  value: string;
  disabled?: boolean;
  required?: boolean;
  height?: number;
}>();

const emit = defineEmits<{
  'update:value': [value: string];
  'update:valid': [value: boolean];
}>();

const host = ref<HTMLDivElement | null>(null);
const viewMode = ref<'source' | 'tree'>('source');
let editor: EditorView | null = null;

function parseDocument(source: string) {
  if (!source.trim()) return { value: undefined, issues: [] as ParseError[] };
  const issues: ParseError[] = [];
  const value = parse(source, issues, { allowTrailingComma: false, disallowComments: true });
  return { value, issues };
}

const parsed = computed(() => parseDocument(props.value));
const errorText = computed(() => {
  if (!props.value.trim()) return props.required ? '请输入 JSON 内容' : '';
  return parsed.value.issues.map(issue => {
    const line = props.value.slice(0, issue.offset).split('\n').length;
    return `第 ${line} 行：${printParseErrorCode(issue.error)}`;
  }).join('；');
});

watch(errorText, error => emit('update:valid', !error), { immediate: true });

const treeData = computed<TreeOption[]>(() => {
  if (errorText.value) return [];
  return [toTree('根对象', parsed.value.value, 'root')];
});

function toTree(label: string, value: unknown, key: string): TreeOption {
  if (Array.isArray(value)) {
    return { key, label: `${label}（数组，${value.length} 项）`, children: value.map((item, index) => toTree(`[${index}]`, item, `${key}.${index}`)) };
  }
  if (value !== null && typeof value === 'object') {
    const entries = Object.entries(value);
    return {
      key,
      label: `${label}（对象，${entries.length} 个字段）`,
      children: entries.map(([childKey, child]) => toTree(childKey, child, `${key}.${childKey}`))
    };
  }
  return { key, label: `${label}：${JSON.stringify(value)}` };
}

function formatDocument() {
  if (!props.value.trim()) return;
  if (errorText.value) {
    window.$message?.warning('请先修正 JSON 错误，再进行格式化');
    return;
  }
  const edits = format(props.value, undefined, { insertSpaces: true, tabSize: 2, eol: '\n' });
  emit('update:value', applyEdits(props.value, edits));
}

onMounted(() => {
  if (!host.value) return;
  editor = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: props.value,
      extensions: [
        minimalSetup,
        json(),
        EditorState.readOnly.of(Boolean(props.disabled)),
        EditorView.updateListener.of(update => {
          if (update.docChanged) emit('update:value', update.state.doc.toString());
        })
      ]
    })
  });
});

watch(() => props.value, value => {
  if (!editor || value === editor.state.doc.toString()) return;
  editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: value } });
});

watch(() => props.disabled, disabled => {
  if (!editor) return;
  const current = editor.state.doc.toString();
  editor.destroy();
  if (!host.value) return;
  editor = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: current,
      extensions: [minimalSetup, json(), EditorState.readOnly.of(Boolean(disabled)), EditorView.updateListener.of(update => {
        if (update.docChanged) emit('update:value', update.state.doc.toString());
      })]
    })
  });
});

onBeforeUnmount(() => editor?.destroy());
</script>

<template>
  <div>
    <NSpace justify="space-between" class="mb-8px">
      <NButton size="small" secondary :disabled="disabled" @click="formatDocument">格式化</NButton>
      <NRadioGroup v-model:value="viewMode" size="small">
        <NRadioButton value="source">源码</NRadioButton>
        <NRadioButton value="tree">树状预览</NRadioButton>
      </NRadioGroup>
    </NSpace>
    <div v-show="viewMode === 'source'" ref="host" class="json-code-editor" :style="{ '--json-editor-height': `${height || 180}px` }" />
    <NCard v-show="viewMode === 'tree'" size="small" class="json-tree-preview" :style="{ maxHeight: `${(height || 180) + 24}px` }">
      <NTree v-if="!errorText" :data="treeData" block-line default-expand-all />
    </NCard>
    <NAlert v-if="errorText" type="error" :bordered="false" size="small" class="mt-8px">{{ errorText }}</NAlert>
  </div>
</template>

<style scoped>
.json-code-editor {
  overflow: hidden;
  border: 1px solid #e0e0e6;
  border-radius: 6px;
}

.json-code-editor :deep(.cm-editor) {
  height: var(--json-editor-height);
  font-size: 13px;
}

.json-code-editor :deep(.cm-editor.cm-focused) {
  outline: none;
}

.json-tree-preview {
  overflow: auto;
}

:global(.dark) .json-code-editor {
  border-color: #3a3a40;
}

:global(.dark) .json-code-editor :deep(.cm-editor) {
  color: #e5e7eb;
  background: #1f1f23;
}

:global(.dark) .json-code-editor :deep(.cm-gutters) {
  color: #8b8b93;
  background: #1f1f23;
  border-right-color: #3a3a40;
}
</style>
