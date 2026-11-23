import { request } from '../request';

/** Get projects of the current organization */
export function fetchProjects() {
  return request<Api.Management.Project[]>({ url: '/projects' });
}

/** Get applications of the current organization */
export function fetchApplications() {
  return request<Api.Management.Application[]>({ url: '/applications' });
}

/** Get departments of the current organization */
export function fetchDepartments() {
  return request<Api.Management.Department[]>({ url: '/departments' });
}

/** Get organizations; platform admins see all, others see their own */
export function fetchOrganizations() {
  return request<Api.Management.Organization[]>({ url: '/organizations' });
}

/** Create an organization; requires platform admin */
export function fetchCreateOrganization(params: Api.Management.CreateOrganizationParams) {
  return request<{ id: string }>({ url: '/organizations', method: 'post', data: params });
}

/** Update organization name; slug is immutable */
export function fetchUpdateOrganization(id: string, params: Api.Management.UpdateOrganizationParams) {
  return request({ url: `/organizations/${id}`, method: 'patch', data: params });
}

/** Create a project under a department */
export function fetchCreateProject(params: Api.Management.CreateProjectParams) {
  return request<{ id: string }>({ url: '/projects', method: 'post', data: params });
}

/** Delete a project; blocked when related applications, API keys or usage records exist */
export function fetchDeleteProject(id: string) {
  return request({ url: `/projects/${id}`, method: 'delete' });
}

/** Create an application under a project */
export function fetchCreateApplication(params: Api.Management.CreateApplicationParams) {
  return request<{ id: string }>({ url: '/applications', method: 'post', data: params });
}

/** Delete an application; blocked when related API keys or usage records exist */
export function fetchDeleteApplication(id: string) {
  return request({ url: `/applications/${id}`, method: 'delete' });
}

/** Get provider accounts of the current organization (credentials excluded) */
export function fetchProviderAccounts() {
  return request<Api.Management.ProviderAccount[]>({ url: '/provider-accounts' });
}

/** Create a provider account; the API key is encrypted server-side */
export function fetchCreateProviderAccount(params: Api.Management.CreateProviderAccountParams) {
  return request<{ id: string }>({ url: '/provider-accounts', method: 'post', data: params });
}

/** Delete a provider account */
export function fetchProviderCatalog() {
  return request<Api.Management.ProviderCatalogGroup[]>({ url: '/provider-catalog' });
}

export function fetchProviderBalance(id: string) {
  return request<{ balances: Api.Management.ProviderBalanceEntry[] }>({ url: `/provider-accounts/${id}/balance` });
}

export function fetchDeleteProviderAccount(id: string) {
  return request({ url: `/provider-accounts/${id}`, method: 'delete' });
}

/** Get route policies of the current organization */
export function fetchRoutePolicies() {
  return request<Api.Management.RoutePolicy[]>({ url: '/route-policies' });
}

/** Create a route policy */
export function fetchCreateRoutePolicy(params: Api.Management.CreateRoutePolicyParams) {
  return request<{ id: string }>({ url: '/route-policies', method: 'post', data: params });
}

/** Delete a route policy; blocked when referenced by model services or has decision records */
export function fetchDeleteRoutePolicy(id: string) {
  return request({ url: `/route-policies/${id}`, method: 'delete' });
}

/** Get model services with candidates of the current organization */
export function fetchModelServices() {
  return request<Api.Management.ModelService[]>({ url: '/model-services' });
}

/** Create a model service with candidates */
export function fetchCreateModelService(params: Api.Management.CreateModelServiceParams) {
  return request<{ id: string }>({ url: '/model-services', method: 'post', data: params });
}

/** Delete a model service with its candidates; blocked when usage records exist */
export function fetchDeleteModelService(id: string) {
  return request({ url: `/model-services/${id}`, method: 'delete' });
}

/** Get API keys of the current organization (no secret material) */
export function fetchApiKeys() {
  return request<Api.Management.ApiKey[]>({ url: '/api-keys' });
}

/** Create an API key; the plaintext key is returned only once */
export function fetchCreateApiKey(params: Api.Management.CreateApiKeyParams) {
  return request<Api.Management.CreateApiKeyResult>({ url: '/api-keys', method: 'post', data: params });
}

/** Update API key status and/or per-key rate limit */
export function fetchUpdateApiKey(id: string, params: Api.Management.UpdateApiKeyParams) {
  return request<{ status: string }>({ url: `/api-keys/${id}`, method: 'patch', data: params });
}

/** Soft delete an API key; authentication stops immediately and it disappears from lists */
export function fetchDeleteApiKey(id: string) {
  return request({ url: `/api-keys/${id}`, method: 'delete' });
}

/** Get usage summary of recent days */
export function fetchUsageSummary(days: number) {
  return request<Api.Management.UsageSummary>({ url: `/usage/summary?days=${days}` });
}

/** Get paged usage records */
export function fetchUsageRecords(page: number, pageSize: number) {
  return request<Api.Management.Paged<Api.Management.UsageRecord>>({ url: '/usage/records', params: { page, page_size: pageSize } });
}

/** Get paged audit events */
export function fetchAuditEvents(page: number, pageSize: number) {
  return request<Api.Management.Paged<Api.Management.AuditEvent>>({ url: '/audit-events', params: { page, page_size: pageSize } });
}


/** Update project name */
export function fetchUpdateProject(id: string, params: Api.Management.UpdateProjectParams) {
  return request({ url: `/projects/${id}`, method: 'patch', data: params });
}

/** Update application name */
export function fetchUpdateApplication(id: string, params: Api.Management.UpdateApplicationParams) {
  return request({ url: `/applications/${id}`, method: 'patch', data: params });
}

/** Update provider account name/region/base URL */
export function fetchUpdateProviderAccount(id: string, params: Api.Management.UpdateProviderAccountParams) {
  return request({ url: `/provider-accounts/${id}`, method: 'patch', data: params });
}

/** Update route policy name and scheduling params */
export function fetchUpdateRoutePolicy(id: string, params: Api.Management.UpdateRoutePolicyParams) {
  return request({ url: `/route-policies/${id}`, method: 'patch', data: params });
}

/** Update model service and fully replace its candidates */
export function fetchUpdateModelService(id: string, params: Api.Management.UpdateModelServiceParams) {
  return request({ url: `/model-services/${id}`, method: 'patch', data: params });
}

/** Get budget windows of the current organization */
export function fetchBudgets() {
  return request<Api.Management.Budget[]>({ url: '/budgets' });
}

/** Create a budget window */
export function fetchCreateBudget(params: Api.Management.CreateBudgetParams) {
  return request<{ id: string }>({ url: '/budgets', method: 'post', data: params });
}

/** Update budget limit */
export function fetchUpdateBudget(id: string, limitAmount: string) {
  return request({ url: `/budgets/${id}`, method: 'patch', data: { limit_amount: limitAmount } });
}

/** Delete a budget window; blocked when settlement records exist */
export function fetchDeleteBudget(id: string) {
  return request({ url: `/budgets/${id}`, method: 'delete' });
}

/** Create a department; requires org admin */
export function fetchCreateDepartment(params: Api.Management.CreateDepartmentParams) {
  return request<{ id: string }>({ url: '/departments', method: 'post', data: params });
}

/** Update department name */
export function fetchUpdateDepartment(id: string, params: Api.Management.CreateDepartmentParams) {
  return request({ url: `/departments/${id}`, method: 'patch', data: params });
}

/** Delete a department; blocked when projects reference it */
export function fetchDeleteDepartment(id: string) {
  return request({ url: `/departments/${id}`, method: 'delete' });
}

/** Get users of the current organization; requires org owner. Platform admins may pass another organization id */
export function fetchUsers(organizationId?: string) {
  const query = organizationId ? `?organization_id=${organizationId}` : '';
  return request<Api.Management.User[]>({ url: `/users${query}` });
}

/** Create a user; requires org owner */
export function fetchCreateUser(params: Api.Management.CreateUserParams) {
  return request<{ id: string }>({ url: '/users', method: 'post', data: params });
}

/** Update user role/status or reset password; requires org owner */
export function fetchUpdateUser(id: string, params: Api.Management.UpdateUserParams) {
  return request({ url: `/users/${id}`, method: 'patch', data: params });
}

/** Test provider account connectivity with its stored credential */
export function fetchTestProviderAccount(id: string) {
  return request<Api.Management.ProviderTestResult>({ url: `/provider-accounts/${id}/test`, method: 'post' });
}

/** Get one audit event with before/after states */
export function fetchAuditEventDetail(id: string) {
  return request<Api.Management.AuditEventDetail>({ url: `/audit-events/${id}` });
}

/** Get home dashboard overview stats */
export function fetchDashboard() {
  return request<Api.Management.Dashboard>({ url: '/dashboard' });
}

/** List organization-scoped enterprise assets */
export function fetchAssets(type?: 'prompt_template' | 'agent') {
  return request<Api.Management.Asset[]>({ url: '/assets', params: type ? { type } : undefined });
}

export function fetchAssetDetail<S = Api.Management.PromptSpec>(id: string) {
  return request<Api.Management.AssetDetail<S>>({ url: `/assets/${id}` });
}

export function fetchCreateAsset<S = Api.Management.PromptSpec>(params: Api.Management.CreateAssetParams<S>) {
  return request<{ id: string; version_id: string; version: number }>({ url: '/assets', method: 'post', data: params });
}

export function fetchUpdateAsset<S = Api.Management.PromptSpec>(id: string, params: Api.Management.UpdateAssetParams<S>) {
  return request<{ version_id: string; version: number }>({ url: `/assets/${id}`, method: 'patch', data: params });
}

export function fetchPublishAsset(id: string, versionId: string) {
  return request({ url: `/assets/${id}/publish`, method: 'post', data: { version_id: versionId } });
}

export function fetchRollbackAsset(id: string, versionId: string, expectedVersion: number) {
  return request<{ version_id: string; version: number }>({
    url: `/assets/${id}/rollback`,
    method: 'post',
    data: { version_id: versionId, expected_version: expectedVersion }
  });
}

export function fetchUpdateAssetStatus(id: string, status: Api.Management.AssetStatus) {
  return request({ url: `/assets/${id}/status`, method: 'patch', data: { status } });
}

export function fetchAssetVersion(id: string, versionId: string) {
  return request<Api.Management.AssetVersion>({ url: `/assets/${id}/versions/${versionId}` });
}

export function fetchAssetDiff(id: string, from: string, to: string) {
  return request<{ from_version: number; to_version: number; diff: string }>({
    url: `/assets/${id}/versions/diff`,
    params: { from, to }
  });
}

export function fetchRenderAssetPreview(id: string, versionId: string, variables: Record<string, unknown>) {
  return request<Api.Management.PromptRenderResult>({
    url: `/assets/${id}/render`,
    method: 'post',
    data: { version_id: versionId, variables }
  });
}

export function fetchAssetGrants(id: string) {
  return request<Api.Management.AssetGrant[]>({ url: `/assets/${id}/grants` });
}

export function fetchUpsertAssetGrant(id: string, params: Api.Management.UpsertAssetGrantParams) {
  return request<{ id: string }>({ url: `/assets/${id}/grants`, method: 'post', data: params });
}

export function fetchRevokeAssetGrant(id: string, grantId: string) {
  return request({ url: `/assets/${id}/grants/${grantId}`, method: 'delete' });
}

export function fetchRunPrompt(id: string, params: Api.Management.RunPromptParams) {
  return request<Api.Management.RunPromptResult>({ url: `/assets/${id}/run`, method: 'post', data: params });
}

export function fetchPromptRunHistory(id: string) {
  return request<Api.Management.PromptRun[]>({ url: `/assets/${id}/runs` });
}

export function fetchPromptEvaluationSets(id: string) {
  return request<Api.Management.EvaluationSet[]>({ url: `/assets/${id}/evaluation-sets` });
}

export function fetchCreatePromptEvaluationSet(id: string, params: Api.Management.CreateEvaluationSetParams) {
  return request<{ id: string }>({ url: `/assets/${id}/evaluation-sets`, method: 'post', data: params });
}

export function fetchDeletePromptEvaluationSet(id: string, setId: string) {
  return request({ url: `/assets/${id}/evaluation-sets/${setId}`, method: 'delete' });
}

export function fetchEvaluatePrompt(id: string, params: { version_id: string; evaluation_set_id: string; model_service_ids?: string[] }) {
  return request<{ group_id: string; runs: Api.Management.PromptRun[]; summary: Api.Management.EvaluationSummary }>({
    url: `/assets/${id}/evaluate`, method: 'post', data: params
  });
}

interface StreamPromptCallbacks {
  onDelta: (delta: string) => void;
  onResult: (result: Api.Management.PromptRun) => void;
  onError: (result: Api.Management.PromptRun) => void;
}

export async function streamPromptRun(id: string, params: Api.Management.RunPromptParams, callbacks: StreamPromptCallbacks, signal?: AbortSignal) {
  const baseURL = import.meta.env.VITE_HTTP_PROXY === 'Y' ? '/proxy-default' : import.meta.env.VITE_SERVICE_BASE_URL;
  const response = await fetch(`${baseURL}/assets/${id}/run`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
    signal
  });
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new Error(body?.msg || '模型运行请求失败');
  }
  const reader = response.body?.getReader();
  if (!reader) throw new Error('当前浏览器不支持流式响应');
  const decoder = new TextDecoder();
  let buffer = '';
  const processBlock = (block: string) => {
    let event = '';
    let data = '';
    for (const line of block.split('\n')) {
      if (line.startsWith('event:')) event = line.slice(6).trim();
      if (line.startsWith('data:')) data += line.slice(5).trim();
    }
    if (!data) return;
    const payload = JSON.parse(data);
    if (event === 'delta') callbacks.onDelta(payload.delta);
    if (event === 'result') callbacks.onResult(payload);
    if (event === 'error') callbacks.onError(payload);
  };
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const blocks = buffer.split('\n\n');
    buffer = blocks.pop() ?? '';
    for (const block of blocks) processBlock(block);
  }
  buffer += decoder.decode();
  if (buffer.trim()) processBlock(buffer);
}

export function fetchAllPromptEvaluationSets() {
  return request<Api.Management.EvaluationSetOverview[]>({ url: '/evaluation-sets' });
}

export function fetchUpdatePromptEvaluationSet(assetId: string, setId: string, params: Api.Management.CreateEvaluationSetParams) {
  return request({ url: `/assets/${assetId}/evaluation-sets/${setId}`, method: 'patch', data: params });
}

export function fetchAgentSessions(id: string) {
  return request<Api.Management.AgentSession[]>({ url: `/assets/${id}/agent/sessions` });
}

export function fetchAgentSession(id: string, sessionId: string) {
  return request<{ session: Api.Management.AgentSession; messages: Api.Management.AgentMessage[] }>({
    url: `/assets/${id}/agent/sessions/${sessionId}`
  });
}

export function fetchDeleteAgentSession(id: string, sessionId: string) {
  return request({ url: `/assets/${id}/agent/sessions/${sessionId}`, method: 'delete' });
}

export function fetchAgentRuns(id: string) {
  return request<Api.Management.AgentRun[]>({ url: `/assets/${id}/agent/runs` });
}

interface StreamAgentCallbacks {
  onStarted: (value: { run_id: string; session_id: string; version: number }) => void;
  onDelta: (delta: string) => void;
  onResult: (result: Api.Management.AgentRun) => void;
  onError: (message: string) => void;
}

export async function streamAgentChat(id: string, params: Api.Management.AgentChatParams, callbacks: StreamAgentCallbacks, signal?: AbortSignal) {
  const baseURL = import.meta.env.VITE_HTTP_PROXY === 'Y' ? '/proxy-default' : import.meta.env.VITE_SERVICE_BASE_URL;
  const response = await fetch(`${baseURL}/assets/${id}/agent/chat`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
    signal
  });
  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new Error(body?.msg || 'Agent 运行请求失败');
  }
  const reader = response.body?.getReader();
  if (!reader) throw new Error('当前浏览器不支持流式响应');
  const decoder = new TextDecoder();
  let buffer = '';
  const processBlock = (block: string) => {
    let event = '';
    const dataLines: string[] = [];
    for (const line of block.split('\n')) {
      if (line.startsWith('event:')) event = line.slice(6).trim();
      if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart());
    }
    if (!dataLines.length) return;
    const payload = JSON.parse(dataLines.join('\n'));
    if (event === 'started') callbacks.onStarted(payload);
    if (event === 'delta') callbacks.onDelta(payload.delta);
    if (event === 'result') callbacks.onResult(payload);
    if (event === 'error') callbacks.onError(payload.error || 'Agent 运行失败');
  };
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const blocks = buffer.split('\n\n');
    buffer = blocks.pop() ?? '';
    for (const block of blocks) processBlock(block);
  }
  buffer += decoder.decode();
  if (buffer.trim()) processBlock(buffer);
}

export function fetchProviderModels(id: string) {
  return request<{ models: Api.Management.ProviderModel[]; pricing_source: string }>({
    url: `/provider-accounts/${id}/models`
  });
}

export function fetchUpdateProviderAccountStatus(id: string, status: 'active' | 'disabled') {
  return request({ url: `/provider-accounts/${id}/status`, method: 'patch', data: { status } });
}

export function fetchUpdateModelServiceStatus(id: string, status: 'active' | 'disabled') {
  return request({ url: `/model-services/${id}/status`, method: 'patch', data: { status } });
}

export function fetchUpdateModelCandidateStatus(id: string, status: 'active' | 'disabled') {
  return request({ url: `/model-candidates/${id}/status`, method: 'patch', data: { status } });
}

export function fetchUpdateProjectStatus(id: string, status: 'active' | 'disabled') {
  return request({ url: `/projects/${id}/status`, method: 'patch', data: { status } });
}

export function fetchUpdateApplicationStatus(id: string, status: 'active' | 'disabled') {
  return request({ url: `/applications/${id}/status`, method: 'patch', data: { status } });
}

export function fetchUpdateRoutePolicyStatus(id: string, status: 'active' | 'disabled') {
  return request({ url: `/route-policies/${id}/status`, method: 'patch', data: { status } });
}

export function fetchModelServiceGrants(id: string) {
  return request<Api.Management.ModelServiceGrant[]>({ url: `/model-services/${id}/grants` });
}

export function fetchUpsertModelServiceGrant(id: string, params: Api.Management.UpsertAssetGrantParams) {
  return request<{ id: string }>({ url: `/model-services/${id}/grants`, method: 'post', data: params });
}

export function fetchRevokeModelServiceGrant(id: string, grantId: string) {
  return request({ url: `/model-services/${id}/grants/${grantId}`, method: 'delete' });
}
