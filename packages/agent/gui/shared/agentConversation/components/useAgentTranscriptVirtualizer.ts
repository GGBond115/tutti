import {
  useCallback,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type Ref,
  type RefObject
} from "react";
import { useOptionalAgentActivityRuntime } from "../../../agentActivityRuntime";
import { requestUiAnimationFrame } from "./agentTranscriptPresentationScheduler";
import { useElementResizeObserver } from "@tutti-os/ui-react-hooks";
import type { AgentConversationFollowEndMode } from "../agentConversationFollowEndController";
import {
  buildAgentTranscriptVirtualLayout,
  agentTranscriptVirtualLayoutsEqual,
  compensateAgentTranscriptDistanceForAnchor,
  distanceFromBottomForAgentTranscriptTurn,
  findAgentTranscriptCompensationAnchor,
  findAgentTranscriptTurnIndexAtOffset,
  findAgentTranscriptVirtualRange,
  projectAgentTranscriptVirtualRange,
  updateAgentTranscriptVirtualViewportState,
  AGENT_TRANSCRIPT_INITIAL_VIEWPORT_HEIGHT_PX,
  type AgentTranscriptVirtualLayout,
  type AgentTranscriptVirtualLayoutEntry,
  type AgentTranscriptVirtualViewportState
} from "./agentTranscriptVirtualizerLayout";
import {
  readAgentTranscriptVirtualMeasurements,
  writeAgentTranscriptVirtualMeasurements
} from "./agentTranscriptVirtualMeasurementStore";
import { useAgentTranscriptLayoutPreservation } from "./useAgentTranscriptLayoutPreservation";
import { useAgentTranscriptMeasurements } from "./useAgentTranscriptMeasurements";
import { useAgentTranscriptVirtualLocate } from "./useAgentTranscriptVirtualLocate";
import type {
  AgentTranscriptRowVirtualizer,
  AgentTranscriptViewportSnapshot,
  AgentTranscriptVirtualItem,
  AgentTranscriptVirtualScrollController
} from "./agentTranscriptVirtualizerTypes";

export type {
  AgentTranscriptRowVirtualizer,
  AgentTranscriptViewportSnapshot,
  AgentTranscriptVirtualItem,
  AgentTranscriptVirtualScrollController
} from "./agentTranscriptVirtualizerTypes";
import {
  agentTranscriptDistanceFromBottom,
  agentTranscriptDistanceFromTop,
  agentTranscriptLogicalScrollTop,
  agentTranscriptNativeScrollTopForDistance,
  cancelAgentTranscriptScroll,
  connectAgentTranscriptScrollInput,
  readAgentTranscriptScrollPadding,
  setAgentTranscriptScrollTop,
  AGENT_TRANSCRIPT_TOP_LOADING_THRESHOLD_PX,
  type AgentTranscriptUserScrollDirection
} from "./agentTranscriptScrollController";

const AGENT_TRANSCRIPT_END_THRESHOLD_PX = 24;
const AGENT_TRANSCRIPT_SWITCH_DEBUG_MARKER =
  "[TEMP:agent-transcript-virtual-switch]";

interface AgentTranscriptVirtualizer {
  layoutRevision: number;
  rowVirtualizer: AgentTranscriptRowVirtualizer;
  setVirtualizerHostElement(node: HTMLDivElement | null): void;
  totalHeightPx: number;
  virtualItems: readonly AgentTranscriptVirtualItem[];
  virtualizerHostRef: RefObject<HTMLDivElement | null>;
  windowOffsetPx: number;
}

export function useAgentTranscriptVirtualizer({
  agentSessionId,
  entries,
  followEndMode = "following",
  hasMovingTurnDisclosure,
  virtualScrollControllerRef
}: {
  agentSessionId: string;
  entries: readonly AgentTranscriptVirtualLayoutEntry[];
  followEndMode?: AgentConversationFollowEndMode;
  hasMovingTurnDisclosure: boolean;
  virtualScrollControllerRef?: Ref<AgentTranscriptVirtualScrollController>;
}): AgentTranscriptVirtualizer {
  const agentActivityRuntime = useOptionalAgentActivityRuntime();
  const logAgentTranscriptSwitchDebug = useCallback(
    (phase: string, details: Record<string, unknown>): void => {
      const reportDiagnostic = agentActivityRuntime?.reportDiagnostic;
      if (!reportDiagnostic || !agentActivityRuntime) return;
      void Promise.resolve(
        reportDiagnostic.call(agentActivityRuntime, {
          details: {
            marker: AGENT_TRANSCRIPT_SWITCH_DEBUG_MARKER,
            phase,
            ...details
          },
          event: "agent.gui.transcript_virtual.temp",
          level: "info",
          source: "agent-gui"
        })
      ).catch(() => {});
    },
    [agentActivityRuntime]
  );
  const retainedMeasurements = useMemo(
    () => readAgentTranscriptVirtualMeasurements(agentSessionId),
    [agentSessionId]
  );
  const preserveBeforeMeasurementCommitRef = useRef<() => void>(() => {});
  const prepareMeasurementCommitRef = useRef<
    (nextHeightsByKey: Readonly<Record<string, number>>) => void
  >(() => {});
  const {
    disconnect: disconnectMeasurements,
    measureElement,
    measuredElementsRef,
    measuredHeightsByKey,
    measuredHeightsRef
  } = useAgentTranscriptMeasurements(
    retainedMeasurements?.turnHeightsByKey ?? {},
    () => preserveBeforeMeasurementCommitRef.current(),
    (nextHeightsByKey) => prepareMeasurementCommitRef.current(nextHeightsByKey),
    entries.at(-1)?.key
  );
  const layout = useMemo(
    () => buildAgentTranscriptVirtualLayout(entries, measuredHeightsByKey),
    [entries, measuredHeightsByKey]
  );
  const [virtualViewportState, setVirtualViewportState] =
    useState<AgentTranscriptVirtualViewportState>(() => {
      const renderedRange = findAgentTranscriptVirtualRange({
        distanceFromBottomPx: 0,
        layout,
        viewportHeightPx: AGENT_TRANSCRIPT_INITIAL_VIEWPORT_HEIGHT_PX
      });
      logAgentTranscriptSwitchDebug("mount", {
        agentSessionId,
        entryCount: entries.length,
        layoutHeightPx: layout.totalHeightPx,
        renderedRange,
        retainedHeightCount: Object.keys(
          retainedMeasurements?.turnHeightsByKey ?? {}
        ).length
      });
      return {
        distanceFromBottomPx: 0,
        renderedRange,
        turnKeys: layout.turnKeys,
        viewportHeightPx: AGENT_TRANSCRIPT_INITIAL_VIEWPORT_HEIGHT_PX
      };
    });
  const [locatingTurnKey, setLocatingTurnKey] = useState<string | null>(null);
  const virtualizerHostRef = useRef<HTMLDivElement | null>(null);
  const resizeObservation = useElementResizeObserver();
  const layoutRef = useRef(layout);
  const nextLayoutRef = useRef(layout);
  const layoutRevisionRef = useRef(0);
  const virtualViewportRef = useRef(virtualViewportState);
  const scrollTopRef = useRef(0);
  const scrollElementRef = useRef<HTMLElement | null>(null);
  const disconnectScrollElementRef = useRef<(() => void) | null>(null);
  const scrollMarginRef = useRef(0);
  const committedScrollMarginRef = useRef(0);
  const scrollPaddingBottomRef = useRef(0);
  const scrollPaddingBottomBaseRef = useRef(0);
  const scrollPaddingTopRef = useRef(0);
  const viewportListenersRef = useRef(
    new Set<(snapshot: AgentTranscriptViewportSnapshot) => void>()
  );
  const userScrollListenersRef = useRef(
    new Set<(direction: AgentTranscriptUserScrollDirection) => void>()
  );
  const topLoadingHandlerRef = useRef<(() => Promise<"stop" | void>) | null>(
    null
  );
  const topLoadingInFlightRef = useRef(false);
  const followsEndRef = useRef(followEndMode === "following");
  const movingDisclosureRef = useRef(hasMovingTurnDisclosure);
  const activeLocateRef = useRef<object | null>(null);
  const pendingMeasuredLayoutRef = useRef<{
    distanceFromBottomPx: number;
    layout: AgentTranscriptVirtualLayout;
  } | null>(null);
  const getDistanceFromBottomPx = useCallback(
    () => virtualViewportRef.current.distanceFromBottomPx,
    []
  );
  const layoutPreservation = useAgentTranscriptLayoutPreservation({
    getDistanceFromBottomPx,
    scrollElementRef,
    scrollPaddingBottomRef
  });
  preserveBeforeMeasurementCommitRef.current =
    layoutPreservation.preserveForNextLayout;

  if (nextLayoutRef.current !== layout) {
    nextLayoutRef.current = layout;
    layoutRevisionRef.current += 1;
  }
  followsEndRef.current = followEndMode === "following";
  movingDisclosureRef.current = hasMovingTurnDisclosure;

  const commitVirtualViewport = useCallback(
    (
      nextDistanceFromBottomPx: number,
      nextViewportHeightPx: number,
      nextLayout = layoutRef.current
    ) => {
      const nextState = updateAgentTranscriptVirtualViewportState({
        current: virtualViewportRef.current,
        distanceFromBottomPx: nextDistanceFromBottomPx,
        layout: nextLayout,
        viewportHeightPx: nextViewportHeightPx
      });
      if (nextState === virtualViewportRef.current) return;
      virtualViewportRef.current = nextState;
      setVirtualViewportState(nextState);
    },
    []
  );
  prepareMeasurementCommitRef.current = (nextHeightsByKey) => {
    const previousLayout = layoutRef.current;
    const nextLayout = buildAgentTranscriptVirtualLayout(
      entries,
      nextHeightsByKey
    );
    if (agentTranscriptVirtualLayoutsEqual(previousLayout, nextLayout)) return;
    const preservedDistance = layoutPreservation.consumeDistance();
    const currentDistance =
      preservedDistance ?? virtualViewportRef.current.distanceFromBottomPx;
    const anchorKey = findAgentTranscriptCompensationAnchor({
      distanceFromBottomPx: currentDistance,
      fallbackRange: virtualViewportRef.current.renderedRange,
      layout: previousLayout,
      measuredHeightsByKey: measuredHeightsRef.current,
      viewportHeightPx: virtualViewportRef.current.viewportHeightPx
    });
    let nextDistance = currentDistance;
    if (
      followsEndRef.current &&
      !movingDisclosureRef.current &&
      activeLocateRef.current === null
    ) {
      nextDistance = 0;
    } else if (anchorKey) {
      nextDistance =
        compensateAgentTranscriptDistanceForAnchor({
          anchorKey,
          distanceFromBottomPx: currentDistance,
          nextLayout,
          previousLayout
        }) ?? currentDistance;
    }
    pendingMeasuredLayoutRef.current = {
      distanceFromBottomPx: nextDistance,
      layout: nextLayout
    };
    logAgentTranscriptSwitchDebug("measurement-commit", {
      agentSessionId,
      currentDistance,
      measuredHeightCount: Object.keys(nextHeightsByKey).length,
      nextDistance,
      nextLayoutHeightPx: nextLayout.totalHeightPx,
      previousLayoutHeightPx: previousLayout.totalHeightPx,
      renderedRange: virtualViewportRef.current.renderedRange
    });
    commitVirtualViewport(
      nextDistance,
      virtualViewportRef.current.viewportHeightPx,
      nextLayout
    );
  };

  const readViewportSnapshot = useCallback(
    (): AgentTranscriptViewportSnapshot => ({
      contentHeightPx:
        scrollMarginRef.current +
        layoutRef.current.totalHeightPx +
        scrollPaddingBottomRef.current,
      distanceFromBottomPx: virtualViewportRef.current.distanceFromBottomPx,
      scrollPaddingBottomPx: scrollPaddingBottomRef.current,
      scrollPaddingTopPx: scrollPaddingTopRef.current,
      scrollTopPx: scrollTopRef.current,
      viewportHeightPx: virtualViewportRef.current.viewportHeightPx
    }),
    []
  );

  const notifyViewportListeners = useCallback((): void => {
    const snapshot = readViewportSnapshot();
    for (const listener of viewportListenersRef.current) {
      listener(snapshot);
    }
  }, [readViewportSnapshot]);

  const notifyUserScrollListeners = useCallback(
    (direction: AgentTranscriptUserScrollDirection): void => {
      for (const listener of userScrollListenersRef.current) {
        listener(direction);
      }
    },
    []
  );
  const loadOlderWhileAtTop = useCallback(async (): Promise<void> => {
    if (topLoadingInFlightRef.current) return;
    topLoadingInFlightRef.current = true;
    try {
      for (;;) {
        const element = scrollElementRef.current;
        const handler = topLoadingHandlerRef.current;
        if (
          !element ||
          !handler ||
          agentTranscriptDistanceFromTop(element) >
            AGENT_TRANSCRIPT_TOP_LOADING_THRESHOLD_PX
        ) {
          return;
        }
        if ((await handler()) === "stop") return;
        await new Promise<void>((resolve) => {
          requestUiAnimationFrame(() => resolve());
        });
      }
    } finally {
      topLoadingInFlightRef.current = false;
    }
  }, []);

  const commitFromScrollElement = useCallback(
    (
      element: HTMLElement,
      viewportHeightPx = virtualViewportRef.current.viewportHeightPx,
      committedLayout = layoutRef.current
    ): number => {
      const actualScrollTop = element.scrollTop;
      const actualDistance = agentTranscriptDistanceFromBottom(
        actualScrollTop,
        scrollPaddingBottomRef.current
      );
      const previousState = virtualViewportRef.current;
      scrollTopRef.current = agentTranscriptLogicalScrollTop(
        actualScrollTop,
        viewportHeightPx,
        scrollMarginRef.current,
        committedLayout.totalHeightPx,
        scrollPaddingBottomRef.current
      );
      commitVirtualViewport(actualDistance, viewportHeightPx, committedLayout);
      logAgentTranscriptSwitchDebug("dom-position-committed", {
        agentSessionId,
        actualDistance,
        actualScrollTop,
        clientHeight: element.clientHeight,
        layoutHeightPx: committedLayout.totalHeightPx,
        nextRenderedRange: virtualViewportRef.current.renderedRange,
        previousDistance: previousState.distanceFromBottomPx,
        previousRenderedRange: previousState.renderedRange,
        scrollHeight: element.scrollHeight,
        scrollMarginPx: scrollMarginRef.current,
        scrollTopPx: scrollTopRef.current,
        viewportHeightPx
      });
      notifyViewportListeners();
      return actualDistance;
    },
    [
      agentSessionId,
      commitVirtualViewport,
      logAgentTranscriptSwitchDebug,
      notifyViewportListeners
    ]
  );

  const applyDistance = useCallback(
    (nextDistanceFromBottomPx: number, behavior: ScrollBehavior = "auto") => {
      const element = scrollElementRef.current;
      if (!element) return;
      const nextDistance = Math.max(0, nextDistanceFromBottomPx);
      const nativeScrollTop = agentTranscriptNativeScrollTopForDistance(
        nextDistance,
        scrollPaddingBottomRef.current
      );
      setAgentTranscriptScrollTop(element, nativeScrollTop, behavior, () =>
        commitFromScrollElement(element)
      );
    },
    [commitFromScrollElement]
  );

  const scrollToEnd = useCallback(
    (options?: { behavior?: ScrollBehavior }) => {
      applyDistance(0, options?.behavior);
    },
    [applyDistance]
  );

  const connectScrollElement = useCallback(
    (nextScrollElement: HTMLElement | null): void => {
      if (scrollElementRef.current === nextScrollElement) return;
      logAgentTranscriptSwitchDebug(
        nextScrollElement ? "viewport-connect" : "viewport-disconnect",
        {
          agentSessionId,
          currentDistance: virtualViewportRef.current.distanceFromBottomPx,
          currentRenderedRange: virtualViewportRef.current.renderedRange,
          layoutHeightPx: layoutRef.current.totalHeightPx,
          ...(nextScrollElement
            ? {
                clientHeight: nextScrollElement.clientHeight,
                nativeScrollTop: nextScrollElement.scrollTop,
                scrollHeight: nextScrollElement.scrollHeight
              }
            : {})
        }
      );
      disconnectScrollElementRef.current?.();
      disconnectScrollElementRef.current = null;
      scrollElementRef.current = nextScrollElement;
      if (!nextScrollElement) return;
      const previousOverflowAnchor = nextScrollElement.style.overflowAnchor;
      nextScrollElement.style.overflowAnchor = "none";
      const refreshScrollPadding = (): void => {
        const padding = readAgentTranscriptScrollPadding(nextScrollElement);
        scrollPaddingBottomBaseRef.current = padding.bottom;
        scrollPaddingBottomRef.current = padding.bottom;
        scrollPaddingTopRef.current = padding.top;
      };
      const updateFromScroll = (): {
        nextDistanceFromBottomPx: number;
        previousDistanceFromBottomPx: number;
      } | null => {
        const nextViewportHeightPx =
          virtualViewportRef.current.viewportHeightPx;
        if (nextViewportHeightPx <= 0) return null;
        const previousDistanceFromBottomPx =
          virtualViewportRef.current.distanceFromBottomPx;
        logAgentTranscriptSwitchDebug("native-scroll", {
          agentSessionId,
          clientHeight: nextScrollElement.clientHeight,
          nativeScrollTop: nextScrollElement.scrollTop,
          previousDistanceFromBottomPx,
          renderedRange: virtualViewportRef.current.renderedRange,
          scrollHeight: nextScrollElement.scrollHeight
        });
        if (layoutPreservation.restoreAfterScrollHeightChange() !== null) {
          // The browser may clamp the compensated target. Always commit the
          // actual DOM position below.
        }
        const nextDistanceFromBottomPx =
          commitFromScrollElement(nextScrollElement);
        return {
          nextDistanceFromBottomPx,
          previousDistanceFromBottomPx
        };
      };
      const disconnectInput = connectAgentTranscriptScrollInput({
        element: nextScrollElement,
        getViewportHeightPx: () => virtualViewportRef.current.viewportHeightPx,
        onCancelLayoutPreservation: layoutPreservation.cancel,
        onDirection: notifyUserScrollListeners,
        onScroll: updateFromScroll,
        onUserScrollToTop: () => {
          void loadOlderWhileAtTop();
        },
        onWheelDelta: layoutPreservation.addWheelDelta
      });
      const disconnectResize = resizeObservation.observe(
        nextScrollElement,
        (entry) => {
          const nextViewportHeightPx =
            entry.borderBoxSize?.[0]?.blockSize ?? entry.contentRect.height;
          if (nextViewportHeightPx <= 0) return;
          const nextDistance =
            followsEndRef.current && !movingDisclosureRef.current
              ? 0
              : virtualViewportRef.current.distanceFromBottomPx;
          const nextNativeScrollTop = agentTranscriptNativeScrollTopForDistance(
            nextDistance,
            scrollPaddingBottomRef.current
          );
          setAgentTranscriptScrollTop(
            nextScrollElement,
            nextNativeScrollTop,
            "auto",
            () =>
              commitFromScrollElement(nextScrollElement, nextViewportHeightPx)
          );
        }
      );
      refreshScrollPadding();
      const initialViewportHeightPx = nextScrollElement.clientHeight;
      if (initialViewportHeightPx > 0) {
        const initialDistance = 0;
        const initialNativeScrollTop =
          agentTranscriptNativeScrollTopForDistance(
            initialDistance,
            scrollPaddingBottomRef.current
          );
        setAgentTranscriptScrollTop(
          nextScrollElement,
          initialNativeScrollTop,
          "auto",
          () =>
            commitFromScrollElement(nextScrollElement, initialViewportHeightPx)
        );
      }
      disconnectScrollElementRef.current = () => {
        scrollPaddingBottomRef.current = 0;
        scrollPaddingBottomBaseRef.current = 0;
        scrollPaddingTopRef.current = 0;
        nextScrollElement.style.overflowAnchor = previousOverflowAnchor;
        disconnectInput();
        disconnectResize();
      };
    },
    [
      agentSessionId,
      notifyUserScrollListeners,
      commitFromScrollElement,
      layoutPreservation.addWheelDelta,
      layoutPreservation.cancel,
      layoutPreservation.restoreAfterScrollHeightChange,
      loadOlderWhileAtTop,
      logAgentTranscriptSwitchDebug,
      resizeObservation
    ]
  );

  const syncLayout = useCallback(
    (scrollMarginPx?: number): void => {
      if (scrollMarginPx !== undefined) {
        scrollMarginRef.current = scrollMarginPx;
      }
      const element = scrollElementRef.current;
      const previousLayout = layoutRef.current;
      const nextLayout = nextLayoutRef.current;
      const layoutChanged = !agentTranscriptVirtualLayoutsEqual(
        previousLayout,
        nextLayout
      );
      const pendingMeasuredLayout = pendingMeasuredLayoutRef.current;
      const hasPreparedMeasurementCommit =
        pendingMeasuredLayout !== null &&
        agentTranscriptVirtualLayoutsEqual(
          pendingMeasuredLayout.layout,
          nextLayout
        );
      if (hasPreparedMeasurementCommit) {
        pendingMeasuredLayoutRef.current = null;
      }
      logAgentTranscriptSwitchDebug("layout-sync", {
        agentSessionId,
        currentDistance: virtualViewportRef.current.distanceFromBottomPx,
        currentRenderedRange: virtualViewportRef.current.renderedRange,
        hasPreparedMeasurementCommit,
        layoutChanged,
        nextLayoutHeightPx: nextLayout.totalHeightPx,
        previousLayoutHeightPx: previousLayout.totalHeightPx,
        scrollMarginPx: scrollMarginRef.current
      });
      if (
        !layoutChanged &&
        committedScrollMarginRef.current === scrollMarginRef.current
      ) {
        layoutRef.current = nextLayout;
        return;
      }
      if (layoutChanged && !hasPreparedMeasurementCommit) {
        const preservedDistance = layoutPreservation.consumeDistance();
        if (preservedDistance !== null) {
          commitVirtualViewport(
            preservedDistance,
            virtualViewportRef.current.viewportHeightPx,
            previousLayout
          );
        }
      }
      let nextDistance =
        hasPreparedMeasurementCommit && pendingMeasuredLayout
          ? pendingMeasuredLayout.distanceFromBottomPx
          : virtualViewportRef.current.distanceFromBottomPx;
      const anchorKey = hasPreparedMeasurementCommit
        ? null
        : findAgentTranscriptCompensationAnchor({
            distanceFromBottomPx: nextDistance,
            fallbackRange: virtualViewportRef.current.renderedRange,
            layout: previousLayout,
            measuredHeightsByKey: measuredHeightsRef.current,
            viewportHeightPx: virtualViewportRef.current.viewportHeightPx
          });
      if (
        !hasPreparedMeasurementCommit &&
        followsEndRef.current &&
        !movingDisclosureRef.current &&
        activeLocateRef.current === null
      ) {
        nextDistance = 0;
      } else if (!hasPreparedMeasurementCommit && anchorKey) {
        nextDistance =
          compensateAgentTranscriptDistanceForAnchor({
            anchorKey,
            distanceFromBottomPx: nextDistance,
            nextLayout,
            previousLayout
          }) ?? nextDistance;
      }
      layoutRef.current = nextLayout;
      committedScrollMarginRef.current = scrollMarginRef.current;
      if (
        element &&
        !movingDisclosureRef.current &&
        activeLocateRef.current === null
      ) {
        const nextNativeScrollTop = agentTranscriptNativeScrollTopForDistance(
          nextDistance,
          scrollPaddingBottomRef.current
        );
        setAgentTranscriptScrollTop(element, nextNativeScrollTop, "auto", () =>
          commitFromScrollElement(
            element,
            virtualViewportRef.current.viewportHeightPx,
            nextLayout
          )
        );
        return;
      }
      if (element) {
        commitFromScrollElement(
          element,
          virtualViewportRef.current.viewportHeightPx,
          nextLayout
        );
        return;
      }
      commitVirtualViewport(
        nextDistance,
        virtualViewportRef.current.viewportHeightPx,
        nextLayout
      );
      notifyViewportListeners();
    },
    [
      agentSessionId,
      commitFromScrollElement,
      commitVirtualViewport,
      layoutPreservation.consumeDistance,
      logAgentTranscriptSwitchDebug,
      notifyViewportListeners
    ]
  );

  const getVirtualItems = useCallback(
    (): readonly AgentTranscriptVirtualItem[] =>
      layoutRef.current.turnKeys
        .slice(
          virtualViewportRef.current.renderedRange.startIndex,
          virtualViewportRef.current.renderedRange.endIndex
        )
        .map((key, relativeIndex) => {
          const index =
            virtualViewportRef.current.renderedRange.startIndex + relativeIndex;
          return {
            index,
            key,
            measured: measuredHeightsRef.current[key] !== undefined,
            size: layoutRef.current.heightsPx[index] ?? 0,
            start: layoutRef.current.topOffsetsPx[index] ?? 0
          };
        }),
    []
  );
  const scrollToIndex = useCallback(
    (
      index: number,
      options: { align: "center" | "top"; behavior?: ScrollBehavior }
    ) => {
      const key = layoutRef.current.turnKeys[index];
      if (!key) return;
      const nextDistance = distanceFromBottomForAgentTranscriptTurn({
        align: options.align,
        layout: layoutRef.current,
        turnKey: key,
        viewportHeightPx: virtualViewportRef.current.viewportHeightPx
      });
      if (nextDistance !== null) {
        applyDistance(nextDistance, options.behavior);
      }
    },
    [applyDistance]
  );
  const { cancelLocate, scrollToKey } = useAgentTranscriptVirtualLocate({
    activeLocateRef,
    applyDistance,
    layoutRef,
    measuredElementsRef,
    scrollElementRef,
    scrollPaddingBottomRef,
    scrollPaddingTopRef,
    scrollToIndex,
    setLocatingTurnKey,
    viewportStateRef: virtualViewportRef,
    virtualizerHostRef
  });

  const rowVirtualizer = useMemo<AgentTranscriptRowVirtualizer>(
    () => ({
      get scrollOffset() {
        return scrollElementRef.current ? scrollTopRef.current : null;
      },
      get scrollRect() {
        const height = scrollElementRef.current?.clientHeight;
        return height === undefined ? null : { height };
      },
      getVirtualItemForOffset: (offset) => {
        const index = findAgentTranscriptTurnIndexAtOffset(
          layoutRef.current,
          offset - scrollMarginRef.current
        );
        return index === null ? undefined : { index };
      },
      getVirtualItems,
      measureElement,
      subscribeViewport: (listener) => {
        viewportListenersRef.current.add(listener);
        listener(readViewportSnapshot());
        return () => viewportListenersRef.current.delete(listener);
      },
      connectScrollElement,
      scrollToIndex,
      scrollToKey,
      syncLayout
    }),
    [
      connectScrollElement,
      getVirtualItems,
      measureElement,
      readViewportSnapshot,
      scrollToIndex,
      scrollToKey,
      syncLayout
    ]
  );
  const virtualItems = useMemo<readonly AgentTranscriptVirtualItem[]>(() => {
    const renderedRange = projectAgentTranscriptVirtualRange({
      current: virtualViewportState,
      layout,
      locatingTurnKey
    });
    return layout.turnKeys
      .slice(renderedRange.startIndex, renderedRange.endIndex)
      .map((key, relativeIndex) => {
        const index = renderedRange.startIndex + relativeIndex;
        return {
          index,
          key,
          measured: measuredHeightsByKey[key] !== undefined,
          size: layout.heightsPx[index] ?? 0,
          start: layout.topOffsetsPx[index] ?? 0
        };
      });
  }, [layout, locatingTurnKey, measuredHeightsByKey, virtualViewportState]);
  const renderedRange = projectAgentTranscriptVirtualRange({
    current: virtualViewportState,
    layout,
    locatingTurnKey
  });

  useImperativeHandle(
    virtualScrollControllerRef,
    () => ({
      agentSessionId,
      enabled: true,
      cancelScroll: () => {
        const element = scrollElementRef.current;
        if (element) cancelAgentTranscriptScroll(element);
      },
      isAtEnd: (threshold = AGENT_TRANSCRIPT_END_THRESHOLD_PX) =>
        virtualViewportRef.current.distanceFromBottomPx <= threshold,
      scrollToEnd,
      setTopLoadingHandler: (handler) => {
        topLoadingHandlerRef.current = handler;
      },
      subscribeUserScroll: (listener) => {
        userScrollListenersRef.current.add(listener);
        return () => userScrollListenersRef.current.delete(listener);
      },
      subscribeViewport: (listener) => {
        viewportListenersRef.current.add(listener);
        listener(readViewportSnapshot());
        return () => viewportListenersRef.current.delete(listener);
      },
      syncViewport: ({ followEnd, scrollPaddingBottomAdjustmentPx = 0 }) => {
        const element = scrollElementRef.current;
        if (!element) return;
        scrollPaddingBottomRef.current =
          scrollPaddingBottomBaseRef.current +
          Math.max(0, scrollPaddingBottomAdjustmentPx);
        if (followEnd) {
          applyDistance(0);
          return;
        }
        applyDistance(virtualViewportRef.current.distanceFromBottomPx);
      }
    }),
    [agentSessionId, applyDistance, readViewportSnapshot, scrollToEnd]
  );

  const setVirtualizerHostElement = useCallback(
    (node: HTMLDivElement | null): void => {
      virtualizerHostRef.current = node;
      logAgentTranscriptSwitchDebug(
        node ? "host-ref-connect" : "host-ref-disconnect",
        {
          agentSessionId,
          connectedScrollElement: scrollElementRef.current !== null
        }
      );
      if (node) return;
      queueMicrotask(() => {
        if (virtualizerHostRef.current !== null) return;
        cancelLocate();
        topLoadingHandlerRef.current = null;
        layoutPreservation.cancel();
        connectScrollElement(null);
        disconnectMeasurements();
        resizeObservation.disconnect();
        const currentLayout = layoutRef.current;
        const retainedHeights: Record<string, number> = {};
        for (const key of currentLayout.turnKeys) {
          const height = measuredHeightsRef.current[key];
          if (height !== undefined) retainedHeights[key] = height;
        }
        writeAgentTranscriptVirtualMeasurements(agentSessionId, {
          turnHeightsByKey: retainedHeights
        });
        logAgentTranscriptSwitchDebug("unmount", {
          agentSessionId,
          layoutHeightPx: currentLayout.totalHeightPx,
          retainedHeightCount: Object.keys(retainedHeights).length,
          retainedRenderedRange: virtualViewportRef.current.renderedRange
        });
      });
    },
    []
  );

  return {
    layoutRevision: layoutRevisionRef.current,
    rowVirtualizer,
    setVirtualizerHostElement,
    totalHeightPx: layout.totalHeightPx,
    virtualItems,
    virtualizerHostRef,
    windowOffsetPx:
      layout.topOffsetsPx[renderedRange.startIndex] ?? layout.totalHeightPx
  };
}
