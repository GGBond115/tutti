import { useMemo, useSyncExternalStore } from "react";
import type { DesktopRuntimeApi } from "@preload/types";
import { createAgentSessionReplayNodeRuntime } from "../services/agentSessionReplayNodeRuntime.ts";
import { AgentSessionReplayPlaybackControls } from "./AgentSessionReplayPlaybackSpeed.tsx";
import { AgentSessionReplayStatus } from "./AgentSessionReplayStatus.tsx";
import { useAgentSessionReplayWorkspaceCoordinator } from "./AgentSessionReplayWorkspaceContext.tsx";

const subscribeToNothing = (): (() => void) => () => {};
const getEmptySnapshot = (): null => null;

export function AgentSessionReplayNodeRuntime({
  nodeId,
  runtimeApi
}: {
  nodeId: string;
  runtimeApi: Pick<
    DesktopRuntimeApi,
    | "getAgentSessionReplayPlayback"
    | "getAgentSessionReplayStatus"
    | "sendAgentSessionReplayControl"
    | "setAgentSessionReplayPlayback"
  >;
}): React.JSX.Element | null {
  const coordinator = useAgentSessionReplayWorkspaceCoordinator();
  const runtime = useMemo(
    () =>
      coordinator
        ? createAgentSessionReplayNodeRuntime({
            coordinator,
            nodeId,
            runtimeApi
          })
        : null,
    [coordinator, nodeId, runtimeApi]
  );
  const snapshot = useSyncExternalStore(
    runtime?.subscribe ?? subscribeToNothing,
    runtime?.getSnapshot ?? getEmptySnapshot,
    getEmptySnapshot
  );
  if (!runtime) return null;
  return (
    <>
      <AgentSessionReplayStatus status={snapshot?.status ?? null} />
      <AgentSessionReplayPlaybackControls
        runtime={runtime}
        snapshot={snapshot}
      />
    </>
  );
}
