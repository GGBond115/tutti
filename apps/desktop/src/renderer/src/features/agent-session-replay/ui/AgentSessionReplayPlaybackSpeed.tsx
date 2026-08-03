import { useState } from "react";
import {
  ArrowRightIcon,
  Button,
  PauseIcon,
  PlayIcon,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@tutti-os/ui-system";
import type {
  DesktopAgentSessionReplayPlaybackSpeed,
  DesktopSendAgentSessionReplayControlInput
} from "@shared/contracts/ipc";
import { useTranslation } from "@renderer/i18n";
import { Toast } from "@renderer/lib/toast";
import type {
  AgentSessionReplayNodeRuntime,
  AgentSessionReplayNodeSnapshot
} from "../services/agentSessionReplayNodeRuntime.ts";
import { resolveAgentSessionReplayControlAvailability } from "./agentSessionReplayPlaybackControls.ts";

const playbackSpeeds = [0.25, 0.5, 1, 2, 4] as const;

export function AgentSessionReplayPlaybackControls({
  runtime,
  snapshot
}: {
  runtime: AgentSessionReplayNodeRuntime;
  snapshot: AgentSessionReplayNodeSnapshot | null;
}): React.JSX.Element | null {
  const { t } = useTranslation();
  const [updating, setUpdating] = useState(false);

  if (!snapshot) {
    return null;
  }
  const { playback, status } = snapshot;
  const currentCheckpoint = status.currentCheckpoint ?? 0;
  const lastCheckpoint = Math.max(0, (status.totalCheckpoints ?? 1) - 1);
  const { canNext, canPause, canSetSpeed } =
    resolveAgentSessionReplayControlAvailability({
      currentCheckpoint,
      lastCheckpoint,
      phase: status.phase,
      playbackActive: playback.active,
      updating
    });

  const updateSpeed = (value: string): void => {
    const speed = playbackSpeeds.find(
      (candidate) => String(candidate) === value
    );
    if (!speed || speed === playback.speed || updating) {
      return;
    }
    setUpdating(true);
    void runtime
      .updatePlayback({
        command: "set-speed",
        speed
      })
      .catch(() =>
        Toast.Error(t("workspace.agentGui.sessionReplay.replay.speedFailed"))
      )
      .finally(() => setUpdating(false));
  };

  const sendControl = (
    command: DesktopSendAgentSessionReplayControlInput["command"]
  ): void => {
    if (updating) return;
    setUpdating(true);
    void runtime
      .sendControl(command)
      .catch(() =>
        Toast.Error(t("workspace.agentGui.sessionReplay.replay.controlFailed"))
      )
      .finally(() => setUpdating(false));
  };
  const elapsedDuration = formatReplayElapsedDuration(
    status.totalDurationMs === undefined
      ? playback.playbackElapsedMs
      : Math.min(playback.playbackElapsedMs, status.totalDurationMs)
  );
  const totalDuration =
    status.totalDurationMs === undefined
      ? "--:--"
      : formatReplayElapsedDuration(status.totalDurationMs);
  return (
    <div
      aria-label={t("workspace.agentGui.sessionReplay.replay.toolbar")}
      className="nodrag flex items-center gap-0.5 [-webkit-app-region:no-drag]"
      data-testid="agent-session-replay-playback-controls"
      role="toolbar"
    >
      <ReplayControlButton
        disabled={!canPause}
        label={t(
          playback.paused
            ? "workspace.agentGui.sessionReplay.replay.play"
            : "workspace.agentGui.sessionReplay.replay.pause"
        )}
        onClick={() => sendControl(playback.paused ? "resume" : "pause")}
      >
        {playback.paused ? (
          <PlayIcon aria-hidden="true" />
        ) : (
          <PauseIcon aria-hidden="true" />
        )}
      </ReplayControlButton>
      <ReplayControlButton
        disabled={!canNext}
        label={t("workspace.agentGui.sessionReplay.replay.next")}
        onClick={() => sendControl("next-checkpoint")}
      >
        <ArrowRightIcon aria-hidden="true" />
      </ReplayControlButton>
      <span
        aria-label={t("workspace.agentGui.sessionReplay.replay.checkpoint", {
          current: currentCheckpoint,
          total: lastCheckpoint
        })}
        className="min-w-10 px-1 text-center text-xs tabular-nums text-[var(--text-secondary)]"
      >
        {currentCheckpoint}/{lastCheckpoint}
      </span>
      <span
        aria-label={t("workspace.agentGui.sessionReplay.replay.elapsed", {
          elapsed: elapsedDuration,
          total: totalDuration
        })}
        className="min-w-24 px-1 text-center text-xs tabular-nums text-[var(--text-secondary)]"
      >
        {elapsedDuration} / {totalDuration}
      </span>
      <Select value={String(playback.speed)} onValueChange={updateSpeed}>
        <SelectTrigger
          aria-label={t("workspace.agentGui.sessionReplay.replay.speed")}
          className="h-7 min-w-16"
          disabled={!canSetSpeed}
          size="sm"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent style={{ zIndex: "var(--z-panel-popover)" }}>
          {playbackSpeeds.map((speed) => (
            <SelectItem key={speed} value={String(speed)}>
              {formatPlaybackSpeed(speed)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

function ReplayControlButton({
  children,
  disabled,
  label,
  onClick
}: {
  children: React.ReactNode;
  disabled: boolean;
  label: string;
  onClick(): void;
}): React.JSX.Element {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          aria-label={label}
          disabled={disabled}
          onClick={onClick}
          size="icon-sm"
          type="button"
          variant="ghost"
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="top">{label}</TooltipContent>
    </Tooltip>
  );
}

function formatPlaybackSpeed(
  speed: DesktopAgentSessionReplayPlaybackSpeed
): string {
  return `${speed}×`;
}

export function formatReplayElapsedDuration(elapsedMs: number): string {
  const totalSeconds = Math.max(0, Math.floor(elapsedMs / 1_000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}
