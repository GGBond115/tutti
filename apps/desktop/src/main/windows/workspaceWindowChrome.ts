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
      // Keep the native application menu available as an Alt-key fallback,
      // while the custom workspace header exposes the user-facing Help entry.
      // This avoids stacking a classic menu row beneath the native title bar.
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
