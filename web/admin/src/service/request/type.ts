export interface RequestInstanceState {
  /** the promise of refreshing token */
  refreshTokenPromise: Promise<boolean> | null;
  /** the request error message stack */
  errMsgStack: string[];
  /** the notice produced before the app is mounted, shown once the message container exists */
  pendingNotice: string;
  [key: string]: unknown;
}
