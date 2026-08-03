import type { AgentGUIProps } from "@tutti-os/agent-gui";
import { useCallback } from "react";
import { createAgentSessionReplayLauncher } from "../../agent-session-replay/services/agentSessionReplayLauncher.ts";
import { AgentSessionReplayComposerAccessory } from "../../agent-session-replay/ui/AgentSessionReplayComposerAccessory.tsx";
import { AgentSessionReplayNodeRuntime } from "../../agent-session-replay/ui/AgentSessionReplayNodeRuntime.tsx";
import type { DesktopAgentGUIWorkbenchBodyProps } from "./desktopAgentGUIWorkbenchModel.ts";

type ComposerFooterAccessory = NonNullable<
  AgentGUIProps["renderSlots"]["composerFooterAccessory"]
>;

export function useDesktopAgentGUIComposerFooterAccessory(input: {
  agentSessionReplayService: DesktopAgentGUIWorkbenchBodyProps["agentSessionReplayService"];
  nodeId: string;
  runtimeApi: DesktopAgentGUIWorkbenchBodyProps["runtimeApi"];
  sessionRecordingEnabled: boolean;
  workspaceId: string;
}): ComposerFooterAccessory {
  const runtimeApi = input.runtimeApi;
  return useCallback(
    (composer) => (
      <>
        {runtimeApi ? (
          <AgentSessionReplayNodeRuntime
            nodeId={input.nodeId}
            runtimeApi={runtimeApi}
          />
        ) : null}
        {input.sessionRecordingEnabled && input.agentSessionReplayService ? (
          <AgentSessionReplayComposerAccessory
            composer={composer}
            launcher={
              runtimeApi
                ? createAgentSessionReplayLauncher({
                    runtimeApi,
                    service: input.agentSessionReplayService
                  })
                : undefined
            }
            revealCassette={
              runtimeApi
                ? (cassetteId) =>
                    runtimeApi.revealAgentSessionReplayCassette({
                      cassetteId,
                      workspaceId: input.workspaceId
                    })
                : undefined
            }
            service={input.agentSessionReplayService}
          />
        ) : null}
      </>
    ),
    [
      input.agentSessionReplayService,
      input.nodeId,
      input.sessionRecordingEnabled,
      input.workspaceId,
      runtimeApi
    ]
  );
}
