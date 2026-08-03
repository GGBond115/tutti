import assert from "node:assert/strict";
import test from "node:test";
import { startDesktopUpdateDevelopmentMockServer } from "./mockServer.ts";
import { createDevelopmentMinimumVersionChecker } from "./policyChecker.ts";
import {
  resolveDesktopUpdateDevelopmentPolicyScenario,
  type DesktopUpdateDevelopmentPolicyScenario
} from "./policyScenario.ts";
import {
  resolveDesktopUpdateAdmissionDevelopment,
  resolveDesktopUpdateDevelopmentScenario,
  type DesktopUpdateDevelopmentScenario
} from "./scenario.ts";
import {
  completeDevelopmentUpdateInstallation,
  createDevelopmentAppUpdateDriver,
  DevelopmentInstallSuppressedError
} from "./updaterDriver.ts";

const inProcessEnvironment = {
  DESKTOP_UPDATE_ADMISSION_CURRENT_VERSION: "1.0.0",
  DESKTOP_UPDATE_ADMISSION_DEV: "1",
  DESKTOP_UPDATE_ADMISSION_LATEST_VERSION: "1.2.0",
  DESKTOP_UPDATE_ADMISSION_MINIMUM_VERSION: "1.1.0",
  DESKTOP_UPDATE_ADMISSION_POLICY: "upgradeRequired",
  DESKTOP_UPDATE_ADMISSION_UPDATER: "available"
} as const;

const loopbackClientEnvironment = {
  DESKTOP_UPDATE_ADMISSION_CURRENT_VERSION: "1.0.0",
  DESKTOP_UPDATE_ADMISSION_DEV: "1",
  DESKTOP_UPDATE_ADMISSION_MOCK_SERVER_URL: "http://127.0.0.1:43210",
  DESKTOP_UPDATE_ADMISSION_TRANSPORT: "loopback"
} as const;

test("packaged resolution ignores invalid development variables", () => {
  const result = resolveDesktopUpdateAdmissionDevelopment({
    applicationVersion: "2.0.0",
    env: {
      DESKTOP_UPDATE_ADMISSION_CURRENT_VERSION: "invalid",
      DESKTOP_UPDATE_ADMISSION_DEV: "1",
      DESKTOP_UPDATE_ADMISSION_POLICY: "invalid"
    },
    isPackaged: true
  });

  assert.equal(result.scenario, null);
  assert.deepEqual(result.runtime, {
    checksEnabled: true,
    currentVersion: "2.0.0",
    development: false
  });
});

test("loopback client resolves without server policy variables", () => {
  const scenario = resolveDesktopUpdateDevelopmentScenario({
    env: loopbackClientEnvironment,
    isPackaged: false
  });

  assert.deepEqual(scenario, {
    currentVersion: "1.0.0",
    mockServerUrl: "http://127.0.0.1:43210",
    transport: "loopback",
    updater: { check: "unavailable" }
  });
});

test("loopback client rejects policy variables owned by the mock server", () => {
  for (const serverVariable of [
    "DESKTOP_UPDATE_ADMISSION_MINIMUM_VERSION",
    "DESKTOP_UPDATE_ADMISSION_FEATURE_KEYS",
    "DESKTOP_UPDATE_ADMISSION_POLICY",
    "DESKTOP_UPDATE_ADMISSION_POLICY_SEQUENCE",
    "DESKTOP_UPDATE_ADMISSION_SCENARIO"
  ]) {
    assert.throws(
      () =>
        resolveDesktopUpdateDevelopmentScenario({
          env: {
            ...loopbackClientEnvironment,
            [serverVariable]: "upgradeRequired"
          },
          isPackaged: false
        }),
      new RegExp(`${serverVariable} belongs to the loopback mock server`)
    );
  }
});

test("loopback updater can be configured independently from server policy", () => {
  const scenario = resolveDesktopUpdateDevelopmentScenario({
    env: {
      ...loopbackClientEnvironment,
      DESKTOP_UPDATE_ADMISSION_LATEST_VERSION: "1.2.0",
      DESKTOP_UPDATE_ADMISSION_UPDATER: "available"
    },
    isPackaged: false
  });

  assert.deepEqual(scenario?.updater, {
    check: "available",
    download: "success",
    install: "simulated",
    latestVersion: "1.2.0"
  });
});

test("development policy returns normalized feature keys", async () => {
  const policy = resolveDesktopUpdateDevelopmentPolicyScenario({
    env: {
      ...inProcessEnvironment,
      DESKTOP_UPDATE_ADMISSION_FEATURE_KEYS: "workspace.example,agent.preview"
    }
  });
  assert.ok(policy);
  assert.deepEqual(policy.featureKeys, ["agent.preview", "workspace.example"]);

  const response = await createDevelopmentMinimumVersionChecker(policy)(
    {
      architecture: "arm64",
      currentVersion: "1.0.0",
      platform: "macos",
      product: "tutti-desktop"
    },
    new AbortController().signal
  );
  assert.deepEqual(response.featureAvailability, {
    keys: ["agent.preview", "workspace.example"]
  });
});

test("in-process client leaves policy validation to the daemon", () => {
  const scenario = resolveDesktopUpdateDevelopmentScenario({
    env: {
      DESKTOP_UPDATE_ADMISSION_CURRENT_VERSION: "1.0.0",
      DESKTOP_UPDATE_ADMISSION_DEV: "1",
      DESKTOP_UPDATE_ADMISSION_UPDATER: "unavailable"
    },
    isPackaged: false
  });
  assert.equal(scenario?.transport, "in-process");
});

test("client resolution rejects invalid SemVer and non-loopback URL", () => {
  assert.throws(
    () =>
      resolveDesktopUpdateDevelopmentScenario({
        env: {
          ...loopbackClientEnvironment,
          DESKTOP_UPDATE_ADMISSION_CURRENT_VERSION: "01.0.0"
        },
        isPackaged: false
      }),
    /must be valid SemVer/
  );
  assert.throws(
    () =>
      resolveDesktopUpdateDevelopmentScenario({
        env: {
          ...loopbackClientEnvironment,
          DESKTOP_UPDATE_ADMISSION_MOCK_SERVER_URL: "http://localhost:43210"
        },
        isPackaged: false
      }),
    /must be an http:\/\/127\.0\.0\.1 origin/
  );
});

test("target-below-minimum scenario keeps updater and policy versions coherent", () => {
  const scenario = resolveDesktopUpdateDevelopmentScenario({
    env: {
      ...inProcessEnvironment,
      DESKTOP_UPDATE_ADMISSION_LATEST_VERSION: "1.1.0",
      DESKTOP_UPDATE_ADMISSION_MINIMUM_VERSION: "1.2.0",
      DESKTOP_UPDATE_ADMISSION_POLICY: undefined,
      DESKTOP_UPDATE_ADMISSION_SCENARIO: "startup-target-below-minimum",
      DESKTOP_UPDATE_ADMISSION_UPDATER: undefined
    },
    isPackaged: false
  });

  assert.equal(scenario?.updater.check, "targetBelowMinimum");
});

test("named scenarios only influence the client updater outcome", () => {
  const scenario = resolveDesktopUpdateDevelopmentScenario({
    env: {
      ...inProcessEnvironment,
      DESKTOP_UPDATE_ADMISSION_POLICY: undefined,
      DESKTOP_UPDATE_ADMISSION_SCENARIO: "startup-force-success",
      DESKTOP_UPDATE_ADMISSION_UPDATER: undefined
    },
    isPackaged: false
  });
  assert.equal(scenario?.updater.check, "available");
});

test("policy sequences advance and keep their final response", async () => {
  const policy = resolvePolicy({
    DESKTOP_UPDATE_ADMISSION_DEV: "1",
    DESKTOP_UPDATE_ADMISSION_POLICY_SEQUENCE:
      "upgradeRequired@1.1.0,minimumNotConfigured"
  });
  const checker = createDevelopmentMinimumVersionChecker(policy, {
    expectedCurrentVersion: "1.0.0"
  });
  const request = {
    architecture: "arm64",
    currentVersion: "1.0.0",
    platform: "macos",
    product: "tutti-desktop"
  } as const;

  assert.equal(
    (await checker(request, new AbortController().signal)).decision,
    "upgradeRequired"
  );
  assert.equal(
    (await checker(request, new AbortController().signal)).reason,
    "minimumNotConfigured"
  );
  assert.equal(
    (await checker(request, new AbortController().signal)).reason,
    "minimumNotConfigured"
  );
});

test("policy sequences advance independently for each desktop product", async () => {
  const policy = resolvePolicy({
    DESKTOP_UPDATE_ADMISSION_DEV: "1",
    DESKTOP_UPDATE_ADMISSION_POLICY_SEQUENCE:
      "upgradeRequired@1.1.0,minimumNotConfigured"
  });
  const checker = createDevelopmentMinimumVersionChecker(policy);
  const request = {
    architecture: "arm64",
    currentVersion: "1.0.0",
    platform: "macos"
  } as const;

  assert.equal(
    (
      await checker(
        { ...request, product: "tsh-desktop" },
        new AbortController().signal
      )
    ).decision,
    "upgradeRequired"
  );
  assert.equal(
    (
      await checker(
        { ...request, product: "tutti-desktop" },
        new AbortController().signal
      )
    ).decision,
    "upgradeRequired"
  );
});

test("timeout policy remains pending until the caller aborts", async () => {
  const checker = createDevelopmentMinimumVersionChecker(
    resolvePolicy({
      DESKTOP_UPDATE_ADMISSION_DEV: "1",
      DESKTOP_UPDATE_ADMISSION_POLICY: "timeout"
    })
  );
  const controller = new AbortController();
  const pending = checker(
    {
      architecture: "arm64",
      currentVersion: "1.0.0",
      platform: "macos",
      product: "tutti-desktop"
    },
    controller.signal
  );

  controller.abort();

  await assert.rejects(pending, { name: "AbortError" });
});

test("retry-policy-released exposes retry after updater failure", async () => {
  const scenario = resolveDesktopUpdateDevelopmentScenario({
    env: {
      DESKTOP_UPDATE_ADMISSION_CURRENT_VERSION: "1.0.0",
      DESKTOP_UPDATE_ADMISSION_DEV: "1",
      DESKTOP_UPDATE_ADMISSION_LATEST_VERSION: "1.2.0",
      DESKTOP_UPDATE_ADMISSION_MINIMUM_VERSION: "1.1.0",
      DESKTOP_UPDATE_ADMISSION_SCENARIO: "retry-policy-released"
    },
    isPackaged: false
  });
  assert.ok(scenario?.transport === "in-process");
  const checker = createDevelopmentMinimumVersionChecker(
    resolvePolicy({
      DESKTOP_UPDATE_ADMISSION_DEV: "1",
      DESKTOP_UPDATE_ADMISSION_MINIMUM_VERSION: "1.1.0",
      DESKTOP_UPDATE_ADMISSION_SCENARIO: "retry-policy-released"
    }),
    {
      expectedCurrentVersion: scenario.currentVersion
    }
  );
  const request = {
    architecture: "arm64",
    currentVersion: scenario.currentVersion,
    platform: "macos",
    product: "tutti-desktop"
  } as const;

  assert.equal(
    (await checker(request, new AbortController().signal)).decision,
    "upgradeRequired"
  );
  await assert.rejects(
    createDevelopmentAppUpdateDriver(scenario).checkForUpdates(),
    /Development update check failed/
  );
  assert.equal(
    (await checker(request, new AbortController().signal)).reason,
    "minimumNotConfigured"
  );
});

test("development updater emits a deterministic successful download", async () => {
  const scenario = createScenario();
  const driver = createDevelopmentAppUpdateDriver(scenario);
  const events: string[] = [];
  driver.onCheckingForUpdate(() => events.push("checking"));
  driver.onUpdateAvailable((info) => events.push(`available:${info.version}`));
  driver.onDownloadProgress((progress) =>
    events.push(`progress:${progress.percent}`)
  );
  driver.onUpdateDownloaded((info) =>
    events.push(`downloaded:${info.version}`)
  );

  await driver.checkForUpdates();
  await driver.downloadUpdate();

  assert.deepEqual(events, [
    "checking",
    "available:1.2.0",
    "progress:100",
    "downloaded:1.2.0"
  ]);
  assert.throws(
    () => completeDevelopmentUpdateInstallation(scenario),
    DevelopmentInstallSuppressedError
  );
});

test("loopback mock server owns policy and returns its minimum version", async () => {
  const policy = resolvePolicy({
    DESKTOP_UPDATE_ADMISSION_DEV: "1",
    DESKTOP_UPDATE_ADMISSION_FEATURE_KEYS: "workspace.example,agent.preview",
    DESKTOP_UPDATE_ADMISSION_MINIMUM_VERSION: "1.4.0",
    DESKTOP_UPDATE_ADMISSION_POLICY: "upgradeRequired"
  });
  const server = await startDesktopUpdateDevelopmentMockServer({ policy });
  try {
    const response = await fetch(
      `${server.baseUrl}/api/desktop/v1/public/desktop-version/check`,
      {
        body: JSON.stringify({
          architecture: "arm64",
          currentVersion: "1.0.0",
          platform: "macos",
          product: "tsh-desktop"
        }),
        headers: { "content-type": "application/json" },
        method: "POST"
      }
    );
    assert.equal(response.status, 200);
    assert.deepEqual(await response.json(), {
      channel: "stable",
      decision: "upgradeRequired",
      featureAvailability: { keys: ["agent.preview", "workspace.example"] },
      minimumVersion: "1.4.0",
      policyRevision: "development-policy-1",
      reason: "belowMinimum"
    });
    const invalidResponse = await fetch(
      `${server.baseUrl}/api/desktop/v1/public/desktop-version/check`,
      {
        body: "{",
        headers: { "content-type": "application/json" },
        method: "POST"
      }
    );
    assert.equal(invalidResponse.status, 400);
    assert.equal(new URL(server.baseUrl).hostname, "127.0.0.1");
  } finally {
    await server.close();
  }
});

function resolvePolicy(
  env: Readonly<Record<string, string | undefined>>
): DesktopUpdateDevelopmentPolicyScenario {
  const policy = resolveDesktopUpdateDevelopmentPolicyScenario({ env });
  assert.ok(policy);
  return policy;
}

function createScenario(): Extract<
  DesktopUpdateDevelopmentScenario,
  { transport: "in-process" }
> {
  return {
    currentVersion: "1.0.0",
    mockServerUrl: null,
    transport: "in-process",
    updater: {
      check: "available",
      download: "success",
      install: "simulated",
      latestVersion: "1.2.0"
    }
  };
}
