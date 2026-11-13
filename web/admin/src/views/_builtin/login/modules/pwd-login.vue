<script setup lang="ts">
import { computed, reactive } from 'vue';
import { useMessage } from 'naive-ui';
import { $t } from '@/locales';
import { useAuthStore } from '@/store/modules/auth';
import { useFormRules, useNaiveForm } from '@/hooks/common/form';

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
  const { formRules } = useFormRules();

  return {
    email: [
      { required: true, message: $t('page.login.common.userNamePlaceholder') },
      {
        validator: (_rule: unknown, value: string) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value || ''),
        message: '请输入有效的邮箱地址'
      }
    ],
    password: [
      { required: true, message: '请输入密码' },
      {
        validator: (_rule: unknown, value: string) => (value || '').length >= 16,
        message: '密码至少 16 位'
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
      <NInput v-model:value="model.email" autocomplete="username" placeholder="邮箱" />
    </NFormItem>
    <NFormItem path="password">
      <NInput
        v-model:value="model.password"
        type="password"
        show-password-on="click"
        autocomplete="current-password"
        placeholder="密码"
      />
    </NFormItem>
    <NSpace vertical :size="24">
      <NButton type="primary" size="large" round block :loading="authStore.loginLoading" @click="handleSubmit">
        登录
      </NButton>
    </NSpace>
  </NForm>
</template>

<style scoped></style>

