<script setup lang="ts">
import { computed, reactive } from 'vue';
import { useMessage } from 'naive-ui';
import { $t } from '@/locales';
import { useAuthStore } from '@/store/modules/auth';
import { useNaiveForm } from '@/hooks/common/form';

defineOptions({
  name: 'PwdLogin'
});

const authStore = useAuthStore();
const message = useMessage();
const { formRef, validate } = useNaiveForm();

interface FormModel {
  email: string;
  password: string;
}

const model: FormModel = reactive({
  email: '',
  password: ''
});

const rules = computed<Record<keyof FormModel, App.Global.FormRule[]>>(() => {
  return {
    email: [
      { required: true, message: $t('form.email.required') },
      {
        // 未填写时只提示必填,不再同时报格式错误
        validator: (_rule: unknown, value: string) => !value || /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value),
        message: $t('business.enter-a-valid-email-address')
      }
    ],
    password: [
      { required: true, message: $t('business.please-enter-a-password') },
      {
        validator: (_rule: unknown, value: string) => !value || value.length >= 16,
        message: $t('business.password-must-be-at-least')
      }
    ]
  };
});

async function handleSubmit() {
  await validate();
  const result = await authStore.login(model.email, model.password);

  if (!result.success && result.message) {
    message.error(result.message, { duration: 5000, closable: true });
  }
}
</script>

<template>
  <NForm ref="formRef" :model="model" :rules="rules" size="large" :show-label="false" @keyup.enter="handleSubmit">
    <NFormItem path="email">
      <NInput
        v-model:value="model.email"
        :input-props="{ autocomplete: 'username' }"
        :placeholder="$t('business.email')"
      />
    </NFormItem>
    <NFormItem path="password">
      <NInput
        v-model:value="model.password"
        type="password"
        show-password-on="click"
        :input-props="{ autocomplete: 'current-password' }"
        :placeholder="$t('business.password')"
      />
    </NFormItem>
    <NSpace vertical :size="24">
      <NButton type="primary" size="large" round block :loading="authStore.loginLoading" @click="handleSubmit">
        {{ $t('business.log-in') }}
      </NButton>
    </NSpace>
  </NForm>
</template>

<style scoped></style>
