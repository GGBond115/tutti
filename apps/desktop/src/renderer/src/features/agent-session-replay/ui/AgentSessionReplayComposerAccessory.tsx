import { useEffect, useState, useSyncExternalStore } from "react";
import type { AgentGUIProps } from "@tutti-os/agent-gui";
import type { AgentSessionRecording } from "@tutti-os/client-tuttid-ts";
import type { DesktopAgentSessionReplayLaunchPlaybackMode } from "@shared/contracts/ipc";
import {
  Badge,
  Button,
  CheckIcon,
  Checkbox,
  CloseIcon,
  ConfirmationDialog,
  DeleteIcon,
  DownloadIcon,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  EditIcon,
  Input,
  LocateFolderIcon,
  PauseIcon,
  PlayIcon,
  Popover,
  PopoverContent,
  PopoverTrigger,
  ScrollArea,
  TruncatingPillLabel,
  ViewListLinedIcon
} from "@tutti-os/ui-system";
import { useTranslation } from "@renderer/i18n";
import { Toast } from "@renderer/lib/toast";
import {
  agentSessionReplayBatchSelectionState,
  canSelectAgentSessionReplayBatch,
  selectedAgentSessionReplayCassetteIds
} from "../services/agentSessionReplayBatchSelection.ts";
import type { AgentSessionReplayLauncher } from "../services/agentSessionReplayLauncher.ts";
import type {
  AgentSessionReplayPrerequisites,
  AgentSessionReplayService
} from "../services/agentSessionReplayService.ts";
import { replayActionErrorMessage } from "./replayActionErrorMessage.ts";

type ComposerContext = Parameters<
  NonNullable<AgentGUIProps["renderSlots"]["composerFooterAccessory"]>
>[0];

export function AgentSessionReplayComposerAccessory({
  composer,
  launcher,
  revealCassette,
  service
}: {
  composer: ComposerContext;
  launcher?: AgentSessionReplayLauncher;
  revealCassette?: (cassetteId: string) => Promise<void>;
  service: AgentSessionReplayService;
}): React.JSX.Element | null {
  const { t } = useTranslation();
  const snapshot = useSyncExternalStore(
    service.subscribe,
    service.getSnapshot,
    service.getSnapshot
  );
  useEffect(() => {
    void service.refresh();
  }, [service]);
  useEffect(() => {
    if (!snapshot.activeRecording) {
      return;
    }
    const timer = window.setInterval(
      () => void service.refresh({ background: true }),
      1_000
    );
    return () => window.clearInterval(timer);
  }, [service, snapshot.activeRecording?.id]);

  const agentTargetId =
    composer.selectedAgentTarget?.agentTargetId ??
    composer.selectedAgentTarget?.targetId ??
    "";
  const replayPrerequisites = replayPrerequisitesFor(composer.composerSettings);

  return (
    <div
      className="nodrag inline-flex min-w-0 shrink-0 items-center gap-1"
      data-testid="agent-session-replay-tools"
    >
      <RecordingToolbar
        agentSessionId={composer.agentSessionId ?? null}
        agentTargetId={agentTargetId}
        disabled={snapshot.loading}
        replayPrerequisites={replayPrerequisites}
        service={service}
        status={snapshot.activeRecording?.status ?? null}
        recordingId={snapshot.activeRecording?.id ?? null}
      />
      <RecordingList
        disabled={snapshot.loading}
        launcher={launcher}
        recordings={snapshot.recordings}
        revealCassette={revealCassette}
        service={service}
      />
      {snapshot.error ? (
        <span className="sr-only" role="alert">
          {t("workspace.agentGui.sessionReplay.failed")}
        </span>
      ) : null}
    </div>
  );
}

function RecordingToolbar({
  agentSessionId,
  agentTargetId,
  disabled,
  replayPrerequisites,
  recordingId,
  service,
  status
}: {
  agentSessionId: string | null;
  agentTargetId: string;
  disabled: boolean;
  replayPrerequisites: AgentSessionReplayPrerequisites | null;
  recordingId: string | null;
  service: AgentSessionReplayService;
  status: string | null;
}): React.JSX.Element {
  const { t } = useTranslation();
  const describeError = (error: unknown): string =>
    replayActionErrorMessage(error, (table) =>
      t("workspace.agentGui.sessionReplay.replay.stateMismatch", { table })
    );
  if (!recordingId) {
    return (
      <Button
        aria-label={t("workspace.agentGui.sessionReplay.record.start")}
        data-testid="agent-session-recording-start"
        disabled={disabled || !agentTargetId || !replayPrerequisites}
        size="icon-sm"
        variant="ghost"
        onClick={() =>
          void service
            .startRecording({
              agentSessionId,
              agentTargetId,
              replayPrerequisites: replayPrerequisites!
            })
            .catch((error) =>
              Toast.Error(
                t("workspace.agentGui.sessionReplay.failed"),
                describeError(error)
              )
            )
        }
      >
        <span aria-hidden="true" className="size-3 rounded-full bg-current" />
      </Button>
    );
  }
  return (
    <>
      <Button
        aria-label={t("workspace.agentGui.sessionReplay.record.stop")}
        data-testid="agent-session-recording-stop"
        disabled={disabled || status !== "recording"}
        size="icon-sm"
        variant="ghost"
        onClick={() =>
          void service
            .completeRecording(recordingId)
            .catch((error) =>
              Toast.Error(
                t("workspace.agentGui.sessionReplay.failed"),
                describeError(error)
              )
            )
        }
      >
        <span aria-hidden="true" className="size-3 rounded-[2px] bg-current" />
      </Button>
      <Button
        aria-label={t("workspace.agentGui.sessionReplay.record.cancel")}
        data-testid="agent-session-recording-cancel"
        disabled={disabled}
        size="icon-sm"
        variant="ghost"
        onClick={() =>
          void service
            .cancelRecording(recordingId)
            .catch((error) =>
              Toast.Error(
                t("workspace.agentGui.sessionReplay.failed"),
                describeError(error)
              )
            )
        }
      >
        <CloseIcon aria-hidden="true" className="size-3" />
      </Button>
    </>
  );
}

function replayPrerequisitesFor(
  settings: ComposerContext["composerSettings"]
): AgentSessionReplayPrerequisites | null {
  const model = settingValue(
    settings.selectedModelValue,
    settings.draftSettings.model,
    settings.effectiveModelValue
  );
  const permissionModeId = settingValue(
    settings.selectedPermissionModeValue,
    settings.draftSettings.permissionModeId
  );
  const reasoningEffort = settingValue(
    settings.selectedReasoningEffortValue,
    settings.draftSettings.reasoningEffort
  );
  const speed = settingValue(
    settings.selectedSpeedValue,
    settings.draftSettings.speed
  );
  if (!model || !permissionModeId || !reasoningEffort || !speed) {
    return null;
  }
  return {
    composerDefaults: { model, permissionModeId, reasoningEffort, speed }
  };
}

function settingValue(
  ...values: readonly (string | null | undefined)[]
): string | null {
  for (const value of values) {
    if (typeof value === "string" && value.trim()) {
      return value.trim();
    }
  }
  return null;
}

function RecordingList({
  disabled,
  launcher,
  recordings,
  revealCassette,
  service
}: {
  disabled: boolean;
  launcher?: AgentSessionReplayLauncher;
  recordings: readonly AgentSessionRecording[];
  revealCassette?: (cassetteId: string) => Promise<void>;
  service: AgentSessionReplayService;
}): React.JSX.Element {
  const { t } = useTranslation();
  const describeError = (error: unknown): string =>
    replayActionErrorMessage(error, (table) =>
      t("workspace.agentGui.sessionReplay.replay.stateMismatch", { table })
    );
  const [open, setOpen] = useState(false);
  const [launching, setLaunching] = useState(false);
  const [batchSelecting, setBatchSelecting] = useState(false);
  const [selectedRecordingIds, setSelectedRecordingIds] = useState<
    ReadonlySet<string>
  >(new Set());
  const [editingRecordingId, setEditingRecordingId] = useState<string | null>(
    null
  );
  const [draftName, setDraftName] = useState("");
  const [renamingRecordingId, setRenamingRecordingId] = useState<string | null>(
    null
  );
  const [deleteTarget, setDeleteTarget] = useState<{
    id: string;
    name: string;
  } | null>(null);
  const [deletingRecordingId, setDeletingRecordingId] = useState<string | null>(
    null
  );

  const exitBatchSelection = (): void => {
    setBatchSelecting(false);
    setSelectedRecordingIds(new Set());
  };
  const launchReplay = (
    cassetteIds: readonly string[],
    playbackMode: DesktopAgentSessionReplayLaunchPlaybackMode
  ): void => {
    if (!launcher || launching || cassetteIds.length === 0) return;
    const count = cassetteIds.length;
    setLaunching(true);
    exitBatchSelection();
    setOpen(false);
    const toast = Toast.Loading(
      count === 1
        ? t("workspace.agentGui.sessionReplay.replay.launching")
        : t("workspace.agentGui.sessionReplay.replay.launchingBatch", { count })
    );
    void launcher
      .launch(cassetteIds, playbackMode)
      .then(({ completion }) => {
        toast.resolve(
          count === 1
            ? t("workspace.agentGui.sessionReplay.replay.opened")
            : t("workspace.agentGui.sessionReplay.replay.openedBatch", {
                count
              })
        );
        setLaunching(false);
        void completion.catch(() => undefined);
      })
      .catch((error) => {
        toast.reject(
          t("workspace.agentGui.sessionReplay.failed"),
          describeError(error)
        );
        setLaunching(false);
      });
  };
  const selectedCassetteIds = selectedAgentSessionReplayCassetteIds(
    recordings,
    selectedRecordingIds
  );
  const canSelectBatch = canSelectAgentSessionReplayBatch(recordings);
  const showCassetteInFinder = (cassetteId: string): void => {
    if (!revealCassette) return;
    void revealCassette(cassetteId).catch((error) =>
      Toast.Error(
        t("workspace.agentGui.sessionReplay.record.revealFailed"),
        describeError(error)
      )
    );
  };
  const importSelectedCassettes = (): void => {
    if (disabled) return;
    void service
      .importCassettes()
      .then((result) => {
        if (result.outcome === "canceled") return;
        if (result.outcome === "complete") {
          Toast.Success(
            t("workspace.agentGui.sessionReplay.record.imported", {
              count: result.importedCount
            })
          );
        } else if (result.outcome === "partial") {
          Toast.Error(
            t("workspace.agentGui.sessionReplay.record.importPartial", {
              failedCount: result.failedCount,
              importedCount: result.importedCount
            })
          );
        } else {
          Toast.Error(
            t("workspace.agentGui.sessionReplay.record.importAllFailed", {
              count: result.failedCount
            })
          );
        }
      })
      .catch((error) =>
        Toast.Error(
          t("workspace.agentGui.sessionReplay.record.importFailed"),
          describeError(error)
        )
      );
  };
  const renameRecording = (recordingId: string): void => {
    const name = draftName.trim();
    if (!name || renamingRecordingId) return;
    setRenamingRecordingId(recordingId);
    void service
      .renameRecording(recordingId, name)
      .then(() => {
        setEditingRecordingId(null);
        setDraftName("");
      })
      .catch((error) =>
        Toast.Error(
          t("workspace.agentGui.sessionReplay.record.renameFailed"),
          describeError(error)
        )
      )
      .finally(() => setRenamingRecordingId(null));
  };
  const deleteRecording = (): void => {
    if (!deleteTarget || deletingRecordingId) return;
    setDeletingRecordingId(deleteTarget.id);
    void service
      .deleteRecording(deleteTarget.id)
      .then(() => setDeleteTarget(null))
      .catch((error) =>
        Toast.Error(
          t("workspace.agentGui.sessionReplay.record.deleteFailed"),
          describeError(error)
        )
      )
      .finally(() => setDeletingRecordingId(null));
  };

  return (
    <>
      <Popover
        open={open}
        onOpenChange={(nextOpen) => {
          setOpen(nextOpen);
          if (!nextOpen) exitBatchSelection();
        }}
      >
        <PopoverTrigger asChild>
          <Button
            aria-label={t("workspace.agentGui.sessionReplay.list")}
            size="icon-sm"
            variant="ghost"
          >
            <ViewListLinedIcon aria-hidden="true" className="size-3.5" />
          </Button>
        </PopoverTrigger>
        <PopoverContent
          align="start"
          className="nodrag w-72 overflow-hidden p-2 [-webkit-app-region:no-drag]"
          side="top"
        >
          <div className="flex items-center justify-between gap-2 px-2 pb-2">
            <p className="text-xs font-medium">
              {t("workspace.agentGui.sessionReplay.list")}
            </p>
            {batchSelecting ? (
              <Button
                className="text-xs font-medium"
                size="sm"
                variant="ghost"
                onClick={exitBatchSelection}
              >
                {t("workspace.agentGui.sessionReplay.replay.batchCancel")}
              </Button>
            ) : (
              <div className="flex items-center gap-1">
                <Button
                  className="gap-1 text-xs font-medium"
                  disabled={disabled || launching}
                  size="sm"
                  variant="ghost"
                  onClick={importSelectedCassettes}
                >
                  <DownloadIcon aria-hidden="true" className="size-3" />
                  {t("workspace.agentGui.sessionReplay.record.import")}
                </Button>
                {canSelectBatch ? (
                  <Button
                    className="text-xs font-medium"
                    disabled={disabled || launching}
                    size="sm"
                    variant="ghost"
                    onClick={() => setBatchSelecting(true)}
                  >
                    {t("workspace.agentGui.sessionReplay.replay.batchSelect")}
                  </Button>
                ) : null}
              </div>
            )}
          </div>
          <ScrollArea viewportClassName="max-h-64">
            {recordings.length === 0 ? (
              <p className="px-2 py-3 text-xs text-[var(--text-secondary)]">
                {t("workspace.agentGui.sessionReplay.empty")}
              </p>
            ) : (
              <div className="flex flex-col gap-1">
                {recordings.map((recording) => {
                  const selection = agentSessionReplayBatchSelectionState(
                    recording,
                    recordings,
                    selectedRecordingIds
                  );
                  const batchEligible =
                    selection.disabledReason !== "ineligible";
                  return (
                    <div
                      key={recording.id}
                      className="flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5"
                    >
                      {batchSelecting ? (
                        batchEligible ? (
                          <Checkbox
                            aria-label={t(
                              "workspace.agentGui.sessionReplay.replay.batchSelectRecording",
                              { name: recording.name }
                            )}
                            checked={selection.selected}
                            disabled={
                              selection.disabledReason ===
                              "root-session-conflict"
                            }
                            onCheckedChange={(checked) => {
                              setSelectedRecordingIds((current) => {
                                const next = new Set(current);
                                if (checked === true) {
                                  next.add(recording.id);
                                } else {
                                  next.delete(recording.id);
                                }
                                return next;
                              });
                            }}
                          />
                        ) : (
                          <span
                            aria-hidden="true"
                            className="size-4 shrink-0"
                          />
                        )
                      ) : null}
                      {batchSelecting ? (
                        <div className="min-w-0 flex-1">
                          <TruncatingPillLabel
                            className="block w-full text-xs"
                            tooltip={recording.name}
                            withTooltipProvider={false}
                          >
                            {recording.name}
                          </TruncatingPillLabel>
                          {selection.disabledReason ===
                          "root-session-conflict" ? (
                            <p className="truncate text-[10px] text-[var(--text-secondary)]">
                              {t(
                                "workspace.agentGui.sessionReplay.replay.batchSameSession"
                              )}
                            </p>
                          ) : null}
                        </div>
                      ) : editingRecordingId === recording.id ? (
                        <>
                          <Input
                            aria-label={t(
                              "workspace.agentGui.sessionReplay.record.rename"
                            )}
                            autoFocus
                            className="h-7 min-w-0 flex-1 text-xs"
                            maxLength={120}
                            value={draftName}
                            onChange={(event) =>
                              setDraftName(event.target.value)
                            }
                            onKeyDown={(event) => {
                              if (event.key === "Enter") {
                                renameRecording(recording.id);
                              } else if (event.key === "Escape") {
                                setEditingRecordingId(null);
                              }
                            }}
                          />
                          <Button
                            aria-label={t(
                              "workspace.agentGui.sessionReplay.record.renameSave"
                            )}
                            disabled={
                              !draftName.trim() ||
                              renamingRecordingId === recording.id
                            }
                            size="icon-sm"
                            variant="ghost"
                            onClick={() => renameRecording(recording.id)}
                          >
                            <CheckIcon aria-hidden="true" className="size-3" />
                          </Button>
                          <Button
                            aria-label={t(
                              "workspace.agentGui.sessionReplay.record.renameCancel"
                            )}
                            disabled={renamingRecordingId === recording.id}
                            size="icon-sm"
                            variant="ghost"
                            onClick={() => setEditingRecordingId(null)}
                          >
                            <CloseIcon aria-hidden="true" className="size-3" />
                          </Button>
                        </>
                      ) : (
                        <>
                          <TruncatingPillLabel
                            className="min-w-0 flex-1 text-xs"
                            tooltip={recording.name}
                            withTooltipProvider={false}
                          >
                            {recording.name}
                          </TruncatingPillLabel>
                          {recording.status === "complete" &&
                          recording.cassetteId ? (
                            <Button
                              aria-label={t(
                                "workspace.agentGui.sessionReplay.record.rename"
                              )}
                              disabled={disabled}
                              size="icon-sm"
                              variant="ghost"
                              onClick={() => {
                                setEditingRecordingId(recording.id);
                                setDraftName(recording.name);
                              }}
                            >
                              <EditIcon aria-hidden="true" className="size-3" />
                            </Button>
                          ) : null}
                        </>
                      )}
                      {recording.status === "complete" ? null : (
                        <Badge variant="outline">{recording.status}</Badge>
                      )}
                      {!batchSelecting &&
                      recording.status === "complete" &&
                      recording.cassetteId ? (
                        <Button
                          aria-label={t(
                            "workspace.agentGui.sessionReplay.record.reveal"
                          )}
                          disabled={disabled || !revealCassette}
                          size="icon-sm"
                          variant="ghost"
                          onClick={() =>
                            showCassetteInFinder(recording.cassetteId!)
                          }
                        >
                          <LocateFolderIcon
                            aria-hidden="true"
                            className="size-3"
                          />
                        </Button>
                      ) : null}
                      {!batchSelecting &&
                      recording.status === "complete" &&
                      recording.cassetteId ? (
                        <ReplayLaunchMenu
                          disabled={disabled || !launcher || launching}
                          iconOnly
                          onLaunch={(playbackMode) =>
                            launchReplay([recording.cassetteId!], playbackMode)
                          }
                        />
                      ) : null}
                      {!batchSelecting ? (
                        <Button
                          aria-label={t(
                            "workspace.agentGui.sessionReplay.record.delete"
                          )}
                          disabled={
                            disabled ||
                            [
                              "preparing",
                              "ready",
                              "recording",
                              "finalizing"
                            ].includes(recording.status)
                          }
                          size="icon-sm"
                          variant="ghost"
                          onClick={() => {
                            setOpen(false);
                            setDeleteTarget({
                              id: recording.id,
                              name: recording.name
                            });
                          }}
                        >
                          <DeleteIcon aria-hidden="true" className="size-3" />
                        </Button>
                      ) : null}
                    </div>
                  );
                })}
              </div>
            )}
          </ScrollArea>
          {batchSelecting ? (
            <div className="mt-2 flex items-center justify-between gap-2 border-t border-[var(--border-1)] px-2 pt-2">
              <span className="text-xs text-[var(--text-secondary)]">
                {t(
                  "workspace.agentGui.sessionReplay.replay.batchSelectedCount",
                  { count: selectedCassetteIds.length }
                )}
              </span>
              <ReplayLaunchMenu
                disabled={
                  disabled ||
                  !launcher ||
                  launching ||
                  selectedCassetteIds.length < 2
                }
                onLaunch={(playbackMode) =>
                  launchReplay(selectedCassetteIds, playbackMode)
                }
              />
            </div>
          ) : null}
        </PopoverContent>
      </Popover>
      <ConfirmationDialog
        cancelLabel={t("workspace.agentGui.sessionReplay.record.deleteCancel")}
        confirmBusy={deletingRecordingId !== null}
        confirmLabel={t(
          "workspace.agentGui.sessionReplay.record.deleteConfirm"
        )}
        description={
          deleteTarget
            ? t("workspace.agentGui.sessionReplay.record.deleteDescription", {
                name: deleteTarget.name
              })
            : undefined
        }
        open={deleteTarget !== null}
        title={t("workspace.agentGui.sessionReplay.record.deleteTitle")}
        tone="destructive"
        onConfirm={deleteRecording}
        onOpenChange={(nextOpen) => {
          if (!nextOpen && deletingRecordingId === null) {
            setDeleteTarget(null);
          }
        }}
      />
    </>
  );
}

function ReplayLaunchMenu({
  disabled,
  iconOnly = false,
  onLaunch
}: {
  disabled: boolean;
  iconOnly?: boolean;
  onLaunch: (playbackMode: DesktopAgentSessionReplayLaunchPlaybackMode) => void;
}): React.JSX.Element {
  const { t } = useTranslation();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          aria-label={t(
            "workspace.agentGui.sessionReplay.replay.choosePlaybackMode"
          )}
          disabled={disabled}
          size={iconOnly ? "icon-sm" : "sm"}
          variant={iconOnly ? "ghost" : "default"}
        >
          {iconOnly ? (
            <PlayIcon aria-hidden="true" className="size-3" />
          ) : (
            t("workspace.agentGui.sessionReplay.replay.batchLaunch")
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="nodrag [-webkit-app-region:no-drag]"
        style={{ zIndex: "var(--z-panel-popover)" }}
      >
        <DropdownMenuItem onSelect={() => onLaunch("automatic")}>
          <PlayIcon aria-hidden="true" className="size-3" />
          {t("workspace.agentGui.sessionReplay.replay.automatic")}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => onLaunch("manual")}>
          <PauseIcon aria-hidden="true" className="size-3" />
          {t("workspace.agentGui.sessionReplay.replay.manual")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
