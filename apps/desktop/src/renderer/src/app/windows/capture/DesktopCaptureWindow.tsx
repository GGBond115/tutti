import {
  useEffect,
  useMemo,
  useSyncExternalStore,
  type PointerEvent
} from "react";
import {
  Button,
  CloseIcon,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Textarea
} from "@tutti-os/ui-system";
import { createTranslator } from "../../../../../shared/i18n/index.ts";
import type { DesktopCaptureWindowController } from "./desktopCaptureWindowController.ts";

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
  }, [snapshot.capture]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        controller.cancelSelection();
        return;
      }
      if (
        snapshot.stage === "composing" &&
        event.key === "Enter" &&
        (event.metaKey || event.ctrlKey) &&
        snapshot.agentTargetId
      ) {
        event.preventDefault();
        void controller.submit("create-and-run");
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [controller, snapshot.agentTargetId, snapshot.stage]);

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

  const attachment = snapshot.attachment;
  return (
    <main className="fixed inset-0 flex overflow-hidden bg-transparent p-2.5 font-[var(--font-ui)] text-[var(--text-primary)]">
      <section className="flex min-h-0 w-full flex-1 flex-col gap-2.5 rounded-xl border border-[var(--border-1)] bg-[var(--background-fronted)] p-3 shadow-[0_16px_48px_var(--shadow-elevated)]">
        <header className="flex items-start justify-between gap-3">
          <div>
            <h1 className="m-0 text-[14px] leading-5 font-medium">
              {translator.t("capture.title")}
            </h1>
            <p className="mt-0.5 mb-0 text-[12px] leading-[18px] text-[var(--text-secondary)]">
              {translator.t("capture.subtitle")}
            </p>
          </div>
          <Button
            aria-label={translator.t("common.close")}
            disabled={snapshot.submitting}
            onClick={() => controller.cancelSelection()}
            size="icon-sm"
            variant="chrome"
          >
            <CloseIcon size={14} />
          </Button>
        </header>

        {attachment ? (
          <div className="relative grid min-h-[120px] max-h-40 place-items-center overflow-hidden rounded-lg border border-[var(--border-1)] bg-[var(--transparency-block)]">
            <img
              alt={translator.t("capture.selectionPreviewAlt")}
              className="min-h-[120px] max-h-40 h-full w-full object-contain"
              src={attachment.dataUrl}
            />
            <span className="absolute right-1.5 bottom-1.5 rounded bg-[color-mix(in_srgb,var(--black-stationary)_64%,transparent)] px-[5px] py-0.5 text-[10px] text-[var(--white-stationary)]">
              {attachment.width} × {attachment.height}
            </span>
          </div>
        ) : null}

        <Textarea
          autoFocus
          className="min-h-[72px] resize-none"
          disabled={snapshot.submitting}
          onChange={(event) => controller.setNote(event.currentTarget.value)}
          placeholder={translator.t("capture.notePlaceholder")}
          value={snapshot.note}
        />

        <div className="grid grid-cols-2 gap-2">
          <label className="flex min-w-0 flex-col gap-1 text-[11px] text-[var(--text-secondary)]">
            <span>{translator.t("capture.topicLabel")}</span>
            <Select
              disabled={snapshot.submitting}
              onValueChange={(value) => controller.setTopicId(value)}
              value={snapshot.topicId}
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {snapshot.capture.topics.map((topic) => (
                  <SelectItem key={topic.id} value={topic.id}>
                    {topic.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          <label className="flex min-w-0 flex-col gap-1 text-[11px] text-[var(--text-secondary)]">
            <span>{translator.t("capture.agentLabel")}</span>
            <Select
              disabled={
                snapshot.submitting || snapshot.capture.agents.length === 0
              }
              onValueChange={(value) => controller.setAgentTargetId(value)}
              value={snapshot.agentTargetId}
            >
              <SelectTrigger className="w-full">
                <SelectValue placeholder={translator.t("capture.noAgent")} />
              </SelectTrigger>
              <SelectContent>
                {snapshot.capture.agents.map((agent) => (
                  <SelectItem key={agent.id} value={agent.id}>
                    {agent.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
        </div>

        {snapshot.failed ? (
          <p
            className="m-0 text-[11px] leading-4 text-[var(--state-danger)]"
            role="alert"
          >
            {translator.t("capture.error")}
          </p>
        ) : null}

        <footer className="mt-auto flex justify-end gap-2">
          <Button
            disabled={snapshot.submitting}
            onClick={() => void controller.submit("create")}
            variant="secondary"
          >
            {translator.t("capture.createOnly")}
          </Button>
          <Button
            disabled={snapshot.submitting || !snapshot.agentTargetId}
            onClick={() => void controller.submit("create-and-run")}
          >
            {snapshot.submitting
              ? translator.t("capture.submitting")
              : translator.t("capture.createAndRun")}
          </Button>
        </footer>
      </section>
    </main>
  );
}
