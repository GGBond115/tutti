#!/usr/bin/env node

import { resolveDesktopUpdateDevelopmentPolicyScenario } from "./policyScenario.ts";
import { startDesktopUpdateDevelopmentMockServer } from "./mockServer.ts";

function log(level: "info" | "error", details: Record<string, unknown>): void {
  const output = `[minimum-version-check] ${JSON.stringify(details)}`;
  if (level === "error") {
    process.stderr.write(`${output}\n`);
  } else {
    process.stdout.write(`${output}\n`);
  }
}

async function main(): Promise<void> {
  const policy = resolveDesktopUpdateDevelopmentPolicyScenario({
    env: process.env
  });
  if (!policy) {
    throw new Error(
      "DESKTOP_UPDATE_ADMISSION_DEV=1 is required to start the mock server"
    );
  }
  const rawPort = process.env.DESKTOP_UPDATE_ADMISSION_MOCK_SERVER_PORT?.trim();
  const port = rawPort ? Number(rawPort) : undefined;
  const server = await startDesktopUpdateDevelopmentMockServer({
    port,
    policy
  });
  log("info", {
    baseUrl: server.baseUrl,
    result: "listening",
    stage: "development-mock-server"
  });
  let closing = false;
  const close = async (signal: string): Promise<void> => {
    if (closing) {
      return;
    }
    closing = true;
    await server.close();
    log("info", {
      result: "stopped",
      signal,
      stage: "development-mock-server"
    });
  };
  process.once("SIGINT", () => {
    void close("SIGINT");
  });
  process.once("SIGTERM", () => {
    void close("SIGTERM");
  });
}

void main().catch((error) => {
  log("error", {
    error: error instanceof Error ? error.message : String(error),
    result: "failure",
    stage: "development-mock-server"
  });
  process.exitCode = 1;
});
