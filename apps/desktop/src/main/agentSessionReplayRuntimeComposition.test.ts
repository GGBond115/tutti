import assert from "node:assert/strict";
import test from "node:test";
import { desktopIpcChannels } from "../shared/contracts/ipc.ts";
import {
  installAgentSessionReplayRuntimeComposition,
  type AgentSessionReplayRuntimeCompositionInput
} from "./agentSessionReplayRuntimeComposition.ts";

test("disabled replay runtime composition creates and registers nothing", () => {
  const registeredChannels: string[] = [];

  const manager = installAgentSessionReplayRuntimeComposition(
    createInput(false, registeredChannels)
  );

  assert.equal(manager, null);
  assert.deepEqual(registeredChannels, []);
});

test("enabled replay runtime composition owns all replay IPC registration", () => {
  const registeredChannels: string[] = [];

  const manager = installAgentSessionReplayRuntimeComposition(
    createInput(true, registeredChannels)
  );

  assert.ok(manager);
  assert.deepEqual(registeredChannels, [
    desktopIpcChannels.runtime.launchAgentSessionReplay,
    desktopIpcChannels.runtime.getAgentSessionReplayPlayback,
    desktopIpcChannels.runtime.getAgentSessionReplayStatus,
    desktopIpcChannels.runtime.importAgentSessionReplayCassettes,
    desktopIpcChannels.runtime.revealAgentSessionReplayCassette,
    desktopIpcChannels.runtime.setAgentSessionReplayPlayback,
    desktopIpcChannels.runtime.sendAgentSessionReplayControl,
    desktopIpcChannels.runtime.waitForAgentSessionReplay
  ]);
  manager.dispose();
});

function createInput(
  enabled: boolean,
  registeredChannels: string[]
): AgentSessionReplayRuntimeCompositionInput {
  return {
    electronEntry: null,
    electronExecutable: "electron",
    enabled,
    environment: {},
    fileDialogs: {
      selectUploadFiles: async () => []
    },
    isPackaged: true,
    logger: {
      async close() {},
      debug() {},
      error() {},
      info() {},
      warn() {}
    },
    nodeExecutable: "node",
    registerIpcHandler: ((channel: string) => {
      registeredChannels.push(channel);
    }) as AgentSessionReplayRuntimeCompositionInput["registerIpcHandler"],
    resolveOwnerWindow: (() =>
      null) as AgentSessionReplayRuntimeCompositionInput["resolveOwnerWindow"],
    showItemInFolder() {},
    tuttidClient: {
      async getAgentSessionReplayTransportPlayback() {
        throw new Error("not called");
      },
      async importAgentSessionCassettes() {
        throw new Error("not called");
      },
      async prepareAgentSessionReplayWorkspace() {
        throw new Error("not called");
      },
      async updateAgentSessionReplayTransportPlayback() {
        throw new Error("not called");
      }
    } as unknown as AgentSessionReplayRuntimeCompositionInput["tuttidClient"]
  };
}
