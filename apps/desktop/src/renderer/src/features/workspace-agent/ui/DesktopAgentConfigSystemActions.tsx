import { useRef, useState } from "react";
import {
  DownloadIcon,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  RefreshIcon
} from "@tutti-os/ui-system";
import { ArrowRightIcon } from "@tutti-os/ui-system/icons";
import { useAppUpdateService } from "@renderer/features/app-update";
import { useTranslation } from "@renderer/i18n";
import { useWorkspaceSettingsService } from "../../workspace-workbench/ui/useWorkspaceSettingsService";
import {
  createSubmenuGraceCloseController,
  shouldKeepOpenSubmenuOnTriggerKeyDown,
  shouldKeepOpenSubmenuOnTriggerPointerDown
} from "./desktopAgentConfigSystemActionsModel.ts";

const actionClassName =
  "nodrag flex h-7 w-full items-center gap-2 rounded-[6px] px-2 text-[13px] text-[var(--text-primary)] transition-colors hover:bg-[var(--transparency-hover)] hover:text-[var(--text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] disabled:text-[var(--text-tertiary)] [-webkit-app-region:no-drag]";

export function DesktopAgentConfigSystemActions(): React.JSX.Element {
  const { t } = useTranslation();
  const { service: appUpdateService, state: appUpdateState } =
    useAppUpdateService();
  const { service: settingsService, state: settingsState } =
    useWorkspaceSettingsService();
  const [exportMenuOpen, setExportMenuOpen] = useState(false);
  const exportMenuTriggerRef = useRef<HTMLButtonElement>(null);
  const exportMenuGraceCloseRef = useRef<
    ReturnType<typeof createSubmenuGraceCloseController> | undefined
  >(undefined);
  exportMenuGraceCloseRef.current ??= createSubmenuGraceCloseController({
    close: () => setExportMenuOpen(false)
  });
  const exportMenuGraceClose = exportMenuGraceCloseRef.current;

  const exportLogs = (input: {
    includeAgentSessions: boolean;
    scope: "recent-10-minutes" | "recent-3-days";
  }): void => {
    void settingsService.exportDeveloperLogs(input);
  };
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <button
        className={actionClassName}
        data-testid="agent-gui-config-check-updates"
        disabled={appUpdateState.isActing}
        onClick={() => void appUpdateService.checkForUpdates()}
        type="button"
      >
        <RefreshIcon aria-hidden="true" className="size-4" />
        <span>
          {appUpdateState.isActing
            ? t("updates.checkingTitle")
            : t("desktop.menu.checkForUpdates")}
        </span>
      </button>
      <DropdownMenu
        modal={false}
        open={exportMenuOpen}
        onOpenChange={(open) => {
          exportMenuGraceClose.cancel();
          setExportMenuOpen(open);
        }}
      >
        <DropdownMenuTrigger asChild>
          <button
            className={actionClassName}
            data-testid="agent-gui-config-export-logs"
            disabled={settingsState.developerLogs.exporting}
            ref={exportMenuTriggerRef}
            onClick={() => setExportMenuOpen(true)}
            onKeyDown={(event) => {
              if (event.key === "ArrowRight") {
                event.preventDefault();
                setExportMenuOpen(true);
                return;
              }
              if (
                event.key === "ArrowLeft" ||
                shouldKeepOpenSubmenuOnTriggerKeyDown({
                  key: event.key,
                  open: exportMenuOpen
                })
              ) {
                event.preventDefault();
                if (event.key === "ArrowLeft") setExportMenuOpen(false);
              }
            }}
            onPointerEnter={() => {
              exportMenuGraceClose.cancel();
              setExportMenuOpen(true);
            }}
            onPointerLeave={() => exportMenuGraceClose.schedule()}
            onPointerDown={(event) => {
              if (
                shouldKeepOpenSubmenuOnTriggerPointerDown({
                  button: event.button,
                  ctrlKey: event.ctrlKey,
                  open: exportMenuOpen
                })
              ) {
                event.preventDefault();
              }
            }}
            type="button"
          >
            <DownloadIcon aria-hidden="true" className="size-4" />
            <span>{t("workspace.settings.developer.exportLogs")}</span>
            <ArrowRightIcon
              aria-hidden="true"
              className="ml-auto size-4 text-[var(--text-tertiary)]"
            />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          className="w-64"
          data-agent-gui-config-owned-layer=""
          side="right"
          style={{ zIndex: "calc(var(--z-panel-popover) + 1)" }}
          onKeyDown={(event) => {
            if (event.key !== "ArrowLeft") return;
            event.preventDefault();
            setExportMenuOpen(false);
            requestAnimationFrame(() => exportMenuTriggerRef.current?.focus());
          }}
          onPointerEnter={() => exportMenuGraceClose.cancel()}
          onPointerLeave={() => exportMenuGraceClose.schedule()}
        >
          <DropdownMenuItem
            disabled={settingsState.developerLogs.exporting}
            onClick={() =>
              exportLogs({
                includeAgentSessions: false,
                scope: "recent-10-minutes"
              })
            }
          >
            {t("workspace.settings.developer.exportRecentTenMinutesLogsOnly")}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={settingsState.developerLogs.exporting}
            onClick={() =>
              exportLogs({
                includeAgentSessions: true,
                scope: "recent-10-minutes"
              })
            }
          >
            {t(
              "workspace.settings.developer.exportRecentTenMinutesLogsWithSessions"
            )}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={settingsState.developerLogs.exporting}
            onClick={() =>
              exportLogs({
                includeAgentSessions: false,
                scope: "recent-3-days"
              })
            }
          >
            {t("workspace.settings.developer.exportRecentThreeDaysLogsOnly")}
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={settingsState.developerLogs.exporting}
            onClick={() =>
              exportLogs({
                includeAgentSessions: true,
                scope: "recent-3-days"
              })
            }
          >
            {t(
              "workspace.settings.developer.exportRecentThreeDaysLogsWithSessions"
            )}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
