import { $t } from '@/locales';

export function translateBackendError(reason?: string, params?: Record<string, unknown>, detail?: string) {
  const localeKey = `backendError.${reason || 'unknownError'}` as App.I18n.I18nKey;
  const named = { ...(params ?? {}) };
  if (detail) named.detail = detail;

  return Object.keys(named).length > 0 ? $t(localeKey, named) : $t(localeKey);
}
