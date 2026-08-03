import { useEffect, useRef } from "react";
import type { DesktopAgentSessionReplayStatus } from "@shared/contracts/ipc";
import { useTranslation } from "@renderer/i18n";
import { Toast } from "@renderer/lib/toast";
import { replayStatusVisibleError } from "./replayActionErrorMessage.ts";

export function AgentSessionReplayStatus({
  status
}: {
  status: DesktopAgentSessionReplayStatus | null;
}): null {
  const { t } = useTranslation();
  const notifiedPhaseRef =
    useRef<DesktopAgentSessionReplayStatus["phase"]>(undefined);

  useEffect(() => {
    if (!status?.phase || notifiedPhaseRef.current === status.phase) return;
    notifiedPhaseRef.current = status.phase;
    if (status.phase === "complete") {
      Toast.Success(
        t("workspace.agentGui.sessionReplay.replay.validationComplete")
      );
    } else if (status.phase === "failed") {
      Toast.Error(
        t("workspace.agentGui.sessionReplay.replay.validationFailed"),
        replayStatusVisibleError(status, (table) =>
          t("workspace.agentGui.sessionReplay.replay.stateMismatch", {
            table
          })
        )
      );
    }
  }, [status, t]);

  return null;
}
