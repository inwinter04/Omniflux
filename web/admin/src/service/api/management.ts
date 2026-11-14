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
