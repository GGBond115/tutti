import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import { InstantiationService } from "@tutti-os/infra/di";
import type { AccountSession } from "./mobileDomain";
import { MobileApplicationService } from "./mobileApplicationService";
import type {
  ApplicationVisibility,
  ClockPort,
  MobileServicePorts
} from "./servicePorts";

const session: AccountSession = {
  email: "person@example.com",
  name: "Person",
  sessionId: "session-cookie",
  userId: "user-1"
};

describe("MobileApplicationService scopes", () => {
  test("replaces the unauthenticated child with one authenticated child", async () => {
    const harness = createHarness(null);
    const service = new MobileApplicationService(
      new InstantiationService(),
      harness.ports
    );
    await service.start();

    expect(service.getSnapshot().route).toBe("login");
    expect(harness.legacyCookieClearCalls).toBe(1);
    const login = service.loginService!;
    login.setEmail(session.email);
    await login.submitEmail();
    login.setCode("123456");
    await login.submitEmail();

    expect(service.getSnapshot().route).toBe("devices");
    expect(service.loginService).toBeNull();
    expect(service.deviceService).not.toBeNull();

    await service.signOut();
    expect(service.getSnapshot().route).toBe("login");
    expect(service.deviceService).toBeNull();
    expect(harness.legacyCookieClearCalls).toBe(2);
  });

  test("background grace closes DeviceLink only after the deadline", async () => {
    const harness = createHarness(session);
    const service = new MobileApplicationService(
      new InstantiationService(),
      harness.ports
    );
    await service.start();

    harness.emitLifecycle("background");
    expect(harness.closeCalls).toBe(0);
    harness.clock.advanceBy(14_999);
    expect(harness.closeCalls).toBe(0);
    harness.clock.advanceBy(1);
    await Promise.resolve();
    expect(harness.closeCalls).toBe(1);
    expect(service.getSnapshot().route).toBe("devices");
  });

  test("inactive transitions do not start the background disconnect grace", async () => {
    const harness = createHarness(session);
    const service = new MobileApplicationService(
      new InstantiationService(),
      harness.ports
    );
    await service.start();

    harness.emitLifecycle("inactive");
    harness.clock.advanceBy(15_000);
    await Promise.resolve();

    expect(harness.closeCalls).toBe(0);
  });

  test("creates an authenticated device scope with the current background level", async () => {
    const storedSession = deferred<AccountSession | null>();
    const harness = createHarness(storedSession.promise);
    const service = new MobileApplicationService(
      new InstantiationService(),
      harness.ports
    );

    const start = service.start();
    harness.emitLifecycle("background");
    storedSession.resolve(session);
    await start;

    expect(service.getSnapshot().route).toBe("devices");
    expect(harness.registerCalls).toBe(0);

    harness.emitLifecycle("active");
    await flushPromises();
    expect(harness.registerCalls).toBe(1);
  });
});

function createHarness(
  storedSession: AccountSession | null | Promise<AccountSession | null>
): {
  clock: ManualClock;
  closeCalls: number;
  emitLifecycle(visibility: ApplicationVisibility): void;
  ports: MobileServicePorts;
  legacyCookieClearCalls: number;
  registerCalls: number;
} {
  const clock = new ManualClock();
  let lifecycleListener: ((visibility: ApplicationVisibility) => void) | null =
    null;
  const harness = {
    clock,
    closeCalls: 0,
    legacyCookieClearCalls: 0,
    registerCalls: 0,
    emitLifecycle(visibility: ApplicationVisibility) {
      lifecycleListener?.(visibility);
    },
    ports: null as unknown as MobileServicePorts
  };
  harness.ports = {
    account: {
      sendEmailCode: async () => undefined,
      signInWithGitHub: async () => session,
      verifyEmailCode: async () => session
    },
    clock,
    createRemoteClient: () =>
      ({
        listAgentTargets: async () => ({ targets: [] }),
        listWorkspaces: async () => ({ workspaces: [] })
      }) as unknown as TuttidClient,
    deviceLink: {
      closeLink: async () => {
        harness.closeCalls += 1;
      },
      requestAgentHTTP: async () => ({
        body: "",
        errorCode: "",
        headers: {},
        protocolEpoch: 1,
        status: 204
      }),
      subscribeAgentLive: () => ({ close() {} })
    },
    diagnostics: {
      record: () => undefined
    },
    legacySessionCookie: {
      clear: async () => {
        harness.legacyCookieClearCalls += 1;
      }
    },
    lifecycle: {
      subscribe(listener) {
        lifecycleListener = listener;
        return () => {
          lifecycleListener = null;
        };
      }
    },
    pairing: {
      claimPairing: async () => ({
        challengeId: "challenge-1",
        expiresAt: new Date(Date.now() + 1_000).toISOString(),
        state: "awaiting_confirmation"
      }),
      connectPairedDevice: async () => undefined,
      getPairingChallenge: async () => ({
        challengeId: "challenge-1",
        expiresAt: new Date(Date.now() + 1_000).toISOString(),
        state: "confirmed"
      }),
      listDevices: async () => [],
      listPairings: async () => [],
      registerCurrentDevice: async () => {
        harness.registerCalls += 1;
        return { userDeviceId: "mobile-1" };
      }
    },
    qrCodeScanner: {
      start: () => ({
        cancel: async () => undefined,
        result: Promise.resolve("")
      })
    },
    sessionStorage: {
      clearSession: async () => undefined,
      loadSession: async () => storedSession,
      saveSession: async () => undefined
    }
  };
  return harness;
}

interface Deferred<T> {
  promise: Promise<T>;
  resolve(value: T): void;
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolver) => {
    resolve = resolver;
  });
  return { promise, resolve };
}

async function flushPromises(): Promise<void> {
  for (let index = 0; index < 8; index += 1) {
    await Promise.resolve();
  }
}

class ManualClock implements ClockPort {
  private nowValue = 0;
  private readonly tasks: Array<{
    at: number;
    canceled: boolean;
    callback(): void;
  }> = [];

  now(): number {
    return this.nowValue;
  }

  schedule(delayMs: number, callback: () => void): { cancel(): void } {
    const task = {
      at: this.nowValue + delayMs,
      callback,
      canceled: false
    };
    this.tasks.push(task);
    return {
      cancel: () => {
        task.canceled = true;
      }
    };
  }

  advanceBy(delayMs: number): void {
    this.nowValue += delayMs;
    for (const task of this.tasks) {
      if (!task.canceled && task.at <= this.nowValue) {
        task.canceled = true;
        task.callback();
      }
    }
  }
}
