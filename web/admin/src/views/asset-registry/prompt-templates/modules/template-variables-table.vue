<script setup lang="ts">
import { computed, h, ref, watch } from 'vue';
import { NButton, NCheckbox, NInput, NInputNumber, NSelect, NSwitch } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { $t } from '@/locales';
import type { VariableDraft } from './template-form-types';

const props = defineProps<{
  variables: VariableDraft[];
  disabled?: boolean;
}>();

const emit = defineEmits<{
  'update:variables': [value: VariableDraft[]];
}>();

const page = ref(1);
const pageSize = 5;
const maxVariables = 64;
const pageCount = computed(() => Math.max(1, Math.ceil(props.variables.length / pageSize)));
const pagedVariables = computed(() => props.variables.slice((page.value - 1) * pageSize, page.value * pageSize));
const typeOptions = computed(() => [
  { label: $t('business.text'), value: 'string' },
  { label: $t('business.number'), value: 'number' },
  { label: $t('business.integer'), value: 'integer' },
  { label: $t('business.boolean'), value: 'boolean' }
]);

watch(
  () => props.variables.length,
  () => {
    if (page.value > pageCount.value) page.value = pageCount.value;
  }
);

function numberValue(value: unknown) {
  return typeof value === 'number' ? value : null;
}

function changeType(row: VariableDraft) {
  row.default = row.type === 'boolean' ? false : null;
}

const columns = computed<DataTableColumns<VariableDraft>>(() => [
  {
    title: $t('business.variable-name'),
    key: 'name',
    minWidth: 150,
    render: row =>
      h(NInput, {
        size: 'small',
        value: row.name,
        disabled: props.disabled,
        placeholder: $t('business.for-example-customer-name'),
        'onUpdate:value': value => (row.name = value)
      })
  },
  {
    title: $t('business.type'),
    key: 'type',
    width: 105,
    render: row =>
      h(NSelect, {
        size: 'small',
        value: row.type,
        options: typeOptions.value,
        disabled: props.disabled,
        'onUpdate:value': value => {
          row.type = value;
          changeType(row);
        }
      })
  },
  {
    title: $t('business.description'),
    key: 'description',
    minWidth: 160,
    render: row =>
      h(NInput, {
        size: 'small',
        value: row.description,
        disabled: props.disabled,
        placeholder: $t('business.variable-purpose'),
        'onUpdate:value': value => (row.description = value)
      })
  },
  {
    title: $t('business.required'),
    key: 'required',
    width: 65,
    render: row =>
      h(NCheckbox, {
        checked: row.required,
        disabled: props.disabled,
        'onUpdate:checked': value => (row.required = value)
      })
  },
  {
    title: $t('business.default-value'),
    key: 'default',
    width: 145,
    render(row) {
      if (row.type === 'boolean')
        return h(NSwitch, {
          size: 'small',
          value: Boolean(row.default),
          disabled: props.disabled,
          'onUpdate:value': value => (row.default = value)
        });
      if (row.type === 'number' || row.type === 'integer')
        return h(NInputNumber, {
          size: 'small',
          value: numberValue(row.default),
          precision: row.type === 'integer' ? 0 : undefined,
          disabled: props.disabled,
          clearable: true,
          'onUpdate:value': value => (row.default = value)
        });
      return h(NInput, {
        size: 'small',
        value: String(row.default ?? ''),
        disabled: props.disabled,
        placeholder: $t('business.optional'),
        'onUpdate:value': value => (row.default = value)
      });
    }
  },
  {
    title: '',
    key: 'actions',
    width: 60,
    render: row =>
      h(
        NButton,
        {
          size: 'small',
          quaternary: true,
          type: 'error',
          disabled: props.disabled,
          onClick: () =>
            emit(
              'update:variables',
              props.variables.filter(item => item !== row)
            )
        },
        { default: () => $t('business.remove') }
      )
  }
]);

function addVariable() {
  if (props.variables.length >= maxVariables) {
    window.$message?.info($t('business.maxVariables', { count: maxVariables }));
    return;
  }
  emit('update:variables', [
    ...props.variables,
    {
      name: '',
      type: 'string',
      description: '',
      required: false,
      default: null
    }
  ]);
  page.value = Math.ceil((props.variables.length + 1) / pageSize);
}
</script>

<template>
  <div>
    <div class="mb-12px flex items-center justify-between">
      <NText depth="2">
        {{ $t('business.added') }} {{ variables.length }} / {{ maxVariables }}
        {{ $t('business.variables-you-can-also-continue-1cevi35') }}
      </NText>
      <NButton
        size="small"
        type="primary"
        secondary
        :disabled="disabled || variables.length >= maxVariables"
        @click="addVariable"
      >
        {{ $t('business.add-variable') }}
      </NButton>
    </div>
    <NDataTable :columns="columns" :data="pagedVariables" :bordered="false" size="small" :scroll-x="760" />
    <NSpace v-if="variables.length > pageSize" justify="end" class="mt-12px">
      <NPagination v-model:page="page" size="small" :page-count="pageCount" />
    </NSpace>
  </div>
</template>
