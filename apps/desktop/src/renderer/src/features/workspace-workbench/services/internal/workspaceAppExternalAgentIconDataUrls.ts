import { resolveAgentGUIProviderCatalogIdentity } from "@tutti-os/agent-gui/provider-catalog";
import claudeCodeIconDataUrl from "../../../../assets/workspace-app-external/agent-mentions/claudecode.webp?inline";
import codexIconDataUrl from "../../../../assets/workspace-app-external/agent-mentions/codex.webp?inline";
import cursorIconDataUrl from "../../../../assets/workspace-app-external/agent-mentions/cursor.webp?inline";
import openclawIconDataUrl from "../../../../assets/workspace-app-external/agent-mentions/openclaw.webp?inline";
import opencodeIconDataUrl from "../../../../assets/workspace-app-external/agent-mentions/opencode.webp?inline";
import tuttiIconDataUrl from "../../../../assets/workspace-app-external/agent-mentions/tutti.webp?inline";

export const workspaceAppExternalAgentIconDataUrlsByIconKey: Readonly<
  Record<string, string>
> = {
  "claude-code": claudeCodeIconDataUrl,
  codex: codexIconDataUrl,
  cursor: cursorIconDataUrl,
  openclaw: openclawIconDataUrl,
  opencode: opencodeIconDataUrl,
  tutti: tuttiIconDataUrl
};

export function serializeWorkspaceAppExternalAgentIconUrl(
  iconUrl: string | null | undefined,
  agentProviderId: string | null | undefined
): string {
  const normalizedIconUrl = iconUrl?.trim() ?? "";
  if (!normalizedIconUrl.startsWith("file:")) {
    return normalizedIconUrl;
  }
  const iconKey =
    resolveAgentGUIProviderCatalogIdentity(agentProviderId)?.iconKey ?? "";
  return (
    workspaceAppExternalAgentIconDataUrlsByIconKey[iconKey] ?? normalizedIconUrl
  );
}
