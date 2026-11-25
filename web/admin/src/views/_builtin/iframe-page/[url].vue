<script setup lang="ts">
import { computed } from 'vue';
import { $t } from '@/locales';

interface Props {
  url: string;
}

const props = defineProps<Props>();

const iframeURL = computed(() => {
  try {
    const parsed = new URL(props.url);
    return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed.toString() : '';
  } catch {
    return '';
  }
});
</script>

<template>
  <div class="h-full">
    <NAlert v-if="!iframeURL" type="error" :show-icon="true" class="m-16px">
      {{ $t('business.invalid-iframe-url') }}
    </NAlert>
    <iframe
      v-else
      id="iframePage"
      class="size-full"
      :src="iframeURL"
      sandbox="allow-scripts allow-forms allow-popups allow-downloads"
      referrerpolicy="no-referrer"
    ></iframe>
  </div>
</template>

<style scoped></style>
