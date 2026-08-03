import assert from "node:assert/strict";
import test from "node:test";
import type { WebContents } from "electron";
import { desktopFeatureAvailabilityIpcChannels } from "../contracts/index.ts";
import { registerDesktopFeatureAvailabilityIpc } from "./registerDesktopFeatureAvailabilityIpc.ts";

test("feature availability IPC rejects untrusted senders and invalid keys", async () => {
  type FeatureHandler = (
    event: { sender: WebContents },
    key?: unknown
  ) => unknown;
  const handlers = new Map<string, FeatureHandler>();
  const trustedSender = { id: 1 } as WebContents;
  const untrustedSender = { id: 2 } as WebContents;
  const registration = registerDesktopFeatureAvailabilityIpc({
    electron: {
      broadcast() {},
      ipcMain: {
        handle(channel: string, handler: FeatureHandler) {
          handlers.set(channel, handler);
        },
        removeHandler(channel: string) {
          handlers.delete(channel);
        }
      } as never,
      isTrustedSender: (sender) => sender === trustedSender
    },
    runtime: {
      getSnapshot: () => ({
        architecture: "arm64",
        currentVersion: "1.0.0",
        fetchedAt: null,
        keys: [],
        platform: "macos",
        policyRevision: null,
        product: "tutti-desktop",
        source: "empty"
      }),
      isSupported: (key) => key === "workspace.example",
      subscribe: () => () => undefined
    }
  });

  const getSnapshot = handlers.get(
    desktopFeatureAvailabilityIpcChannels.getSnapshot
  );
  const isSupported = handlers.get(
    desktopFeatureAvailabilityIpcChannels.isSupported
  );
  assert.ok(getSnapshot);
  assert.ok(isSupported);
  assert.throws(() => getSnapshot({ sender: untrustedSender }), /not trusted/);
  assert.throws(
    () => isSupported({ sender: trustedSender }, "../invalid"),
    /invalid/
  );
  assert.equal(
    isSupported({ sender: trustedSender }, "workspace.example"),
    true
  );

  registration.dispose();
  assert.equal(handlers.size, 0);
});
