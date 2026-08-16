import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkbenchNode } from "../core/types.ts";
import { createWorkbenchGenieNodeVisibilityStore } from "./genieNodeVisibility.ts";
import { WorkbenchDockFrame } from "./WorkbenchDockFrame.tsx";
import { WorkbenchProvider } from "./WorkbenchProvider.tsx";
import type { WorkbenchGenieController } from "./useWorkbenchGenieAnimation.tsx";
import { createWorkbenchController } from "../store/createWorkbenchController.ts";

function createTestGenieController(): WorkbenchGenieController & {
  nodeVisibility: ReturnType<typeof createWorkbenchGenieNodeVisibilityStore>;
} {
  return {
    genieLayer: null,
    isPendingMinimizedDockNode: () => false,
    launchNodeFromAnchor: () => {},
    minimizeNodeToAnchor: () => {},
    nodeVisibility: createWorkbenchGenieNodeVisibilityStore(),
    pendingMinimizedNode: null,
    registerDockAnchor: () => {},
    shouldAnimateMinimizedDockEnter: () => false
  };
}

function dispatchMousePointerEvent(
  element: Element,
  type: "pointerout" | "pointerover",
  init: MouseEventInit = {}
): void {
  const event = new MouseEvent(type, { bubbles: true, ...init });
  Object.defineProperty(event, "pointerType", { value: "mouse" });
  element.dispatchEvent(event);
}

function createFullscreenNode(): WorkbenchNode {
  return {
    data: null,
    displayMode: "fullscreen",
    frame: { height: 600, width: 800, x: 0, y: 0 },
    id: "fullscreen",
    isMinimized: false,
    kind: "test",
    restoreFrame: { height: 400, width: 600, x: 100, y: 80 },
    title: "Fullscreen"
  };
}

describe("WorkbenchDockFrame automatic hiding", () => {
  const previousActEnvironment = (
    globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
  ).IS_REACT_ACT_ENVIRONMENT;

  beforeEach(() => {
    vi.useFakeTimers();
    (
      globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
    ).IS_REACT_ACT_ENVIRONMENT = true;
  });

  afterEach(() => {
    vi.useRealTimers();
    (
      globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
    ).IS_REACT_ACT_ENVIRONMENT = previousActEnvironment;
  });

  it("keeps the Dock visible by default, including in fullscreen", async () => {
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);
    const fullscreenNode = createFullscreenNode();
    const controller = createWorkbenchController({
      nodes: [fullscreenNode],
      nodeStack: [fullscreenNode.id]
    });
    const genie = createTestGenieController();

    try {
      await act(async () => {
        root.render(
          <WorkbenchProvider controller={controller}>
            <WorkbenchDockFrame
              genie={genie}
              renderDock={() => <div>Dock</div>}
            />
          </WorkbenchProvider>
        );
      });

      expect(
        container
          .querySelector(".workbench-dock-frame")
          ?.getAttribute("data-auto-hide-state")
      ).toBe("disabled");
      expect(
        container.querySelector(".workbench-dock-frame__auto-hide-hover-zone")
      ).toBeNull();
    } finally {
      await act(async () => {
        root.unmount();
      });
      genie.nodeVisibility.dispose();
      container.remove();
    }
  });

  it("reveals after a deliberate inner-hot-zone hover and hides after leaving", async () => {
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);
    const controller = createWorkbenchController();
    const genie = createTestGenieController();

    try {
      await act(async () => {
        root.render(
          <WorkbenchProvider controller={controller}>
            <WorkbenchDockFrame
              dockAutoHide
              genie={genie}
              renderDock={() => <div>Dock</div>}
            />
          </WorkbenchProvider>
        );
      });

      const frame = container.querySelector<HTMLElement>(
        ".workbench-dock-frame"
      );
      const hoverZone = container.querySelector<HTMLElement>(
        ".workbench-dock-frame__auto-hide-hover-zone"
      );
      expect(frame?.dataset.autoHideState).toBe("hidden");
      expect(hoverZone).not.toBeNull();
      if (!frame || !hoverZone) {
        return;
      }

      await act(async () => {
        dispatchMousePointerEvent(hoverZone, "pointerover");
        vi.advanceTimersByTime(219);
      });
      expect(frame.dataset.autoHideState).toBe("hidden");

      await act(async () => {
        vi.advanceTimersByTime(1);
      });
      expect(frame.dataset.autoHideState).toBe("visible");

      await act(async () => {
        dispatchMousePointerEvent(hoverZone, "pointerout", {
          relatedTarget: document.body
        });
        vi.advanceTimersByTime(499);
      });
      expect(frame.dataset.autoHideState).toBe("visible");

      await act(async () => {
        vi.advanceTimersByTime(1);
      });
      expect(frame.dataset.autoHideState).toBe("hidden");
    } finally {
      await act(async () => {
        root.unmount();
      });
      genie.nodeVisibility.dispose();
      container.remove();
    }
  });

  it("does not reveal while a workbench window is being dragged", async () => {
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);
    const controller = createWorkbenchController({
      activeDragNodeId: "dragging"
    });
    const genie = createTestGenieController();

    try {
      await act(async () => {
        root.render(
          <WorkbenchProvider controller={controller}>
            <WorkbenchDockFrame
              dockAutoHide
              genie={genie}
              renderDock={() => <div>Dock</div>}
            />
          </WorkbenchProvider>
        );
      });

      const frame = container.querySelector<HTMLElement>(
        ".workbench-dock-frame"
      );
      const hoverZone = container.querySelector<HTMLElement>(
        ".workbench-dock-frame__auto-hide-hover-zone"
      );
      expect(frame).not.toBeNull();
      expect(hoverZone).not.toBeNull();
      if (!frame || !hoverZone) {
        return;
      }

      await act(async () => {
        dispatchMousePointerEvent(hoverZone, "pointerover");
        vi.advanceTimersByTime(220);
      });

      expect(frame.dataset.autoHideState).toBe("hidden");
    } finally {
      await act(async () => {
        root.unmount();
      });
      genie.nodeVisibility.dispose();
      container.remove();
    }
  });
});
