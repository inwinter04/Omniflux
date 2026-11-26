<script setup lang="ts">
import { computed } from 'vue';
import { $t } from '@/locales';

defineOptions({
  name: 'PermissionMatrix'
});

const props = withDefaults(
  defineProps<{
    modules: Api.Management.PermissionModule[];
    modelValue: string[];
    basePermissions?: string[] | null;
    disabledPermissions?: string[];
    readonly?: boolean;
  }>(),
  {
    basePermissions: null,
    disabledPermissions: () => [],
    readonly: false
  }
);

const emit = defineEmits<{
  'update:modelValue': [value: string[]];
}>();

const selectedSet = computed(() => new Set(props.modelValue));
const baseSet = computed(() => (props.basePermissions ? new Set(props.basePermissions) : null));
const disabledSet = computed(() => new Set(props.disabledPermissions));

function moduleTitle(key: string): string {
  return $t(`business.permModule_${key}` as App.I18n.I18nKey);
}

function permissionTitle(code: string): string {
  return $t(`business.perm_${code.replace(/:/g, '_')}` as App.I18n.I18nKey);
}

function togglePermission(code: string, checked: boolean) {
  if (props.readonly || disabledSet.value.has(code)) return;
  const next = new Set(props.modelValue);
  if (checked) {
    next.add(code);
  } else {
    next.delete(code);
  }
  emit('update:modelValue', Array.from(next));
}

function toggleModule(mod: Api.Management.PermissionModule, selectAll: boolean) {
  if (props.readonly) return;
  const next = new Set(props.modelValue);
  for (const code of mod.permissions) {
    if (disabledSet.value.has(code)) continue;
    if (selectAll) {
      next.add(code);
    } else {
      next.delete(code);
    }
  }
  emit('update:modelValue', Array.from(next));
}
</script>

<template>
  <div class="flex flex-col gap-12px">
    <div
      v-for="mod in modules"
      :key="mod.key"
      class="border border-#e5e7eb rounded-8px p-12px dark:border-#2d2d30"
    >
      <div class="mb-10px flex items-center justify-between gap-8px">
        <div class="flex items-center gap-8px">
          <span class="text-14px font-medium">{{ moduleTitle(mod.key) }}</span>
          <NTag size="tiny" :bordered="false">
            {{ mod.permissions.filter(p => selectedSet.has(p)).length }} / {{ mod.permissions.length }}
          </NTag>
        </div>
        <NSpace v-if="!readonly" :size="8">
          <NButton
            text
            size="tiny"
            type="primary"
            :disabled="mod.permissions.every(p => disabledSet.has(p))"
            @click="toggleModule(mod, true)"
          >
            {{ $t('business.select-all-module') }}
          </NButton>
          <NButton
            text
            size="tiny"
            :disabled="mod.permissions.every(p => disabledSet.has(p))"
            @click="toggleModule(mod, false)"
          >
            {{ $t('business.clear-module') }}
          </NButton>
        </NSpace>
      </div>
      <div class="grid grid-cols-1 gap-8px sm:grid-cols-2">
        <div
          v-for="code in mod.permissions"
          :key="code"
          class="flex items-start justify-between gap-8px rounded-6px bg-#f9fafb px-10px py-8px dark:bg-#18181c"
        >
          <NTooltip :disabled="!disabledSet.has(code)">
            <template #trigger>
              <NCheckbox
                :checked="selectedSet.has(code)"
                :disabled="readonly || disabledSet.has(code)"
                @update:checked="checked => togglePermission(code, checked)"
              >
                <div class="flex flex-col">
                  <span class="text-13px leading-snug">{{ permissionTitle(code) }}</span>
                  <span class="text-11px font-mono text-#9ca3af">{{ code }}</span>
                </div>
              </NCheckbox>
            </template>
            {{ $t('business.beyond-your-permissions') }}
          </NTooltip>
          <NTag
            v-if="baseSet && selectedSet.has(code) && !baseSet.has(code)"
            size="tiny"
            type="success"
            :bordered="false"
            class="shrink-0"
          >
            {{ $t('business.override-allow') }}
          </NTag>
          <NTag
            v-else-if="baseSet && !selectedSet.has(code) && baseSet.has(code)"
            size="tiny"
            type="warning"
            :bordered="false"
            class="shrink-0"
          >
            {{ $t('business.override-deny') }}
          </NTag>
        </div>
      </div>
    </div>
  </div>
</template>
