import type { App } from 'vue';
import { createI18n } from 'vue-i18n';
import { localStg } from '@/utils/storage';
import messages from './locale';

const initialLocale: App.I18n.LangType = localStg.get('lang') || 'zh-CN';

function syncHtmlLang(locale: App.I18n.LangType) {
  document?.documentElement?.setAttribute('lang', locale);
}

syncHtmlLang(initialLocale);

const i18n = createI18n({
  locale: initialLocale,
  fallbackLocale: 'en-US',
  messages,
  legacy: false
});

/**
 * Setup plugin i18n
 *
 * @param app
 */
export function setupI18n(app: App) {
  syncHtmlLang(i18n.global.locale.value as App.I18n.LangType);
  app.use(i18n);
}

export const $t = i18n.global.t as App.I18n.$T;

export function setLocale(locale: App.I18n.LangType) {
  i18n.global.locale.value = locale;

  syncHtmlLang(locale);
}

export function getLocale(): App.I18n.LangType {
  return i18n.global.locale.value as App.I18n.LangType;
}
