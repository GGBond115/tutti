import {
  AGENT_PASTED_TEXT_BLOCK_KIND,
  type AgentComposerDraft,
  type AgentComposerDraftContent,
  type AgentComposerDraftImage
} from "./agentGuiNodeTypes";

const draftRevisionByIdentity = new WeakMap<AgentComposerDraft, number>();

export function createAgentComposerDraftRevision(): number {
  const random = new Uint32Array(1);
  if (typeof globalThis.crypto?.getRandomValues === "function") {
    globalThis.crypto.getRandomValues(random);
    return random[0] || 1;
  }
  return Math.floor(Math.random() * 0xffffffff) + 1;
}

export function emptyAgentComposerDraft(): AgentComposerDraft {
  const draft: AgentComposerDraft = [{ type: "text", text: "" }];
  draftRevisionByIdentity.set(draft, createAgentComposerDraftRevision());
  return draft;
}

export function agentComposerDraftRevision(
  draft: AgentComposerDraft | undefined
): number | null {
  if (!draft) return null;
  return draftRevisionByIdentity.get(draft) ?? null;
}

export function withAgentComposerDraftRevision(
  draft: AgentComposerDraft,
  revision: number
): AgentComposerDraft {
  draftRevisionByIdentity.set(draft, revision);
  return draft;
}

type AgentComposerDraftIntentBlock =
  | { type: "text"; text: string }
  | { type: "quote"; id: string; text: string }
  | { type: "connector"; connectorKey: string }
  | {
      type: "image";
      id: string;
      name: string;
      mimeType: AgentComposerDraftImage["mimeType"];
    }
  | {
      type: "file";
      kind: "file";
      id: string;
      name: string;
      mimeType?: string;
    }
  | {
      type: "file";
      kind: typeof AGENT_PASTED_TEXT_BLOCK_KIND;
      id: string;
      name: string;
      mimeType?: string;
      text: string;
    };

type AgentComposerDraftPastedTextIntent = Extract<
  AgentComposerDraftIntentBlock,
  { type: "file"; kind: typeof AGENT_PASTED_TEXT_BLOCK_KIND }
>;

/** Projects only user-owned draft intent, excluding preparation metadata. */
export function projectAgentComposerDraftIntent(
  draft: AgentComposerDraft
): AgentComposerDraftIntentBlock[] {
  return draft.map((block) => {
    if (block.type === "text") {
      return { type: "text", text: block.text };
    }
    if (block.type === "image") {
      return {
        type: "image",
        id: block.id,
        name: block.name,
        mimeType: block.mimeType
      };
    }
    if (block.type === "quote") {
      return { type: "quote", id: block.id, text: block.text };
    }
    if (block.type === "connector") {
      return { type: "connector", connectorKey: block.connectorKey };
    }
    if (block.type === "file" && block.kind === AGENT_PASTED_TEXT_BLOCK_KIND) {
      const pastedTextBlock = block as Extract<
        AgentComposerDraftContent[number],
        { type: "file"; kind: typeof AGENT_PASTED_TEXT_BLOCK_KIND }
      >;
      return {
        type: "file",
        kind: AGENT_PASTED_TEXT_BLOCK_KIND,
        id: pastedTextBlock.id,
        name: pastedTextBlock.name,
        ...(pastedTextBlock.mimeType
          ? { mimeType: pastedTextBlock.mimeType }
          : {}),
        text: pastedTextBlock.text
      };
    }
    return {
      type: "file",
      kind: "file",
      id: block.id,
      name: block.name,
      ...(block.mimeType ? { mimeType: block.mimeType } : {})
    };
  });
}

export function agentComposerDraftIntentEqual(
  left: AgentComposerDraft,
  right: AgentComposerDraft
): boolean {
  const leftIntent = projectAgentComposerDraftIntent(left);
  const rightIntent = projectAgentComposerDraftIntent(right);
  if (leftIntent.length !== rightIntent.length) return false;
  return leftIntent.every((block, index) => {
    const other = rightIntent[index];
    if (!other || block.type !== other.type) return false;
    if (block.type === "text" && other.type === "text") {
      return block.text === other.text;
    }
    if (block.type === "image" && other.type === "image") {
      return (
        block.id === other.id &&
        block.name === other.name &&
        block.mimeType === other.mimeType
      );
    }
    if (block.type === "quote" && other.type === "quote") {
      return block.id === other.id && block.text === other.text;
    }
    if (block.type === "connector" && other.type === "connector") {
      return block.connectorKey === other.connectorKey;
    }
    if (block.type !== "file" || other.type !== "file") return false;
    const sameFileIntent =
      block.kind === other.kind &&
      block.id === other.id &&
      block.name === other.name &&
      block.mimeType === other.mimeType;
    if (!sameFileIntent) return false;
    if (
      block.kind !== AGENT_PASTED_TEXT_BLOCK_KIND ||
      other.kind !== AGENT_PASTED_TEXT_BLOCK_KIND
    ) {
      return true;
    }
    return (
      (block as AgentComposerDraftPastedTextIntent).text ===
      (other as AgentComposerDraftPastedTextIntent).text
    );
  });
}

export function snapshotAgentComposerDraft(
  draft: AgentComposerDraft
): AgentComposerDraft {
  const [textBlock, ...attachmentBlocks] = draft;
  const snapshot = [
    { ...textBlock },
    ...attachmentBlocks.map((block) => ({ ...block }))
  ] as AgentComposerDraft;
  return withAgentComposerDraftRevision(
    snapshot,
    agentComposerDraftRevision(draft) ?? createAgentComposerDraftRevision()
  );
}
