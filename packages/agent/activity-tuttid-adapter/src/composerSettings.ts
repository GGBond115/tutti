import type { AgentActivitySessionSettings } from "@tutti-os/agent-activity-core";
import type { AgentSessionComposerSettings } from "@tutti-os/client-tuttid-ts";

export function tuttiAgentSessionComposerSettingsFromActivity(
  settings: AgentActivitySessionSettings | null | undefined
): AgentSessionComposerSettings {
  return {
    ...(settings?.model !== undefined ? { model: settings.model } : {}),
    ...(settings?.permissionModeId !== undefined
      ? { permissionModeId: settings.permissionModeId }
      : {}),
    ...(settings?.planMode !== undefined
      ? { planMode: settings.planMode }
      : {}),
    ...(settings?.browserUse !== undefined
      ? { browserUse: settings.browserUse }
      : {}),
    ...(settings?.reasoningEffort !== undefined
      ? { reasoningEffort: settings.reasoningEffort }
      : {}),
    ...(settings?.speed !== undefined ? { speed: settings.speed } : {})
  };
}
