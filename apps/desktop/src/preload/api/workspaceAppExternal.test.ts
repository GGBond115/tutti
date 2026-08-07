import assert from "node:assert/strict";
import test from "node:test";
import type { IpcRendererEvent } from "electron";
import { desktopIpcChannels } from "../../shared/contracts/ipc.ts";
import { createWorkspaceAppExternalDesktopApi } from "./workspaceAppExternal.ts";

class FakeIpcRenderer {
  readonly sent: Array<{ channel: string; payload: unknown }> = [];

  off(): void {}

  on(
    _channel: string,
    _listener: (event: IpcRendererEvent, request: never) => void
  ): void {}

  send(channel: string, payload: unknown): void {
    this.sent.push({ channel, payload });
  }
}

test("workspace external preload announces readiness only after installing its request handler", async () => {
  const renderer = new FakeIpcRenderer();
  const api = createWorkspaceAppExternalDesktopApi(renderer);
  const unsubscribe = api.onRequest(async () => undefined);

  assert.deepEqual(renderer.sent, []);
  await Promise.resolve();
  assert.deepEqual(renderer.sent, [
    {
      channel: desktopIpcChannels.appExternal.rendererReady,
      payload: { ready: true }
    }
  ]);

  unsubscribe();
  assert.deepEqual(renderer.sent.at(-1), {
    channel: desktopIpcChannels.appExternal.rendererReady,
    payload: { ready: false }
  });
});

test("workspace external preload does not announce a handler removed in the same turn", async () => {
  const renderer = new FakeIpcRenderer();
  const api = createWorkspaceAppExternalDesktopApi(renderer);
  const unsubscribe = api.onRequest(async () => undefined);

  unsubscribe();
  await Promise.resolve();
  assert.deepEqual(renderer.sent, []);
});
