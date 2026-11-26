declare namespace Api {
  /**
   * namespace Auth
   *
   * backend api module: "auth"
   */
  namespace Auth {
    interface LoginToken {
      token: string;
      refreshToken: string;
    }

    interface UserInfo {
      userId: string;
      userName: string;
      role?: string;
      roles: string[];
      permissions?: string[];
      permissionGroupId?: string | null;
      permissionGroupName?: string | null;
      buttons: string[];
    }
  }
}
