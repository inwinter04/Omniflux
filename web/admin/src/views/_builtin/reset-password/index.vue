<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useMessage } from 'naive-ui';
import { $t } from '@/locales';
import { fetchResetPassword } from '@/service/api';
import { useNaiveForm } from '@/hooks/common/form';
import { validatePasswordPolicy } from '@/utils/password';
import AuthShell from '../components/auth-shell.vue';

defineOptions({
  name: 'ResetPassword'
});

const route = useRoute();
const router = useRouter();
const message = useMessage();
const { formRef, validate } = useNaiveForm();
const token = computed(() => (typeof route.query.token === 'string' ? route.query.token : ''));
const submitting = ref(false);
const model = reactive({ password: '', confirmPassword: '' });

const rules = {
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
    { validator: (_rule: unknown, value: string) => value === model.password, message: $t('business.the-two-passwords-entered-do') }
  ]
};

async function handleSubmit() {
  await validate();
  submitting.value = true;
  const { error } = await fetchResetPassword(token.value, model.password);
  submitting.value = false;
  if (!error) {
    message.success($t('business.password-reset'), { duration: 5000, closable: true });
    await router.replace({ name: 'login' });
  }
}
</script>

<template>
  <AuthShell>
    <h3 class="text-18px text-primary font-medium">{{ $t('business.reset-password') }}</h3>
    <NAlert v-if="!token" type="error" :show-icon="true" class="mt-24px">
      {{ $t('business.email-token-missing') }}
    </NAlert>
    <NForm v-else ref="formRef" class="pt-24px" :model="model" :rules="rules" size="large" :show-label="false">
      <NFormItem path="password">
        <div class="w-full">
          <NInput
            v-model:value="model.password"
            type="password"
            show-password-on="click"
            :input-props="{ autocomplete: 'new-password' }"
            :placeholder="$t('business.new-password')"
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
      <NButton type="primary" size="large" round block :loading="submitting" @click="handleSubmit">
        {{ $t('business.reset-password') }}
      </NButton>
    </NForm>
  </AuthShell>
</template>

