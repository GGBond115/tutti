import {
  lazy,
  Suspense,
  useEffect,
  useMemo,
  useSyncExternalStore,
  type PointerEvent
} from "react";
import type { AgentGUIAgentTarget } from "@tutti-os/agent-gui";
import { Button, CloseIcon } from "@tutti-os/ui-system";
import { createTranslator } from "../../../../../shared/i18n/index.ts";
import type { DesktopCaptureWindowController } from "./desktopCaptureWindowController.ts";

const loadAgentGUIQuickComposer = () =>
  import("@tutti-os/agent-gui/quick-composer");
const AgentGUIQuickComposer = lazy(async () => ({
  default: (await loadAgentGUIQuickComposer()).AgentGUIQuickComposer
}));

export function DesktopCaptureWindow({
  controller
}: {
  controller: DesktopCaptureWindowController;
}) {
  const snapshot = useSyncExternalStore(
    controller.subscribe,
    controller.getSnapshot,
    controller.getSnapshot
  );
  const translator = useMemo(
    () => createTranslator(snapshot.capture?.locale ?? "en"),
    [snapshot.capture?.locale]
  );
  const agentTargets = useMemo<AgentGUIAgentTarget[]>(
    () =>
      (snapshot.capture?.agents ?? []).map((agent) => ({
        agentTargetId: agent.id,
        description: agent.description ?? undefined,
        iconUrl: agent.iconUrl,
        label: agent.name,
        provider: agent.provider,
        ref: { kind: "desktop-capture", provider: agent.provider },
        targetId: agent.id
      })),
    [snapshot.capture?.agents]
  );

  useEffect(() => {
    void controller.initialize();
  }, [controller]);

  useEffect(() => {
    const capture = snapshot.capture;
    if (!capture) {
      return;
    }
    document.documentElement.lang = capture.locale;
    document.documentElement.dataset.theme = capture.themeAppearance;
    document.documentElement.style.background = "transparent";
    document.body.style.background = "transparent";
  }, [snapshot.capture]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        controller.cancelSelection();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [controller]);

  const pointerPosition = (
    event: PointerEvent<HTMLDivElement>
  ): { x: number; y: number } => {
    const bounds = event.currentTarget.getBoundingClientRect();
    return {
      x: Math.max(0, Math.min(bounds.width, event.clientX - bounds.left)),
      y: Math.max(0, Math.min(bounds.height, event.clientY - bounds.top))
    };
  };

  if (!snapshot.capture || snapshot.stage === "loading") {
    return (
      <div className="fixed inset-0 grid place-items-center bg-[var(--background)] font-[var(--font-ui)] text-[13px] leading-[1.4] text-[var(--text-secondary)]">
        {translator.t(snapshot.failed ? "capture.error" : "capture.loading")}
      </div>
    );
  }

  if (snapshot.stage === "selecting") {
    return (
      <div
        className="fixed inset-0 cursor-crosshair overflow-hidden bg-transparent select-none"
        onPointerDown={(event) => {
          void loadAgentGUIQuickComposer();
          event.currentTarget.setPointerCapture(event.pointerId);
          controller.beginSelection(pointerPosition(event));
        }}
        onPointerMove={(event) =>
          controller.updateSelection(pointerPosition(event))
        }
        onPointerUp={(event) => {
          void controller.finishSelection().finally(() => {
            if (event.currentTarget.hasPointerCapture(event.pointerId)) {
              event.currentTarget.releasePointerCapture(event.pointerId);
            }
          });
        }}
      >
        <img
          alt={translator.t("capture.screenPreviewAlt")}
          className="pointer-events-none absolute inset-0 h-full w-full object-fill"
          draggable={false}
          src={snapshot.capture.screenshotDataUrl}
        />
        {snapshot.selection ? (
          <div
            className="pointer-events-none absolute z-[1] border border-[color-mix(in_srgb,var(--white-stationary)_92%,transparent)]"
            style={{
              boxShadow:
                "0 0 0 99999px color-mix(in srgb, var(--black-stationary) 46%, transparent), 0 0 0 1px color-mix(in srgb, var(--black-stationary) 36%, transparent)",
              height: snapshot.selection.height,
              left: snapshot.selection.x,
              top: snapshot.selection.y,
              width: snapshot.selection.width
            }}
          />
        ) : (
          <div className="pointer-events-none absolute inset-0 bg-[color-mix(in_srgb,var(--black-stationary)_46%,transparent)]" />
        )}
        <div className="pointer-events-none absolute top-6 left-1/2 z-[2] -translate-x-1/2 rounded-lg border border-[color-mix(in_srgb,var(--white-stationary)_18%,transparent)] bg-[color-mix(in_srgb,var(--black-stationary)_78%,transparent)] px-3 py-[7px] font-[var(--font-ui)] text-[13px] leading-[1.4] text-[var(--white-stationary)] shadow-[0_8px_30px_color-mix(in_srgb,var(--black-stationary)_26%,transparent)]">
          {translator.t("capture.selectHint")}
        </div>
        {snapshot.failed ? (
          <p
            className="absolute bottom-6 left-1/2 z-[2] m-0 -translate-x-1/2 rounded-lg bg-[var(--background-fronted)] px-3 py-2 text-[12px] text-[var(--state-danger)] shadow-panel"
            role="alert"
          >
            {translator.t("capture.error")}
          </p>
        ) : null}
      </div>
    );
  }

  return (
    <main className="fixed inset-0 flex overflow-hidden bg-transparent font-[var(--font-ui)] text-[var(--text-primary)]">
      <section className="flex min-h-0 w-full flex-1 flex-col overflow-hidden rounded-[14px] border border-[var(--border-1)] bg-[var(--background-fronted)]">
        <header className="flex h-10 shrink-0 cursor-grab items-center justify-between gap-3 border-b border-[var(--border-1)] px-3 [-webkit-app-region:drag] active:cursor-grabbing">
          <div className="flex min-w-0 items-center gap-2">
            <h1 className="m-0 truncate text-[13px] leading-5 font-medium">
              {translator.t("capture.title")}
            </h1>
          </div>
          <Button
            aria-label={translator.t("common.close")}
            className="[-webkit-app-region:no-drag]"
            disabled={snapshot.submitting}
            onClick={() => controller.cancelSelection()}
            size="icon-sm"
            variant="chrome"
          >
            <CloseIcon size={14} />
          </Button>
        </header>

        <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-2.5 [-webkit-app-region:no-drag]">
          <div className="flex items-center gap-2">
            <Button
              disabled={snapshot.submitting}
              onClick={() =>
                controller.insertPrompt(translator.t("capture.taskPrompt"))
              }
              size="sm"
              variant="secondary"
            >
              {translator.t("capture.taskPromptAction")}
            </Button>
            <span className="text-[11px] leading-4 text-[var(--text-secondary)]">
              {translator.t("capture.taskPromptHint")}
            </span>
          </div>

          <div className="min-h-0 flex-1">
            <Suspense
              fallback={
                <div
                  className="grid min-h-24 place-items-center rounded-[10px] border border-[var(--border-1)] text-[12px] text-[var(--text-secondary)]"
                  role="status"
                >
                  {translator.t("capture.loading")}
                </div>
              }
            >
              <AgentGUIQuickComposer
                agentTargets={agentTargets}
                content={snapshot.content}
                disabled={snapshot.submitting}
                locale={snapshot.capture.locale}
                placeholder={translator.t("capture.notePlaceholder")}
                selectedAgentTargetId={snapshot.agentTargetId}
                workspaceId={snapshot.capture.workspaceId}
                onAgentTargetChange={(agentTargetId) =>
                  controller.setAgentTargetId(agentTargetId)
                }
                onContentChange={(content) => controller.setContent(content)}
                onSubmit={(content, displayPrompt) =>
                  void controller.submit(content, displayPrompt)
                }
              />
            </Suspense>
          </div>

          {snapshot.failed ? (
            <p
              className="m-0 text-[11px] leading-4 text-[var(--state-danger)]"
              role="alert"
            >
              {translator.t("capture.error")}
            </p>
          ) : null}
        </div>
      </section>
    </main>
  );
}
