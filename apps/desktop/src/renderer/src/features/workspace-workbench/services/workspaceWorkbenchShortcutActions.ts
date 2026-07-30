import type { WorkspaceAgentProvider } from "@tutti-os/client-tuttid-ts";
import {
  selectFocusedWorkbenchNode,
  type WorkbenchHostHandle
} from "@tutti-os/workbench-surface";
import { dispatchAgentGuiWorkbenchCommand } from "@tutti-os/agent-gui/workbench";
import { requestWorkspaceAgentGuiLaunch } from "@renderer/features/workspace-agent/services/workspaceAgentGuiLaunchCoordinator.ts";
import { workspaceAgentGuiNodeID } from "./workspaceAgentGuiLaunch.ts";
import { createWorkspaceAgentGuiSameTypeWindowLaunchRequest } from "./workspaceWorkbenchShortcutAgentLaunch.ts";
export {
  resolveWorkspaceAgentGuiNodeAgentTargetId,
  resolveWorkspaceAgentGuiNodeProvider
} from "./workspaceWorkbenchShortcutAgentLaunch.ts";

type WorkspaceWorkbenchNode = ReturnType<
  WorkbenchHostHandle["getSnapshot"]
>["nodes"][number];

export function resolveActiveWorkspaceWorkbenchNode(
  host: WorkbenchHostHandle
): WorkspaceWorkbenchNode | null {
  return selectFocusedWorkbenchNode(host.getSnapshot());
}

export function isWorkspaceAgentGuiWorkbenchNode(
  node: WorkspaceWorkbenchNode
): boolean {
  return node.data.typeId === workspaceAgentGuiNodeID;
}

export async function openWorkspaceWorkbenchAgentConversationShortcut(input: {
  defaultProvider: WorkspaceAgentProvider;
  host: WorkbenchHostHandle;
  workspaceId: string;
}): Promise<void> {
  const activeNode = resolveActiveWorkspaceWorkbenchNode(input.host);
  if (activeNode && isWorkspaceAgentGuiWorkbenchNode(activeNode)) {
    input.host.focusNode(activeNode.id);
    dispatchAgentGuiWorkbenchCommand({
      instanceId: activeNode.data.instanceId,
      type: "new-conversation"
    });
    return;
  }
  await requestWorkspaceAgentGuiLaunch({
    provider: input.defaultProvider,
    workspaceId: input.workspaceId
  });
}

export async function openWorkspaceWorkbenchSameTypeWindowShortcut(input: {
  defaultProvider: WorkspaceAgentProvider;
  host: WorkbenchHostHandle;
}): Promise<void> {
  const activeNode = resolveActiveWorkspaceWorkbenchNode(input.host);
  if (!activeNode) {
    return;
  }
  if (isWorkspaceAgentGuiWorkbenchNode(activeNode)) {
    await input.host.launchNode(
      createWorkspaceAgentGuiSameTypeWindowLaunchRequest(
        activeNode,
        input.defaultProvider
      )
    );
    return;
  }
  await input.host.launchNode({
    ...(activeNode.data.dockEntryId
      ? { dockEntryId: activeNode.data.dockEntryId }
      : {}),
    payload: activeNode.data.snapshotNodeState,
    reason: "host",
    typeId: activeNode.data.typeId
  });
}
