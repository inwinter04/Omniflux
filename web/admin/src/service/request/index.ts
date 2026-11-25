import type { AxiosResponse } from 'axios';
import { BACKEND_ERROR_CODE, createFlatRequest, createRequest } from '@sa/axios';
import { getServiceBaseURL } from '@/utils/service';
import { localStg } from '@/utils/storage';
import { $t } from '@/locales';
import { useAuthStore } from '@/store/modules/auth';
import { clearAuthStorage, getToken } from '@/store/modules/auth/shared';
import { getAuthorization, handleExpiredRequest, showErrorMsg } from './shared';
import { translateBackendError } from './backend-error';
import type { RequestInstanceState } from './type';

const isHttpProxy = import.meta.env.DEV && import.meta.env.VITE_HTTP_PROXY === 'Y';
const { baseURL, otherBaseURL } = getServiceBaseURL(import.meta.env, isHttpProxy);

/** 环境变量里的错误码以逗号分隔;未配置时要得到空数组,而不是只含空字符串的数组 */
function getErrorCodes(codes?: string) {
  return (codes || '').split(',').filter(Boolean);
}

/**
 * Handle the invalid session
 *
 * 会话已失效(登录过期,或在别处退出了登录):清掉本地登录状态并回到登录页,重新登录后会回到当前页面。
 */
async function handleSessionExpired(requestGeneration?: number) {
  const authStore = useAuthStore();
  if (requestGeneration !== undefined && !authStore.isSessionCurrent(requestGeneration)) return;

  // 只处理“本地仍是登录态”的情况,同一次失效里的其它请求不再重复提示
  if (!getToken()) {
    return;
  }

  // 先移除本地令牌，避免 resetStore 里的退出登录再次收到 401 并递归触发本函数。
  clearAuthStorage();
  const stillCurrent = await useAuthStore().resetStore();
  if (!stillCurrent) {
    return;
  }

  const notice = $t('backendError.authentication-required');

  // 直接打开页面时会话校验先于应用挂载完成,这时还没有消息容器,先记下来,挂载后补显示
  if (window.$message) {
    window.$message.warning(notice, { duration: 5000 });
  } else {
    request.state.pendingNotice = notice;
  }

}

export const request = createFlatRequest(
  {
    baseURL,
    withCredentials: true
  },
  {
    defaultState: {
      errMsgStack: [],
      pendingNotice: '',
      refreshTokenPromise: null
    } as RequestInstanceState,
    transform(response: AxiosResponse<App.Service.Response<any>>) {
      return response.data.data;
    },
    async onRequest(config) {
      const Authorization = getAuthorization();
      Object.assign(config.headers, { Authorization });
      Object.assign(config, { sessionGeneration: useAuthStore().getSessionGeneration() });

      return config;
    },
    isBackendSuccess(response) {
      // when the backend response code is "0000"(default), it means the request is success
      // to change this logic by yourself, you can modify the `VITE_SERVICE_SUCCESS_CODE` in `.env` file
      return String(response.data.code) === import.meta.env.VITE_SERVICE_SUCCESS_CODE;
    },
    async onBackendFail(response, instance) {
      const authStore = useAuthStore();
      const responseCode = String(response.data.code);
      const backendMessage = translateBackendError(response.data.reason, response.data.params, response.data.detail);

      function handleLogout() {
        authStore.resetStore();
      }

      function logoutAndCleanup() {
        handleLogout();
        window.removeEventListener('beforeunload', handleLogout);

        request.state.errMsgStack = request.state.errMsgStack.filter(msg => msg !== backendMessage);
      }

      // when the backend response code is in `logoutCodes`, it means the user will be logged out and redirected to login page
      const logoutCodes = getErrorCodes(import.meta.env.VITE_SERVICE_LOGOUT_CODES);
      if (logoutCodes.includes(responseCode)) {
        handleLogout();
        return null;
      }

      // when the backend response code is in `modalLogoutCodes`, it means the user will be logged out by displaying a modal
      const modalLogoutCodes = getErrorCodes(import.meta.env.VITE_SERVICE_MODAL_LOGOUT_CODES);
      if (modalLogoutCodes.includes(responseCode) && !request.state.errMsgStack?.includes(backendMessage)) {
        request.state.errMsgStack = [...(request.state.errMsgStack || []), backendMessage];

        // prevent the user from refreshing the page
        window.addEventListener('beforeunload', handleLogout);

        window.$dialog?.error({
          title: $t('common.error'),
          content: backendMessage,
          positiveText: $t('common.confirm'),
          maskClosable: false,
          closeOnEsc: false,
          onPositiveClick() {
            logoutAndCleanup();
          },
          onClose() {
            logoutAndCleanup();
          }
        });

        return null;
      }

      // when the backend response code is in `expiredTokenCodes`, it means the token is expired, and refresh token
      // the api `refreshToken` can not return error code in `expiredTokenCodes`, otherwise it will be a dead loop, should return `logoutCodes` or `modalLogoutCodes`
      const expiredTokenCodes = getErrorCodes(import.meta.env.VITE_SERVICE_EXPIRED_TOKEN_CODES);
      if (expiredTokenCodes.includes(responseCode)) {
        const success = await handleExpiredRequest(request.state);
        if (success) {
          const Authorization = getAuthorization();
          Object.assign(response.config.headers, { Authorization });

          return instance.request(response.config) as Promise<AxiosResponse>;
        }
      }

      return null;
    },
    onError(error) {
      // Login failures are reported by the login form through Naive UI message.
      const requestURL = error.response?.config?.url || error.config?.url || '';
      if (requestURL === '/auth/login') {
        return;
      }

      // the session is no longer valid: clear the local sign-in state and go back to the login page
      if (error.response?.status === 401) {
        const requestGeneration = (error.config as { sessionGeneration?: number } | undefined)?.sessionGeneration;
        void handleSessionExpired(requestGeneration);
        return;
      }

      const backendError = error.response?.data;

      let message: string;
      let backendErrorCode = '';

      // get backend error message and code
      if (error.code === BACKEND_ERROR_CODE || backendError?.reason) {
        backendErrorCode = String(backendError?.code || '');
        message = translateBackendError(backendError?.reason, backendError?.params, backendError?.detail);
      } else {
        // responses without the backend error structure: network errors, timeouts and gateway errors
        message = `${$t('backendError.requestFailed')}${error.response?.status ? ` (HTTP ${error.response.status})` : ''}`;
      }

      // the error message is displayed in the modal
      const modalLogoutCodes = getErrorCodes(import.meta.env.VITE_SERVICE_MODAL_LOGOUT_CODES);
      if (modalLogoutCodes.includes(backendErrorCode)) {
        return;
      }

      // when the token is expired, refresh token and retry request, so no need to show error message
      const expiredTokenCodes = getErrorCodes(import.meta.env.VITE_SERVICE_EXPIRED_TOKEN_CODES);
      if (expiredTokenCodes.includes(backendErrorCode)) {
        return;
      }

      showErrorMsg(request.state, message);
    }
  }
);

export const demoRequest = createRequest(
  {
    baseURL: otherBaseURL.demo
  },
  {
    transform(response: AxiosResponse<App.Service.DemoResponse>) {
      return response.data.result;
    },
    async onRequest(config) {
      const { headers } = config;

      // set token
      const token = localStg.get('token');
      const Authorization = token ? `Bearer ${token}` : null;
      Object.assign(headers, { Authorization });

      return config;
    },
    isBackendSuccess(response) {
      // when the backend response code is "200", it means the request is success
      // you can change this logic by yourself
      return response.data.status === '200';
    },
    async onBackendFail(_response) {
      // when the backend response code is not "200", it means the request is fail
      // for example: the token is expired, refresh token and retry request
    },
    onError(error) {
      let message = error.message;

      // show backend error message
      if (error.code === BACKEND_ERROR_CODE) {
        message = error.response?.data?.message || message;
      }

      window.$message?.error(message);
    }
  }
);
