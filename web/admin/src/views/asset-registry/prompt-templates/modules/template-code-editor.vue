<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { markdown } from '@codemirror/lang-markdown';
import { syntaxTree } from '@codemirror/language';
import { Compartment, EditorState, StateEffect } from '@codemirror/state';
import type { Range } from '@codemirror/state';
import { Decoration, EditorView, MatchDecorator, ViewPlugin, WidgetType } from '@codemirror/view';
import type { DecorationSet, ViewUpdate } from '@codemirror/view';
import { minimalSetup } from 'codemirror';
import { $t } from '@/locales';

const props = defineProps<{
  value: string;
  variables: Api.Management.PromptVariable[];
  disabled?: boolean;
}>();

const emit = defineEmits<{
  'update:value': [value: string];
}>();

const host = ref<HTMLDivElement | null>(null);
let view: EditorView | null = null;
const editable = new Compartment();
const selectedVariable = ref<string | null>(null);
const suggestionVisible = ref(false);
const suggestionStyle = ref<Record<string, string>>({});
const suggestionQuery = ref('');
const refreshVariableDecorations = StateEffect.define<null>();

class VariableWidget extends WidgetType {
  constructor(
    readonly name: string,
    readonly label: string,
    readonly known: boolean
  ) {
    super();
  }

  eq(other: VariableWidget) {
    return other.name === this.name && other.label === this.label && other.known === this.known;
  }

  toDOM() {
    const element = document.createElement('span');
    element.className = `cm-prompt-variable${this.known ? '' : ' cm-prompt-variable-unknown'}`;
    element.textContent = this.name;
    element.title = this.known
      ? $t('business.variableSummary', { name: this.name, label: this.label })
      : $t('business.variableUndefined', { name: this.name });
    element.setAttribute('aria-label', element.title);
    return element;
  }
}

class MarkdownMarkWidget extends WidgetType {
  constructor(readonly text: string) {
    super();
  }

  eq(other: MarkdownMarkWidget) {
    return other.text === this.text;
  }

  toDOM() {
    const element = document.createElement('span');
    element.className = 'cm-md-list-bullet';
    element.textContent = this.text;
    return element;
  }
}

function variableDecorations(getVariables: () => Api.Management.PromptVariable[]) {
  const matcher = new MatchDecorator({
    regexp: /\{\{\s*\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/g,
    decoration: match => {
      const variable = getVariables().find(item => item.name === match[1]);
      return Decoration.replace({
        widget: new VariableWidget(
          match[1],
          variable ? typeLabel(variable.type) : $t('business.undefined'),
          Boolean(variable)
        )
      });
    }
  });
  return ViewPlugin.fromClass(
    class {
      decorations: DecorationSet;

      constructor(view: EditorView) {
        this.decorations = matcher.createDeco(view);
      }

      update(update: ViewUpdate) {
        const refreshed = update.transactions.some(transaction =>
          transaction.effects.some(effect => effect.is(refreshVariableDecorations))
        );
        if (update.docChanged || update.viewportChanged || refreshed)
          this.decorations = matcher.createDeco(update.view);
      }
    },
    {
      decorations: value => value.decorations,
      provide: plugin => EditorView.atomicRanges.of(view => view.plugin(plugin)?.decorations ?? Decoration.none)
    }
  );
}

function markdownDecorations() {
  const build = (view: EditorView) => {
    const decorations: Range<Decoration>[] = [];
    const ranges = view.state.selection.ranges;
    const intersects = (from: number, to: number) => ranges.some(range => range.from <= to && range.to >= from);
    const hide = (from: number, to: number, revealFrom = from, revealTo = to) => {
      if (!intersects(revealFrom, revealTo)) decorations.push(Decoration.replace({}).range(from, to));
    };
    syntaxTree(view.state).iterate({
      enter(node) {
        const heading = /^ATXHeading([1-6])$/.exec(node.name);
        if (heading) {
          decorations.push(
            Decoration.line({
              class: `cm-md-heading cm-md-heading-${heading[1]}`
            }).range(view.state.doc.lineAt(node.from).from)
          );
        }
        if (node.name === 'HeaderMark')
          hide(node.from, node.to, view.state.doc.lineAt(node.from).from, view.state.doc.lineAt(node.from).to);
        if (node.name === 'StrongEmphasis')
          decorations.push(Decoration.mark({ class: 'cm-md-strong' }).range(node.from, node.to));
        if (node.name === 'Emphasis')
          decorations.push(Decoration.mark({ class: 'cm-md-emphasis' }).range(node.from, node.to));
        if (node.name === 'EmphasisMark')
          hide(node.from, node.to, node.node.parent?.from ?? node.from, node.node.parent?.to ?? node.to);
        if (node.name === 'InlineCode')
          decorations.push(Decoration.mark({ class: 'cm-md-inline-code' }).range(node.from, node.to));
        if (node.name === 'CodeMark')
          hide(node.from, node.to, node.node.parent?.from ?? node.from, node.node.parent?.to ?? node.to);
        if (node.name === 'Link') decorations.push(Decoration.mark({ class: 'cm-md-link' }).range(node.from, node.to));
        if (node.name === 'LinkMark' || node.name === 'URL')
          hide(node.from, node.to, node.node.parent?.from ?? node.from, node.node.parent?.to ?? node.to);
        if (node.name === 'ListMark') {
          const marker = view.state.doc.sliceString(node.from, node.to).trim();
          if (marker === '-' || marker === '*' || marker === '+') {
            decorations.push(Decoration.replace({ widget: new MarkdownMarkWidget('•') }).range(node.from, node.to));
          }
        }
        if (node.name === 'QuoteMark')
          hide(node.from, node.to, view.state.doc.lineAt(node.from).from, view.state.doc.lineAt(node.from).to);
        if (node.name === 'FencedCode' || node.name === 'Blockquote') {
          const start = view.state.doc.lineAt(node.from);
          const end = view.state.doc.lineAt(node.to);
          for (let lineNumber = start.number; lineNumber <= end.number; lineNumber += 1) {
            const line = view.state.doc.line(lineNumber);
            decorations.push(
              Decoration.line({
                class: node.name === 'FencedCode' ? 'cm-md-code-block' : 'cm-md-blockquote'
              }).range(line.from)
            );
          }
        }
      }
    });
    return Decoration.set(decorations, true);
  };
  return ViewPlugin.fromClass(
    class {
      decorations: DecorationSet;

      constructor(view: EditorView) {
        this.decorations = build(view);
      }

      update(update: ViewUpdate) {
        if (update.docChanged || update.selectionSet || update.viewportChanged) this.decorations = build(update.view);
      }
    },
    {
      decorations: value => value.decorations
    }
  );
}

const variableOptions = computed(() =>
  props.variables.map(variable => ({
    label: `${variable.name}（${typeLabel(variable.type)}）`,
    value: variable.name
  }))
);

function typeLabel(type: string) {
  switch (type) {
    case 'number':
      return $t('business.number');
    case 'integer':
      return $t('business.integer');
    case 'boolean':
      return $t('business.boolean');
    default:
      return $t('business.text');
  }
}

const filteredSuggestions = computed(() =>
  props.variables.filter(variable => variable.name.includes(suggestionQuery.value.trim()))
);

function updateSuggestion(editor: EditorView) {
  const cursor = editor.state.selection.main.from;
  const before = editor.state.doc.sliceString(0, cursor);
  const partial = before.match(/\{\{\s*\.?[A-Za-z0-9_]*$/)?.[0];
  const coordinates = editor.coordsAtPos(cursor);
  if (!partial || !coordinates) {
    suggestionVisible.value = false;
    return;
  }
  suggestionQuery.value = partial.replace(/^\{\{\s*\.?/, '');
  suggestionStyle.value = {
    left: `${Math.max(12, Math.min(coordinates.left, window.innerWidth - 300))}px`,
    top: `${Math.min(coordinates.bottom + 8, window.innerHeight - 250)}px`
  };
  suggestionVisible.value = true;
}

function insertVariable(name: string) {
  if (!view) return;
  const selection = view.state.selection.main;
  const before = view.state.doc.sliceString(0, selection.from);
  const partial = before.match(/\{\{\s*\.?[A-Za-z0-9_]*$/)?.[0] ?? '';
  const from = selection.from - partial.length;
  const text = `{{ .${name} }}`;
  const trailingClosers = partial && view.state.doc.sliceString(selection.to, selection.to + 2) === '}}' ? 2 : 0;
  view.dispatch({
    changes: { from, to: selection.to + trailingClosers, insert: text },
    selection: { anchor: from + text.length },
    scrollIntoView: true
  });
  view.focus();
  selectedVariable.value = null;
  suggestionVisible.value = false;
}

onMounted(() => {
  if (!host.value) return;
  view = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: props.value,
      extensions: [
        minimalSetup,
        markdown(),
        markdownDecorations(),
        variableDecorations(() => props.variables),
        EditorView.updateListener.of(update => {
          if (update.docChanged) {
            emit('update:value', update.state.doc.toString());
            updateSuggestion(update.view);
          } else if (update.selectionSet) {
            updateSuggestion(update.view);
          }
        }),
        EditorView.domEventHandlers({
          keydown(event) {
            if (event.key === 'Escape' && suggestionVisible.value) {
              suggestionVisible.value = false;
              return true;
            }
            return false;
          }
        }),
        editable.of([EditorView.editable.of(!props.disabled), EditorState.readOnly.of(Boolean(props.disabled))])
      ]
    })
  });
});

watch(
  () => props.value,
  value => {
    if (!view || value === view.state.doc.toString()) return;
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: value }
    });
  }
);

watch(
  () => props.variables,
  () => {
    view?.dispatch({ effects: refreshVariableDecorations.of(null) });
  },
  { deep: true }
);

watch(
  () => props.disabled,
  disabled => {
    view?.dispatch({
      effects: editable.reconfigure([EditorView.editable.of(!disabled), EditorState.readOnly.of(Boolean(disabled))])
    });
  }
);

onBeforeUnmount(() => {
  view?.destroy();
  view = null;
});
</script>

<template>
  <div>
    <NSpace justify="space-between" align="center" class="mb-8px">
      <NText depth="3">{{ $t('business.type-two-opening-curly-braces') }}</NText>
      <NSelect
        v-model:value="selectedVariable"
        size="small"
        class="min-w-200px w-220px"
        filterable
        :options="variableOptions"
        :disabled="disabled || variables.length === 0"
        :placeholder="$t('business.select-a-variable-to-insert')"
        @update:value="insertVariable"
      />
    </NSpace>
    <div ref="host" class="template-code-editor" />
  </div>
  <Teleport to="body">
    <NCard
      v-if="suggestionVisible"
      size="small"
      :title="$t('business.select-variable')"
      class="prompt-variable-suggestion"
      :style="suggestionStyle"
    >
      <NScrollbar style="max-height: 210px">
        <NButton
          v-for="variable in filteredSuggestions"
          :key="variable.name"
          block
          quaternary
          class="prompt-variable-option"
          @mousedown.prevent="insertVariable(variable.name)"
        >
          {{ variable.name }} · {{ typeLabel(variable.type) }}
        </NButton>
        <NEmpty v-if="filteredSuggestions.length === 0" :description="$t('business.no-matching-variables')" />
      </NScrollbar>
    </NCard>
  </Teleport>
</template>

<style scoped>
.template-code-editor {
  border: 1px solid #e0e0e6;
  border-radius: 6px;
}

:global(.prompt-variable-suggestion) {
  position: fixed;
  z-index: 10000;
  width: 280px;
  box-shadow: 0 8px 24px rgb(15 23 42 / 16%);
}

:global(.prompt-variable-option) {
  justify-content: flex-start;
  font-family: 'Cascadia Code', monospace;
}
:deep(.cm-md-heading) {
  font-weight: 650;
  line-height: 1.45;
}

:deep(.cm-md-heading-1) {
  padding-top: 8px;
  padding-bottom: 6px;
  font-size: 1.65em;
}

:deep(.cm-md-heading-2) {
  padding-top: 6px;
  padding-bottom: 4px;
  font-size: 1.35em;
}

:deep(.cm-md-heading-3) {
  font-size: 1.18em;
}

:deep(.cm-md-strong) {
  font-weight: 700;
}

:deep(.cm-md-emphasis) {
  font-style: italic;
}

:deep(.cm-md-inline-code) {
  padding: 0 3px;
  color: #c7254e;
  border-radius: 3px;
  background: rgb(201 37 78 / 7%);
  font-family: 'Cascadia Code', monospace;
  font-size: 0.9em;
}

:deep(.cm-md-code-block) {
  background: #f6f8fa;
  font-family: 'Cascadia Code', monospace;
  font-size: 0.92em;
}

:deep(.cm-md-blockquote) {
  padding-left: 12px;
  color: #6a737d;
  border-left: 3px solid #dfe2e5;
}

:deep(.cm-md-link) {
  color: #315efb;
  text-decoration: underline;
}

:deep(.cm-md-list-bullet) {
  display: inline-block;
  min-width: 16px;
  color: #315efb;
  font-weight: 700;
}

:deep(.cm-prompt-variable) {
  display: inline-flex;
  align-items: center;
  margin: 0 1px;
  padding: 0 6px;
  color: #315efb;
  border: 1px solid rgb(49 94 251 / 22%);
  border-radius: 4px;
  background: rgb(49 94 251 / 9%);
  font-family: inherit;
  font-size: 0.92em;
  font-weight: 500;
  line-height: 1.65;
  vertical-align: baseline;
}

:deep(.cm-prompt-variable-unknown) {
  color: #d03050;
  border-color: rgb(208 48 80 / 24%);
  background: rgb(208 48 80 / 8%);
}

:deep(.cm-editor) {
  height: 320px;
  font-size: 13px;
}

:deep(.cm-editor.cm-focused) {
  outline: none;
}

:deep(.cm-scroller) {
  font-family: 'Cascadia Code', monospace;
  line-height: 1.6;
}

:global(.dark) .template-code-editor {
  border-color: #3a3a40;
}

:global(.dark) :deep(.cm-editor) {
  color: #e5e7eb;
  background: #1f1f23;
}

:global(.dark) :deep(.cm-gutters) {
  color: #8b8b93;
  background: #1f1f23;
  border-right-color: #3a3a40;
}
</style>
