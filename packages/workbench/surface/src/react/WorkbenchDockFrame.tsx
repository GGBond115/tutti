import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FocusEvent,
  type PointerEvent as ReactPointerEvent,
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
    (event: ReactPointerEvent<HTMLDivElement>) => {
      cancelTimers();
      if (
        !dockAutoHide ||
        workbenchInteractionActive ||
        event.buttons !== 0 ||
        event.pointerType !== "mouse"
      ) {
        return;
      }
      revealTimerRef.current = setTimeout(() => {
        revealTimerRef.current = null;
        setDockVisible(true);
      }, 220);
    },
    [cancelTimers, dockAutoHide, workbenchInteractionActive]
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

    window.addEventListener("blur", handleWindowBlur);
    document.addEventListener("pointerover", handleDocumentPointerOver);
    document.addEventListener("pointerout", handleDocumentPointerOut);
    return () => {
      window.removeEventListener("blur", handleWindowBlur);
      document.removeEventListener("pointerover", handleDocumentPointerOver);
      document.removeEventListener("pointerout", handleDocumentPointerOut);
    };
  }, [cancelTimers, dockAutoHide, scheduleHide, showDock]);

  useEffect(() => () => cancelTimers(), [cancelTimers]);

  if (!renderDock && minimizedNodes.length === 0) {
    return null;
  }

  return (
    <>
      {dockAutoHide ? (
        <div
          className="workbench-dock-frame__auto-hide-hover-zone"
          data-dock-placement={dockPlacement}
          onPointerEnter={scheduleReveal}
          onPointerLeave={handleHotZoneLeave}
          aria-hidden
        />
      ) : null}
      <div
        className="workbench-dock-frame"
        data-dock-placement={dockPlacement}
        data-auto-hide-state={
          dockAutoHide ? (dockVisible ? "visible" : "hidden") : "disabled"
        }
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
