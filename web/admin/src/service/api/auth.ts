import { request } from '../request';

/**
 * Login with an Omniflux administrator email.
 */
export function fetchLogin(email: string, password: string) {
  return request<Api.Auth.LoginToken>({
    url: '/auth/login',
    method: 'post',
    data: {
      email,
      password
    }
  });
}

/** Get the current Omniflux administrator */
export function fetchGetUserInfo() {
  return request<Api.Auth.UserInfo>({ url: '/auth/me' });
}

/**
 * Logout is delegated to the backend session cookie.
 */
export function fetchLogout() {
  return request({ url: '/auth/logout', method: 'post' });
}