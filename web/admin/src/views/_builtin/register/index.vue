<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useMessage } from 'naive-ui';
import { $t } from '@/locales';
import { fetchRegistrationOptions, fetchResendVerification } from '@/service/api';
import { useAuthStore } from '@/store/modules/auth';
import { useNaiveForm } from '@/hooks/common/form';
import { validatePasswordPolicy } from '@/utils/password';
import AuthShell from '../components/auth-shell.vue';

defineOptions({
  name: 'Register'
});

const router = useRouter();
const authStore = useAuthStore();
const message = useMessage();
const { formRef, validate } = useNaiveForm();
const checking = ref(true);
const verificationSent = ref(false);

interface FormModel {
  email: string;
  password: string;
  confirmPassword: string;
}

const model: FormModel = reactive({
  email: '',
  password: '',
  confirmPassword: ''
});

const rules = computed<Record<keyof FormModel, App.Global.FormRule[]>>(() => ({
  email: [
    { required: true, message: $t('form.email.required') },
    {
      validator: (_rule: unknown, value: string) => !value || /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value),
      message: $t('business.enter-a-valid-email-address')
    }
  ],
  password: [
    { required: true, message: $t('business.please-enter-a-password') },
    {
      validator: (_rule: unknown, value: string) => !value || validatePasswordPolicy(value),
      message: $t('business.password-must-be-at-least'),
      trigger: 'blur'
    }
  ],
  confirmPassword: [
    { required: true, message: $t('business.please-enter-the-password-again') },
    {
      validator: (_rule: unknown, value: string) => value === model.password,
      message: $t('business.the-two-passwords-entered-do')
    }
  ]
}));

onMounted(async () => {
  const { data, error } = await fetchRegistrationOptions();
  checking.value = false;

  if (!error && !data.enabled) {
    await router.replace({ name: 'login' });
  }
});

async function handleSubmit() {
  await validate();
  const result = await authStore.register(model.email, model.password);

  if (result.status === 'verification_email_sent') {
    verificationSent.value = true;
    message.success($t('business.verification-email-sent'), { duration: 5000, closable: true });
    return;
  }
  if (!result.success && result.message) {
    message.error(result.message, { duration: 5000, closable: true });
  }
}

async function handleResend() {
  const { error } = await fetchResendVerification(model.email);
  if (!error) {
    message.success($t('business.verification-email-sent'), { duration: 5000, closable: true });
  }
}
</script>

<template>
  <AuthShell>
    <NSpin :show="checking">
      <h3 class="text-18px text-primary font-medium">{{ $t('business.create-an-account') }}</h3>
      <NForm v-if="!verificationSent" ref="formRef" class="pt-24px" :model="model" :rules="rules" size="large" :show-label="false">
        <NFormItem path="email">
          <NInput
            v-model:value="model.email"
            :input-props="{ autocomplete: 'username' }"
            :placeholder="$t('business.email')"
          />
        </NFormItem>
        <NFormItem path="password">
          <div class="w-full">
            <NInput
              v-model:value="model.password"
              type="password"
              show-password-on="click"
              :input-props="{ autocomplete: 'new-password' }"
              :placeholder="$t('business.password')"
            />
            <PasswordStrength :password="model.password" />
          </div>
        </NFormItem>
        <NFormItem path="confirmPassword">
          <NInput
            v-model:value="model.confirmPassword"
            type="password"
            show-password-on="click"
            :input-props="{ autocomplete: 'new-password' }"
            :placeholder="$t('business.confirm-password')"
            @keyup.enter="handleSubmit"
          />
        </NFormItem>
        <NButton
          type="primary"
          size="large"
          round
          block
          :loading="authStore.registerLoading"
          @click="handleSubmit"
        >
          {{ $t('business.register') }}
        </NButton>
        <div class="mt-16px flex-center gap-6px text-14px text-#6b7280">
          <span>{{ $t('business.already-have-account') }}</span>
          <NButton text type="primary" @click="router.push({ name: 'login' })">
            {{ $t('business.back-to-log-in') }}
          </NButton>
        </div>
      </NForm>
      <template v-else>
        <NAlert type="success" :show-icon="true" class="mt-24px">
          {{ $t('business.verification-email-description') }}
        </NAlert>
        <NSpace vertical :size="12" class="mt-16px">
          <NButton size="large" round block :loading="authStore.registerLoading" @click="handleResend">
            {{ $t('business.resend-verification-email') }}
          </NButton>
          <div class="flex-center">
            <NButton text type="primary" @click="router.push({ name: 'login' })">
              {{ $t('business.back-to-log-in') }}
            </NButton>
          </div>
        </NSpace>
      </template>
    </NSpin>
  </AuthShell>
</template>

<style scoped></style>
