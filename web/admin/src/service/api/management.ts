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

/** Enable or disable an API key */
export function fetchUpdateApiKeyStatus(id: string, status: 'active' | 'disabled') {
  return request<{ status: string }>({ url: `/api-keys/${id}`, method: 'patch', data: { status } });
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
