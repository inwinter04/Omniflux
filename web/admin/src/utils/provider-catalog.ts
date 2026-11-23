import { $t } from '@/locales';

const providerGroupLabelKeys: Record<string, App.I18n.I18nKey> = {
  cn: 'business.providerGroupCN',
  global: 'business.providerGroupGlobal',
  aggregator: 'business.providerGroupAggregator',
  third_party: 'business.providerGroupThirdParty',
  local: 'business.providerGroupLocal',
  custom: 'business.providerGroupCustom'
};

const providerFamilyLabelKeys: Record<string, App.I18n.I18nKey> = {
  kimi: 'business.providerFamilyKimi',
  zhipu: 'business.providerFamilyZhipu',
  volcengine: 'business.providerFamilyVolcengine',
  qwencloud: 'business.providerFamilyQwenCloud',
  qwenai: 'business.providerFamilyQwenAI',
  stepfun: 'business.providerFamilyStepFun',
  streamlake: 'business.providerFamilyStreamLake',
  minimax: 'business.providerFamilyMiniMax',
  siliconflow: 'business.providerFamilySiliconFlow',
  'baidu-qianfan': 'business.providerFamilyBaiduQianfan',
  'xiaomi-mimo': 'business.providerFamilyXiaomiMiMo',
  compshare: 'business.providerFamilyCompshare',
  moark: 'business.providerFamilyMoArk',
  openai_compatible: 'business.providerFamilyCustomOpenAICompatible'
};

const providerVersionLabelKeys: Record<string, App.I18n.I18nKey> = {
  standard: 'business.providerVersionStandardAPI',
  coding: 'business.providerVersionCoding',
  agent: 'business.providerVersionAgent',
  token: 'business.providerVersionToken',
  subscription: 'business.providerVersionSubscription',
  custom: 'business.providerVersionCustom'
};

const providerRegionLabelKeys: Record<string, App.I18n.I18nKey> = {
  china: 'business.providerRegionChina',
  global: 'business.providerRegionGlobal'
};

export function providerGroupLabel(group: Api.Management.ProviderCatalogGroup) {
  const key = providerGroupLabelKeys[group.group];
  return key ? $t(key) : group.group;
}

export function providerFamilyLabel(vendor: Api.Management.ProviderCatalogEntry) {
  const key = providerFamilyLabelKeys[vendor.family_id || vendor.id];
  return key ? $t(key) : vendor.name;
}

export function providerVersionLabel(vendor: Api.Management.ProviderCatalogEntry | null) {
  if (!vendor) return $t('business.providerVersionCustom');
  if (vendor.version_kind === 'standard' && !vendor.region_kind) {
    return $t('business.providerVersionStandardService');
  }
  const versionKey = providerVersionLabelKeys[vendor.version_kind];
  const regionKey = providerRegionLabelKeys[vendor.region_kind];
  if (!versionKey) return vendor.name;
  if (!regionKey) return $t(versionKey);
  return `${$t(versionKey)} · ${$t(regionKey)}`;
}
