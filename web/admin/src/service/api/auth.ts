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

/**
 * Public registration availability for the login and registration pages.
 */
export function fetchRegistrationOptions() {
  return request<{ enabled: boolean; email_verification_required: boolean }>({
    url: '/auth/registration-options'
  });
}

/**
 * Register an account. Email-verification mode returns a status instead of a session.
 */
export function fetchRegister(email: string, password: string) {
  return request<{ status: string } | Api.Auth.LoginToken>({
    url: '/auth/register',
    method: 'post',
    data: {
      email,
      password
    }
  });
}

export function fetchResendVerification(email: string) {
  return request<{ status: string }>({
    url: '/auth/resend-verification',
    method: 'post',
    data: { email }
  });
}

export function fetchVerifyEmail(token: string) {
  return request<Api.Auth.LoginToken>({
    url: '/auth/verify-email',
    method: 'post',
    data: { token }
  });
}

export function fetchForgotPassword(email: string) {
  return request<{ status: string }>({
    url: '/auth/forgot-password',
    method: 'post',
    data: { email }
  });
}

export function fetchResetPassword(token: string, password: string) {
  return request<{ status: string }>({
    url: '/auth/reset-password',
    method: 'post',
    data: {
      token,
      password
    }
  });
}

export function fetchAcceptInvitation(token: string, password: string) {
  return request<Api.Auth.LoginToken>({
    url: '/auth/accept-invitation',
    method: 'post',
    data: {
      token,
      password
    }
  });
}
