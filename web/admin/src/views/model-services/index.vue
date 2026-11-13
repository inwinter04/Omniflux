<script setup lang="ts">
import { h, onMounted, reactive, ref } from 'vue';
import { NButton, NDataTable, NPopconfirm, NTag } from 'naive-ui';
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import { fetchCreateModelService, fetchDeleteModelService, fetchModelServices, fetchProviderAccounts, fetchRoutePolicies } from '@/service/api';

defineOptions({
  name: 'ModelServices'
});

interface CandidateForm {
  provider_account_id: string | null;
  upstream_model: string;
  priority: number;
  weight: number;
  input_cost_per_million: number;
  output_cost_per_million: number;
}

const loading = ref(true);
const list = ref<Api.Management.ModelService[]>([]);
const accountOptions = ref<{ label: string; value: string }[]>([]);
const policyOptions = ref<{ label: string; value: string }[]>([]);

const showCreate = ref(false);
const creating = ref(false);
const formRef = ref<FormInst | null>(null);
const formModel = reactive({
  name: '',
  route_policy_id: null as string | null,
  candidates: [] as CandidateForm[]
});

const formRules: FormRules = {
  name: { required: true, message: '请填写模型服务名称', trigger: 'blur' }
};

function formatTime(value: string) {
  return new Date(value).toLocaleString();
}

function accountLabel(accountId: string | null) {
  if (!accountId) return '-';
  return accountOptions.value.find(item => item.value === accountId)?.label ?? '-';
}

const candidateColumns: DataTableColumns<Api.Management.ModelCandidate> = [
  { title: '上游模型', key: 'upstream_model', minWidth: 160 },
  { title: 'Provider 账号', key: 'provider_account_id', minWidth: 200, render: row => accountLabel(row.provider_account_id) },
  { title: '优先级', key: 'priority', width: 90 },
  { title: '权重', key: 'weight', width: 80 },
  { title: '输入成本(/M)', key: 'input_cost_per_million', width: 120 },
  { title: '输出成本(/M)', key: 'output_cost_per_million', width: 120 },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render(row) {
      return h(NTag, { type: row.status === 'active' ? 'success' : 'default', size: 'small', round: true }, { default: () => row.status });
    }
  }
];

const columns: DataTableColumns<Api.Management.ModelService> = [
  {
    type: 'expand',
    renderExpand(row) {
      return h(NDataTable, {
        columns: candidateColumns,
        data: row.candidates,
        size: 'small',
        bordered: false
      });
    }
  },
  { title: '名称', key: 'name', width: 180 },
  {
    title: '路由策略',
    key: 'route_policy_id',
    width: 160,
    render(row) {
      if (!row.route_policy_id) return '-';
      return policyOptions.value.find(item => item.value === row.route_policy_id)?.label ?? '-';
    }
  },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render(row) {
      return h(NTag, { type: row.status === 'active' ? 'success' : 'default', size: 'small', round: true }, { default: () => row.status });
    }
  },
  { title: '候选数', key: 'candidates', width: 90, render: row => String(row.candidates.length) },
  { title: '创建时间', key: 'created_at', width: 180, render: row => formatTime(row.created_at) },
  {
    title: '操作',
    key: 'actions',
    width: 100,
    render(row) {
      return h(
        NPopconfirm,
        { onPositiveClick: () => handleDelete(row.id) },
        {
          trigger: () => h(NButton, { size: 'small', type: 'error', quaternary: true }, { default: () => '删除' }),
          default: () => '存在用量记录时将无法删除,候选会一并删除,确认删除?'
        }
      );
    }
  }
];

async function loadData() {
  loading.value = true;
  try {
    const [servicesRes, accountsRes, policiesRes] = await Promise.all([fetchModelServices(), fetchProviderAccounts(), fetchRoutePolicies()]);
    if (!servicesRes.error && servicesRes.data) {
      list.value = servicesRes.data;
    }
    if (!accountsRes.error && accountsRes.data) {
      accountOptions.value = accountsRes.data.map(item => ({ label: `${item.name} (${item.provider})`, value: item.id }));
    }
    if (!policiesRes.error && policiesRes.data) {
      policyOptions.value = policiesRes.data.map(item => ({ label: item.name, value: item.id }));
    }
  } finally {
    loading.value = false;
  }
}

function addCandidate() {
  formModel.candidates.push({
    provider_account_id: accountOptions.value[0]?.value ?? null,
    upstream_model: '',
    priority: 100,
    weight: 1,
    input_cost_per_million: 0,
    output_cost_per_million: 0
  });
}

function removeCandidate(index: number) {
  formModel.candidates.splice(index, 1);
}

function openCreate() {
  formModel.name = '';
  formModel.route_policy_id = null;
  formModel.candidates = [];
  addCandidate();
  showCreate.value = true;
}

async function handleCreate() {
  await formRef.value?.validate();
  if (formModel.candidates.length === 0) {
    window.$message?.error('至少需要一个模型候选');
    return;
  }
  if (formModel.candidates.some(item => !item.provider_account_id || !item.upstream_model.trim())) {
    window.$message?.error('候选必须选择 Provider 账号并填写上游模型');
    return;
  }
  creating.value = true;
  try {
    const { error } = await fetchCreateModelService({
      name: formModel.name.trim(),
      route_policy_id: formModel.route_policy_id,
      candidates: formModel.candidates.map(item => ({
        provider_account_id: item.provider_account_id!,
        upstream_model: item.upstream_model.trim(),
        priority: item.priority,
        weight: item.weight,
        input_cost_per_million: item.input_cost_per_million,
        output_cost_per_million: item.output_cost_per_million
      }))
    });
    if (!error) {
      window.$message?.success('模型服务已创建');
      showCreate.value = false;
      await loadData();
    }
  } finally {
    creating.value = false;
  }
}

async function handleDelete(id: string) {
  const { error } = await fetchDeleteModelService(id);
  if (!error) {
    window.$message?.success('模型服务已删除');
    await loadData();
  }
}

onMounted(() => {
  void loadData();
});
</script>

<template>
  <div class="min-h-full bg-#f6f7f9 p-4px dark:bg-#101014">
    <NCard title="模型服务" :bordered="false" class="card-wrapper">
      <template #header-extra>
        <NSpace>
          <NButton quaternary type="primary" :loading="loading" @click="loadData">刷新</NButton>
          <NButton type="primary" :disabled="accountOptions.length === 0" @click="openCreate">
            {{ accountOptions.length === 0 ? '请先创建 Provider 账号' : '新建模型服务' }}
          </NButton>
        </NSpace>
      </template>
      <NDataTable :columns="columns" :data="list" :loading="loading" :bordered="false" :scroll-x="900" />
    </NCard>

    <NModal v-model:show="showCreate" preset="card" title="新建模型服务" class="w-760px">
      <NForm ref="formRef" :model="formModel" :rules="formRules" label-placement="left" label-width="100">
        <NFormItem label="服务名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="业务调用的逻辑模型名" />
        </NFormItem>
        <NFormItem label="路由策略" path="route_policy_id">
          <NSelect v-model:value="formModel.route_policy_id" :options="policyOptions" clearable placeholder="可选,不选则使用默认超时配置" />
        </NFormItem>

        <NDivider title-placement="left">模型候选</NDivider>
        <div v-for="(item, index) in formModel.candidates" :key="index" class="mb-12px rounded-4px bg-#f8f8fa p-12px dark:bg-#ffffff0d">
          <NGrid responsive="screen" item-responsive :x-gap="12" :y-gap="8">
            <NGi span="24 m:12">
              <NFormItem label="Provider 账号" label-placement="top">
                <NSelect v-model:value="item.provider_account_id" :options="accountOptions" placeholder="选择账号" />
              </NFormItem>
            </NGi>
            <NGi span="24 m:12">
              <NFormItem label="上游模型" label-placement="top">
                <NInput v-model:value="item.upstream_model" placeholder="例如:gpt-4o-mini" />
              </NFormItem>
            </NGi>
            <NGi span="12 m:6">
              <NFormItem label="优先级" label-placement="top">
                <NInputNumber v-model:value="item.priority" :min="1" class="w-full" />
              </NFormItem>
            </NGi>
            <NGi span="12 m:6">
              <NFormItem label="权重" label-placement="top">
                <NInputNumber v-model:value="item.weight" :min="0" class="w-full" />
              </NFormItem>
            </NGi>
            <NGi span="12 m:6">
              <NFormItem label="输入成本(/M)" label-placement="top">
                <NInputNumber v-model:value="item.input_cost_per_million" :min="0" class="w-full" />
              </NFormItem>
            </NGi>
            <NGi span="12 m:6">
              <NFormItem label="输出成本(/M)" label-placement="top">
                <NInputNumber v-model:value="item.output_cost_per_million" :min="0" class="w-full" />
              </NFormItem>
            </NGi>
          </NGrid>
          <div class="flex justify-end">
            <NButton size="small" type="error" quaternary :disabled="formModel.candidates.length <= 1" @click="removeCandidate(index)">
              删除候选
            </NButton>
          </div>
        </div>
        <NButton size="small" dashed block @click="addCandidate">+ 添加候选</NButton>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="showCreate = false">取消</NButton>
          <NButton type="primary" :loading="creating" @click="handleCreate">创建</NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped></style>


