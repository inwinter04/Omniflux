/** 管理台业务状态与动作的中文展示,集中维护避免各页面散落映射。 */

export function statusText(status: string) {
  switch (status) {
    case 'active':
      return '启用中';
    case 'published':
      return '已发布';
    case 'draft':
      return '草稿';
    case 'archived':
      return '已归档';
    case 'disabled':
      return '已停用';
    default:
      return status;
  }
}

export function statusTagType(status: string): 'success' | 'warning' | 'error' | 'info' | 'default' {
  switch (status) {
    case 'active':
      return 'success';
    case 'published':
      return 'success';
    case 'draft':
      return 'info';
    case 'archived':
      return 'default';
    case 'disabled':
      return 'default';
    default:
      return 'default';
  }
}

export function usageStateText(state: string) {
  switch (state) {
    case 'pending':
      return '进行中';
    case 'success':
      return '成功';
    case 'failed':
      return '失败';
    case 'interrupted':
      return '已中断';
    default:
      return state;
  }
}

export function usageStateTagType(state: string): 'success' | 'warning' | 'error' | 'info' | 'default' {
  switch (state) {
    case 'success':
      return 'success';
    case 'failed':
      return 'error';
    case 'interrupted':
      return 'warning';
    case 'pending':
      return 'info';
    default:
      return 'default';
  }
}

const actionTextMap: Record<string, string> = {
  'bootstrap.created': '初始化系统',
  'auth.login': '登录',
  'auth.login_failed': '登录失败',
  'project.created': '创建项目',
  'project.updated': '更新项目',
  'project.deleted': '删除项目',
  'application.created': '创建应用',
  'application.updated': '更新应用',
  'application.deleted': '删除应用',
  'provider_account.created': '创建 Provider 账号',
  'provider_account.updated': '更新 Provider 账号',
  'provider_account.deleted': '删除 Provider 账号',
  'route_policy.created': '创建路由策略',
  'route_policy.updated': '更新路由策略',
  'route_policy.deleted': '删除路由策略',
  'model_service.created': '创建模型服务',
  'model_service.updated': '更新模型服务',
  'model_service.deleted': '删除模型服务',
  'api_key.created': '创建 API Key',
  'api_key.status_changed': '切换 API Key 状态',
  'api_key.deleted': '删除 API Key',
  'asset.created': '创建资产',
  'asset.draft_created': '保存资产草稿',
  'asset.published': '发布资产',
  'asset.rolled_back': '回滚资产',
  'asset.status_changed': '变更资产状态',
  'asset_grant.upserted': '保存资产授权',
  'asset_grant.revoked': '撤销资产授权',
  'prompt_template.previewed': '预览 PromptTemplate',
  'prompt_template.invoked': '调用 PromptTemplate'
};

export function auditActionText(action: string) {
  return actionTextMap[action] ?? action;
}

const resourceTextMap: Record<string, string> = {
  project: '项目',
  application: '应用',
  provider_account: 'Provider 账号',
  route_policy: '路由策略',
  model_service: '模型服务',
  api_key: 'API Key',
  user: '用户',
  session: '会话',
  asset: '企业资产',
  asset_grant: '资产授权'
};

export function auditResourceText(resourceType: string) {
  return resourceTextMap[resourceType] ?? resourceType;
}

export function auditResultText(result: string) {
  return result === 'success' ? '成功' : '失败';
}

export function auditResultTagType(result: string): 'success' | 'error' | 'default' {
  return result === 'success' ? 'success' : 'error';
}

export function actorTypeText(actorType: string) {
  switch (actorType) {
    case 'user':
      return '用户';
    case 'api_key':
      return 'API Key';
    case 'system':
      return '系统';
    default:
      return actorType;
  }
}

export function formatTime(value: string | null) {
  return value ? new Date(value).toLocaleString() : '-';
}
