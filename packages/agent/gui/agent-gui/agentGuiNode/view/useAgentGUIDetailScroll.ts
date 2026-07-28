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
import type {
  AgentTranscriptViewportSnapshot,
  AgentTranscriptVirtualScrollController
} from "../../../shared/agentConversation/components/AgentTranscriptView";
import { AGENT_TRANSCRIPT_TOP_LOADING_THRESHOLD_PX } from "../../../shared/agentConversation/components/agentTranscriptScrollController";
import {
  createAgentConversationFollowEndController,
  type AgentConversationFollowEndEvent
} from "../../../shared/agentConversation/agentConversationFollowEndController";
import type { AgentGUINodeViewModel } from "../model/agentGuiNodeTypes";
import type { AgentGUINodeViewProps } from "../AgentGUINodeView";
import {
  hasStaleVirtualScrollController,
  matchingVirtualScrollController,
  readBottomDockSafeArea,
  userScrollBehavior,
  writeBottomDockSafeArea,
  type BottomDockSafeArea
} from "./agentGUIDetailScrollHelpers";

const AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX = 1;
const AGENT_GUI_REACHED_END_EPSILON_PX = 1;

interface Input {
  actions: AgentGUINodeViewProps["actions"];
  bottomDockRef: RefObject<HTMLDivElement | null>;
  bottomDockStoreRevision: string;
  conversation: AgentConversationVM | null;
  isVisible: boolean;
  pendingPrependScrollAnchorRef: MutableRefObject<{
    conversationId: string;
    scrollHeight: number;
    scrollTop: number;
  } | null>;
  showTimelineSkeleton: boolean;
  submittedPromptScrollConversationRef: MutableRefObject<string | null>;
  timelineConversationId: string | null;
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
    isVisible,
    pendingPrependScrollAnchorRef,
    showTimelineSkeleton,
    submittedPromptScrollConversationRef,
    timelineConversationId,
    timelineRef,
    timelineScrollAnchorRef,
    virtualScrollControllerRef,
    viewModel
  } = input;
  const [isTimelineScrolledToTop, setIsTimelineScrolledToTop] = useState(true);
  const followEndControllerRef = useRef(
    createAgentConversationFollowEndController()
  );
  const followEndController = followEndControllerRef.current;
  const [followEndMode, setFollowEndMode] = useState(
    followEndController.getSnapshot
  );
  const dispatchFollowEnd = useCallback(
    (event: AgentConversationFollowEndEvent): void => {
      setFollowEndMode(followEndController.dispatch(event));
    },
    [followEndController]
  );
  const userScrollDirectionRef = useRef<"away" | "toward-end" | null>(null);
  const lastTopLoadViewportRef = useRef<{
    contentHeightPx: number;
    conversationId: string;
    scrollTopPx: number;
  } | null>(null);
  const lastShowTimelineSkeletonRef = useRef(showTimelineSkeleton);
  const bottomDockSafeAreaRef = useRef<BottomDockSafeArea | null>(null);
  const [virtualScrollControllerRevision, setVirtualScrollControllerRevision] =
    useState(0);
  const setVirtualScrollController = useCallback(
    (controller: AgentTranscriptVirtualScrollController | null) => {
      if (virtualScrollControllerRef.current === controller) {
        return;
      }
      virtualScrollControllerRef.current = controller;
      if (controller) {
        setVirtualScrollControllerRevision((revision) => revision + 1);
      }
    },
    [virtualScrollControllerRef]
  );
  const lastVirtualScrollControllerRevisionRef = useRef(
    virtualScrollControllerRevision
  );
  useLayoutEffect(() => {
    if (!isVisible) {
      return;
    }
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
      pendingPrependScrollAnchorRef.current = null;
      userScrollDirectionRef.current = null;
      lastTopLoadViewportRef.current = null;
      submittedPromptScrollConversationRef.current = null;
      setIsTimelineScrolledToTop(true);
      return;
    }
    if (activeConversationId !== viewModel.rail.activeConversationId) {
      return;
    }
    const anchor = timelineScrollAnchorRef.current;
    const conversationChanged =
      !anchor || anchor.conversationId !== activeConversationId;
    if (conversationChanged) {
      dispatchFollowEnd("conversation-changed");
      userScrollDirectionRef.current = null;
      lastTopLoadViewportRef.current = null;
    }
    if (
      hasStaleVirtualScrollController(
        virtualScrollControllerRef,
        activeConversationId
      )
    ) {
      return;
    }

    const prependAnchor = pendingPrependScrollAnchorRef.current;
    const shouldScrollSubmittedPromptToBottom =
      submittedPromptScrollConversationRef.current === activeConversationId;
    const shouldRestorePrependAnchor =
      prependAnchor?.conversationId === activeConversationId;
    const virtualScrollControllerChanged =
      lastVirtualScrollControllerRevisionRef.current !==
      virtualScrollControllerRevision;
    if (conversationChanged && showTimelineSkeleton) {
      setIsTimelineScrolledToTop(true);
      return;
    }
    lastVirtualScrollControllerRevisionRef.current =
      virtualScrollControllerRevision;
    if (
      !conversationChanged &&
      !shouldScrollSubmittedPromptToBottom &&
      !shouldRestorePrependAnchor &&
      !timelineSkeletonChanged &&
      !virtualScrollControllerChanged
    ) {
      return;
    }
    const virtualScrollController = matchingVirtualScrollController(
      virtualScrollControllerRef,
      activeConversationId
    );
    if (!virtualScrollController) {
      return;
    }
    if (
      conversationChanged ||
      shouldScrollSubmittedPromptToBottom ||
      (virtualScrollControllerChanged &&
        followEndController.getSnapshot() === "following")
    ) {
      if (shouldScrollSubmittedPromptToBottom) {
        dispatchFollowEnd("prompt-submitted");
      }
      userScrollDirectionRef.current = null;
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
  }, [
    conversation,
    dispatchFollowEnd,
    followEndController,
    isVisible,
    showTimelineSkeleton,
    timelineConversationId,
    virtualScrollControllerRevision,
    viewModel.rail.activeConversationId,
    viewModel.detail.isLoadingOlderMessages
  ]);

  const hasTimelineConversation = timelineConversationId !== null;
  useLayoutEffect(() => {
    if (!isVisible) {
      return;
    }
    const timeline = timelineRef.current;
    const bottomDock = bottomDockRef.current;
    if (!hasTimelineConversation || !timeline || !bottomDock) {
      return;
    }

    const syncBottomDockSafeArea = (forceMeasurement: boolean): void => {
      const cachedSafeArea = bottomDockSafeAreaRef.current;
      if (
        !forceMeasurement &&
        cachedSafeArea?.bottomDock === bottomDock &&
        cachedSafeArea.revision === bottomDockStoreRevision
      ) {
        writeBottomDockSafeArea(timeline, cachedSafeArea);
        matchingVirtualScrollController(
          virtualScrollControllerRef,
          timelineConversationId
        )?.syncViewport({
          followEnd: followEndController.getSnapshot() === "following",
          scrollPaddingBottomAdjustmentPx: cachedSafeArea.timelineOverflowHeight
        });
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
      matchingVirtualScrollController(
        virtualScrollControllerRef,
        timelineConversationId
      )?.syncViewport({
        followEnd: followEndController.getSnapshot() === "following",
        scrollPaddingBottomAdjustmentPx: nextSafeArea.timelineOverflowHeight
      });
    };

    syncBottomDockSafeArea(false);
    if (typeof ResizeObserver === "undefined") {
      return () => {
        timeline.style.removeProperty("--agent-gui-bottom-dock-safe-area");
        bottomDock.style.removeProperty(
          "--agent-gui-bottom-dock-floating-safe-area"
        );
      };
    }

    const observer = new ResizeObserver(() => {
      syncBottomDockSafeArea(true);
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
      observer.disconnect();
    };
  }, [
    bottomDockStoreRevision,
    followEndController,
    hasTimelineConversation,
    isVisible
  ]);

  useEffect(() => {
    if (!isVisible) {
      return;
    }
    const timeline = timelineRef.current;
    const activeConversationId = timelineConversationId;
    if (!timeline || !activeConversationId) {
      return;
    }
    const virtualScrollController = matchingVirtualScrollController(
      virtualScrollControllerRef,
      activeConversationId
    );
    if (!virtualScrollController) {
      return;
    }

    const loadOlderMessagesAtTop = async (): Promise<"stop" | void> => {
      const anchor = timelineScrollAnchorRef.current;
      const previousLoadViewport = lastTopLoadViewportRef.current;
      const bottomLocked = followEndController.getSnapshot() === "following";
      const needsMoreContentToFillViewport =
        anchor !== null && anchor.scrollHeight <= anchor.clientHeight;
      if (
        anchor?.conversationId === activeConversationId &&
        activeConversationId === viewModel.rail.activeConversationId &&
        viewModel.detail.hasOlderMessages &&
        !viewModel.detail.isLoadingOlderMessages &&
        !showTimelineSkeleton &&
        (!bottomLocked || needsMoreContentToFillViewport)
      ) {
        if (
          previousLoadViewport?.conversationId === activeConversationId &&
          previousLoadViewport.contentHeightPx === anchor.scrollHeight &&
          previousLoadViewport.scrollTopPx === anchor.scrollTop
        ) {
          lastTopLoadViewportRef.current = null;
          return "stop";
        }
        lastTopLoadViewportRef.current = {
          contentHeightPx: anchor.scrollHeight,
          conversationId: activeConversationId,
          scrollTopPx: anchor.scrollTop
        };
        pendingPrependScrollAnchorRef.current = {
          conversationId: activeConversationId,
          scrollHeight: anchor.scrollHeight,
          scrollTop: anchor.scrollTop
        };
        await actions.loadOlderConversationMessages();
        return;
      }
      return "stop";
    };
    virtualScrollController.setTopLoadingHandler(loadOlderMessagesAtTop);

    const captureVirtualViewport = (
      snapshot: AgentTranscriptViewportSnapshot
    ): void => {
      if (
        followEndController.getSnapshot() === "detached" &&
        userScrollDirectionRef.current === "toward-end" &&
        snapshot.distanceFromBottomPx <= AGENT_GUI_REACHED_END_EPSILON_PX
      ) {
        dispatchFollowEnd("user-reached-end");
      }
      timelineScrollAnchorRef.current = {
        clientHeight: snapshot.viewportHeightPx,
        conversationId: activeConversationId,
        scrollHeight: snapshot.contentHeightPx,
        scrollTop: snapshot.scrollTopPx
      };
      if (snapshot.scrollTopPx > AGENT_TRANSCRIPT_TOP_LOADING_THRESHOLD_PX) {
        lastTopLoadViewportRef.current = null;
      }
      setIsTimelineScrolledToTop(
        snapshot.scrollTopPx <= AGENT_GUI_TOP_MASK_SCROLL_EPSILON_PX
      );
    };
    const captureUserScroll = (direction: "away" | "toward-end"): void => {
      userScrollDirectionRef.current = direction;
      if (direction === "away") {
        dispatchFollowEnd("user-scrolled-away");
      }
    };

    const unsubscribeViewport = virtualScrollController.subscribeViewport(
      captureVirtualViewport
    );
    const unsubscribeUserScroll =
      virtualScrollController.subscribeUserScroll(captureUserScroll);
    return () => {
      virtualScrollController.setTopLoadingHandler(null);
      unsubscribeViewport();
      unsubscribeUserScroll();
    };
  }, [
    actions,
    dispatchFollowEnd,
    followEndController,
    isVisible,
    timelineConversationId,
    showTimelineSkeleton,
    viewModel.rail.activeConversationId,
    viewModel.detail.hasOlderMessages,
    viewModel.detail.isLoadingOlderMessages
  ]);

  const scrollTimelineToBottom = useCallback(() => {
    const timeline = timelineRef.current;
    const activeConversationId = timelineConversationId;
    if (!isVisible || !timeline || !activeConversationId) {
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
    if (!virtualScrollController) {
      return;
    }
    dispatchFollowEnd("scroll-to-end-requested");
    userScrollDirectionRef.current = null;
    virtualScrollController.scrollToEnd({
      behavior: userScrollBehavior()
    });
  }, [
    dispatchFollowEnd,
    isVisible,
    timelineConversationId,
    viewModel.rail.activeConversationId,
    virtualScrollControllerRef
  ]);

  return {
    followEndMode,
    isTimelineScrolledToBottom: followEndMode === "following",
    isTimelineScrolledToTop,
    setVirtualScrollController,
    scrollTimelineToBottom
  };
}
