import { useEffect, useRef } from "react";
import type { AgentGUIRuntime } from "@tutti-os/agent-gui";
import { installAgentSessionReplayWorkspaceBridge } from "../services/agentSessionReplayWorkspaceBridge.ts";
import type {
  AgentSessionReplayNodeLaunchRequest,
  AgentSessionReplayWorkspaceCoordinator
} from "../services/agentSessionReplayWorkspaceCoordinator.ts";
import { useAgentSessionReplayWorkspaceCoordinator } from "./AgentSessionReplayWorkspaceContext.tsx";

export function AgentSessionReplayWorkspaceBinding({
  arrangeNodes,
  coordinator,
  launchNode
}: {
  arrangeNodes(nodeIds: readonly string[]): void;
  coordinator: AgentSessionReplayWorkspaceCoordinator;
  launchNode(
    request: AgentSessionReplayNodeLaunchRequest
  ): Promise<string | null>;
}): null {
  const arrangeNodesRef = useRef(arrangeNodes);
  const launchNodeRef = useRef(launchNode);
  useEffect(() => {
    arrangeNodesRef.current = arrangeNodes;
    launchNodeRef.current = launchNode;
  }, [arrangeNodes, launchNode]);

  useEffect(() => {
    const binding = installAgentSessionReplayWorkspaceBridge({
      arrangeNodes: (nodeIds) => arrangeNodesRef.current(nodeIds),
      coordinator,
      launchNode: (request) => launchNodeRef.current(request)
    });
    return () => {
      binding.dispose();
    };
  }, [coordinator]);
  return null;
}

export function useAgentSessionReplayNodeReadiness(input: {
  agentActivityRuntime: Pick<AgentGUIRuntime, "getSession" | "subscribe">;
  nodeId: string;
  selectedAgentSessionId: string | null;
  workspaceId: string;
}): void {
  const coordinator = useAgentSessionReplayWorkspaceCoordinator();
  useEffect(() => {
    if (!coordinator) return;
    const report = (): void => {
      if (!coordinator.getCassetteForNode(input.nodeId)) return;
      coordinator.reportNodeMounted(input.nodeId, true);
      coordinator.reportSelectedAgentSession(
        input.nodeId,
        input.selectedAgentSessionId
      );
    };
    report();
    const unsubscribe = coordinator.subscribe(report);
    return () => {
      unsubscribe();
      if (coordinator.getCassetteForNode(input.nodeId)) {
        coordinator.reportNodeMounted(input.nodeId, false);
      }
    };
  }, [coordinator, input.nodeId, input.selectedAgentSessionId]);

  useEffect(() => {
    const selectedAgentSessionId = input.selectedAgentSessionId?.trim();
    if (!coordinator || !selectedAgentSessionId) {
      return;
    }
    let disposed = false;
    let checking = false;
    let retryRequested = false;
    const hydrate = (): void => {
      if (disposed) return;
      const binding = coordinator.getCassetteForNode(input.nodeId);
      if (binding && selectedAgentSessionId !== binding.rootAgentSessionId) {
        return;
      }
      if (checking) {
        retryRequested = true;
        return;
      }
      checking = true;
      void input.agentActivityRuntime
        .getSession(input.workspaceId, selectedAgentSessionId)
        .then((session) => {
          if (disposed) return;
          if (binding) {
            coordinator.reportSessionCanonicalObservation(
              selectedAgentSessionId,
              {
                messageVersion: session.messageVersion,
                updatedAtUnixMs: session.updatedAtUnixMs
              }
            );
          }
        })
        .catch(() => undefined)
        .finally(() => {
          checking = false;
          if (retryRequested) {
            retryRequested = false;
            hydrate();
          }
        });
    };
    const unsubscribe = input.agentActivityRuntime.subscribe(
      input.workspaceId,
      hydrate
    );
    const unsubscribeCoordinator = coordinator.subscribe(hydrate);
    hydrate();
    return () => {
      disposed = true;
      unsubscribe();
      unsubscribeCoordinator();
    };
  }, [
    input.agentActivityRuntime,
    coordinator,
    input.nodeId,
    input.selectedAgentSessionId,
    input.workspaceId
  ]);
}
