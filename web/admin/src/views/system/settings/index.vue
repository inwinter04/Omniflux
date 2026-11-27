<script setup lang="ts">
import { computed, h, onMounted, reactive, ref } from 'vue';
import { NButton, NTag, useMessage } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules, SelectOption } from 'naive-ui';
import { formatTime, resolveAutoGatewayOrigin } from '@/utils/admin-status';
import {
  fetchEmailSettings,
  fetchFXRates,
  fetchOrganizations,
  fetchSyncFXRates,
  fetchSystemSettings,
  fetchTestEmailSettings,
  fetchUpdateEmailSettings,
  fetchUpdateFXRate,
  fetchUpdateFXSettings,
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

const fxLoaded = ref(false);
const fxSyncing = ref(false);
const fxSaving = ref(false);
const fxSettings = reactive<Api.Management.FXRateSettings>({
  auto_sync_enabled: true,
  sync_interval_hours: 12,
  primary_source: 'frankfurter_ecb',
  fallback_source: 'open_er_api',
  last_synced_at: null,
  last_sync_status: 'never',
  last_sync_source: null,
  last_sync_error: null
});
const fxRates = ref<Api.Management.FXRateEntry[]>([]);
const fxRateModalVisible = ref(false);
const fxEditingRate = reactive<{ currency: string; rate_per_usd: number; manual_override: boolean }>({
  currency: 'CNY',
  rate_per_usd: 7.2,
  manual_override: true
});
const fxSavingRate = ref(false);

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
  gateway_base_url: '',
  expose_upstream_balance_to_clients: false,
  updated_by: null,
  updated_at: ''
});
const autoGatewayOrigin = resolveAutoGatewayOrigin();

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
  const [settingsResult, orgResult, emailResult, fxResult] = await Promise.all([
    fetchSystemSettings(),
    fetchOrganizations(),
    fetchEmailSettings(),
    fetchFXRates()
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
  if (!fxResult.error && fxResult.data) {
    Object.assign(fxSettings, fxResult.data.settings);
    fxRates.value = fxResult.data.rates ?? [];
    fxLoaded.value = true;
  }

  loading.value = false;
}

function fxSourceLabel(source: string) {
  switch (source) {
    case 'frankfurter_ecb':
      return 'Frankfurter (ECB)';
    case 'open_er_api':
      return 'Open ER-API';
    case 'manual':
      return $t('business.fx-source-manual');
    default:
      return $t('business.fx-source-builtin');
  }
}

function fxSyncStatusTagType(status: string): 'default' | 'success' | 'warning' | 'error' {
  switch (status) {
    case 'ok':
      return 'success';
    case 'partial':
      return 'warning';
    case 'error':
      return 'error';
    default:
      return 'default';
  }
}

function fxSyncStatusText(status: string) {
  switch (status) {
    case 'ok':
      return $t('business.fx-sync-ok');
    case 'partial':
      return $t('business.fx-sync-partial');
    case 'error':
      return $t('business.fx-sync-error');
    default:
      return $t('business.fx-sync-never');
  }
}

const fxRateColumns = computed<DataTableColumns<Api.Management.FXRateEntry>>(() => [
  { title: $t('business.currency'), key: 'currency', width: 90 },
  {
    title: $t('business.fx-rate-per-usd'),
    key: 'rate_per_usd',
    width: 140,
    render: row => `1 USD = ${Number(row.rate_per_usd).toFixed(4)} ${row.currency}`
  },
  {
    title: $t('business.fx-source'),
    key: 'source',
    width: 160,
    render: row =>
      h(
        NTag,
        { size: 'small', type: row.manual_override ? 'warning' : 'info', bordered: false },
        { default: () => fxSourceLabel(row.source) }
      )
  },
  {
    title: $t('business.fx-effective-date'),
    key: 'effective_date',
    width: 140,
    render: row => row.effective_date || '-'
  },
  {
    title: $t('business.updated-at'),
    key: 'updated_at',
    minWidth: 160,
    render: row => formatTime(row.updated_at)
  },
  {
    title: $t('business.actions'),
    key: 'actions',
    width: 90,
    render: row =>
      h(
        NButton,
        {
          size: 'small',
          quaternary: true,
          disabled: row.currency === 'USD',
          onClick: () => openEditFXRate(row)
        },
        { default: () => $t('business.edit') }
      )
  }
]);

function openEditFXRate(row: Api.Management.FXRateEntry) {
  fxEditingRate.currency = row.currency;
  fxEditingRate.rate_per_usd = Number(row.rate_per_usd);
  fxEditingRate.manual_override = row.manual_override;
  fxRateModalVisible.value = true;
}

async function handleSaveFXSettings() {
  fxSaving.value = true;
  try {
    const { data, error } = await fetchUpdateFXSettings({
      auto_sync_enabled: fxSettings.auto_sync_enabled,
      sync_interval_hours: fxSettings.sync_interval_hours
    });
    if (!error && data) {
      Object.assign(fxSettings, data);
      message.success($t('business.fx-settings-saved'));
    }
  } finally {
    fxSaving.value = false;
  }
}

async function handleSyncFXRates() {
  fxSyncing.value = true;
  try {
    const { data, error } = await fetchSyncFXRates();
    if (!error && data) {
      Object.assign(fxSettings, data.settings);
      fxRates.value = data.rates ?? [];
      message.success($t('business.fx-sync-completed'));
    }
  } finally {
    fxSyncing.value = false;
  }
}

async function handleSaveFXRate() {
  fxSavingRate.value = true;
  try {
    const { data, error } = await fetchUpdateFXRate(fxEditingRate.currency, {
      rate_per_usd: fxEditingRate.rate_per_usd,
      manual_override: fxEditingRate.manual_override
    });
    if (!error && data) {
      fxRateModalVisible.value = false;
      const idx = fxRates.value.findIndex(item => item.currency === data.currency);
      if (idx >= 0) fxRates.value[idx] = data;
      message.success($t('business.fx-rate-updated'));
    }
  } finally {
    fxSavingRate.value = false;
  }
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
    registration_role: settings.registration_role,
    gateway_base_url: settings.gateway_base_url.trim(),
    expose_upstream_balance_to_clients: settings.expose_upstream_balance_to_clients
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
          label-width="auto"
          label-align="left"
          class="max-w-720px"
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
          <NFormItem :label="$t('business.gateway-base-url')" path="gateway_base_url">
            <div class="w-full">
              <NInput
                v-model:value="settings.gateway_base_url"
                clearable
                :placeholder="$t('business.gateway-base-url-placeholder', { origin: autoGatewayOrigin })"
              />
              <NText depth="3" class="mt-4px block text-12px">
                {{ $t('business.gateway-base-url-hint') }}
              </NText>
            </div>
          </NFormItem>
          <NFormItem :label="$t('business.expose-upstream-balance-to-clients')" path="expose_upstream_balance_to_clients">
            <div class="w-full">
              <NSwitch v-model:value="settings.expose_upstream_balance_to_clients" />
              <NText depth="3" class="mt-4px block text-12px">
                {{ $t('business.expose-upstream-balance-to-clients-hint') }}
              </NText>
            </div>
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
        <NForm :model="emailSettings" label-placement="left" label-width="auto" label-align="left" class="max-w-760px">
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
          <div class="flex flex-wrap items-center justify-end gap-8px">
            <NInput v-model:value="testRecipient" :placeholder="$t('business.test-recipient')" class="min-w-220px w-240px" />
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

    <NCard :title="$t('business.fx-rates')" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace align="center">
          <NTag size="small" :type="fxSyncStatusTagType(fxSettings.last_sync_status)" round>
            {{ fxSyncStatusText(fxSettings.last_sync_status) }}
          </NTag>
          <NText depth="3">{{ fxSettings.last_synced_at ? formatTime(fxSettings.last_synced_at) : '-' }}</NText>
        </NSpace>
      </template>
      <NSpin :show="loading">
        <NAlert type="info" :show-icon="true" class="mb-16px">
          {{ $t('business.fx-rates-description') }}
        </NAlert>
        <div class="mb-16px flex flex-wrap items-center justify-between gap-12px">
          <NSpace align="center" :size="16">
            <NSpace align="center" :size="8">
              <NText>{{ $t('business.fx-auto-sync') }}</NText>
              <NSwitch v-model:value="fxSettings.auto_sync_enabled" />
            </NSpace>
            <NSpace align="center" :size="8">
              <NText>{{ $t('business.fx-sync-interval') }}</NText>
              <NInputNumber
                v-model:value="fxSettings.sync_interval_hours"
                :min="1"
                :max="168"
                size="small"
                class="w-110px"
              />
            </NSpace>
            <NButton size="small" :loading="fxSaving" :disabled="!fxLoaded" @click="handleSaveFXSettings">
              {{ $t('common.update') }}
            </NButton>
          </NSpace>
          <NButton type="primary" secondary size="small" :loading="fxSyncing" :disabled="!fxLoaded" @click="handleSyncFXRates">
            {{ $t('business.fx-sync-now') }}
          </NButton>
        </div>
        <NAlert v-if="fxSettings.last_sync_error" type="warning" :bordered="false" class="mb-12px">
          {{ fxSettings.last_sync_error }}
        </NAlert>
        <NDataTable :columns="fxRateColumns" :data="fxRates" size="small" :bordered="false" :scroll-x="780" />
      </NSpin>
    </NCard>

    <NModal v-model:show="fxRateModalVisible" preset="card" :title="$t('business.fx-edit-rate-title', { currency: fxEditingRate.currency })" class="max-w-440px">
      <NForm label-placement="top">
        <NFormItem :label="$t('business.fx-rate-per-usd')">
          <NInputNumber v-model:value="fxEditingRate.rate_per_usd" :min="0.0001" :precision="6" class="w-full" />
        </NFormItem>
        <NFormItem :label="$t('business.fx-manual-override')">
          <NSpace align="center" :size="8">
            <NSwitch v-model:value="fxEditingRate.manual_override" />
            <NText depth="3" class="text-12px">{{ $t('business.fx-manual-override-hint') }}</NText>
          </NSpace>
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="fxRateModalVisible = false">{{ $t('common.cancel') }}</NButton>
          <NButton type="primary" :loading="fxSavingRate" @click="handleSaveFXRate">{{ $t('common.confirm') }}</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>

