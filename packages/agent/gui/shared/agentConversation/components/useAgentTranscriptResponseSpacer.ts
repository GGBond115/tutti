import { useCallback, useRef, useState } from "react";
import type { AgentConversationFollowEndMode } from "../agentConversationFollowEndController";
import { agentTranscriptResponseSpacerHeight } from "./agentTranscriptVirtualizerLayout";

export function useAgentTranscriptResponseSpacer(input: {
  bottomInsetPx(): number;
  followEndMode: AgentConversationFollowEndMode;
  latestTurnKey: string | null;
}) {
  const [spacer, setSpacer] = useState<{
    heightPx: number;
    turnKey: string;
  } | null>(null);
  const followsEnd = input.followEndMode === "following";
  const heightPx = spacer?.heightPx ?? 0;
  const followsEndRef = useRef(followsEnd);
  const latestTurnKeyRef = useRef(input.latestTurnKey);
  const bottomInsetPxRef = useRef(input.bottomInsetPx);
  const spacerRef = useRef(spacer);
  const heightRef = useRef(heightPx);
  const updateForViewportRef = useRef<(heightPx: number) => void>(() => {});

  followsEndRef.current = followsEnd;
  latestTurnKeyRef.current = input.latestTurnKey;
  bottomInsetPxRef.current = input.bottomInsetPx;
  spacerRef.current = spacer;
  heightRef.current = heightPx;
  updateForViewportRef.current = (viewportHeightPx) => {
    if (followsEndRef.current && latestTurnKeyRef.current === null) {
      if (spacerRef.current === null) return;
      spacerRef.current = null;
      heightRef.current = 0;
      setSpacer(null);
      return;
    }
    const activeTurnKey = followsEndRef.current
      ? latestTurnKeyRef.current
      : null;
    const spacerTurnKey = activeTurnKey ?? spacerRef.current?.turnKey ?? null;
    if (!spacerTurnKey) return;
    const nextHeightPx = agentTranscriptResponseSpacerHeight({
      bottomInsetPx: bottomInsetPxRef.current(),
      viewportHeightPx
    });
    const nextSpacer = { heightPx: nextHeightPx, turnKey: spacerTurnKey };
    if (
      spacerRef.current?.turnKey === spacerTurnKey &&
      spacerRef.current.heightPx === nextHeightPx
    ) {
      return;
    }
    spacerRef.current = nextSpacer;
    heightRef.current = nextHeightPx;
    setSpacer(nextSpacer);
  };

  const growHeight = useCallback((heightDeltaPx: number): void => {
    if (heightDeltaPx <= 0 || spacerRef.current === null) return;
    const nextHeightPx = spacerRef.current.heightPx + heightDeltaPx;
    const nextSpacer = {
      heightPx: nextHeightPx,
      turnKey: spacerRef.current.turnKey
    };
    spacerRef.current = nextSpacer;
    heightRef.current = nextHeightPx;
    setSpacer(nextSpacer);
  }, []);

  return {
    activationKey: followsEnd ? input.latestTurnKey : null,
    growHeight,
    heightPx,
    heightRef,
    spacerRef,
    updateForViewportRef
  };
}
