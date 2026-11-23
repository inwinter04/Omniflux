import { enUSBackendErrors, zhCNBackendErrors } from './backend-errors';
import enUS from './langs/en-us';
import zhCN from './langs/zh-cn';

const locales: Record<App.I18n.LangType, App.I18n.Schema> = {
  'zh-CN': { ...zhCN, backendError: zhCNBackendErrors },
  'en-US': { ...enUS, backendError: enUSBackendErrors }
};

export default locales;
