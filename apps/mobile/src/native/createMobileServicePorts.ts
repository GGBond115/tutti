import { AppState, DeviceEventEmitter } from "react-native";
import { deviceLink, mobileSecurity } from "./mobileNative";
import {
  sendEmailCode,
  signInWithGitHub,
  verifyEmailCode
} from "../services/accountClient";
import {
  claimPairing,
  connectPairedDevice,
  getPairingChallenge,
  listDevices,
  listPairings,
  parsePairingQR,
  registerCurrentDevice
} from "../services/pairingClient";
import { createRemoteTuttidClient } from "../services/remoteTuttidClient";
import type { MobileServicePorts } from "../services/servicePorts";
import { parseAgentLiveDeliveries } from "./agentLiveNativeBridge";

const AGENT_LIVE_EVENT_NAME = "TuttiDeviceLinkAgentLive";

export function createMobileServicePorts(): MobileServicePorts {
  return {
    account: {
      sendEmailCode,
      signInWithGitHub,
      verifyEmailCode
    },
    clock: {
      now: () => Date.now(),
      schedule(delayMs, callback) {
        const timer = setTimeout(callback, delayMs);
        return { cancel: () => clearTimeout(timer) };
      }
    },
    deviceLink: {
      closeLink: () => deviceLink.closeLink(),
      requestAgentHTTP: (method, path, body, timeoutMillis) =>
        deviceLink.requestAgentHTTP(method, path, body, timeoutMillis),
      subscribeAgentLive(workspaceId, listener) {
        let active = true;
        const subscription = DeviceEventEmitter.addListener(
          AGENT_LIVE_EVENT_NAME,
          (payload: string) => {
            if (!active) return;
            for (const delivery of parseAgentLiveDeliveries(
              workspaceId,
              payload
            )) {
              listener(delivery);
            }
          }
        );
        void deviceLink.startAgentLive(workspaceId).catch(() => {
          if (active) {
            listener({
              kind: "connection",
              reason: "subscribe_failed",
              status: "disconnected"
            });
          }
        });
        return {
          close() {
            if (!active) return;
            active = false;
            subscription.remove();
            void deviceLink.stopAgentLive().catch(() => undefined);
          }
        };
      }
    },
    deviceSecurity: mobileSecurity,
    lifecycle: {
      subscribe(listener) {
        const subscription = AppState.addEventListener("change", (state) =>
          listener(state === "active")
        );
        return () => subscription.remove();
      }
    },
    pairing: {
      claimPairing,
      connectPairedDevice,
      getPairingChallenge,
      listDevices,
      listPairings,
      parsePairingQR,
      registerCurrentDevice
    },
    sessionStorage: mobileSecurity,
    createRemoteClient: () => createRemoteTuttidClient(deviceLink)
  };
}
