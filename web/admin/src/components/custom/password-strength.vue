<script setup lang="ts">
import { computed } from 'vue';
import { getPasswordStrength, validatePasswordPolicy } from '@/utils/password';
import { $t } from '@/locales';

defineOptions({
  name: 'PasswordStrength'
});

const props = defineProps<{
  password: string;
}>();

const valid = computed(() => validatePasswordPolicy(props.password));
const strength = computed(() => getPasswordStrength(props.password));

const strengthLabel = computed(() => {
  if (strength.value === 'strong') return $t('business.strength-strong');
  if (strength.value === 'medium') return $t('business.strength-medium');
  return $t('business.strength-weak');
});

const activeBars = computed(() => {
  if (!props.password) return 0;
  if (!valid.value || strength.value === 'weak') return 1;
  if (strength.value === 'medium') return 2;
  return 3;
});

const barColor = computed(() => {
  if (activeBars.value === 1) return 'bg-#d03050';
  if (activeBars.value === 2) return 'bg-#f0a020';
  if (activeBars.value === 3) return 'bg-#18a058';
  return 'bg-#e5e7eb dark:bg-#374151';
});
</script>

<template>
  <div v-if="password" class="mt-6px flex items-center gap-8px">
    <div class="grid flex-1 grid-cols-3 gap-4px">
      <span
        v-for="idx in 3"
        :key="idx"
        class="h-4px rounded-full transition-colors"
        :class="idx <= activeBars ? barColor : 'bg-#e5e7eb dark:bg-#374151'"
      />
    </div>
    <span class="text-12px text-#6b7280">{{ $t('business.password-strength') }}: {{ strengthLabel }}</span>
  </div>
</template>
