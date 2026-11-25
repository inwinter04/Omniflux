<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { $t } from '@/locales';
import { fetchVerifyEmail } from '@/service/api';
import { useAuthStore } from '@/store/modules/auth';
import AuthShell from '../components/auth-shell.vue';

defineOptions({
  name: 'VerifyEmail'
});

const route = useRoute();
const authStore = useAuthStore();
const token = computed(() => (typeof route.query.token === 'string' ? route.query.token : ''));
const state = ref<'verifying' | 'success' | 'error'>('verifying');
const errorMessage = ref('');

onMounted(async () => {
  if (!token.value) {
    state.value = 'error';
    errorMessage.value = $t('business.email-token-missing');
    return;
  }
  const { data, error } = await fetchVerifyEmail(token.value);
  if (!error && data) {
    const pass = await authStore.loginWithToken(data);
    state.value = pass ? 'success' : 'error';
    errorMessage.value = pass ? '' : $t('business.verification-email-failed');
    return;
  }
  state.value = 'error';
  errorMessage.value = $t('business.verification-email-failed');
});
</script>

<template>
  <AuthShell>
    <h3 class="text-18px text-primary font-medium">{{ $t('business.verify-email') }}</h3>
    <NSpin v-if="state === 'verifying'" class="mt-24px">
      <div class="h-48px w-300px"></div>
    </NSpin>
    <NAlert v-else :type="state === 'success' ? 'success' : 'error'" :show-icon="true" class="mt-24px">
      {{ state === 'success' ? $t('business.verification-email-succeeded') : errorMessage }}
    </NAlert>
  </AuthShell>
</template>
