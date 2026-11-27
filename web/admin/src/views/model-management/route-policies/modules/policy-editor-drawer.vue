<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import type { FormInst, FormRules, SelectGroupOption, SelectOption } from 'naive-ui';
import { fetchCreateRoutePolicy, fetchUpdateRoutePolicy } from '@/service/api';
import { $t } from '@/locales';
import {
  COMPARE_CURRENCY_OPTIONS,
  PRESET_WEIGHTS,
  SCORING_STRATEGIES,
  buildPolicyPayload,
  createEmptyPolicyDraft,
  draftFromPolicy,
  useCapabilityOptions,
  useStrategyOptions,
  type PolicyDraftModel
} from './policy-shared';

const props = defineProps<{
  show: boolean;
  mode: 'create' | 'edit';
  policy: Api.Management.RoutePolicy | null;
  providerOptions: (SelectOption | SelectGroupOption)[];
  modelServices: Api.Management.ModelService[];
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
  saved: [];
  simulateDraft: [draft: PolicyDraftModel];
}>();

const formRef = ref<FormInst | null>(null);
const activeTab = ref<'basic' | 'rules' | 'resilience'>('basic');
const submitting = ref(false);
const draft = reactive<PolicyDraftModel>(createEmptyPolicyDraft());

const strategyOptions = useStrategyOptions();
const capabilityOptions = useCapabilityOptions();

const rules = computed<FormRules>(() => ({
  name: { required: true, message: $t('business.enter-a-policy-name'), trigger: 'blur' }
}));

const isScoringStrategy = computed(() => SCORING_STRATEGIES.has(draft.strategy));
const isOrchestratorStrategy = computed(() => draft.strategy === 'auto_orchestrator');
const weightSum = computed(() => {
  const s = draft.weights.cost + draft.weights.quality + draft.weights.latency + draft.weights.reliability;
  return Math.round(s * 100) / 100;
});

const tierCandidateOptions = computed<SelectOption[]>(() => {
  const seen = new Set<string>();
  const items: SelectOption[] = [];
  for (const svc of props.modelServices) {
    for (const cand of svc.candidates ?? []) {
      const model = cand.upstream_model?.trim();
      if (model && !seen.has(model)) {
        seen.add(model);
        items.push({ label: model, value: model });
      }
      for (const tag of cand.tags ?? []) {
        const cleanTag = tag.trim();
        if (cleanTag && !seen.has(cleanTag)) {
          seen.add(cleanTag);
          items.push({ label: `#${cleanTag}`, value: cleanTag });
        }
      }
    }
  }
  return items.sort((a, b) => String(a.label).localeCompare(String(b.label)));
});

const orchestratorSessionModeOptions = computed<SelectOption[]>(() => [
  { label: $t('business.orchestrator-session-adaptive'), value: 'adaptive' },
  { label: $t('business.orchestrator-session-per-turn'), value: 'per_turn' },
  { label: $t('business.orchestrator-session-sticky-upgrade'), value: 'sticky_upgrade' }
]);

const baselineCandidateOptions = computed<SelectOption[]>(() => {
  const items: SelectOption[] = [];
  for (const svc of props.modelServices) {
    for (const cand of svc.candidates ?? []) {
      items.push({
        label: `${svc.name} · ${cand.upstream_model} (${cand.input_cost_per_million}/${cand.output_cost_per_million} ${cand.cost_currency})`,
        value: cand.id
      });
    }
  }
  return items;
});

const streamConditionOptions = computed(() => [
  { label: $t('business.rule-stream-any'), value: 'any' },
  { label: $t('business.rule-stream-only'), value: 'stream' },
  { label: $t('business.rule-non-stream-only'), value: 'non_stream' }
]);

const sessionAffinityStrategyOptions = computed(() => [
  { label: $t('business.session-affinity-strategy-auto'), value: 'auto' },
  { label: $t('business.session-affinity-strategy-user-id'), value: 'user_id' },
  { label: $t('business.session-affinity-strategy-header-only'), value: 'header_only' }
]);

watch(
  () => [props.show, props.mode, props.policy],
  () => {
    if (!props.show) return;
    activeTab.value = 'basic';
    if (props.mode === 'edit' && props.policy) {
      Object.assign(draft, draftFromPolicy(props.policy));
    } else {
      Object.assign(draft, createEmptyPolicyDraft());
    }
  },
  { immediate: true }
);

function handleStrategyChange(nextStrategy: string) {
  draft.strategy = nextStrategy;
  if (PRESET_WEIGHTS[nextStrategy]) {
    draft.weights = { ...PRESET_WEIGHTS[nextStrategy] };
  }
}

function applyPresetWeights(presetKey: string) {
  if (PRESET_WEIGHTS[presetKey]) {
    draft.weights = { ...PRESET_WEIGHTS[presetKey] };
  }
}

function addRule() {
  const nextIdx = draft.rules.length + 1;
  draft.rules.push({
    id: `rule-${Date.now().toString(36)}-${nextIdx}`,
    name: $t('business.default-rule-name', { index: nextIdx }),
    enabled: true,
    priority: nextIdx * 10,
    when: {
      min_input_tokens: null,
      max_input_tokens: null,
      stream: null,
      capabilities: [],
      keywords: []
    },
    target: {
      strategy: '',
      candidate_tags: [],
      allowed_providers: [],
      blocked_providers: [],
      allowed_models: []
    }
  });
}

function removeRule(index: number) {
  draft.rules.splice(index, 1);
}

function moveRule(index: number, delta: number) {
  const target = index + delta;
  if (target < 0 || target >= draft.rules.length) return;
  const [item] = draft.rules.splice(index, 1);
  draft.rules.splice(target, 0, item);
}

function metadataTextOf(rule: Api.Management.RouteRule) {
  const entries = Object.entries(rule.when.metadata ?? {});
  return entries.map(([k, vals]) => `${k}=${vals.join('|')}`).join(', ');
}

function setMetadataFromText(rule: Api.Management.RouteRule, raw: string) {
  const next: Record<string, string[]> = {};
  for (const part of raw.split(',')) {
    const idx = part.indexOf('=');
    if (idx <= 0) continue;
    const key = part.slice(0, idx).trim();
    const vals = part
      .slice(idx + 1)
      .split('|')
      .map(v => v.trim())
      .filter(Boolean);
    if (key && vals.length) next[key] = vals;
  }
  rule.when.metadata = Object.keys(next).length ? next : undefined;
}

function streamValueOf(rule: Api.Management.RouteRule) {
  if (rule.when.stream === true) return 'stream';
  if (rule.when.stream === false) return 'non_stream';
  return 'any';
}

function setStreamValue(rule: Api.Management.RouteRule, val: string) {
  if (val === 'stream') rule.when.stream = true;
  else if (val === 'non_stream') rule.when.stream = false;
  else rule.when.stream = null;
}

async function handleSave() {
  await formRef.value?.validate();
  if (isScoringStrategy.value && weightSum.value <= 0) {
    window.$message?.error($t('business.weights-sum-must-be-positive'));
    return;
  }
  for (const r of draft.rules) {
    if (!r.name.trim() || !r.id.trim()) {
      window.$message?.error($t('business.rule-name-required'));
      activeTab.value = 'rules';
      return;
    }
  }
  submitting.value = true;
  try {
    const payload = buildPolicyPayload(draft);
    const { error } =
      props.mode === 'create' ? await fetchCreateRoutePolicy(payload) : await fetchUpdateRoutePolicy(draft.id, payload);
    if (!error) {
      window.$message?.success(
        props.mode === 'create' ? $t('business.route-policy-created') : $t('business.route-policy-updated')
      );
      emit('update:show', false);
      emit('saved');
    }
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <NDrawer
    :show="show"
    :width="840"
    :mask-closable="false"
    @update:show="val => emit('update:show', val)"
  >
    <NDrawerContent
      :title="mode === 'create' ? $t('business.create-route-policy') : $t('business.edit-route-policy')"
      closable
    >
      <NForm ref="formRef" :model="draft" :rules="rules" label-placement="top">
        <NTabs v-model:value="activeTab" type="line" animated>
          <NTabPane name="basic" :tab="$t('business.policy-tab-basic')">
            <NGrid :cols="2" :x-gap="16">
              <NFormItemGi :label="$t('business.policy-name')" path="name">
                <NInput v-model:value="draft.name" :placeholder="$t('business.for-example-default-balanced-policy')" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.compare-currency')">
                <NSelect v-model:value="draft.compare_currency" :options="COMPARE_CURRENCY_OPTIONS" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.scheduling-strategy')">
                <NSelect
                  :value="draft.strategy"
                  :options="strategyOptions"
                  @update:value="handleStrategyChange"
                />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.baseline-candidate')">
                <NSelect
                  v-model:value="draft.baseline_candidate_id"
                  :options="baselineCandidateOptions"
                  :placeholder="$t('business.baseline-candidate-auto-hint')"
                  clearable
                  filterable
                />
              </NFormItemGi>
            </NGrid>

            <NAlert v-if="!isScoringStrategy && !isOrchestratorStrategy" type="info" :bordered="false" class="mb-12px">
              {{ $t('business.non-scoring-strategy-hint') }}
            </NAlert>

            <NCard v-if="isOrchestratorStrategy" size="small" :title="$t('business.orchestrator-title')" class="mt-8px">
              <NAlert type="info" :bordered="false" class="mb-12px">
                {{ $t('business.orchestrator-auto-hint') }}
              </NAlert>
              <NGrid :cols="2" :x-gap="16">
                <NFormItemGi :span="2" :label="$t('business.orchestrator-session-mode')">
                  <NSelect
                    v-model:value="draft.orchestrator_config.session_mode"
                    :options="orchestratorSessionModeOptions"
                  />
                </NFormItemGi>
                <NFormItemGi :label="$t('business.orchestrator-sticky-context-tokens')">
                  <NInputNumber
                    v-model:value="draft.orchestrator_config.sticky_context_tokens"
                    :min="1000"
                    :max="200000"
                    :step="1000"
                    :disabled="draft.orchestrator_config.session_mode !== 'adaptive'"
                    class="w-full"
                  />
                </NFormItemGi>
                <NFormItemGi :label="$t('business.orchestrator-long-context-threshold')">
                  <NInputNumber
                    v-model:value="draft.orchestrator_config.long_context_threshold"
                    :min="2000"
                    :max="1000000"
                    :step="4000"
                    class="w-full"
                  />
                </NFormItemGi>
              </NGrid>

              <NDivider title-placement="left">{{ $t('business.orchestrator-thresholds') }}</NDivider>
              <NGrid :cols="3" :x-gap="12">
                <NFormItemGi :label="$t('business.orchestrator-threshold-simple-medium')" :show-feedback="false">
                  <NInputNumber
                    v-model:value="draft.orchestrator_config.thresholds.simple_medium"
                    :min="0.05"
                    :max="0.9"
                    :step="0.02"
                    class="w-full"
                  />
                </NFormItemGi>
                <NFormItemGi :label="$t('business.orchestrator-threshold-medium-complex')" :show-feedback="false">
                  <NInputNumber
                    v-model:value="draft.orchestrator_config.thresholds.medium_complex"
                    :min="0.1"
                    :max="0.95"
                    :step="0.02"
                    class="w-full"
                  />
                </NFormItemGi>
                <NFormItemGi :label="$t('business.orchestrator-threshold-complex-reasoning')" :show-feedback="false">
                  <NInputNumber
                    v-model:value="draft.orchestrator_config.thresholds.complex_reasoning"
                    :min="0.2"
                    :max="0.98"
                    :step="0.02"
                    class="w-full"
                  />
                </NFormItemGi>
              </NGrid>

              <NDivider title-placement="left">{{ $t('business.orchestrator-tiers-optional') }}</NDivider>
              <NGrid v-if="draft.orchestrator_config.tiers" :cols="2" :x-gap="16" :y-gap="8">
                <NFormItemGi :label="$t('business.orchestrator-tier-simple')" :show-feedback="false">
                  <NSelect
                    v-model:value="draft.orchestrator_config.tiers.simple"
                    :options="tierCandidateOptions"
                    :placeholder="$t('business.orchestrator-tiers-placeholder')"
                    multiple
                    filterable
                    tag
                    clearable
                  />
                </NFormItemGi>
                <NFormItemGi :label="$t('business.orchestrator-tier-medium')" :show-feedback="false">
                  <NSelect
                    v-model:value="draft.orchestrator_config.tiers.medium"
                    :options="tierCandidateOptions"
                    :placeholder="$t('business.orchestrator-tiers-placeholder')"
                    multiple
                    filterable
                    tag
                    clearable
                  />
                </NFormItemGi>
                <NFormItemGi :label="$t('business.orchestrator-tier-complex')" :show-feedback="false">
                  <NSelect
                    v-model:value="draft.orchestrator_config.tiers.complex"
                    :options="tierCandidateOptions"
                    :placeholder="$t('business.orchestrator-tiers-placeholder')"
                    multiple
                    filterable
                    tag
                    clearable
                  />
                </NFormItemGi>
                <NFormItemGi :label="$t('business.orchestrator-tier-reasoning')" :show-feedback="false">
                  <NSelect
                    v-model:value="draft.orchestrator_config.tiers.reasoning"
                    :options="tierCandidateOptions"
                    :placeholder="$t('business.orchestrator-tiers-placeholder')"
                    multiple
                    filterable
                    tag
                    clearable
                  />
                </NFormItemGi>
              </NGrid>
            </NCard>

            <NCard v-if="!isOrchestratorStrategy" size="small" :title="$t('business.scoring-weights')" class="mt-8px">
              <template #header-extra>
                <NSpace align="center" :size="8">
                  <NTag size="small" :type="weightSum > 0 ? 'success' : 'error'">
                    {{ $t('business.weights-sum') }}: {{ weightSum }}
                  </NTag>
                  <NButton size="tiny" secondary :disabled="!isScoringStrategy" @click="applyPresetWeights('balanced')">
                    {{ $t('business.balanced') }}
                  </NButton>
                  <NButton size="tiny" secondary :disabled="!isScoringStrategy" @click="applyPresetWeights('cost_first')">
                    {{ $t('business.cost-first') }}
                  </NButton>
                  <NButton size="tiny" secondary :disabled="!isScoringStrategy" @click="applyPresetWeights('latency_first')">
                    {{ $t('business.strategy-latency-first') }}
                  </NButton>
                </NSpace>
              </template>
              <NGrid :cols="2" :x-gap="20" :y-gap="8">
                <NGi>
                  <div class="mb-4px flex justify-between text-12px">
                    <span>{{ $t('business.weight-cost') }}</span>
                    <span>{{ draft.weights.cost.toFixed(2) }}</span>
                  </div>
                  <NSlider v-model:value="draft.weights.cost" :min="0" :max="1" :step="0.05" :disabled="!isScoringStrategy" />
                </NGi>
                <NGi>
                  <div class="mb-4px flex justify-between text-12px">
                    <span>{{ $t('business.weight-quality') }}</span>
                    <span>{{ draft.weights.quality.toFixed(2) }}</span>
                  </div>
                  <NSlider v-model:value="draft.weights.quality" :min="0" :max="1" :step="0.05" :disabled="!isScoringStrategy" />
                </NGi>
                <NGi>
                  <div class="mb-4px flex justify-between text-12px">
                    <span>{{ $t('business.weight-latency') }}</span>
                    <span>{{ draft.weights.latency.toFixed(2) }}</span>
                  </div>
                  <NSlider v-model:value="draft.weights.latency" :min="0" :max="1" :step="0.05" :disabled="!isScoringStrategy" />
                </NGi>
                <NGi>
                  <div class="mb-4px flex justify-between text-12px">
                    <span>{{ $t('business.weight-reliability') }}</span>
                    <span>{{ draft.weights.reliability.toFixed(2) }}</span>
                  </div>
                  <NSlider
                    v-model:value="draft.weights.reliability"
                    :min="0"
                    :max="1"
                    :step="0.05"
                    :disabled="!isScoringStrategy"
                  />
                </NGi>
              </NGrid>
            </NCard>
          </NTabPane>

          <NTabPane name="rules" :tab="`${$t('business.conditional-rules')} (${draft.rules.length})`">
            <NAlert type="info" :bordered="false" class="mb-12px">
              {{ $t('business.conditional-rules-hint') }}
            </NAlert>
            <div class="mb-12px flex justify-end">
              <NButton size="small" type="primary" secondary @click="addRule">
                {{ $t('business.add-rule') }}
              </NButton>
            </div>
            <NEmpty v-if="!draft.rules.length" :description="$t('business.no-conditional-rules')" />
            <div class="flex flex-col gap-12px">
              <NCard v-for="(rule, idx) in draft.rules" :key="rule.id" size="small">
                <template #header>
                  <NSpace align="center">
                    <NSwitch v-model:value="rule.enabled" size="small" />
                    <NInput v-model:value="rule.name" size="small" class="w-220px" :placeholder="$t('business.rule-name')" />
                    <NInputNumber v-model:value="rule.priority" size="small" :min="1" class="w-100px" />
                  </NSpace>
                </template>
                <template #header-extra>
                  <NSpace :size="6">
                    <NButton size="tiny" quaternary :disabled="idx === 0" @click="moveRule(idx, -1)">
                      {{ $t('business.move-up') }}
                    </NButton>
                    <NButton size="tiny" quaternary :disabled="idx === draft.rules.length - 1" @click="moveRule(idx, 1)">
                      {{ $t('business.move-down') }}
                    </NButton>
                    <NButton size="tiny" quaternary type="error" @click="removeRule(idx)">
                      {{ $t('business.delete') }}
                    </NButton>
                  </NSpace>
                </template>
                <NGrid :cols="2" :x-gap="12" :y-gap="8">
                  <NFormItemGi :label="$t('business.rule-min-tokens')" :show-feedback="false">
                    <NInputNumber v-model:value="rule.when.min_input_tokens" :min="0" clearable class="w-full" />
                  </NFormItemGi>
                  <NFormItemGi :label="$t('business.rule-max-tokens')" :show-feedback="false">
                    <NInputNumber v-model:value="rule.when.max_input_tokens" :min="0" clearable class="w-full" />
                  </NFormItemGi>
                  <NFormItemGi :label="$t('business.rule-stream')" :show-feedback="false">
                    <NSelect
                      :value="streamValueOf(rule)"
                      :options="streamConditionOptions"
                      @update:value="val => setStreamValue(rule, val)"
                    />
                  </NFormItemGi>
                  <NFormItemGi :label="$t('business.rule-capabilities')" :show-feedback="false">
                    <NSelect v-model:value="rule.when.capabilities" :options="capabilityOptions" multiple clearable />
                  </NFormItemGi>
                  <NFormItemGi :label="$t('business.rule-keywords')" :show-feedback="false">
                    <NDynamicTags v-model:value="rule.when.keywords" />
                  </NFormItemGi>
                  <NFormItemGi :label="$t('business.rule-metadata')" :show-feedback="false">
                    <NInput
                      :value="metadataTextOf(rule)"
                      placeholder="tier=vip|enterprise, env=prod"
                      @update:value="val => setMetadataFromText(rule, val)"
                    />
                  </NFormItemGi>
                  <NFormItemGi :label="$t('business.rule-target-strategy')" :show-feedback="false">
                    <NSelect v-model:value="rule.target.strategy" :options="strategyOptions" clearable />
                  </NFormItemGi>
                  <NFormItemGi :label="$t('business.rule-target-tags')" :show-feedback="false">
                    <NDynamicTags v-model:value="rule.target.candidate_tags" />
                  </NFormItemGi>
                  <NFormItemGi :label="$t('business.allowed-providers')" :show-feedback="false">
                    <NSelect v-model:value="rule.target.allowed_providers" :options="providerOptions" multiple clearable />
                  </NFormItemGi>
                  <NFormItemGi :label="$t('business.allowed-upstream-models')" :show-feedback="false">
                    <NDynamicTags v-model:value="rule.target.allowed_models" />
                  </NFormItemGi>
                </NGrid>
              </NCard>
            </div>
          </NTabPane>

          <NTabPane name="resilience" :tab="$t('business.policy-tab-resilience')">
            <NDivider title-placement="left">{{ $t('business.hard-constraints') }}</NDivider>
            <NGrid :cols="2" :x-gap="16">
              <NFormItemGi :label="$t('business.allowed-providers')">
                <NSelect
                  v-model:value="draft.constraints.allowed_providers"
                  :options="providerOptions"
                  :placeholder="$t('business.leave-blank-for-no-limit')"
                  multiple
                  clearable
                />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.blocked-providers')">
                <NSelect
                  v-model:value="draft.constraints.blocked_providers"
                  :options="providerOptions"
                  :placeholder="$t('business.leave-blank-for-no-limit')"
                  multiple
                  clearable
                />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.allowed-upstream-models')">
                <NDynamicTags v-model:value="draft.constraints.allowed_upstream_models" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.blocked-upstream-models')">
                <NDynamicTags v-model:value="draft.constraints.blocked_upstream_models" />
              </NFormItemGi>
              <NFormItemGi :label="`${$t('business.blended-unit-price-ceiling')} (${draft.compare_currency})`">
                <NInputNumber
                  v-model:value="draft.constraints.max_cost_per_million"
                  :min="0"
                  :step="0.5"
                  clearable
                  class="w-full"
                  :placeholder="$t('business.leave-blank-for-no-limit')"
                />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.failover-limit')">
                <NInputNumber v-model:value="draft.fallback_limit" :min="0" :max="20" class="w-full" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.upstream-timeout-ms')">
                <NInputNumber v-model:value="draft.provider_timeout_ms" :min="1000" :step="1000" class="w-full" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.first-token-timeout-ms')">
                <NInputNumber v-model:value="draft.first_token_timeout_ms" :min="0" :step="1000" class="w-full" />
              </NFormItemGi>
            </NGrid>

            <NDivider title-placement="left">{{ $t('business.retry-policy') }}</NDivider>
            <NGrid :cols="3" :x-gap="12" :y-gap="10">
              <NFormItemGi :label="$t('business.retry-429')" :show-feedback="false">
                <NSwitch v-model:value="draft.retry_policy.retry_429" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.retry-5xx')" :show-feedback="false">
                <NSwitch v-model:value="draft.retry_policy.retry_5xx" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.retry-timeout')" :show-feedback="false">
                <NSwitch v-model:value="draft.retry_policy.retry_timeout" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.fallback-on-rate-limit')" :show-feedback="false">
                <NSwitch v-model:value="draft.retry_policy.fallback_on_rate_limit" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.fallback-on-server-error')" :show-feedback="false">
                <NSwitch v-model:value="draft.retry_policy.fallback_on_server_error" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.fallback-on-ttft-timeout')" :show-feedback="false">
                <NSwitch v-model:value="draft.retry_policy.fallback_on_ttft_timeout" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.fallback-on-auth-error')" :show-feedback="false">
                <NSwitch v-model:value="draft.retry_policy.fallback_on_auth_error" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.fallback-on-not-found')" :show-feedback="false">
                <NSwitch v-model:value="draft.retry_policy.fallback_on_not_found" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.max-retries-per-candidate')" :show-feedback="false">
                <NInputNumber v-model:value="draft.retry_policy.max_retries_per_candidate" :min="0" :max="3" class="w-full" />
              </NFormItemGi>
            </NGrid>

            <NDivider title-placement="left">{{ $t('business.circuit-breaker') }}</NDivider>
            <NGrid :cols="3" :x-gap="16">
              <NFormItemGi :label="$t('business.circuit-breaker-enabled')">
                <NSwitch v-model:value="draft.circuit_breaker.enabled" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.circuit-failure-threshold')">
                <NInputNumber
                  v-model:value="draft.circuit_breaker.failure_threshold"
                  :min="1"
                  :max="50"
                  :disabled="!draft.circuit_breaker.enabled"
                  class="w-full"
                />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.circuit-cooldown-seconds')">
                <NInputNumber
                  v-model:value="draft.circuit_breaker.cooldown_seconds"
                  :min="5"
                  :max="3600"
                  :disabled="!draft.circuit_breaker.enabled"
                  class="w-full"
                />
              </NFormItemGi>
            </NGrid>

            <NDivider title-placement="left">{{ $t('business.session-affinity') }}</NDivider>
            <NGrid :cols="3" :x-gap="16">
              <NFormItemGi :label="$t('business.session-affinity-enabled')">
                <NSwitch v-model:value="draft.session_affinity.enabled" />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.session-affinity-ttl')">
                <NInputNumber
                  v-model:value="draft.session_affinity.ttl_seconds"
                  :min="30"
                  :max="86400"
                  :step="60"
                  :disabled="!draft.session_affinity.enabled"
                  class="w-full"
                />
              </NFormItemGi>
              <NFormItemGi :label="$t('business.session-affinity-strategy')">
                <NSelect
                  v-model:value="draft.session_affinity.key_strategy"
                  :options="sessionAffinityStrategyOptions"
                  :disabled="!draft.session_affinity.enabled"
                />
              </NFormItemGi>
            </NGrid>
            <NAlert type="info" :bordered="false" class="mt-4px">
              {{ $t('business.session-affinity-hint') }}
            </NAlert>
          </NTabPane>
        </NTabs>
      </NForm>

      <template #footer>
        <div class="flex w-full items-center justify-between">
          <NButton secondary @click="emit('simulateDraft', { ...draft })">
            {{ $t('business.simulate-draft') }}
          </NButton>
          <NSpace>
            <NButton @click="emit('update:show', false)">{{ $t('common.cancel') }}</NButton>
            <NButton type="primary" :loading="submitting" @click="handleSave">{{ $t('common.confirm') }}</NButton>
          </NSpace>
        </div>
      </template>
    </NDrawerContent>
  </NDrawer>
</template>
