<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useMessage } from 'naive-ui';
import { getPaletteColorByNumber, mixColor } from '@sa/color';
import { $t } from '@/locales';
import { fetchRegistrationOptions, fetchResendVerification } from '@/service/api';
import { useAppStore } from '@/store/modules/app';
import { useAuthStore } from '@/store/modules/auth';
import { useThemeStore } from '@/store/modules/theme';
import { useNaiveForm } from '@/hooks/common/form';

defineOptions({
  name: 'Register'
});

const router = useRouter();
const appStore = useAppStore();
const themeStore = useThemeStore();
const authStore = useAuthStore();
const message = useMessage();
const { formRef, validate } = useNaiveForm();
const checking = ref(true);
const verificationSent = ref(false);

const bgThemeColor = computed(() =>
  themeStore.darkMode ? getPaletteColorByNumber(themeStore.themeColor, 600) : themeStore.themeColor
);

const bgColor = computed(() => {
  const COLOR_WHITE = '#ffffff';
  const ratio = themeStore.darkMode ? 0.5 : 0.2;

  return mixColor(COLOR_WHITE, themeStore.themeColor, ratio);
});

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
      validator: (_rule: unknown, value: string) => !value || value.length >= 16,
      message: $t('business.password-must-be-at-least')
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
  <div class="relative size-full flex-center overflow-hidden" :style="{ backgroundColor: bgColor }">
    <WaveBg :theme-color="bgThemeColor" />
    <NCard :bordered="false" class="relative z-4 w-auto rd-12px">
      <NSpin :show="checking">
        <div class="w-400px lt-sm:w-300px">
          <header class="flex-y-center justify-between">
            <SystemLogo class="size-64px lt-sm:size-48px" />
            <h3 class="text-28px text-primary font-500 lt-sm:text-22px">{{ $t('system.title') }}</h3>
            <div class="i-flex-col">
              <ThemeSchemaSwitch
                :theme-schema="themeStore.themeScheme"
                :show-tooltip="false"
                class="text-20px lt-sm:text-18px"
                @switch="themeStore.toggleThemeScheme"
              />
              <LangSwitch
                v-if="themeStore.header.multilingual.visible"
                :lang="appStore.locale"
                :lang-options="appStore.localeOptions"
                :show-tooltip="false"
                @change-lang="appStore.changeLocale"
              />
            </div>
          </header>
          <main class="pt-24px">
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
                <NInput
                  v-model:value="model.password"
                  type="password"
                  show-password-on="click"
                  :input-props="{ autocomplete: 'new-password' }"
                  :placeholder="$t('business.password')"
                />
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
              <NSpace vertical :size="12">
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
                <NButton quaternary size="large" block @click="router.push({ name: 'login' })">
                  {{ $t('business.back-to-log-in') }}
                </NButton>
              </NSpace>
            </NForm>
            <NAlert v-if="verificationSent" type="success" :show-icon="true" class="mt-24px">
              {{ $t('business.verification-email-description') }}
            </NAlert>
            <NSpace v-if="verificationSent" vertical :size="12" class="mt-16px">
              <NButton size="large" round block :loading="authStore.registerLoading" @click="handleResend">
                {{ $t('business.resend-verification-email') }}
              </NButton>
              <NButton quaternary size="large" block @click="router.push({ name: 'login' })">
                {{ $t('business.back-to-log-in') }}
              </NButton>
            </NSpace>
          </main>
        </div>
      </NSpin>
    </NCard>
  </div>
</template>

<style scoped></style>



