import { computed, reactive, ref } from 'vue';
import { useRoute } from 'vue-router';
import { defineStore } from 'pinia';
import { useLoading } from '@sa/hooks';
import { localStg } from '@/utils/storage';
import { fetchGetUserInfo, fetchLogin, fetchLogout, fetchRegister } from '@/service/api';
import { translateBackendError } from '@/service/request/backend-error';
import { $t } from '@/locales';
import { useRouterPush } from '@/hooks/common/router';
import { SetupStoreId } from '@/enum';
import { useRouteStore } from '../route';
import { useTabStore } from '../tab';
import { clearAuthStorage, getToken } from './shared';

interface AuthResult {
  success: boolean;
  status?: string;
  message?: string;
}

interface LoginErrorPayload {
  code?: string;
  reason?: string;
  params?: Record<string, unknown>;
  detail?: string;
}

export const useAuthStore = defineStore(SetupStoreId.Auth, () => {
  const route = useRoute();
  const routeStore = useRouteStore();
  const tabStore = useTabStore();
  const { toLogin, redirectFromLogin } = useRouterPush(false);
  const { loading: loginLoading, startLoading: startLoginLoading, endLoading: endLoginLoading } = useLoading();
  const { loading: registerLoading, startLoading: startRegisterLoading, endLoading: endRegisterLoading } = useLoading();

  const token = ref('');
  const sessionGeneration = ref(0);

  const userInfo: Api.Auth.UserInfo = reactive({
    userId: '',
    userName: '',
    roles: [],
    buttons: []
  });

  const isStaticSuper = computed(() => {
    const { VITE_AUTH_ROUTE_MODE, VITE_STATIC_SUPER_ROLE } = import.meta.env;

    return VITE_AUTH_ROUTE_MODE === 'static' && userInfo.roles.includes(VITE_STATIC_SUPER_ROLE);
  });

  const isLogin = computed(() => Boolean(token.value));

  function getSessionGeneration() {
    return sessionGeneration.value;
  }

  function isSessionCurrent(generation: number) {
    return generation === sessionGeneration.value;
  }

  async function resetStore(): Promise<boolean> {
    const generation = ++sessionGeneration.value;

    if (getToken()) {
      await fetchLogout();

    }

    if (generation !== sessionGeneration.value) return false;
    recordUserId();

    clearAuthStorage();
    token.value = '';


    // 首次进入页面时路由还在解析,守卫发现未登录后会带上回跳地址自己跳到登录页
    if (!route.meta.constant && route.name) {
      await toLogin();
    }

    tabStore.cacheTabs();
    routeStore.resetStore();
    return true;
  }

  /** 记录最近一次登录的用户,下次登录时据此判断是否换了人 */
  function recordUserId() {
    if (!userInfo.userId) {
      return;
    }

    localStg.set('lastLoginUserId', userInfo.userId);
  }

  function checkTabClear(): boolean {
    if (!userInfo.userId) {
      return false;
    }

    const lastLoginUserId = localStg.get('lastLoginUserId');

    // 换了人或第一次登录时,清空上一个用户留下的标签页
    if (!lastLoginUserId || lastLoginUserId !== userInfo.userId) {
      localStg.remove('globalTabs');
      tabStore.clearTabs();

      return true;
    }

    return false;
  }

  async function establishSession(
    loginToken: Api.Auth.LoginToken,
    redirect: boolean,
    generation: number
  ): Promise<boolean> {
    if (generation !== sessionGeneration.value) return false;
    localStg.set('token', loginToken.token);
    localStg.set('refreshToken', loginToken.refreshToken);

    const info = await getUserInfo(generation);

    if (!info || generation !== sessionGeneration.value) {
      return false;
    }

    Object.assign(userInfo, info);
    token.value = loginToken.token;
    const isClear = checkTabClear();

    recordUserId();
    await redirectFromLogin(redirect && !isClear);

    return generation === sessionGeneration.value;
  }

  async function login(email: string, password: string, redirect = true): Promise<AuthResult> {
    const generation = ++sessionGeneration.value;
    startLoginLoading();

    const { data: loginToken, error } = await fetchLogin(email, password);

    let pass = false;
    if (!error) {
      pass = await establishSession(loginToken, redirect, generation);
    }
    if (pass) {
      window.$notification?.success({
        title: $t('page.login.common.loginSuccess'),
        content: $t('page.login.common.welcomeBack', {
          userName: userInfo.userName
        }),
        duration: 4500
      });
      endLoginLoading();
      return { success: true };
    }


    const axiosError = error as { response?: { data?: LoginErrorPayload } } | null;
    const payload = axiosError?.response?.data;
    let message = payload?.reason
      ? translateBackendError(payload.reason, payload.params, payload.detail)
      : $t('business.login-failed-please-try-again');

    if (payload?.code === 'AUTH_INVALID_CREDENTIALS') {
      message = $t('business.incorrect-email-or-password');
    } else if (payload?.code === 'AUTH_ACCOUNT_LOCKED') {
      const retryAfter = Number(payload.params?.retry_after_seconds ?? 0);

      if (retryAfter >= 60) {
        message = $t('business.accountLockedMinutes', {
          minutes: Math.ceil(retryAfter / 60)
        });
      } else if (retryAfter > 0) {
        message = $t('business.accountLockedSeconds', { seconds: retryAfter });
      }
    }

    if (generation === sessionGeneration.value) {
      clearAuthStorage();
      token.value = '';
    }

    endLoginLoading();
    return { success: false, message };
  }

  async function register(email: string, password: string): Promise<AuthResult> {
    const generation = ++sessionGeneration.value;
    startRegisterLoading();

    const { data: result, error } = await fetchRegister(email, password);
    let pass = false;
    if (!error && result && 'status' in result) {
      endRegisterLoading();
      return { success: true, status: result.status };
    }
    if (!error && result) {
      pass = await establishSession(result as Api.Auth.LoginToken, true, generation);
    }
    if (pass) {
      window.$notification?.success({
        title: $t('business.registration-succeeded'),
        content: $t('business.welcome-to-omniflux', {
          userName: userInfo.userName
        }),
        duration: 4500
      });
      endRegisterLoading();
      return { success: true };
    }


    const axiosError = error as { response?: { data?: LoginErrorPayload } } | null;
    const payload = axiosError?.response?.data;
    const message = payload?.reason
      ? translateBackendError(payload.reason, payload.params, payload.detail)
      : $t('business.registration-failed-please-try-again');

    if (generation === sessionGeneration.value) {
      clearAuthStorage();
      token.value = '';
    }

    endRegisterLoading();
    return { success: false, message };
  }

  async function loginWithToken(loginToken: Api.Auth.LoginToken): Promise<boolean> {
    const generation = ++sessionGeneration.value;
    const pass = await establishSession(loginToken, true, generation);
    if (pass) {
      window.$notification?.success({
        title: $t('page.login.common.loginSuccess'),
        content: $t('page.login.common.welcomeBack', {
          userName: userInfo.userName
        }),
        duration: 4500
      });
    }
    return pass;
  }
  async function getUserInfo(generation: number) {
    const { data: info, error } = await fetchGetUserInfo();

    if (error || generation !== sessionGeneration.value) return null;
    return info;
  }

  async function initUserInfo(): Promise<boolean> {
    const maybeToken = getToken();
    if (!maybeToken) return false;

    const generation = sessionGeneration.value;
    token.value = maybeToken;
    const info = await getUserInfo(generation);
    if (info && generation === sessionGeneration.value) {
      Object.assign(userInfo, info);
      return true;
    }

    if (generation === sessionGeneration.value) await resetStore();
    return false;
  }

  return {
    token,
    userInfo,
    isStaticSuper,
    isLogin,
    loginLoading,
    registerLoading,
    resetStore,
    getSessionGeneration,
    isSessionCurrent,
    login,
    register,
    loginWithToken,
    initUserInfo
  };
});
