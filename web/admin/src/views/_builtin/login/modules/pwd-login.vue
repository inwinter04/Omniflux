<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useMessage } from 'naive-ui';
import { $t } from '@/locales';
import { fetchRegistrationOptions } from '@/service/api';
import { useAuthStore } from '@/store/modules/auth';
import { useNaiveForm } from '@/hooks/common/form';

defineOptions({
  name: 'PwdLogin'
});

const router = useRouter();
const authStore = useAuthStore();
const registrationEnabled = ref(false);
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

onMounted(async () => {
  const { data, error } = await fetchRegistrationOptions();
  registrationEnabled.value = !error && data.enabled;
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
    password: [{ required: true, message: $t('business.please-enter-a-password') }]
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
    <NSpace vertical :size="12">
      <NButton type="primary" size="large" round block :loading="authStore.loginLoading" @click="handleSubmit">
        {{ $t('business.log-in') }}
      </NButton>
      <NButton quaternary size="large" block @click="router.push({ name: 'forgot-password' })">
        {{ $t('business.forgot-password') }}
      </NButton>
      <NButton v-if="registrationEnabled" quaternary size="large" block @click="router.push({ name: 'register' })">
        {{ $t('business.create-an-account') }}
      </NButton>
    </NSpace>
  </NForm>
</template>

<style scoped></style>
