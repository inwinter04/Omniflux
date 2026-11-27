<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import {
  NButton,
  NCode,
  NForm,
  NFormItemGi,
  NGrid,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NTag,
  NText
} from 'naive-ui';
import type { SelectGroupOption } from 'naive-ui';
import SvgIcon from '@/components/custom/svg-icon.vue';
import { fetchGatewayInfo, fetchModelServices } from '@/service/api';
import { $t } from '@/locales';
import { usePermission } from '@/hooks/common/permission';
import { resolveAutoGatewayOrigin } from '@/utils/admin-status';

type ClientTab = 'codex' | 'claude' | 'gemini' | 'openai';
type CCSwitchApp = 'codex' | 'claude' | 'gemini';
type CCSwitchApiFormat = 'openai_responses' | 'anthropic' | 'gemini_native';

interface SnippetBlock {
  id: string;
  titleKey: App.I18n.I18nKey;
  lang: string;
  code: string;
}

const props = defineProps<{
  show: boolean;
  apiKey?: string;
  keyName?: string;
  initialModelService?: string;
  modelServices?: Api.Management.ModelService[];
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
}>();

const { has } = usePermission();
const loadingServices = ref(false);
const localServices = ref<Api.Management.ModelService[]>([]);
const selectedModel = ref<string>('');
const configuredGatewayURL = ref<string>('');
const isGatewayConfigured = ref(false);
const manualApiKey = ref<string>('');
const activeTab = ref<ClientTab>('codex');
const copiedKey = ref<string>('');
let copyTimer: ReturnType<typeof setTimeout> | null = null;

function clearCopyState() {
  if (copyTimer) {
    clearTimeout(copyTimer);
    copyTimer = null;
  }
  copiedKey.value = '';
}

onBeforeUnmount(clearCopyState);

const availableServices = computed(() => (props.modelServices?.length ? props.modelServices : localServices.value));

const directModelNames = computed(() => {
  const names = new Set<string>();
  for (const svc of availableServices.value) {
    if (svc.status !== 'active') continue;
    for (const cand of svc.candidates ?? []) {
      const m = cand.upstream_model?.trim();
      if (m && cand.status === 'active') {
        names.add(m);
      }
    }
  }
  return Array.from(names).sort((a, b) => a.localeCompare(b));
});

const smartRouteNames = computed(() => {
  const directSet = new Set(directModelNames.value);
  return Array.from(
    new Set(
      availableServices.value
        .filter(item => item.status === 'active' && item.expose_router && item.router_model_id?.trim())
        .map(item => item.router_model_id!.trim())
        .filter(id => !directSet.has(id))
    )
  ).sort((a, b) => a.localeCompare(b));
});

const totalAccessibleModels = computed(() => smartRouteNames.value.length + directModelNames.value.length);
const previewDirectModels = computed(() => directModelNames.value.slice(0, 8));
const remainingDirectModelsCount = computed(() => Math.max(0, directModelNames.value.length - 8));

const modelOptions = computed<SelectGroupOption[]>(() => {
  const groups: SelectGroupOption[] = [];
  if (directModelNames.value.length > 0) {
    groups.push({
      type: 'group',
      label: $t('business.quick-connect-group-direct-models'),
      key: 'direct-models',
      children: directModelNames.value.map(name => ({ label: name, value: name }))
    });
  }
  if (smartRouteNames.value.length > 0) {
    groups.push({
      type: 'group',
      label: $t('business.quick-connect-group-smart-routes'),
      key: 'smart-routes',
      children: smartRouteNames.value.map(name => ({ label: name, value: name }))
    });
  }
  return groups;
});

const gatewayOrigin = computed(
  () => configuredGatewayURL.value.trim().replace(/\/+$/, '') || resolveAutoGatewayOrigin()
);
const providedApiKey = computed(() => props.apiKey?.trim() || manualApiKey.value.trim());
const effectiveModel = computed(() => selectedModel.value.trim() || '<YOUR_MODEL_SERVICE>');
const effectiveApiKey = computed(() => providedApiKey.value || '<YOUR_OMNIFLUX_API_KEY>');
const hasPlaintextKey = computed(() => Boolean(props.apiKey?.trim()));
const hasUsableKey = computed(() => Boolean(providedApiKey.value));

function maskKeyPreview(raw: string): string {
  const trimmed = raw.trim();
  if (trimmed.length <= 12) return '••••••••';
  return `${trimmed.slice(0, 7)}••••${trimmed.slice(-4)}`;
}

const providerDisplayName = computed(() => {
  const keyLabel = props.keyName?.trim();
  if (keyLabel) return `Omniflux (${keyLabel})`;
  const modelLabel = selectedModel.value.trim();
  return modelLabel ? `Omniflux (${modelLabel})` : 'Omniflux Gateway';
});

const clientCards: { key: ClientTab; icon: string; titleKey: App.I18n.I18nKey; badge: string }[] = [
  { key: 'codex', icon: 'mdi:console', titleKey: 'business.quick-connect-tab-codex', badge: 'Responses API' },
  { key: 'claude', icon: 'mdi:robot-outline', titleKey: 'business.quick-connect-tab-claude', badge: 'Messages API' },
  { key: 'gemini', icon: 'mdi:auto-fix', titleKey: 'business.quick-connect-tab-gemini', badge: 'Gemini Native' },
  { key: 'openai', icon: 'mdi:code-braces', titleKey: 'business.quick-connect-tab-openai', badge: 'Cursor / SDK' }
];

const clientMetaMap: Record<ClientTab, { briefKey: App.I18n.I18nKey; endpoint: string }> = {
  codex: { briefKey: 'business.quick-connect-codex-brief', endpoint: 'POST /v1/responses' },
  claude: { briefKey: 'business.quick-connect-claude-brief', endpoint: 'POST /v1/messages' },
  gemini: { briefKey: 'business.quick-connect-gemini-brief', endpoint: 'POST /v1beta/models' },
  openai: { briefKey: 'business.quick-connect-openai-brief', endpoint: '/v1/chat/completions · /v1/responses' }
};

const activeClientMeta = computed(() => clientMetaMap[activeTab.value]);

const ccSwitchProfiles: Record<CCSwitchApp, { endpointPath: string; apiFormat: CCSwitchApiFormat }> = {
  codex: { endpointPath: '/v1', apiFormat: 'openai_responses' },
  claude: { endpointPath: '', apiFormat: 'anthropic' },
  gemini: { endpointPath: '', apiFormat: 'gemini_native' }
};

function encodeBase64UTF8(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let binary = '';
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary);
}

function buildCCSwitchUsageScript(app: CCSwitchApp): string {
  const quotaUrl = app === 'codex' ? '{{baseUrl}}/quota' : '{{baseUrl}}/v1/quota';
  return `({
  request: {
    url: "${quotaUrl}",
    method: "GET",
    headers: {
      "Authorization": "Bearer {{apiKey}}",
      "Accept": "application/json"
    }
  },
  extractor: function(res) {
    if (!res || res.valid === false) {
      return { isValid: false, invalidMessage: (res && res.error && res.error.message) || "Invalid API key" };
    }
    if (res.source === "upstream_balance" && Array.isArray(res.items) && res.items.length > 0) {
      return res.items.map(function(item) {
        var isPct = item.unit === "%";
        return {
          isValid: true,
          planName: item.plan_name || item.label_key || item.provider || "Upstream Quota",
          remaining: typeof item.remaining === "number" ? Number(item.remaining.toFixed(isPct ? 1 : 4)) : null,
          used: typeof item.used === "number" ? Number(item.used.toFixed(isPct ? 1 : 4)) : null,
          total: typeof item.total === "number" ? Number(item.total.toFixed(isPct ? 1 : 4)) : null,
          unit: item.unit || "USD"
        };
      });
    }
    return {
      isValid: true,
      planName: res.plan_name || (res.unlimited ? "Pay as you go" : "Budget"),
      remaining: typeof res.remaining === "number" ? Number(res.remaining.toFixed(4)) : null,
      used: typeof res.used === "number" ? Number(res.used.toFixed(4)) : 0,
      total: typeof res.total === "number" ? Number(res.total.toFixed(4)) : null,
      unit: res.unit || "USD"
    };
  }
})`;
}

function buildCCSwitchLink(app: CCSwitchApp): string {
  const profile = ccSwitchProfiles[app];
  const params = new URLSearchParams({
    resource: 'provider',
    app,
    name: providerDisplayName.value,
    endpoint: `${gatewayOrigin.value}${profile.endpointPath}`,
    homepage: gatewayOrigin.value,
    apiFormat: profile.apiFormat,
    usageScript: encodeBase64UTF8(buildCCSwitchUsageScript(app)),
    usageEnabled: 'true',
    usageAutoInterval: '5'
  });
  const model = selectedModel.value.trim();
  if (model) {
    params.set('model', model);
  }
  if (providedApiKey.value) {
    params.set('apiKey', providedApiKey.value);
  }
  return `ccswitch://v1/import?${params.toString()}`;
}

function openCCSwitch(app: CCSwitchApp) {
  if (typeof window === 'undefined') return;
  window.location.assign(buildCCSwitchLink(app));
}

async function loadModelServices() {
  if (props.modelServices?.length || !has('model_service:read')) return;
  loadingServices.value = true;
  try {
    const { data, error } = await fetchModelServices();
    if (!error && data) {
      localServices.value = data;
    }
  } finally {
    loadingServices.value = false;
  }
}

async function loadGatewayInfo() {
  const { data, error } = await fetchGatewayInfo();
  if (!error && data) {
    configuredGatewayURL.value = data.gateway_base_url?.trim() || '';
    isGatewayConfigured.value = Boolean(data.configured);
  }
}

watch(
  () => props.show,
  async visible => {
    if (!visible) {
      manualApiKey.value = '';
      clearCopyState();
      return;
    }
    await Promise.all([loadModelServices(), loadGatewayInfo()]);
    const accessible = new Set([...smartRouteNames.value, ...directModelNames.value]);
    if (props.initialModelService) {
      if (accessible.has(props.initialModelService)) {
        selectedModel.value = props.initialModelService;
        return;
      }
      const matched = availableServices.value.find(
        item => item.name === props.initialModelService || item.id === props.initialModelService
      );
      if (matched) {
        selectedModel.value =
          (matched.expose_router ? matched.router_model_id?.trim() : undefined) ||
          matched.candidates.find(c => c.status === 'active')?.upstream_model?.trim() ||
          directModelNames.value[0] ||
          smartRouteNames.value[0] ||
          '';
        return;
      }
    }
    if (!selectedModel.value || !accessible.has(selectedModel.value)) {
      selectedModel.value = directModelNames.value[0] ?? smartRouteNames.value[0] ?? '';
    }
  },
  { immediate: true }
);

const codexConfigSnippet = computed(() => {
  const baseV1 = `${gatewayOrigin.value}/v1`;
  return `model_provider = "omniflux"
model = "${effectiveModel.value}"

[model_providers.omniflux]
name = "Omniflux Gateway"
base_url = "${baseV1}"
wire_api = "responses"
env_key = "OPENAI_API_KEY"`;
});

const codexEnvSnippet = computed(
  () => `export OPENAI_API_KEY="${effectiveApiKey.value}"
codex -m "${effectiveModel.value}"`
);

const claudeSnippet = computed(
  () => `export ANTHROPIC_BASE_URL="${gatewayOrigin.value}"
export ANTHROPIC_AUTH_TOKEN="${effectiveApiKey.value}"
export ANTHROPIC_MODEL="${effectiveModel.value}"
claude`
);

const geminiSnippet = computed(
  () => `export GOOGLE_GEMINI_BASE_URL="${gatewayOrigin.value}"
export GEMINI_API_KEY="${effectiveApiKey.value}"
export GEMINI_MODEL="${effectiveModel.value}"
gemini`
);

const openaiBaseURL = computed(() => `${gatewayOrigin.value}/v1`);

const openaiCurlSnippet = computed(
  () => `curl -sS "${openaiBaseURL.value}/chat/completions" \\
  -H "Authorization: Bearer ${effectiveApiKey.value}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${effectiveModel.value}",
    "messages": [
      {"role": "user", "content": "Hello from Omniflux"}
    ]
  }'`
);

const activeSnippets = computed<SnippetBlock[]>(() => {
  switch (activeTab.value) {
    case 'codex':
      return [
        {
          id: 'codex-toml',
          titleKey: 'business.quick-connect-codex-config-title',
          lang: 'TOML',
          code: codexConfigSnippet.value
        },
        {
          id: 'codex-env',
          titleKey: 'business.quick-connect-codex-env-title',
          lang: 'Shell',
          code: codexEnvSnippet.value
        }
      ];
    case 'claude':
      return [
        {
          id: 'claude-env',
          titleKey: 'business.quick-connect-shell-env-title',
          lang: 'Shell',
          code: claudeSnippet.value
        }
      ];
    case 'gemini':
      return [
        {
          id: 'gemini-env',
          titleKey: 'business.quick-connect-shell-env-title',
          lang: 'Shell',
          code: geminiSnippet.value
        }
      ];
    case 'openai':
      return [
        {
          id: 'openai-url',
          titleKey: 'business.quick-connect-openai-base-url',
          lang: 'URL',
          code: openaiBaseURL.value
        },
        {
          id: 'openai-curl',
          titleKey: 'business.quick-connect-curl-title',
          lang: 'cURL',
          code: openaiCurlSnippet.value
        }
      ];
    default:
      return [];
  }
});

async function copySnippet(id: string, text: string) {
  await navigator.clipboard.writeText(text);
  copiedKey.value = id;
  if (copyTimer) {
    clearTimeout(copyTimer);
  }
  copyTimer = setTimeout(() => {
    copiedKey.value = '';
    copyTimer = null;
  }, 1800);
  window.$message?.success($t('business.copied-to-clipboard'));
}
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    :title="$t('business.quick-connect-title')"
    class="w-740px max-w-95vw"
    @update:show="val => emit('update:show', val)"
  >
    <NText depth="3" class="mb-14px block text-13px">
      {{ $t('business.quick-connect-subtitle') }}
    </NText>

    <div class="mb-16px rounded-10px border border-gray-200/90 bg-gray-50/70 p-14px dark:border-gray-700/70 dark:bg-gray-800/40">
      <NForm label-placement="top" :show-feedback="false">
        <NGrid :cols="24" :x-gap="12" :y-gap="12">
          <NFormItemGi :span="12" :label="$t('business.quick-connect-model-service')">
            <NSelect
              v-model:value="selectedModel"
              :options="modelOptions"
              :loading="loadingServices"
              filterable
              tag
              clearable
              :placeholder="$t('business.quick-connect-select-model')"
            />
          </NFormItemGi>
          <NFormItemGi :span="12" :label="$t('business.quick-connect-api-key-input')">
            <div
              v-if="hasPlaintextKey"
              class="flex h-34px w-full items-center justify-between gap-8px rounded-6px border border-emerald-500/35 bg-emerald-500/10 px-10px"
            >
              <code class="truncate text-12.5px font-mono text-emerald-700 dark:text-emerald-300">
                {{ maskKeyPreview(props.apiKey ?? '') }}
              </code>
              <NTag type="success" size="small" :bordered="false" class="shrink-0">
                {{ $t('business.quick-connect-plaintext-ready') }}
              </NTag>
            </div>
            <NInput
              v-else
              v-model:value="manualApiKey"
              type="password"
              show-password-on="click"
              clearable
              :placeholder="$t('business.quick-connect-api-key-input-placeholder')"
            />
          </NFormItemGi>
        </NGrid>
      </NForm>

      <div
        class="mt-12px flex flex-col gap-6px border-t border-gray-200/80 pt-10px dark:border-gray-700/60"
      >
        <div class="flex flex-wrap items-center justify-between gap-8px">
          <div class="flex items-center gap-6px">
            <SvgIcon icon="mdi:layers-triple-outline" class="shrink-0 text-15px text-primary" />
            <NText depth="3" class="text-12px">{{ $t('business.quick-connect-accessible-models') }}</NText>
          </div>
          <NText depth="3" class="text-12px">
            {{
              $t('business.quick-connect-accessible-summary', {
                direct: directModelNames.length,
                routers: smartRouteNames.length
              })
            }}
          </NText>
        </div>
        <div v-if="totalAccessibleModels > 0" class="flex flex-wrap items-center gap-6px">
          <NTag
            v-for="routerId in smartRouteNames"
            :key="`router-${routerId}`"
            size="small"
            type="primary"
            round
            class="cursor-pointer font-mono"
            @click="selectedModel = routerId"
          >
            {{ routerId }}
          </NTag>
          <NTag
            v-for="modelId in previewDirectModels"
            :key="`model-${modelId}`"
            size="small"
            :type="selectedModel === modelId ? 'info' : 'default'"
            :bordered="selectedModel === modelId"
            round
            class="cursor-pointer font-mono"
            @click="selectedModel = modelId"
          >
            {{ modelId }}
          </NTag>
          <NTag v-if="remainingDirectModelsCount > 0" size="small" :bordered="false" round>
            {{ $t('business.quick-connect-more-models', { count: totalAccessibleModels }) }}
          </NTag>
        </div>
        <NText v-else depth="3" class="text-12px">
          {{ $t('business.quick-connect-no-accessible-models') }}
        </NText>
      </div>

      <div
        class="mt-10px flex flex-wrap items-center justify-between gap-8px border-t border-gray-200/80 pt-10px dark:border-gray-700/60"
      >
        <div class="flex min-w-0 flex-wrap items-center gap-8px">
          <SvgIcon icon="mdi:server-network" class="shrink-0 text-15px text-primary" />
          <NText depth="3" class="text-12px">{{ $t('business.quick-connect-gateway-label') }}</NText>
          <code class="rounded bg-white px-7px py-2px text-12px font-mono text-gray-800 ring-1 ring-gray-200/80 dark:bg-gray-900 dark:text-gray-100 dark:ring-gray-700">
            {{ gatewayOrigin }}
          </code>
          <NTag size="tiny" :bordered="false" :type="isGatewayConfigured ? 'info' : 'default'">
            {{ isGatewayConfigured ? $t('business.quick-connect-gateway-configured') : $t('business.quick-connect-gateway-auto') }}
          </NTag>
        </div>
        <NButton size="tiny" quaternary @click="copySnippet('gateway', gatewayOrigin)">
          <template #icon>
            <SvgIcon
              :icon="copiedKey === 'gateway' ? 'mdi:check' : 'mdi:content-copy'"
              :class="copiedKey === 'gateway' ? 'text-13px text-emerald-500' : 'text-13px'"
            />
          </template>
          {{ copiedKey === 'gateway' ? $t('business.quick-connect-copied') : $t('business.quick-connect-copy-snippet') }}
        </NButton>
      </div>
    </div>

    <div class="mb-16px grid grid-cols-2 gap-8px sm:grid-cols-4">
      <button
        v-for="client in clientCards"
        :key="client.key"
        type="button"
        class="flex flex-col items-start gap-4px rounded-8px border px-12px py-10px text-left transition-colors"
        :class="
          activeTab === client.key
            ? 'border-primary bg-primary/8 text-primary'
            : 'border-gray-200/90 bg-white hover:border-gray-300 hover:bg-gray-50 dark:border-gray-700/70 dark:bg-gray-800/30 dark:hover:border-gray-600 dark:hover:bg-gray-800/60'
        "
        @click="activeTab = client.key"
      >
        <div class="flex w-full items-center gap-6px">
          <SvgIcon :icon="client.icon" class="shrink-0 text-16px" />
          <span class="truncate text-13px font-600">{{ $t(client.titleKey) }}</span>
        </div>
        <span class="text-11px opacity-75">{{ client.badge }}</span>
      </button>
    </div>

    <div class="mb-12px flex flex-wrap items-center justify-between gap-8px">
      <NText depth="3" class="text-12.5px">
        {{ $t(activeClientMeta.briefKey) }}
      </NText>
      <NTag size="small" :bordered="false" class="shrink-0 font-mono">
        {{ activeClientMeta.endpoint }}
      </NTag>
    </div>

    <div
      v-if="activeTab !== 'openai'"
      class="mb-14px flex flex-wrap items-center justify-between gap-12px rounded-10px border border-primary/25 bg-primary/5 px-14px py-12px dark:border-primary/35 dark:bg-primary/10"
    >
      <div class="flex min-w-0 flex-1 items-center gap-10px">
        <div
          class="flex h-34px w-34px shrink-0 items-center justify-center rounded-8px bg-primary/12 text-primary dark:bg-primary/20"
        >
          <SvgIcon icon="mdi:flash-outline" class="text-18px" />
        </div>
        <div class="min-w-0">
          <div class="text-13px font-600">{{ $t('business.quick-connect-ccswitch-title') }}</div>
          <NText depth="3" class="block text-12px">
            {{ hasUsableKey ? $t('business.quick-connect-ccswitch-ready') : $t('business.quick-connect-ccswitch-no-key') }}
          </NText>
        </div>
      </div>
      <NSpace :size="8" class="shrink-0">
        <NButton size="small" secondary @click="copySnippet('ccswitch-link', buildCCSwitchLink(activeTab))">
          <template #icon>
            <SvgIcon
              :icon="copiedKey === 'ccswitch-link' ? 'mdi:check' : 'mdi:link-variant'"
              :class="copiedKey === 'ccswitch-link' ? 'text-14px text-emerald-500' : 'text-14px'"
            />
          </template>
          {{
            copiedKey === 'ccswitch-link'
              ? $t('business.quick-connect-copied')
              : $t('business.quick-connect-ccswitch-copy-link')
          }}
        </NButton>
        <NButton size="small" type="primary" @click="openCCSwitch(activeTab)">
          <template #icon>
            <SvgIcon icon="mdi:open-in-new" class="text-14px" />
          </template>
          {{ $t('business.quick-connect-ccswitch-import') }}
        </NButton>
      </NSpace>
    </div>

    <div class="mb-8px flex items-center gap-6px text-12px font-600 text-gray-500 dark:text-gray-400">
      <SvgIcon icon="mdi:file-code-outline" class="text-14px" />
      <span>
        {{ activeTab === 'openai' ? $t('business.quick-connect-sdk-setup') : $t('business.quick-connect-manual-setup') }}
      </span>
    </div>

    <div class="flex flex-col gap-10px">
      <div
        v-for="block in activeSnippets"
        :key="block.id"
        class="overflow-hidden rounded-8px border border-gray-200/90 dark:border-gray-700/80"
      >
        <div
          class="flex items-center justify-between border-b border-gray-200/80 bg-gray-50 px-12px py-7px dark:border-gray-700/70 dark:bg-gray-800/80"
        >
          <div class="flex items-center gap-8px">
            <NText strong class="text-12.5px">{{ $t(block.titleKey) }}</NText>
            <span class="rounded bg-gray-200/70 px-6px py-1px text-10.5px font-mono text-gray-600 dark:bg-gray-700 dark:text-gray-300">
              {{ block.lang }}
            </span>
          </div>
          <NButton size="tiny" quaternary @click="copySnippet(block.id, block.code)">
            <template #icon>
              <SvgIcon
                :icon="copiedKey === block.id ? 'mdi:check' : 'mdi:content-copy'"
                :class="copiedKey === block.id ? 'text-13px text-emerald-500' : 'text-13px'"
              />
            </template>
            {{ copiedKey === block.id ? $t('business.quick-connect-copied') : $t('business.quick-connect-copy-snippet') }}
          </NButton>
        </div>
        <div class="bg-white px-12px py-10px dark:bg-[#141418]">
          <NCode :code="block.code" word-wrap />
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex flex-wrap items-center justify-between gap-8px">
        <div class="flex items-center gap-6px text-12px">
          <SvgIcon icon="mdi:cog-outline" class="text-14px text-gray-400" />
          <NText depth="3">{{ $t('business.quick-connect-gateway-settings-tip') }}</NText>
        </div>
        <NButton @click="emit('update:show', false)">{{ $t('business.close') }}</NButton>
      </div>
    </template>
  </NModal>
</template>
