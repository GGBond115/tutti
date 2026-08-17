import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FocusEvent,
  type ReactNode
} from "react";
import {
  selectFocusedWorkbenchNode,
  selectFullscreenNodeToExitBeforeDockLaunch
} from "../core/selectors.ts";
import type { WorkbenchNode } from "../core/types.ts";
import type { WorkbenchDockContext, WorkbenchDockPlacement } from "./types.ts";
import { useWorkbenchController } from "./WorkbenchProvider.tsx";
import { createWorkbenchDockNodesSelector } from "./dockNodeSelectors.ts";
import { useWorkbenchSelector } from "./hooks/useWorkbenchSelector.ts";
import type { WorkbenchGenieController } from "./useWorkbenchGenieAnimation.tsx";

const WORKBENCH_DOCK_AUTO_HIDE_REVEAL_DELAY_MS = 100;
const WORKBENCH_DOCK_AUTO_HIDE_EDGE_GAP_PX = 8;
const WORKBENCH_DOCK_AUTO_HIDE_HOVER_ZONE_PX = 16;
const WORKBENCH_DOCK_AUTO_HIDE_CORNER_MARGIN_PX = 24;

interface DockAutoHidePointerIntent {
  buttons: number;
  pointerType: string;
}

export interface WorkbenchDockFrameProps<TData = unknown> {
  dockAutoHide?: boolean;
  dockPlacement?: WorkbenchDockPlacement;
  genie: WorkbenchGenieController<TData>;
  interactive?: boolean;
  renderDock?: (context: WorkbenchDockContext<TData>) => ReactNode;
}

export function WorkbenchDockFrame<TData>({
  dockAutoHide = false,
  dockPlacement = "bottom",
  genie,
  interactive = true,
  renderDock
}: WorkbenchDockFrameProps<TData>) {
  const controller = useWorkbenchController<TData>();
  const selectDockNodes = useMemo(
    () => createWorkbenchDockNodesSelector<TData>(),
    []
  );
  const workbenchInteractionActive = useWorkbenchSelector(
    (state) =>
      state.activeDragNodeId !== null || state.activeResizeNodeId !== null
  );
  const nodes = useWorkbenchSelector<TData, readonly WorkbenchNode<TData>[]>(
    selectDockNodes
  );
  const minimizedNodesWithPending = useMemo(
    () =>
      mergePendingMinimizedDockNode(
        nodes.filter((node) => node.isMinimized),
        genie.pendingMinimizedNode
      ),
    [genie.pendingMinimizedNode, nodes]
  );
  const minimizedNodes = useMemo(
    () => minimizedNodesWithPending.filter((node) => node.isMinimized),
    [minimizedNodesWithPending]
  );
  const focusedNodeId = useWorkbenchSelector(
    (state) => selectFocusedWorkbenchNode(state)?.id ?? null
  );
  const [dockVisible, setDockVisible] = useState(() => !dockAutoHide);
  const dockAutoHideState = dockAutoHide
    ? dockVisible
      ? "visible"
      : "hidden"
    : "disabled";
  const revealTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const hideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const cancelReveal = useCallback(() => {
    if (revealTimerRef.current !== null) {
      clearTimeout(revealTimerRef.current);
      revealTimerRef.current = null;
    }
  }, []);
  const cancelHide = useCallback(() => {
    if (hideTimerRef.current !== null) {
      clearTimeout(hideTimerRef.current);
      hideTimerRef.current = null;
    }
  }, []);
  const cancelTimers = useCallback(() => {
    cancelReveal();
    cancelHide();
  }, [cancelHide, cancelReveal]);
  const showDock = useCallback(() => {
    cancelTimers();
    if (dockAutoHide && !workbenchInteractionActive) {
      setDockVisible(true);
    }
  }, [cancelTimers, dockAutoHide, workbenchInteractionActive]);
  const scheduleReveal = useCallback(
    (event: DockAutoHidePointerIntent) => {
      cancelHide();
      if (
        !dockAutoHide ||
        workbenchInteractionActive ||
        event.buttons !== 0 ||
        event.pointerType !== "mouse"
      ) {
        cancelReveal();
        return;
      }
      if (revealTimerRef.current !== null) {
        return;
      }
      revealTimerRef.current = setTimeout(() => {
        revealTimerRef.current = null;
        setDockVisible(true);
      }, WORKBENCH_DOCK_AUTO_HIDE_REVEAL_DELAY_MS);
    },
    [cancelHide, cancelReveal, dockAutoHide, workbenchInteractionActive]
  );
  const scheduleHide = useCallback(() => {
    cancelHide();
    if (!dockAutoHide) {
      return;
    }
    hideTimerRef.current = setTimeout(() => {
      hideTimerRef.current = null;
      setDockVisible(false);
    }, 500);
  }, [cancelHide, dockAutoHide]);
  const handleHotZoneLeave = useCallback(() => {
    cancelReveal();
    scheduleHide();
  }, [cancelReveal, scheduleHide]);
  const handleDockBlur = useCallback(
    (event: FocusEvent<HTMLDivElement>) => {
      if (
        event.relatedTarget instanceof Element &&
        isDockAutoHideHoldTarget(event.relatedTarget)
      ) {
        return;
      }
      scheduleHide();
    },
    [scheduleHide]
  );

  useLayoutEffect(() => {
    cancelTimers();
    setDockVisible(!dockAutoHide);
  }, [cancelTimers, dockAutoHide, workbenchInteractionActive]);

  useEffect(() => {
    if (!dockAutoHide) {
      return;
    }

    const handleWindowBlur = () => {
      cancelTimers();
      setDockVisible(false);
    };
    const handleDocumentPointerOver = (event: PointerEvent) => {
      if (
        event.target instanceof Element &&
        isDockAutoHideHoldTarget(event.target)
      ) {
        showDock();
      }
    };
    const handleDocumentPointerOut = (event: PointerEvent) => {
      if (
        event.target instanceof Element &&
        isDockAutoHideHoldTarget(event.target) &&
        !(
          event.relatedTarget instanceof Element &&
          isDockAutoHideHoldTarget(event.relatedTarget)
        )
      ) {
        scheduleHide();
      }
    };
    const handleDocumentPointerMove = (event: PointerEvent) => {
      const pointerWithinHotZone = isPointerWithinDockAutoHideHoverZone(
        event,
        dockPlacement
      );
      if (
        dockVisible &&
        (pointerWithinHotZone ||
          (event.target instanceof Element &&
            isDockAutoHideHoldTarget(event.target)))
      ) {
        return;
      }
      if (dockVisible) {
        scheduleHide();
        return;
      }
      if (pointerWithinHotZone) {
        scheduleReveal(event);
        return;
      }
      cancelReveal();
    };

    window.addEventListener("blur", handleWindowBlur);
    document.addEventListener("pointerover", handleDocumentPointerOver);
    document.addEventListener("pointerout", handleDocumentPointerOut);
    document.addEventListener("pointermove", handleDocumentPointerMove, true);
    return () => {
      window.removeEventListener("blur", handleWindowBlur);
      document.removeEventListener("pointerover", handleDocumentPointerOver);
      document.removeEventListener("pointerout", handleDocumentPointerOut);
      document.removeEventListener(
        "pointermove",
        handleDocumentPointerMove,
        true
      );
    };
  }, [
    cancelReveal,
    cancelTimers,
    dockAutoHide,
    dockPlacement,
    dockVisible,
    scheduleHide,
    scheduleReveal,
    showDock
  ]);

  useEffect(() => () => cancelTimers(), [cancelTimers]);

  if (!renderDock && minimizedNodes.length === 0) {
    return null;
  }

  return (
    <>
      {dockAutoHide ? (
        <div
          className="workbench-dock-frame__auto-hide-hover-zone"
          data-auto-hide-state={dockAutoHideState}
          data-dock-placement={dockPlacement}
          onPointerEnter={scheduleReveal}
          onPointerLeave={handleHotZoneLeave}
          aria-hidden
        />
      ) : null}
      <div
        className="workbench-dock-frame"
        data-dock-placement={dockPlacement}
        data-auto-hide-state={dockAutoHideState}
        onBlurCapture={handleDockBlur}
        onFocusCapture={showDock}
        onPointerEnter={showDock}
        onPointerLeave={scheduleHide}
      >
        {renderDock
          ? renderDock({
              controller,
              focusedNodeId,
              genie: {
                launchNodeFromAnchor: (anchorKey, nodeID, launch) => {
                  const fullscreenNode = interactive
                    ? selectFullscreenNodeToExitBeforeDockLaunch(
                        controller.getSnapshot(),
                        nodeID
                      )
                    : null;
                  if (fullscreenNode) {
                    controller.commands.exitFullscreen(fullscreenNode.id);
                  }
                  genie.launchNodeFromAnchor(anchorKey, nodeID, launch);
                },
                registerDockAnchor: (anchorKey, element) => {
                  genie.registerDockAnchor(anchorKey, element);
                },
                shouldAnimateMinimizedDockEnter: (nodeID) => {
                  return genie.shouldAnimateMinimizedDockEnter(nodeID);
                },
                isPendingMinimizedDockNode: (nodeID) => {
                  return genie.isPendingMinimizedDockNode(nodeID);
                }
              },
              minimizedNodes,
              nodes
            })
          : null}
      </div>
    </>
  );
}

function isDockAutoHideHoldTarget(target: Element): boolean {
  return (
    target.closest(".workbench-dock-frame") !== null ||
    target.closest(".desktop-dock-popup-root") !== null
  );
}

function isPointerWithinDockAutoHideHoverZone(
  pointer: Pick<PointerEvent, "clientX" | "clientY">,
  dockPlacement: WorkbenchDockPlacement
): boolean {
  if (dockPlacement === "left") {
    return (
      pointer.clientX >= WORKBENCH_DOCK_AUTO_HIDE_EDGE_GAP_PX &&
      pointer.clientX <
        WORKBENCH_DOCK_AUTO_HIDE_EDGE_GAP_PX +
          WORKBENCH_DOCK_AUTO_HIDE_HOVER_ZONE_PX &&
      pointer.clientY >= WORKBENCH_DOCK_AUTO_HIDE_CORNER_MARGIN_PX &&
      pointer.clientY <
        window.innerHeight - WORKBENCH_DOCK_AUTO_HIDE_CORNER_MARGIN_PX
    );
  }

  return (
    pointer.clientX >= WORKBENCH_DOCK_AUTO_HIDE_CORNER_MARGIN_PX &&
    pointer.clientX <
      window.innerWidth - WORKBENCH_DOCK_AUTO_HIDE_CORNER_MARGIN_PX &&
    pointer.clientY >=
      window.innerHeight -
        WORKBENCH_DOCK_AUTO_HIDE_EDGE_GAP_PX -
        WORKBENCH_DOCK_AUTO_HIDE_HOVER_ZONE_PX &&
    pointer.clientY < window.innerHeight - WORKBENCH_DOCK_AUTO_HIDE_EDGE_GAP_PX
  );
}

function mergePendingMinimizedDockNode<TData>(
  nodes: readonly WorkbenchNode<TData>[],
  pendingNode: WorkbenchNode<TData> | null
): readonly WorkbenchNode<TData>[] {
  if (!pendingNode) {
    return nodes;
  }

  const existingNode = nodes.find((node) => node.id === pendingNode.id);
  if (existingNode?.isMinimized) {
    return nodes;
  }

  return [...nodes.filter((node) => node.id !== pendingNode.id), pendingNode];
}
