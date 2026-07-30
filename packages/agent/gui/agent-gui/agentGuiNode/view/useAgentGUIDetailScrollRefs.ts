import { useRef } from "react";
import type { AgentTranscriptVirtualScrollController } from "../../../shared/agentConversation/components/AgentTranscriptView";
import type { TimelineScrollAnchor } from "./agentGUIScrollMemory";

export function useAgentGUIDetailScrollRefs() {
  return {
    bottomDockRef: useRef<HTMLDivElement | null>(null),
    pendingPrependScrollAnchorRef: useRef<{
      conversationId: string;
      scrollHeight: number;
      scrollTop: number;
    } | null>(null),
    submittedPromptScrollConversationRef: useRef<string | null>(null),
    timelineContentRef: useRef<HTMLDivElement | null>(null),
    timelineRef: useRef<HTMLDivElement | null>(null),
    timelineScrollAnchorRef: useRef<TimelineScrollAnchor | null>(null),
    virtualScrollControllerRef:
      useRef<AgentTranscriptVirtualScrollController | null>(null)
  };
}
