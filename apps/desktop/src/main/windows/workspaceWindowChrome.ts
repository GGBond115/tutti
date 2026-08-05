export interface WorkspaceWindowChromeOptions {
  autoHideMenuBar?: boolean;
  frame?: boolean;
  maximizable?: boolean;
}

export function resolveWorkspaceWindowChromeOptions(
  platform: NodeJS.Platform,
  windowKind: "agent" | "workspace"
): WorkspaceWindowChromeOptions {
  if (platform === "win32") {
    return {
      autoHideMenuBar: true
    };
  }

  if (windowKind !== "agent") {
    return {};
  }

  return {
    frame: false,
    maximizable: false
  };
}
