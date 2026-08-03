import type { AccountSession } from "./mobileDomain";
import { ObservableService } from "./observableService";
import type { AccountPort } from "./servicePorts";

export type LoginErrorCode =
  | "browser_login_cancelled"
  | "request_failed"
  | null;

export interface LoginSnapshot {
  errorCode: LoginErrorCode;
  pending: boolean;
}

export class LoginService extends ObservableService<LoginSnapshot> {
  readonly _serviceBrand: undefined;
  private snapshot: LoginSnapshot = {
    errorCode: null,
    pending: false
  };

  constructor(
    private readonly account: AccountPort,
    private readonly onAuthenticated: (session: AccountSession) => Promise<void>
  ) {
    super();
  }

  getSnapshot = (): LoginSnapshot => this.snapshot;

  async submitLogin(): Promise<void> {
    if (this.snapshot.pending) return;
    this.patch({ errorCode: null, pending: true });
    try {
      await this.onAuthenticated(await this.account.signInWithBrowser());
    } catch (cause) {
      const cancelled =
        typeof cause === "object" &&
        cause !== null &&
        "code" in cause &&
        cause.code === "BROWSER_LOGIN_CANCELLED";
      this.patch({
        errorCode: cancelled ? "browser_login_cancelled" : "request_failed",
        pending: false
      });
      return;
    }
    this.patch({ pending: false });
  }

  dispose(): void {
    this.clearListeners();
  }

  private patch(patch: Partial<LoginSnapshot>): void {
    this.snapshot = { ...this.snapshot, ...patch };
    this.emitChange();
  }
}
