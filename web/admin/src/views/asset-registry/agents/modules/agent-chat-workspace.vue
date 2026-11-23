<script setup lang="ts">
import { computed, h, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { NButton, NPopconfirm, NScrollbar, NSpace, NTag } from 'naive-ui';
import type { DataTableColumns } from 'naive-ui';
import { formatTime } from '@/utils/admin-status';
import { formatCurrency } from '@/utils/currency';
import {
  fetchAssetDetail,
  fetchAssetVersion,
  fetchAgentRuns,
  fetchAgentSession,
  fetchAgentSessions,
  fetchDeleteAgentSession,
  streamAgentChat
} from '@/service/api';
import { $t } from '@/locales';
import MarkdownPreview from '../../prompt-templates/modules/markdown-preview.vue';

const props = defineProps<{
  asset: Api.Management.Asset;
}>();

const emit = defineEmits<{ back: [] }>();

type ChatMessage = Api.Management.AgentMessage & {
  pending?: boolean;
  error?: string;
};

const loading = ref(true);
const sessionLoading = ref(false);
const running = ref(false);
const activeTab = ref('chat');
const sessions = ref<Api.Management.AgentSession[]>([]);
const sessionID = ref<string | null>(null);
const currentSession = ref<Api.Management.AgentSession | null>(null);
const messages = ref<ChatMessage[]>([]);
const runs = ref<Api.Management.AgentRun[]>([]);
const promptSpec = ref<Api.Management.PromptSpec | null>(null);
const variables = ref<Record<string, unknown>>({});
const variableError = ref('');
const input = ref('');
const messageScroller = ref<InstanceType<typeof NScrollbar> | null>(null);
let abortController: AbortController | null = null;

const runMap = computed(() => new Map(runs.value.map(item => [item.id, item])));
const hasVariableForm = computed(() => !sessionID.value && Boolean(promptSpec.value?.variables.length));
const runColumns = computed<DataTableColumns<Api.Management.AgentRun>>(() => [
  {
    title: $t('business.time'),
    key: 'created_at',
    width: 170,
    render: row => formatTime(row.created_at)
  },
  {
    title: $t('business.source-version'),
    key: 'version',
    width: 100,
    render: row => $t('business.versionValue', { version: row.version })
  },
  {
    title: $t('business.status'),
    key: 'status',
    width: 90,
    render: row =>
      h(
        NTag,
        {
          size: 'small',
          type: row.status === 'success' ? 'success' : row.status === 'failed' ? 'error' : 'warning'
        },
        { default: () => statusText(row.status) }
      )
  },
  {
    title: $t('business.input'),
    key: 'request',
    minWidth: 180,
    ellipsis: { tooltip: true }
  },
  {
    title: $t('business.output'),
    key: 'response',
    minWidth: 220,
    ellipsis: { tooltip: true }
  },
  {
    title: $t('business.tokens'),
    key: 'tokens',
    width: 110,
    render: row => `${row.input_tokens} / ${row.output_tokens}`
  },
  {
    title: $t('business.cost'),
    key: 'cost',
    width: 110,
    render: row => formatCurrency(row.cost, row.cost_currency)
  },
  {
    title: $t('business.latency'),
    key: 'total_ms',
    width: 100,
    render: row => `${row.total_ms} ms`
  }
]);

onMounted(initialize);
onBeforeUnmount(() => abortController?.abort());
watch(
  variables,
  () => {
    variableError.value = '';
  },
  { deep: true }
);

async function initialize() {
  loading.value = true;
  try {
    await Promise.all([loadSessions(), loadRuns(), loadPrompt()]);
    if (sessions.value.length) await selectSession(sessions.value[0].id);
  } finally {
    loading.value = false;
  }
}

async function loadPrompt() {
  const { data, error } = await fetchAssetDetail<Api.Management.AgentSpec>(props.asset.id);
  if (error || !data) return;
  const versionRes = await fetchAssetVersion(data.spec.prompt_template_id, data.spec.prompt_version_id);
  if (versionRes.error || !versionRes.data?.spec) return;
  promptSpec.value = versionRes.data.spec;
  const defaults: Record<string, unknown> = {};
  for (const variable of promptSpec.value.variables) {
    if (variable.default !== undefined) defaults[variable.name] = variable.default;
  }
  variables.value = defaults;
}

async function loadSessions() {
  const { data, error } = await fetchAgentSessions(props.asset.id);
  if (!error && data) sessions.value = data;
}

async function loadRuns() {
  const { data, error } = await fetchAgentRuns(props.asset.id);
  if (!error && data) runs.value = data;
}

async function selectSession(id: string, force = false) {
  if (running.value && !force) return;
  sessionLoading.value = true;
  try {
    const { data, error } = await fetchAgentSession(props.asset.id, id);
    if (error || !data) return;
    sessionID.value = id;
    currentSession.value = data.session;
    messages.value = data.messages;
    await scrollToLatest();
  } finally {
    sessionLoading.value = false;
  }
}

function newSession() {
  if (running.value) return;
  sessionID.value = null;
  currentSession.value = null;
  messages.value = [];
  input.value = '';
  variableError.value = '';
  activeTab.value = 'chat';
  const defaults: Record<string, unknown> = {};
  for (const variable of promptSpec.value?.variables ?? []) {
    if (variable.default !== undefined) defaults[variable.name] = variable.default;
  }
  variables.value = defaults;
}

async function deleteSession(id: string) {
  const { error } = await fetchDeleteAgentSession(props.asset.id, id);
  if (error) return;
  window.$message?.success($t('business.session-deleted'));
  if (sessionID.value === id) newSession();
  await loadSessions();
}

function validateVariables() {
  for (const variable of promptSpec.value?.variables ?? []) {
    const value = variables.value[variable.name];
    if (variable.required && (value === undefined || value === null || value === '')) {
      variableError.value = $t('business.variableRequired', {
        name: variable.name
      });
      return false;
    }
  }
  variableError.value = '';
  return true;
}

async function send() {
  const text = input.value.trim();
  if (!text || running.value) return;
  if (!sessionID.value && !validateVariables()) return;
  activeTab.value = 'chat';
  input.value = '';
  const temporaryRunID = `pending-${Date.now()}`;
  messages.value.push(
    {
      id: `user-${Date.now()}`,
      run_id: temporaryRunID,
      role: 'user',
      content: text,
      created_at: new Date().toISOString()
    },
    {
      id: `assistant-${Date.now()}`,
      run_id: temporaryRunID,
      role: 'assistant',
      content: '',
      created_at: new Date().toISOString(),
      pending: true
    }
  );
  await scrollToLatest();
  running.value = true;
  abortController = new AbortController();
  let completedSessionID = sessionID.value;
  try {
    await streamAgentChat(
      props.asset.id,
      {
        session_id: sessionID.value ?? undefined,
        variables: sessionID.value ? undefined : variables.value,
        message: text,
        stream: true
      },
      {
        onStarted: value => {
          completedSessionID = value.session_id;
          sessionID.value = value.session_id;
          messages.value[messages.value.length - 1].run_id = value.run_id;
        },
        onDelta: delta => {
          const assistant = messages.value[messages.value.length - 1];
          assistant.content += delta;
          void scrollToLatest();
        },
        onResult: result => {
          const assistant = messages.value[messages.value.length - 1];
          assistant.pending = false;
          assistant.error = result.error;
          runs.value = [result, ...runs.value.filter(item => item.id !== result.id)];
        },
        onError: message => {
          const assistant = messages.value[messages.value.length - 1];
          assistant.pending = false;
          assistant.error = message;
          window.$message?.error(message);
        }
      },
      abortController.signal
    );
    await Promise.all([loadSessions(), loadRuns()]);
    if (completedSessionID) await selectSession(completedSessionID, true);
  } catch (error) {
    const assistant = messages.value[messages.value.length - 1];
    assistant.pending = false;
    if (error instanceof DOMException && error.name === 'AbortError') {
      assistant.error = $t('business.generation-stopped');
      if (completedSessionID) await selectSession(completedSessionID, true);
    } else {
      assistant.error = error instanceof Error ? error.message : $t('business.agent-run-failed');
      window.$message?.error(assistant.error);
    }
  } finally {
    running.value = false;
    abortController = null;
  }
}

function stop() {
  abortController?.abort();
}

async function scrollToLatest() {
  await nextTick();
  messageScroller.value?.scrollTo({
    top: Number.MAX_SAFE_INTEGER,
    behavior: 'smooth'
  });
}

function statusText(status: string) {
  return (
    {
      success: $t('business.success'),
      failed: $t('business.failed'),
      interrupted: $t('business.interrupted'),
      running: $t('business.running')
    }[status] ?? status
  );
}

function numberValue(value: unknown) {
  return typeof value === 'number' ? value : null;
}

function formatChatTime(value: string) {
  const date = new Date(value);
  const now = new Date();
  const sameDay =
    date.getFullYear() === now.getFullYear() && date.getMonth() === now.getMonth() && date.getDate() === now.getDate();
  return new Intl.DateTimeFormat(
    'zh-CN',
    sameDay
      ? { hour: '2-digit', minute: '2-digit', hour12: false }
      : {
          month: '2-digit',
          day: '2-digit',
          hour: '2-digit',
          minute: '2-digit',
          hour12: false
        }
  ).format(date);
}
</script>

<template>
  <NCard :bordered="false" class="card-wrapper">
    <template #header>
      <NSpace align="center">
        <NAvatar round color="rgb(var(--primary-color))"><SvgIcon icon="mdi:robot-outline" /></NAvatar>
        <NSpace vertical :size="0">
          <NText strong>{{ props.asset.name }}</NText>
          <NText depth="3" class="text-12px">{{ $t('business.chat-workspace') }}</NText>
        </NSpace>
      </NSpace>
    </template>
    <template #header-extra>
      <NSpace>
        <NButton @click="emit('back')">{{ $t('business.back-to-list') }}</NButton>
        <NButton type="primary" :disabled="running" @click="newSession">
          <template #icon><SvgIcon icon="mdi:plus" /></template>
          {{ $t('business.new-chat') }}
        </NButton>
      </NSpace>
    </template>
    <NSpin :show="loading">
      <NGrid cols="1 900:4" :x-gap="16">
        <NGi>
          <NCard size="small" :title="$t('business.session-history')">
            <template #header-extra>
              <NTag size="small" :bordered="false">{{ sessions.length }}</NTag>
            </template>
            <NScrollbar class="h-[calc(100vh-270px)] min-h-360px">
              <div v-if="sessions.length">
                <NCard
                  v-for="item in sessions"
                  :key="item.id"
                  size="small"
                  :embedded="sessionID !== item.id"
                  :class="['mb-8px cursor-pointer', sessionID === item.id ? 'border-primary' : '']"
                  @click="selectSession(item.id)"
                >
                  <div class="flex items-center justify-between gap-8px">
                    <div class="min-w-0 flex-1">
                      <NEllipsis class="block w-full" :class="sessionID === item.id ? 'text-primary' : ''">
                        {{ item.title }}
                      </NEllipsis>
                      <NText depth="3" class="mt-4px block text-12px">
                        {{ $t('business.version') }} {{ item.version }} ·
                        {{ formatChatTime(item.updated_at) }}
                      </NText>
                    </div>
                    <NPopconfirm @positive-click="deleteSession(item.id)">
                      <template #trigger>
                        <NButton size="tiny" quaternary type="error" class="shrink-0" :disabled="running" @click.stop>
                          {{ $t('business.delete') }}
                        </NButton>
                      </template>
                      {{ $t('business.delete-this-session-and-its') }}
                    </NPopconfirm>
                  </div>
                </NCard>
              </div>
              <NEmpty v-else :description="$t('business.no-session-history')" class="mt-80px" />
            </NScrollbar>
          </NCard>
        </NGi>
        <NGi span="3">
          <NCard size="small">
            <NTabs v-model:value="activeTab" type="line">
              <NTabPane name="chat" :tab="$t('business.chat')">
                <NCard
                  v-if="hasVariableForm"
                  size="small"
                  :title="$t('business.session-variables')"
                  class="mb-12px"
                  embedded
                >
                  <NForm label-placement="top" size="small">
                    <NScrollbar class="max-h-180px">
                      <NGrid cols="1 640:2" :x-gap="12">
                        <NFormItemGi
                          v-for="variable in promptSpec?.variables"
                          :key="variable.name"
                          :label="variable.name"
                          :required="variable.required"
                        >
                          <NInputNumber
                            v-if="variable.type === 'number' || variable.type === 'integer'"
                            :value="numberValue(variables[variable.name])"
                            :precision="variable.type === 'integer' ? 0 : undefined"
                            class="w-full"
                            @update:value="value => (variables[variable.name] = value)"
                          />
                          <NSwitch
                            v-else-if="variable.type === 'boolean'"
                            :value="Boolean(variables[variable.name])"
                            @update:value="value => (variables[variable.name] = value)"
                          />
                          <NInput
                            v-else
                            :value="String(variables[variable.name] ?? '')"
                            :placeholder="
                              variable.description ||
                              $t('business.enterVariable', {
                                name: variable.name
                              })
                            "
                            @update:value="value => (variables[variable.name] = value)"
                          />
                        </NFormItemGi>
                      </NGrid>
                    </NScrollbar>
                  </NForm>
                  <NAlert v-if="variableError" type="warning" :bordered="false" class="mt-8px">
                    {{ variableError }}
                  </NAlert>
                </NCard>
                <NSpace v-if="currentSession" align="center" class="mb-12px">
                  <NTag size="small" type="info" :bordered="false">
                    {{ $t('business.version') }} {{ currentSession.version }}
                  </NTag>
                  <NText depth="2" class="text-13px">{{ $t('business.this-session-always-uses-this') }}</NText>
                </NSpace>
                <NScrollbar
                  ref="messageScroller"
                  :class="hasVariableForm ? 'h-[calc(100vh-600px)] min-h-160px' : 'h-[calc(100vh-480px)] min-h-220px'"
                >
                  <NSpin :show="sessionLoading">
                    <div v-if="messages.length" class="py-4px">
                      <div
                        v-for="message in messages"
                        :key="message.id"
                        :class="[
                          'mb-20px flex items-start gap-12px',
                          message.role === 'user' ? 'flex-row-reverse' : ''
                        ]"
                      >
                        <NAvatar round size="small">
                          <SvgIcon :icon="message.role === 'user' ? 'mdi:account-outline' : 'mdi:robot-outline'" />
                        </NAvatar>
                        <div class="max-w-85% min-w-0">
                          <NSpace align="center" :justify="message.role === 'user' ? 'end' : 'start'" class="mb-6px">
                            <NText strong :type="message.role === 'user' ? 'primary' : 'default'">
                              {{ message.role === 'user' ? $t('business.me') : props.asset.name }}
                            </NText>
                            <NText depth="3" class="text-12px">{{ formatChatTime(message.created_at) }}</NText>
                          </NSpace>
                          <NCard size="small" :embedded="message.role === 'user'" :bordered="message.role === 'user'">
                            <MarkdownPreview v-if="message.content" compact :source="message.content" />
                            <NSpace v-if="message.pending && !message.content" align="center">
                              <NSpin size="small" />
                              <NText depth="2">{{ $t('business.generating-response') }}</NText>
                            </NSpace>
                            <NAlert v-if="message.error" type="error" :bordered="false" class="mt-8px">
                              {{ message.error }}
                            </NAlert>
                          </NCard>
                          <NText
                            v-if="message.role === 'assistant' && message.run_id && runMap.get(message.run_id)"
                            depth="3"
                            class="mt-6px block text-12px"
                          >
                            {{ $t('business.input') }}
                            {{ runMap.get(message.run_id)?.input_tokens }}
                            {{ $t('business.output-3ydow') }}
                            {{ runMap.get(message.run_id)?.output_tokens }} ·
                            {{ runMap.get(message.run_id)?.total_ms }} ms
                          </NText>
                        </div>
                      </div>
                    </div>
                    <NEmpty v-else :description="$t('business.fill-in-variables-and-send')" class="my-80px">
                      <template #icon><SvgIcon icon="mdi:chat-outline" class="text-48px" /></template>
                    </NEmpty>
                  </NSpin>
                </NScrollbar>
                <NCard size="small" embedded class="sticky bottom-0 z-10 mt-12px">
                  <NInput
                    v-model:value="input"
                    type="textarea"
                    :autosize="{ minRows: 2, maxRows: 6 }"
                    :disabled="running"
                    :placeholder="$t('business.enter-a-message-and-press')"
                    @keydown.ctrl.enter="send"
                  />
                  <NSpace justify="space-between" align="center" class="mt-8px">
                    <NText depth="3" class="text-12px">{{ $t('business.markdown-is-supported') }}</NText>
                    <NSpace>
                      <NButton v-if="running" type="error" secondary @click="stop">
                        <template #icon><SvgIcon icon="mdi:stop" /></template>
                        {{ $t('business.stop-generating') }}
                      </NButton>
                      <NButton type="primary" :loading="running" :disabled="!input.trim()" @click="send">
                        <template #icon><SvgIcon icon="mdi:send" /></template>
                        {{ $t('business.send') }}
                      </NButton>
                    </NSpace>
                  </NSpace>
                </NCard>
              </NTabPane>
              <NTabPane name="runs" :tab="$t('business.run-history')">
                <NDataTable
                  :columns="runColumns"
                  :data="runs"
                  size="small"
                  :bordered="false"
                  :scroll-x="1050"
                  :max-height="540"
                  :pagination="{ pageSize: 10 }"
                />
              </NTabPane>
            </NTabs>
          </NCard>
        </NGi>
      </NGrid>
    </NSpin>
  </NCard>
</template>
