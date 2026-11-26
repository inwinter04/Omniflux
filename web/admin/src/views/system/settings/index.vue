<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useMessage } from 'naive-ui';
import type { FormInst, FormRules, SelectOption } from 'naive-ui';
import { formatTime } from '@/utils/admin-status';
import {
  fetchEmailSettings,
  fetchOrganizations,
  fetchSystemSettings,
  fetchTestEmailSettings,
  fetchUpdateEmailSettings,
  fetchUpdateSystemSettings
} from '@/service/api';
import { $t } from '@/locales';

defineOptions({
  name: 'SystemSettings'
});

const emailSettings = reactive<Api.Management.EmailSettings>({
  enabled: false,
  smtp_host: '',
  smtp_port: 587,
  smtp_username: '',
  from_address: '',
  from_name: '',
  encryption: 'starttls',
  public_base_url: '',
  has_password: false
});
const smtpPassword = ref('');
const testRecipient = ref('');
const testingEmail = ref(false);

const encryptionOptions = computed(() => [
  { label: $t('business.smtp-starttls'), value: 'starttls' },
  { label: $t('business.smtp-tls'), value: 'tls' },
  { label: $t('business.smtp-none'), value: 'none' }
]);

const message = useMessage();
const loading = ref(true);
const settingsLoaded = ref(false);
const emailSettingsLoaded = ref(false);
const submitting = ref(false);
const formRef = ref<FormInst | null>(null);
const orgOptions = ref<SelectOption[]>([]);

const settings = reactive<Api.Management.SystemSettings>({
  public_registration_enabled: false,
  registration_organization_id: null,
  registration_role: 'pending',
  email_verification_required: false,
  updated_by: null,
  updated_at: ''
});

const roleOptions = computed(() => [
  { label: $t('business.pending-role-recommended'), value: 'pending' },
  { label: $t('business.viewer'), value: 'viewer' },
  { label: $t('business.developer'), value: 'developer' }
]);

const rules = computed<FormRules>(() => ({
  registration_organization_id: {
    required: settings.public_registration_enabled,
    message: $t('business.select-a-registration-organization'),
    trigger: ['blur', 'change']
  },
  registration_role: {
    required: true,
    message: $t('business.select-a-default-registration-role'),
    trigger: ['blur', 'change']
  }
}));

async function loadSettings() {
  const [settingsResult, orgResult, emailResult] = await Promise.all([
    fetchSystemSettings(),
    fetchOrganizations(),
    fetchEmailSettings()
  ]);

  if (!settingsResult.error && settingsResult.data) {
    Object.assign(settings, settingsResult.data);
    settingsLoaded.value = true;
  }
  if (!emailResult.error && emailResult.data) {
    Object.assign(emailSettings, emailResult.data);
    emailSettingsLoaded.value = true;
  }
  if (!orgResult.error && orgResult.data) {
    orgOptions.value = orgResult.data.map(org => ({ label: org.name, value: org.id }));
  }

  loading.value = false;
}

async function handleSave() {
  if (!settingsLoaded.value) {
    message.error($t('business.settings-load-failed'));
    return;
  }
  await formRef.value?.validate();
  submitting.value = true;

  const { error, data } = await fetchUpdateSystemSettings({
    public_registration_enabled: settings.public_registration_enabled,
    registration_organization_id: settings.registration_organization_id,
    email_verification_required: settings.email_verification_required,
    registration_role: settings.registration_role
  });
  submitting.value = false;

  if (!error && data) {
    Object.assign(settings, data);
    message.success($t('business.system-settings-saved'));
  }
}

async function handleSaveEmailSettings() {
  if (!emailSettingsLoaded.value) {
    message.error($t('business.settings-load-failed'));
    return;
  }
  if (!emailSettings.enabled && settings.email_verification_required) {
    message.error($t('business.disable-email-verification-first'));
    return;
  }
  submitting.value = true;
  const { error, data } = await fetchUpdateEmailSettings({
    enabled: emailSettings.enabled,
    smtp_host: emailSettings.smtp_host,
    smtp_port: emailSettings.smtp_port,
    smtp_username: emailSettings.smtp_username,
    smtp_password: smtpPassword.value || undefined,
    from_address: emailSettings.from_address,
    from_name: emailSettings.from_name,
    encryption: emailSettings.encryption,
    public_base_url: emailSettings.public_base_url
  });
  submitting.value = false;
  if (!error && data) {
    Object.assign(emailSettings, data);
    smtpPassword.value = '';
    message.success($t('business.email-settings-saved'));
  }
}

async function handleTestEmail() {
  if (!emailSettingsLoaded.value || !emailSettings.enabled) {
    return;
  }
  testingEmail.value = true;
  const { error } = await fetchTestEmailSettings(testRecipient.value);
  testingEmail.value = false;
  if (!error) {
    message.success($t('business.test-email-sent'));
  }
}

onMounted(loadSettings);
</script>

<template>
  <div class="flex flex-col gap-16px">
    <NCard :title="$t('business.system-settings')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NText depth="3">{{ $t('business.updated-at') }}: {{ formatTime(settings.updated_at) }}</NText>
      </template>
      <NSpin :show="loading">
        <NAlert type="info" :show-icon="true" class="mb-24px">
          {{ $t('business.public-registration-description') }}
        </NAlert>
        <NForm
          ref="formRef"
          :model="settings"
          :rules="rules"
          label-placement="left"
          label-width="180"
          class="max-w-640px"
        >
          <NFormItem :label="$t('business.enable-public-registration')" path="registration_organization_id">
            <div class="w-full">
              <NSwitch v-model:value="settings.public_registration_enabled" />
              <div class="mt-8px">
                <NSelect
                  v-model:value="settings.registration_organization_id"
                  :options="orgOptions"
                  :placeholder="$t('business.select-a-registration-organization')"
                  clearable
                />
              </div>
            </div>
          </NFormItem>
          <NFormItem :label="$t('business.default-registration-role')" path="registration_role">
            <NSelect
              v-model:value="settings.registration_role"
              :options="roleOptions"
              :placeholder="$t('business.select-a-default-registration-role')"
            />
          </NFormItem>
          <NFormItem :label="$t('business.require-email-verification')" path="email_verification_required">
            <NSwitch v-model:value="settings.email_verification_required" :disabled="!emailSettings.enabled" />
          </NFormItem>
          <div class="flex justify-end">
            <NButton type="primary" :loading="submitting" :disabled="!settingsLoaded" @click="handleSave">
              {{ $t('common.update') }}
            </NButton>
          </div>
        </NForm>
      </NSpin>
    </NCard>

    <NCard :title="$t('business.email-service')" :bordered="false" class="card-wrapper">
      <NSpin :show="loading">
        <NAlert type="info" :show-icon="true" class="mb-24px">
          {{ $t('business.email-service-description') }}
        </NAlert>
        <NForm :model="emailSettings" label-placement="left" label-width="180" class="max-w-720px">
          <NFormItem :label="$t('business.enable-email-service')">
            <NSwitch v-model:value="emailSettings.enabled" />
          </NFormItem>
          <NFormItem :label="$t('business.smtp-server')">
            <div class="flex w-full gap-8px">
              <NInput v-model:value="emailSettings.smtp_host" placeholder="smtp.example.com" />
              <NInputNumber v-model:value="emailSettings.smtp_port" :min="1" :max="65535" class="w-140px" />
            </div>
          </NFormItem>
          <NFormItem :label="$t('business.smtp-encryption')">
            <NSelect v-model:value="emailSettings.encryption" :options="encryptionOptions" />
          </NFormItem>
          <NFormItem :label="$t('business.smtp-account')">
            <NInput v-model:value="emailSettings.smtp_username" :placeholder="$t('business.optional-username')" />
          </NFormItem>
          <NFormItem :label="$t('business.smtp-password')">
            <NInput
              v-model:value="smtpPassword"
              type="password"
              show-password-on="click"
              :placeholder="emailSettings.has_password ? $t('business.password-saved-leave-blank') : $t('business.optional-password')"
            />
          </NFormItem>
          <NFormItem :label="$t('business.sender')">
            <div class="flex w-full gap-8px">
              <NInput v-model:value="emailSettings.from_name" :placeholder="$t('business.sender-name')" />
              <NInput v-model:value="emailSettings.from_address" placeholder="omniflux@example.com" />
            </div>
          </NFormItem>
          <NFormItem :label="$t('business.public-base-url')">
            <NInput v-model:value="emailSettings.public_base_url" placeholder="https://example.com" />
          </NFormItem>
          <div class="flex items-center justify-end gap-8px">
            <NInput v-model:value="testRecipient" :placeholder="$t('business.test-recipient')" class="w-220px" />
            <NButton :disabled="!emailSettingsLoaded || !emailSettings.enabled || !testRecipient" :loading="testingEmail" @click="handleTestEmail">
              {{ $t('business.send-test-email') }}
            </NButton>
            <NButton type="primary" :loading="submitting" :disabled="!emailSettingsLoaded || (!emailSettings.enabled && settings.email_verification_required)" @click="handleSaveEmailSettings">
              {{ $t('common.update') }}
            </NButton>
          </div>
        </NForm>
      </NSpin>
    </NCard>
  </div>
</template>

<style scoped></style>

