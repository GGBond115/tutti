import {
  LocalComputerLinedIcon,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  WorktreeLinedIcon,
  WebIcon,
  cn
} from "@tutti-os/ui-system";
import styles from "../AgentGUINode.styles";
import type { AgentGUISessionLaunchMode } from "../model/agentSessionLaunchMode";

export function AgentSessionLaunchModeSelect(input: {
  labels: {
    launchMode: string;
    local: string;
    worktree: string;
    cloud: string;
  };
  availableModes: readonly AgentGUISessionLaunchMode[];
  mode: AgentGUISessionLaunchMode;
  onModeChange: (mode: AgentGUISessionLaunchMode) => void;
}): React.JSX.Element {
  return (
    <Select value={input.mode} onValueChange={input.onModeChange}>
      <SelectTrigger
        aria-label={input.labels.launchMode}
        className={cn("w-auto", styles.composerMenuTrigger)}
        data-testid="agent-gui-session-launch-mode"
      >
        <span className="flex min-w-0 items-center gap-2">
          <LaunchModeIcon mode={input.mode} />
          <span className="truncate">
            {launchModeLabel(input.mode, input.labels)}
          </span>
        </span>
      </SelectTrigger>
      <SelectContent align="start">
        <SelectItem value="local">
          <span className="flex items-center gap-2">
            <LaunchModeIcon mode="local" />
            {input.labels.local}
          </span>
        </SelectItem>
        {input.availableModes.includes("worktree") ? (
          <SelectItem value="worktree">
            <span className="flex items-center gap-2">
              <LaunchModeIcon mode="worktree" />
              {input.labels.worktree}
            </span>
          </SelectItem>
        ) : null}
        {input.availableModes.includes("cloud") ? (
          <SelectItem value="cloud">
            <span className="flex items-center gap-2">
              <LaunchModeIcon mode="cloud" />
              {input.labels.cloud}
            </span>
          </SelectItem>
        ) : null}
      </SelectContent>
    </Select>
  );
}

function LaunchModeIcon({
  mode
}: {
  mode: AgentGUISessionLaunchMode;
}): React.JSX.Element {
  if (mode === "worktree") {
    return (
      <WorktreeLinedIcon
        aria-hidden="true"
        data-agent-session-launch-icon="worktree"
        size={15}
      />
    );
  }
  if (mode === "cloud") {
    return (
      <WebIcon
        aria-hidden="true"
        data-agent-session-launch-icon="cloud"
        size={15}
      />
    );
  }
  return (
    <LocalComputerLinedIcon
      aria-hidden="true"
      data-agent-session-launch-icon="local"
      size={15}
    />
  );
}

function launchModeLabel(
  mode: AgentGUISessionLaunchMode,
  labels: { local: string; worktree: string; cloud: string }
): string {
  if (mode === "worktree") return labels.worktree;
  if (mode === "cloud") return labels.cloud;
  return labels.local;
}
