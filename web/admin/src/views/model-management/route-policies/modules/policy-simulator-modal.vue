<script setup lang="ts">
import { computed, h, reactive, ref, watch } from 'vue';
import { NTag } from 'naive-ui';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { fetchSimulateRoutePolicy } from '@/service/api';
import { routeStrategyTagType, routeStrategyText } from '@/utils/admin-status';
import { $t } from '@/locales';
import { buildPolicyPayload, useCapabilityOptions, type PolicyDraftModel } from './policy-shared';

const props = defineProps<{
  show: boolean;
  policies: Api.Management.RoutePolicy[];
  modelServices: Api.Management.ModelService[];
  initialPolicyId?: string | null;
  draftOverride?: PolicyDraftModel | null;
}>();

const emit = defineEmits<{
  'update:show': [value: boolean];
}>();

const loading = ref(false);
const result = ref<Api.Management.SimulateRoutePolicyResult | null>(null);
const capabilityOptions = useCapabilityOptions();

const form = reactive<{
  model_service_id: string | null;
  policy_id: string | null;
  estimated_input_tokens: number;
  estimated_output_tokens: number;
  stream: boolean;
  required_capabilities: string[];
  keyword_text: string;
  metadata_text: string;
  reasoning_effort: string | null;
  tool_count: number;
  tool_turn_count: number;
  rr_counter: number;
  simulate_failed_candidate_ids: string[];
}>({
  model_service_id: null,
  policy_id: null,
  estimated_input_tokens: 2000,
  estimated_output_tokens: 500,
  stream: true,
  required_capabilities: [],
  keyword_text: '',
  metadata_text: '',
  reasoning_effort: null,
  tool_count: 0,
  tool_turn_count: 0,
  rr_counter: 0,
  simulate_failed_candidate_ids: []
});

const reasoningEffortOptions: SelectOption[] = [
  { label: 'low', value: 'low' },
  { label: 'medium', value: 'medium' },
  { label: 'high', value: 'high' },
  { label: 'xhigh', value: 'xhigh' }
];

const serviceOptions = computed<SelectOption[]>(() =>
  props.modelServices.map(s => ({ label: s.name, value: s.id }))
);

const policyOptions = computed<SelectOption[]>(() =>
  props.policies.map(p => ({ label: `${p.name} (v${p.version})`, value: p.id }))
);

const activeService = computed(() => props.modelServices.find(s => s.id === form.model_service_id) ?? null);

const candidateFailOptions = computed<SelectOption[]>(() =>
  (activeService.value?.candidates ?? []).map(c => ({
    label: `${c.upstream_model} (${c.provider})`,
    value: c.id
  }))
);

watch(
  () => [props.show, props.initialPolicyId, props.draftOverride],
  () => {
    if (!props.show) return;
    result.value = null;
    form.policy_id = props.draftOverride ? null : (props.initialPolicyId ?? props.policies[0]?.id ?? null);
    const matchingSvc = form.policy_id
      ? props.modelServices.find(s => s.route_policy_id === form.policy_id)
      : undefined;
    form.model_service_id = matchingSvc?.id ?? props.modelServices[0]?.id ?? null;
    form.simulate_failed_candidate_ids = [];
  },
  { immediate: true }
);

function parseMetadataText(raw: string): Record<string, string> | undefined {
  const out: Record<string, string> = {};
  for (const part of raw.split(',')) {
    const idx = part.indexOf('=');
    if (idx <= 0) continue;
    const k = part.slice(0, idx).trim();
    const v = part.slice(idx + 1).trim();
    if (k && v) out[k] = v;
  }
  return Object.keys(out).length ? out : undefined;
}

function excludedReasonText(reason?: string) {
  if (!reason) return '-';
  const map: Record<string, App.I18n.I18nKey> = {
    rule_excluded: 'business.sim-excluded-rule',
    not_in_allowed_providers: 'business.sim-excluded-provider',
    in_blocked_providers: 'business.sim-excluded-provider',
    not_in_allowed_models: 'business.sim-excluded-model',
    in_blocked_models: 'business.sim-excluded-model',
    exceeds_max_cost: 'business.sim-excluded-cost',
    context_window_exceeded: 'business.sim-excluded-context',
    missing_capability: 'business.sim-excluded-capability',
    circuit_open: 'business.sim-excluded-circuit',
    exceeds_max_attempts: 'business.sim-excluded-attempts'
  };
  const key = map[reason];
  return key ? $t(key) : reason;
}

function warningText(warn: string) {
  const map: Record<string, App.I18n.I18nKey> = {
    rule_filter_empty_fallback: 'business.sim-warn-rule-fallback',
    all_circuits_open_fallback: 'business.sim-warn-all-open',
    missing_fx_rate_neutral_cost: 'business.sim-warn-fx-missing'
  };
  const key = map[warn];
  return key ? $t(key) : warn;
}

const columns = computed<DataTableColumns<Api.Management.CandidateEvaluation>>(() => [
  {
    title: $t('business.sim-rank'),
    key: 'rank',
    width: 70,
    render: row =>
      row.selected
        ? h(NTag, { size: 'small', type: row.rank === 1 ? 'success' : 'info', round: true }, { default: () => `#${row.rank}` })
        : h(NTag, { size: 'small', type: 'default', round: true }, { default: () => $t('business.sim-excluded') })
  },
  {
    title: $t('business.model'),
    key: 'upstream_model',
    minWidth: 180,
    render: row => `${row.upstream_model} (${row.provider})`
  },
  { title: $t('business.priority'), key: 'priority', width: 85 },
  {
    title: $t('business.sim-est-cost'),
    key: 'estimated_cost',
    width: 120,
    render: row => (row.cost_comparable ? `${row.estimated_cost.toFixed(6)} ${row.compare_currency}` : '-')
  },
  {
    title: $t('business.weight-cost'),
    key: 'cost_score',
    width: 75,
    render: row => row.cost_score.toFixed(2)
  },
  {
    title: $t('business.weight-quality'),
    key: 'quality_score',
    width: 85,
    render: row => row.quality_score.toFixed(2)
  },
  {
    title: $t('business.weight-latency'),
    key: 'latency_score',
    width: 95,
    render: row =>
      row.latency_ms !== null && row.latency_ms !== undefined
        ? `${row.latency_score.toFixed(2)} (${Math.round(row.latency_ms)}ms)`
        : row.latency_score.toFixed(2)
  },
  {
    title: $t('business.weight-reliability'),
    key: 'reliability_score',
    width: 85,
    render: row => `${Math.round(row.reliability_rate * 100)}%`
  },
  {
    title: $t('business.sim-total-score'),
    key: 'total_score',
    width: 105,
    render: row => row.total_score.toFixed(3)
  },
  {
    title: $t('business.sim-reason'),
    key: 'excluded_reason',
    minWidth: 160,
    render: row => excludedReasonText(row.excluded_reason)
  }
]);

async function handleRunSimulate() {
  if (!form.model_service_id) {
    window.$message?.warning($t('business.select-a-model-service'));
    return;
  }
  loading.value = true;
  try {
    const draftFields = props.draftOverride ? buildPolicyPayload(props.draftOverride) : {};
    const payload: Api.Management.SimulateRoutePolicyParams = {
      ...draftFields,
      model_service_id: form.model_service_id,
      policy_id: props.draftOverride ? undefined : form.policy_id,
      estimated_input_tokens: form.estimated_input_tokens,
      estimated_output_tokens: form.estimated_output_tokens,
      stream: form.stream,
      required_capabilities: form.required_capabilities,
      keyword_text: form.keyword_text || undefined,
      metadata: parseMetadataText(form.metadata_text),
      reasoning_effort: form.reasoning_effort || undefined,
      tool_count: form.tool_count > 0 ? form.tool_count : undefined,
      tool_turn_count: form.tool_turn_count > 0 ? form.tool_turn_count : undefined,
      rr_counter: form.rr_counter,
      simulate_failed_candidate_ids: form.simulate_failed_candidate_ids
    };
    const { data, error } = await fetchSimulateRoutePolicy(payload);
    if (!error && data) {
      result.value = data;
    }
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    :title="$t('business.dry-run-simulator')"
    class="w-1020px max-w-95vw"
    @update:show="val => emit('update:show', val)"
  >
    <NAlert v-if="draftOverride" type="info" :bordered="false" class="mb-12px">
      {{ $t('business.simulating-unsaved-draft', { name: draftOverride.name || '-' }) }}
    </NAlert>
    <NGrid :cols="3" :x-gap="12" :y-gap="8">
      <NFormItemGi :label="$t('business.model-service')" :show-feedback="false">
        <NSelect v-model:value="form.model_service_id" :options="serviceOptions" filterable />
      </NFormItemGi>
      <NFormItemGi v-if="!draftOverride" :label="$t('business.route-policy')" :show-feedback="false">
        <NSelect v-model:value="form.policy_id" :options="policyOptions" clearable filterable />
      </NFormItemGi>
      <NFormItemGi :label="$t('business.rule-stream')" :show-feedback="false">
        <NSwitch v-model:value="form.stream" />
      </NFormItemGi>
      <NFormItemGi :label="$t('business.sim-input-tokens')" :show-feedback="false">
        <NInputNumber v-model:value="form.estimated_input_tokens" :min="1" :step="500" class="w-full" />
      </NFormItemGi>
      <NFormItemGi :label="$t('business.sim-output-tokens')" :show-feedback="false">
        <NInputNumber v-model:value="form.estimated_output_tokens" :min="1" :step="100" class="w-full" />
      </NFormItemGi>
      <NFormItemGi :label="$t('business.rule-capabilities')" :show-feedback="false">
        <NSelect v-model:value="form.required_capabilities" :options="capabilityOptions" multiple clearable />
      </NFormItemGi>
      <NFormItemGi :label="$t('business.rule-keywords')" :show-feedback="false">
        <NInput v-model:value="form.keyword_text" :placeholder="$t('business.sim-keyword-placeholder')" />
      </NFormItemGi>
      <NFormItemGi :label="$t('business.rule-metadata')" :show-feedback="false">
        <NInput v-model:value="form.metadata_text" placeholder="tier=vip, env=prod" />
      </NFormItemGi>
      <NFormItemGi :label="$t('business.sim-failed-candidates')" :show-feedback="false">
        <NSelect v-model:value="form.simulate_failed_candidate_ids" :options="candidateFailOptions" multiple clearable />
      </NFormItemGi>
      <NFormItemGi :label="$t('business.sim-reasoning-effort')" :show-feedback="false">
        <NSelect v-model:value="form.reasoning_effort" :options="reasoningEffortOptions" clearable />
      </NFormItemGi>
      <NFormItemGi :label="$t('business.sim-tool-count')" :show-feedback="false">
        <NInputNumber v-model:value="form.tool_count" :min="0" :max="100" class="w-full" />
      </NFormItemGi>
      <NFormItemGi :label="$t('business.sim-tool-turns')" :show-feedback="false">
        <NInputNumber v-model:value="form.tool_turn_count" :min="0" :max="100" class="w-full" />
      </NFormItemGi>
    </NGrid>

    <div class="my-12px flex justify-end gap-8px">
      <NButton type="primary" :loading="loading" @click="handleRunSimulate">
        {{ $t('business.run-simulation') }}
      </NButton>
    </div>

    <template v-if="result">
      <NSpace align="center" :size="12" class="mb-12px flex-wrap">
        <NTag :type="routeStrategyTagType(result.decision.effective_strategy)" round>
          {{ $t('business.sim-effective-strategy') }}: {{ routeStrategyText(result.decision.effective_strategy) }}
        </NTag>
        <NTag v-if="result.decision.orchestrator" type="primary" round>
          {{
            $t('business.orchestrator-tier-badge', {
              tier: result.decision.orchestrator.tier,
              score: result.decision.orchestrator.score.toFixed(2)
            })
          }}
        </NTag>
        <NTag v-if="result.decision.orchestrator?.upgraded_from_tier" type="warning" round>
          {{ $t('business.orchestrator-upgraded-from', { from: result.decision.orchestrator.upgraded_from_tier }) }}
        </NTag>
        <NTag v-if="result.decision.orchestrator?.cache_protected" type="info" round>
          {{ $t('business.orchestrator-cache-protected') }}
        </NTag>
        <NTag v-if="result.decision.orchestrator?.auto_bucketed" size="small" round>
          {{ $t('business.orchestrator-auto-bucketed') }}
        </NTag>
        <NTag v-if="result.decision.matched_rule_name" type="primary" round>
          {{ $t('business.sim-matched-rule') }}: {{ result.decision.matched_rule_name }}
        </NTag>
        <NTag v-if="result.estimated_savings.savings_rate !== null && result.estimated_savings.savings_rate !== undefined" type="success" round>
          {{ $t('business.sim-estimated-savings') }}: {{ Math.round(result.estimated_savings.savings_rate * 1000) / 10 }}%
        </NTag>
      </NSpace>

      <NSpace v-if="result.decision.orchestrator?.signals?.length" align="center" :size="6" class="mb-8px flex-wrap">
        <NText depth="3" class="text-12px">{{ $t('business.orchestrator-signals') }}:</NText>
        <NTag v-for="sig in result.decision.orchestrator.signals" :key="sig" size="tiny" :bordered="false">
          {{ sig }}
        </NTag>
      </NSpace>

      <NAlert v-for="w in result.decision.warnings ?? []" :key="w" type="warning" :bordered="false" class="mb-8px">
        {{ warningText(w) }}
      </NAlert>

      <NDataTable :columns="columns" :data="result.decision.evaluations" size="small" :bordered="false" :scroll-x="1020" />

      <div v-if="result.simulated_failover?.length" class="mt-12px">
        <NText strong>{{ $t('business.sim-failover-trace') }}</NText>
        <NSpace class="mt-6px" :size="8">
          <NTag
            v-for="step in result.simulated_failover"
            :key="step.attempt"
            :type="step.outcome === 'served' ? 'success' : 'error'"
            size="small"
          >
            #{{ step.attempt }} {{ step.upstream_model }} ({{
              step.outcome === 'served' ? $t('business.sim-outcome-served') : $t('business.sim-outcome-failed')
            }})
          </NTag>
        </NSpace>
      </div>
    </template>
  </NModal>
</template>
