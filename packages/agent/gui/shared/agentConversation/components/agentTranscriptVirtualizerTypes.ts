import type { RefObject } from "react";

export interface AgentTranscriptVirtualizer {
  layoutRevision: number;
  responseSpacerHeightPx: number;
  rowVirtualizer: AgentTranscriptRowVirtualizer;
  setVirtualizerHostElement(node: HTMLDivElement | null): void;
  totalHeightPx: number;
  virtualItems: readonly AgentTranscriptVirtualItem[];
  virtualizerHostRef: RefObject<HTMLDivElement | null>;
  windowOffsetPx: number;
}

export interface AgentTranscriptVirtualScrollController {
  agentSessionId: string;
  enabled: boolean;
  isAtEnd(threshold?: number): boolean;
  cancelScroll(): void;
  scrollToEnd(options?: { behavior?: ScrollBehavior }): void;
  setTopLoadingHandler(handler: (() => Promise<"stop" | void>) | null): void;
  subscribeUserScroll(
    listener: (direction: "away" | "toward-end") => void
  ): () => void;
  subscribeViewport(
    listener: (snapshot: AgentTranscriptViewportSnapshot) => void
  ): () => void;
  syncViewport(options: {
    followEnd: boolean;
    scrollPaddingBottomAdjustmentPx?: number;
  }): void;
}

export interface AgentTranscriptViewportSnapshot {
  contentHeightPx: number;
  distanceFromBottomPx: number;
  scrollPaddingBottomPx: number;
  scrollPaddingTopPx: number;
  scrollTopPx: number;
  viewportHeightPx: number;
}

export interface AgentTranscriptVirtualItem {
  index: number;
  key: string;
  measured: boolean;
  size: number;
  start: number;
}

export interface AgentTranscriptRowVirtualizer {
  readonly scrollOffset: number | null;
  readonly scrollRect: { readonly height: number } | null;
  getVirtualItemForOffset(
    offset: number
  ): { readonly index: number } | undefined;
  getVirtualItems(): readonly AgentTranscriptVirtualItem[];
  subscribeViewport(
    listener: (snapshot: AgentTranscriptViewportSnapshot) => void
  ): () => void;
  connectScrollElement(element: HTMLElement | null): void;
  measureElement(turnKey: string, element: HTMLElement | null): void;
  scrollToIndex(
    index: number,
    options: { align: "center" | "top"; behavior?: ScrollBehavior }
  ): void;
  scrollToKey(
    turnKey: string,
    findTarget?: () => HTMLElement | null,
    options?: {
      align?: "center" | "top";
      behavior?: ScrollBehavior;
      signal?: AbortSignal;
    }
  ): Promise<HTMLElement | null>;
  syncLayout(scrollMarginPx?: number): void;
}
