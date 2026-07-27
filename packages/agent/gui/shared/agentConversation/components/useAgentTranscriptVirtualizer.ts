import {
  useCallback,
  useImperativeHandle,
  useRef,
  type Ref,
  type RefObject
} from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { AgentTranscriptTurnGroup } from "./agentTranscriptModel";
import { signalAgentMessageLocatorScrollToEnd } from "./useAgentMessageLocatorSelection";

const AGENT_TRANSCRIPT_VIRTUALIZATION_OVERSCAN = 6;
export const AGENT_TRANSCRIPT_ESTIMATED_TURN_HEIGHT_PX = 280;
const preventVirtualScrollAdjustment = () => false;

export interface AgentTranscriptVirtualScrollController {
  agentSessionId: string;
  enabled: boolean;
  isAtEnd(): boolean;
  scrollToEnd(options?: { behavior?: ScrollBehavior }): void;
}

interface AgentTranscriptVirtualizer {
  setVirtualizerHostElement(node: HTMLDivElement | null): void;
  rowVirtualizer: ReturnType<typeof useVirtualizer<HTMLElement, Element>>;
  virtualizerHostRef: RefObject<HTMLDivElement | null>;
}

export function useAgentTranscriptVirtualizer({
  agentSessionId,
  hasMovingTurnDisclosure,
  isVisible,
  scrollElement,
  scrollMargin,
  shouldVirtualize,
  turnGroups,
  virtualScrollControllerRef
}: {
  agentSessionId: string;
  hasMovingTurnDisclosure: boolean;
  isVisible: boolean;
  scrollElement: HTMLElement | null;
  scrollMargin: number;
  shouldVirtualize: boolean;
  turnGroups: readonly AgentTranscriptTurnGroup[];
  virtualScrollControllerRef?: Ref<AgentTranscriptVirtualScrollController>;
}): AgentTranscriptVirtualizer {
  const virtualizerHostRef = useRef<HTMLDivElement | null>(null);
  const virtualizationEnabled = shouldVirtualize && isVisible;
  const getVirtualItemKey = useCallback(
    (index: number) =>
      `${agentSessionId}\u0000${turnGroups[index]?.key ?? index}`,
    [agentSessionId, turnGroups]
  );
  const rowVirtualizer = useVirtualizer<HTMLElement, Element>({
    anchorTo:
      virtualizationEnabled && hasMovingTurnDisclosure ? "start" : "end",
    count: turnGroups.length,
    directDomUpdates: true,
    directDomUpdatesMode: "transform",
    enabled: virtualizationEnabled,
    estimateSize: () => AGENT_TRANSCRIPT_ESTIMATED_TURN_HEIGHT_PX,
    followOnAppend: virtualizationEnabled && !hasMovingTurnDisclosure,
    getItemKey: getVirtualItemKey,
    getScrollElement: () => scrollElement,
    overscan: AGENT_TRANSCRIPT_VIRTUALIZATION_OVERSCAN,
    scrollMargin,
    scrollEndThreshold: 24,
    useFlushSync: true
  });
  rowVirtualizer.shouldAdjustScrollPositionOnItemSizeChange =
    virtualizationEnabled && hasMovingTurnDisclosure
      ? preventVirtualScrollAdjustment
      : undefined;
  useImperativeHandle(
    virtualScrollControllerRef,
    () => ({
      agentSessionId,
      enabled: virtualizationEnabled,
      isAtEnd: () => virtualizationEnabled && rowVirtualizer.isAtEnd(),
      scrollToEnd: (options) => {
        if (virtualizationEnabled) {
          signalAgentMessageLocatorScrollToEnd(scrollElement);
          rowVirtualizer.scrollToEnd(options);
        }
      }
    }),
    [agentSessionId, rowVirtualizer, scrollElement, virtualizationEnabled]
  );
  const setVirtualizerHostElement = useCallback(
    (node: HTMLDivElement | null) => {
      virtualizerHostRef.current = node;
      rowVirtualizer.containerRef(node);
    },
    [rowVirtualizer]
  );

  return {
    setVirtualizerHostElement,
    rowVirtualizer,
    virtualizerHostRef
  };
}
