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
      // Keep the native application menu visible on Windows. The Help menu
      // contains the developer log export entry, so hiding the menu behind
      // the Alt key makes that support path effectively undiscoverable.
      autoHideMenuBar: false
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
