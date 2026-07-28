import { useCallback, type JSX, type ReactNode } from "react";
import type { AgentTranscriptRowVirtualizer } from "./agentTranscriptVirtualizerTypes";

export function AgentTranscriptVirtualTurn({
  children,
  constrainedHeightPx,
  gapAfterPx,
  index,
  rowVirtualizer,
  turnKey
}: {
  children: ReactNode;
  constrainedHeightPx?: number;
  gapAfterPx: number;
  index: number;
  rowVirtualizer: AgentTranscriptRowVirtualizer;
  turnKey: string;
}): JSX.Element {
  const handleElement = useCallback(
    (element: HTMLDivElement | null) =>
      rowVirtualizer.measureElement(turnKey, element),
    [rowVirtualizer, turnKey]
  );
  return (
    <div
      ref={handleElement}
      className="agent-gui-transcript-virtual-item"
      data-index={index}
      data-agent-transcript-virtual-turn={turnKey}
      style={{
        height:
          constrainedHeightPx === undefined
            ? undefined
            : `${constrainedHeightPx}px`,
        marginBottom: `${gapAfterPx}px`,
        overflow: constrainedHeightPx === undefined ? undefined : "hidden"
      }}
    >
      {children}
    </div>
  );
}
