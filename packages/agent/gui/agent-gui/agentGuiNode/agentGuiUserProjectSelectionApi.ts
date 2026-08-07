import type { WorkspaceUserProjectApi } from "@tutti-os/workspace-user-project/contracts";
import type { AgentHostUserProjectsApi } from "../../host/agentHostApi";

export function createAgentGUIUserProjectSelectionApi({
  selectProjectDirectory,
  userProjects
}: {
  selectProjectDirectory?: () => Promise<{ path: string } | null>;
  userProjects: AgentHostUserProjectsApi | null | undefined;
}): WorkspaceUserProjectApi | null {
  if (!userProjects) {
    if (!selectProjectDirectory) {
      return null;
    }
    return {
      list: async () => ({ projects: [] }),
      prepareSelection: async ({ selectedPath }) => {
        const path = selectedPath?.trim() ?? "";
        return {
          isSelectedPathMissing: false,
          projects: path
            ? [{ id: path, label: path, path, pinnedAtUnixMs: 0 }]
            : [],
          selection: { kind: "none" }
        };
      },
      selectDirectory: selectProjectDirectory
    };
  }
  return {
    ...userProjects,
    selectDirectory: selectProjectDirectory
  };
}
