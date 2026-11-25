<script setup lang="ts">
import { reactive, ref } from 'vue';
import { useMessage } from 'naive-ui';
import { $t } from '@/locales';
import { fetchForgotPassword } from '@/service/api';
import { useNaiveForm } from '@/hooks/common/form';
import AuthShell from '../components/auth-shell.vue';

defineOptions({
  name: 'ForgotPassword'
});

const message = useMessage();
const { formRef, validate } = useNaiveForm();
const submitted = ref(false);
const model = reactive({ email: '' });

const rules = {
  email: [
    { required: true, message: $t('form.email.required') },
    {
      validator: (_rule: unknown, value: string) => !value || /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value),
      message: $t('business.enter-a-valid-email-address')
    }
  ]
};

async function handleSubmit() {
  await validate();
  const { error } = await fetchForgotPassword(model.email);
  if (!error) {
    submitted.value = true;
    message.success($t('business.reset-email-sent'), { duration: 5000, closable: true });
  }
}
</script>

<template>
  <AuthShell>
    <h3 class="text-18px text-primary font-medium">{{ $t('business.forgot-password') }}</h3>
    <NAlert v-if="submitted" type="success" :show-icon="true" class="mt-24px">
      {{ $t('business.reset-email-description') }}
    </NAlert>
    <NForm v-else ref="formRef" class="pt-24px" :model="model" :rules="rules" size="large" :show-label="false">
      <NFormItem path="email">
        <NInput v-model:value="model.email" :placeholder="$t('business.email')" @keyup.enter="handleSubmit" />
      </NFormItem>
      <NButton type="primary" size="large" round block @click="handleSubmit">
        {{ $t('business.send-reset-email') }}
      </NButton>
    </NForm>
    <NButton quaternary size="large" block class="mt-12px" @click="$router.push({ name: 'login' })">
      {{ $t('business.back-to-log-in') }}
    </NButton>
  </AuthShell>
</template>

