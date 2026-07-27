import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type MutableRefObject,
  type RefObject
} from "react";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import type { AgentTranscriptVirtualScrollController } from "../../../shared/agentConversation/components/AgentTranscriptView";
import type { AgentGUINodeViewModel } from "../model/agentGuiNodeTypes";
import type { AgentGUINodeViewProps } from "../AgentGUINodeView";
import {
  hasStaleVirtualScrollController,
  matchingVirtualScrollController,
  readBottomDockSafeArea,
  readTimelineGeometry,
  userScrollBehavior,
  writeBottomDockSafeArea,
  type BottomDockSafeArea
} from "./agentGUIDetailScrollHelpers";
import {
  setTimelineScrollTopInstantly,
  setTimelineScrollTopWithUserTransition
} from "./AgentGUIConversationTimelinePane";

const AGENT_GUI_STICK_TO_BOTTOM_THRESHOLD_PX = 24;
const AGENT_GUI_TOP_HISTORY_PREFETCH_THRESHOLD_PX = 240;
const AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX = 1;

interface Input {
  actions: AgentGUINodeViewProps["actions"];
  bottomDockRef: RefObject<HTMLDivElement | null>;
  bottomDockStoreRevision: string;
  conversation: AgentConversationVM | null;
  pendingPrependScrollAnchorRef: MutableRefObject<{
    conversationId: string;
    scrollHeight: number;
    scrollTop: number;
  } | null>;
  showTimelineSkeleton: boolean;
  submittedPromptScrollConversationRef: MutableRefObject<string | null>;
  timelineConversationId: string | null;
  timelineContentRef: RefObject<HTMLDivElement | null>;
  timelineRef: RefObject<HTMLDivElement | null>;
  timelineScrollAnchorRef: MutableRefObject<{
    conversationId: string;
    scrollHeight: number;
    scrollTop: number;
    clientHeight: number;
  } | null>;
  virtualScrollControllerRef: RefObject<AgentTranscriptVirtualScrollController | null>;
  viewModel: AgentGUINodeViewModel;
}

export function useAgentGUIDetailScroll(input: Input) {
  const {
    actions,
    bottomDockRef,
    bottomDockStoreRevision,
    conversation,
    pendingPrependScrollAnchorRef,
    showTimelineSkeleton,
    submittedPromptScrollConversationRef,
    timelineConversationId,
    timelineContentRef,
    timelineRef,
    timelineScrollAnchorRef,
    virtualScrollControllerRef,
    viewModel
  } = input;
  const [isTimelineScrolledToTop, setIsTimelineScrolledToTop] = useState(true);
  const [isTimelineScrolledToBottom, setIsTimelineScrolledToBottom] =
    useState(true);
  const bottomLockOwnerRef = useRef<string | null>(null);
  const pointerScrollConversationRef = useRef<string | null>(null);
  const userScrollAwayIntentConversationRef = useRef<string | null>(null);
  const lastShowTimelineSkeletonRef = useRef(showTimelineSkeleton);
  const bottomDockSafeAreaRef = useRef<BottomDockSafeArea | null>(null);
  useLayoutEffect(() => {
    const timelineSkeletonChanged =
      lastShowTimelineSkeletonRef.current !== showTimelineSkeleton;
    lastShowTimelineSkeletonRef.current = showTimelineSkeleton;
    const timeline = timelineRef.current;
    if (!timeline) {
      return;
    }
    const activeConversationId = timelineConversationId;
    if (!activeConversationId) {
      timelineScrollAnchorRef.current = null;
      bottomLockOwnerRef.current = null;
      pendingPrependScrollAnchorRef.current = null;
      pointerScrollConversationRef.current = null;
      submittedPromptScrollConversationRef.current = null;
      userScrollAwayIntentConversationRef.current = null;
      setIsTimelineScrolledToTop(true);
      setIsTimelineScrolledToBottom(true);
      return;
    }
    if (activeConversationId !== viewModel.rail.activeConversationId) {
      bottomLockOwnerRef.current = null;
      return;
    }
    if (
      hasStaleVirtualScrollController(
        virtualScrollControllerRef,
        activeConversationId
      )
    ) {
      bottomLockOwnerRef.current = null;
      return;
    }

    const anchor = timelineScrollAnchorRef.current;
    const prependAnchor = pendingPrependScrollAnchorRef.current;
    const shouldScrollSubmittedPromptToBottom =
      submittedPromptScrollConversationRef.current === activeConversationId;
    const conversationChanged =
      !anchor || anchor.conversationId !== activeConversationId;
    const shouldRestorePrependAnchor =
      prependAnchor?.conversationId === activeConversationId;
    if (
      !conversationChanged &&
      bottomLockOwnerRef.current === null &&
      anchor.scrollHeight - anchor.scrollTop - anchor.clientHeight <=
        AGENT_GUI_STICK_TO_BOTTOM_THRESHOLD_PX
    ) {
      bottomLockOwnerRef.current = activeConversationId;
    }
    if (conversationChanged && showTimelineSkeleton) {
      bottomLockOwnerRef.current = activeConversationId;
      pointerScrollConversationRef.current = null;
      userScrollAwayIntentConversationRef.current = null;
      setIsTimelineScrolledToTop(true);
      setIsTimelineScrolledToBottom(true);
      return;
    }
    if (
      !conversationChanged &&
      !shouldScrollSubmittedPromptToBottom &&
      !shouldRestorePrependAnchor &&
      !timelineSkeletonChanged
    ) {
      return;
    }
    const virtualScrollController = matchingVirtualScrollController(
      virtualScrollControllerRef,
      activeConversationId
    );
    if (virtualScrollController) {
      if (conversationChanged || shouldScrollSubmittedPromptToBottom) {
        bottomLockOwnerRef.current = activeConversationId;
        pointerScrollConversationRef.current = null;
        userScrollAwayIntentConversationRef.current = null;
        virtualScrollController.scrollToEnd({ behavior: "auto" });
        submittedPromptScrollConversationRef.current = null;
        if (shouldScrollSubmittedPromptToBottom) {
          pendingPrependScrollAnchorRef.current = null;
        }
      } else if (
        shouldRestorePrependAnchor &&
        !viewModel.detail.isLoadingOlderMessages
      ) {
        pendingPrependScrollAnchorRef.current = null;
      }
      const atBottom = virtualScrollController.isAtEnd();
      if (atBottom) {
        bottomLockOwnerRef.current = activeConversationId;
      }
      const virtualAnchor =
        anchor?.conversationId === activeConversationId
          ? anchor
          : {
              clientHeight: 0,
              conversationId: activeConversationId,
              scrollHeight: Number.POSITIVE_INFINITY,
              scrollTop: 0
            };
      timelineScrollAnchorRef.current = {
        ...virtualAnchor,
        conversationId: activeConversationId
      };
      setIsTimelineScrolledToTop(
        virtualAnchor.scrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
      );
      setIsTimelineScrolledToBottom(
        atBottom || bottomLockOwnerRef.current === activeConversationId
      );
      return;
    }
    const geometry = readTimelineGeometry(timeline);
    const maxScrollTop = geometry.maxScrollTop;
    let nextScrollTop: number;
    if (conversationChanged || shouldScrollSubmittedPromptToBottom) {
      bottomLockOwnerRef.current = activeConversationId;
      pointerScrollConversationRef.current = null;
      userScrollAwayIntentConversationRef.current = null;
    }
    const shouldKeepBottomLocked =
      bottomLockOwnerRef.current === activeConversationId;

    if (
      conversationChanged ||
      shouldScrollSubmittedPromptToBottom ||
      shouldKeepBottomLocked
    ) {
      setTimelineScrollTopInstantly(timeline, maxScrollTop);
      nextScrollTop = maxScrollTop;
      submittedPromptScrollConversationRef.current = null;
      if (shouldScrollSubmittedPromptToBottom) {
        pendingPrependScrollAnchorRef.current = null;
      }
    } else if (shouldRestorePrependAnchor && prependAnchor) {
      const nextScrollHeight = geometry.scrollHeight;
      const delta = nextScrollHeight - prependAnchor.scrollHeight;
      nextScrollTop = Math.max(0, prependAnchor.scrollTop + delta);
      timeline.scrollTop = nextScrollTop;
      if (viewModel.detail.isLoadingOlderMessages) {
        pendingPrependScrollAnchorRef.current = {
          conversationId: activeConversationId,
          scrollHeight: nextScrollHeight,
          scrollTop: nextScrollTop
        };
      } else {
        pendingPrependScrollAnchorRef.current = null;
      }
    } else {
      const distanceFromBottom =
        anchor.scrollHeight - anchor.scrollTop - anchor.clientHeight;
      if (distanceFromBottom <= AGENT_GUI_STICK_TO_BOTTOM_THRESHOLD_PX) {
        bottomLockOwnerRef.current = activeConversationId;
        setTimelineScrollTopInstantly(timeline, maxScrollTop);
        nextScrollTop = maxScrollTop;
      } else {
        nextScrollTop = Math.min(maxScrollTop, anchor.scrollTop);
        timeline.scrollTop = nextScrollTop;
      }
    }

    timelineScrollAnchorRef.current = {
      conversationId: activeConversationId,
      scrollHeight: geometry.scrollHeight,
      scrollTop: nextScrollTop,
      clientHeight: geometry.clientHeight
    };
    setIsTimelineScrolledToTop(
      nextScrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
    );
    setIsTimelineScrolledToBottom(
      maxScrollTop - nextScrollTop <= AGENT_GUI_STICK_TO_BOTTOM_THRESHOLD_PX
    );
  }, [
    conversation,
    showTimelineSkeleton,
    timelineConversationId,
    viewModel.rail.activeConversationId,
    viewModel.detail.isLoadingOlderMessages
  ]);

  const hasTimelineConversation = timelineConversationId !== null;
  useLayoutEffect(() => {
    const timeline = timelineRef.current;
    const bottomDock = bottomDockRef.current;
    if (!hasTimelineConversation || !timeline || !bottomDock) {
      return;
    }

    let animationFrameId: number | null = null;
    const resolveBottomLockConversation = (): string | null => {
      const activeConversationId = bottomLockOwnerRef.current;
      if (!activeConversationId) {
        return null;
      }
      const anchor = timelineScrollAnchorRef.current;
      if (!anchor || anchor.conversationId !== activeConversationId) {
        return null;
      }
      return activeConversationId;
    };

    const syncBottomDockSafeArea = (forceMeasurement: boolean): void => {
      const cachedSafeArea = bottomDockSafeAreaRef.current;
      if (
        !forceMeasurement &&
        cachedSafeArea?.bottomDock === bottomDock &&
        cachedSafeArea.revision === bottomDockStoreRevision
      ) {
        writeBottomDockSafeArea(timeline, cachedSafeArea);
        return;
      }
      const measuredSafeArea = readBottomDockSafeArea(bottomDock);
      const nextSafeArea: BottomDockSafeArea = {
        bottomDock,
        revision: bottomDockStoreRevision,
        ...measuredSafeArea
      };
      bottomDockSafeAreaRef.current = nextSafeArea;
      writeBottomDockSafeArea(timeline, nextSafeArea);
    };

    const syncConversationBottomLock = (): void => {
      const scheduledConversationId = resolveBottomLockConversation();
      if (!scheduledConversationId) {
        return;
      }

      if (animationFrameId !== null) {
        window.cancelAnimationFrame(animationFrameId);
      }
      animationFrameId = window.requestAnimationFrame(() => {
        animationFrameId = null;
        if (
          resolveBottomLockConversation() !== scheduledConversationId ||
          timelineRef.current !== timeline
        ) {
          return;
        }
        if (
          hasStaleVirtualScrollController(
            virtualScrollControllerRef,
            scheduledConversationId
          )
        ) {
          return;
        }
        const virtualScrollController = matchingVirtualScrollController(
          virtualScrollControllerRef,
          scheduledConversationId
        );
        if (virtualScrollController) {
          virtualScrollController.scrollToEnd({ behavior: "auto" });
          setIsTimelineScrolledToBottom(true);
          return;
        }
        const geometry = readTimelineGeometry(timeline);
        const maxScrollTop = geometry.maxScrollTop;
        timeline.scrollTop = maxScrollTop;
        timelineScrollAnchorRef.current = {
          conversationId: scheduledConversationId,
          scrollHeight: geometry.scrollHeight,
          scrollTop: maxScrollTop,
          clientHeight: geometry.clientHeight
        };
        setIsTimelineScrolledToTop(
          maxScrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
        );
        setIsTimelineScrolledToBottom(true);
      });
    };

    syncBottomDockSafeArea(false);
    syncConversationBottomLock();
    if (typeof ResizeObserver === "undefined") {
      return () => {
        timeline.style.removeProperty("--agent-gui-bottom-dock-safe-area");
        bottomDock.style.removeProperty(
          "--agent-gui-bottom-dock-floating-safe-area"
        );
        if (animationFrameId !== null) {
          window.cancelAnimationFrame(animationFrameId);
        }
      };
    }

    const observer = new ResizeObserver(() => {
      syncBottomDockSafeArea(true);
      syncConversationBottomLock();
    });
    observer.observe(bottomDock);
    const promptInputArea = bottomDock.querySelector(
      ".agent-gui-node__composer-prompt-input-area"
    );
    if (promptInputArea instanceof Element) {
      observer.observe(promptInputArea);
    }
    return () => {
      timeline.style.removeProperty("--agent-gui-bottom-dock-safe-area");
      bottomDock.style.removeProperty(
        "--agent-gui-bottom-dock-floating-safe-area"
      );
      if (animationFrameId !== null) {
        window.cancelAnimationFrame(animationFrameId);
      }
      observer.disconnect();
    };
  }, [bottomDockStoreRevision, hasTimelineConversation]);

  useEffect(() => {
    const timeline = timelineRef.current;
    const timelineContent = timelineContentRef.current;
    const activeConversationId = timelineConversationId;
    if (!timeline || !activeConversationId) {
      return;
    }

    const loadOlderMessagesNearTop = (
      scrollTop: number,
      scrollHeight: number,
      clientHeight: number
    ): void => {
      const virtualScrollController = matchingVirtualScrollController(
        virtualScrollControllerRef,
        activeConversationId
      );
      const bottomLocked =
        bottomLockOwnerRef.current === activeConversationId ||
        virtualScrollController?.isAtEnd() === true;
      const needsMoreContentToFillViewport = scrollHeight <= clientHeight;
      if (
        activeConversationId === viewModel.rail.activeConversationId &&
        viewModel.detail.hasOlderMessages &&
        !viewModel.detail.isLoadingOlderMessages &&
        !showTimelineSkeleton &&
        (!bottomLocked || needsMoreContentToFillViewport) &&
        scrollTop <= AGENT_GUI_TOP_HISTORY_PREFETCH_THRESHOLD_PX
      ) {
        pendingPrependScrollAnchorRef.current = {
          conversationId: activeConversationId,
          scrollHeight,
          scrollTop
        };
        actions.loadOlderConversationMessages();
      }
    };

    const captureScrollAnchor = (): void => {
      const previousAnchor = timelineScrollAnchorRef.current;
      if (
        !previousAnchor ||
        previousAnchor.conversationId !== activeConversationId
      ) {
        return;
      }
      let scrollTop = timeline.scrollTop;
      const pointerDrivenScrollAway =
        pointerScrollConversationRef.current === activeConversationId &&
        scrollTop < previousAnchor.scrollTop - 1;
      const explicitUserScrollAway =
        userScrollAwayIntentConversationRef.current === activeConversationId;
      if (explicitUserScrollAway || pointerDrivenScrollAway) {
        bottomLockOwnerRef.current = null;
        userScrollAwayIntentConversationRef.current = activeConversationId;
      }
      const virtualScrollController = matchingVirtualScrollController(
        virtualScrollControllerRef,
        activeConversationId
      );
      if (virtualScrollController) {
        const virtualizerAtBottom = virtualScrollController.isAtEnd();
        const scrollAwayPending =
          userScrollAwayIntentConversationRef.current === activeConversationId;
        const atBottom = virtualizerAtBottom && !scrollAwayPending;
        if (!virtualizerAtBottom && scrollAwayPending) {
          userScrollAwayIntentConversationRef.current = null;
        }
        if (atBottom) {
          bottomLockOwnerRef.current = activeConversationId;
        }
        timelineScrollAnchorRef.current = {
          conversationId: activeConversationId,
          scrollHeight: previousAnchor.scrollHeight,
          scrollTop,
          clientHeight: previousAnchor.clientHeight
        };
        const effectiveAtBottom =
          atBottom || bottomLockOwnerRef.current === activeConversationId;
        setIsTimelineScrolledToTop(
          scrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
        );
        setIsTimelineScrolledToBottom(effectiveAtBottom);
        loadOlderMessagesNearTop(
          scrollTop,
          previousAnchor.scrollHeight,
          previousAnchor.clientHeight
        );
        return;
      }
      const bottomLocked = bottomLockOwnerRef.current === activeConversationId;
      const anchoredMaxScrollTop = Math.max(
        0,
        previousAnchor.scrollHeight - previousAnchor.clientHeight
      );
      if (
        bottomLocked &&
        anchoredMaxScrollTop - scrollTop >
          AGENT_GUI_STICK_TO_BOTTOM_THRESHOLD_PX
      ) {
        setTimelineScrollTopInstantly(timeline, anchoredMaxScrollTop);
        scrollTop = anchoredMaxScrollTop;
      }
      timelineScrollAnchorRef.current = {
        conversationId: activeConversationId,
        scrollHeight: previousAnchor.scrollHeight,
        scrollTop,
        clientHeight: previousAnchor.clientHeight
      };
      const atBottom =
        previousAnchor.scrollHeight - scrollTop - previousAnchor.clientHeight <=
        AGENT_GUI_STICK_TO_BOTTOM_THRESHOLD_PX;
      const scrollAwayPending =
        userScrollAwayIntentConversationRef.current === activeConversationId;
      const effectiveAtBottom = atBottom && !scrollAwayPending;
      if (!atBottom && scrollAwayPending) {
        userScrollAwayIntentConversationRef.current = null;
      }
      if (effectiveAtBottom) {
        bottomLockOwnerRef.current = activeConversationId;
      }
      setIsTimelineScrolledToTop(
        scrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
      );
      setIsTimelineScrolledToBottom(
        effectiveAtBottom || bottomLockOwnerRef.current === activeConversationId
      );
      loadOlderMessagesNearTop(
        scrollTop,
        previousAnchor.scrollHeight,
        previousAnchor.clientHeight
      );
    };

    const syncObservedTimelineGeometry = (
      entries: readonly ResizeObserverEntry[]
    ): void => {
      const anchor = timelineScrollAnchorRef.current;
      if (!anchor || anchor.conversationId !== activeConversationId) {
        return;
      }

      const virtualScrollController = matchingVirtualScrollController(
        virtualScrollControllerRef,
        activeConversationId
      );
      if (virtualScrollController) {
        const scrollTop = timeline.scrollTop;
        const observedClientHeight = entries.find(
          (entry) => entry.target === timeline
        )?.contentRect.height;
        const observedScrollHeight = entries.find(
          (entry) => entry.target === timelineContent
        )?.contentRect.height;
        const clientHeight = observedClientHeight ?? anchor.clientHeight;
        const scrollHeight =
          observedScrollHeight === undefined
            ? anchor.scrollHeight
            : Math.max(clientHeight, observedScrollHeight);
        const virtualizerAtBottom = virtualScrollController.isAtEnd();
        const scrollAwayPending =
          userScrollAwayIntentConversationRef.current === activeConversationId;
        const atBottom = virtualizerAtBottom && !scrollAwayPending;
        if (!virtualizerAtBottom && scrollAwayPending) {
          userScrollAwayIntentConversationRef.current = null;
        }
        if (atBottom) {
          bottomLockOwnerRef.current = activeConversationId;
        }
        timelineScrollAnchorRef.current = {
          ...anchor,
          clientHeight,
          scrollHeight,
          scrollTop
        };
        setIsTimelineScrolledToTop(
          scrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
        );
        setIsTimelineScrolledToBottom(
          atBottom || bottomLockOwnerRef.current === activeConversationId
        );
        loadOlderMessagesNearTop(scrollTop, scrollHeight, clientHeight);
        return;
      }
      const geometry = readTimelineGeometry(timeline);
      const { clientHeight, maxScrollTop, scrollHeight } = geometry;
      const bottomLocked = bottomLockOwnerRef.current === activeConversationId;
      let scrollTop = Math.min(maxScrollTop, timeline.scrollTop);
      if (bottomLocked) {
        setTimelineScrollTopInstantly(timeline, maxScrollTop);
        scrollTop = maxScrollTop;
      }
      timelineScrollAnchorRef.current = {
        conversationId: activeConversationId,
        scrollHeight,
        scrollTop,
        clientHeight
      };
      const atBottom =
        maxScrollTop - scrollTop <= AGENT_GUI_STICK_TO_BOTTOM_THRESHOLD_PX;
      const scrollAwayPending =
        userScrollAwayIntentConversationRef.current === activeConversationId;
      const effectiveAtBottom = atBottom && !scrollAwayPending;
      if (!atBottom && scrollAwayPending) {
        userScrollAwayIntentConversationRef.current = null;
      }
      if (effectiveAtBottom) {
        bottomLockOwnerRef.current = activeConversationId;
      }
      setIsTimelineScrolledToTop(
        scrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
      );
      setIsTimelineScrolledToBottom(bottomLocked || effectiveAtBottom);
    };

    const captureWheelIntent = (event: WheelEvent): void => {
      if (event.deltaY < 0) {
        bottomLockOwnerRef.current = null;
        userScrollAwayIntentConversationRef.current = activeConversationId;
      } else if (event.deltaY > 0) {
        userScrollAwayIntentConversationRef.current = null;
      }
    };
    const captureKeyboardIntent = (event: KeyboardEvent): void => {
      if (
        event.key === "ArrowUp" ||
        event.key === "Home" ||
        event.key === "PageUp"
      ) {
        bottomLockOwnerRef.current = null;
        userScrollAwayIntentConversationRef.current = activeConversationId;
      }
    };
    const captureSemanticScrollAwayIntent = (event: MouseEvent): void => {
      if (
        event.target instanceof Element &&
        event.target.closest("[data-agent-transcript-scroll-away-intent]")
      ) {
        bottomLockOwnerRef.current = null;
        userScrollAwayIntentConversationRef.current = activeConversationId;
      }
    };
    const capturePointerIntent = (): void => {
      pointerScrollConversationRef.current = activeConversationId;
    };
    const clearPointerIntent = (): void => {
      if (pointerScrollConversationRef.current === activeConversationId) {
        pointerScrollConversationRef.current = null;
      }
    };

    const initialAnchor = timelineScrollAnchorRef.current;
    if (initialAnchor?.conversationId === activeConversationId) {
      loadOlderMessagesNearTop(
        initialAnchor.scrollTop,
        initialAnchor.scrollHeight,
        initialAnchor.clientHeight
      );
    }
    timeline.addEventListener("scroll", captureScrollAnchor, { passive: true });
    timeline.addEventListener("wheel", captureWheelIntent, { passive: true });
    timeline.addEventListener("keydown", captureKeyboardIntent);
    timeline.addEventListener("click", captureSemanticScrollAwayIntent);
    timeline.addEventListener("pointerdown", capturePointerIntent, {
      passive: true
    });
    window.addEventListener("pointerup", clearPointerIntent, { passive: true });
    window.addEventListener("pointercancel", clearPointerIntent, {
      passive: true
    });
    const geometryObserver =
      timelineContent && typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(syncObservedTimelineGeometry)
        : null;
    geometryObserver?.observe(timeline);
    if (timelineContent) {
      geometryObserver?.observe(timelineContent);
    }
    return () => {
      geometryObserver?.disconnect();
      timeline.removeEventListener("scroll", captureScrollAnchor);
      timeline.removeEventListener("wheel", captureWheelIntent);
      timeline.removeEventListener("keydown", captureKeyboardIntent);
      timeline.removeEventListener("click", captureSemanticScrollAwayIntent);
      timeline.removeEventListener("pointerdown", capturePointerIntent);
      window.removeEventListener("pointerup", clearPointerIntent);
      window.removeEventListener("pointercancel", clearPointerIntent);
    };
  }, [
    actions,
    timelineConversationId,
    showTimelineSkeleton,
    viewModel.rail.activeConversationId,
    viewModel.detail.hasOlderMessages,
    viewModel.detail.isLoadingOlderMessages
  ]);

  const scrollTimelineToBottom = useCallback(() => {
    const timeline = timelineRef.current;
    const activeConversationId = timelineConversationId;
    if (!timeline || !activeConversationId) {
      return;
    }
    if (activeConversationId !== viewModel.rail.activeConversationId) {
      return;
    }
    if (
      hasStaleVirtualScrollController(
        virtualScrollControllerRef,
        activeConversationId
      )
    ) {
      return;
    }

    const virtualScrollController = matchingVirtualScrollController(
      virtualScrollControllerRef,
      activeConversationId
    );
    if (virtualScrollController) {
      bottomLockOwnerRef.current = activeConversationId;
      userScrollAwayIntentConversationRef.current = null;
      virtualScrollController.scrollToEnd({
        behavior: userScrollBehavior()
      });
      setIsTimelineScrolledToBottom(true);
      return;
    }
    const geometry = readTimelineGeometry(timeline);
    const maxScrollTop = geometry.maxScrollTop;
    bottomLockOwnerRef.current = activeConversationId;
    userScrollAwayIntentConversationRef.current = null;
    setTimelineScrollTopWithUserTransition(timeline, maxScrollTop);
    timelineScrollAnchorRef.current = {
      conversationId: activeConversationId,
      scrollHeight: geometry.scrollHeight,
      scrollTop: maxScrollTop,
      clientHeight: geometry.clientHeight
    };
    setIsTimelineScrolledToTop(
      maxScrollTop <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
    );
    setIsTimelineScrolledToBottom(true);
  }, [
    timelineConversationId,
    viewModel.rail.activeConversationId,
    virtualScrollControllerRef
  ]);

  return {
    isTimelineScrolledToBottom,
    isTimelineScrolledToTop,
    scrollTimelineToBottom
  };
}
