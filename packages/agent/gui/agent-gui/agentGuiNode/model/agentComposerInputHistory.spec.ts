import { describe, expect, it } from "vitest";
import type { AgentConversationVM } from "../../../shared/agentConversation/contracts/agentConversationVM";
import {
  agentComposerDraftImages,
  agentComposerDraftPrompt,
  buildAgentComposerDraft,
  emptyAgentComposerDraft
} from "./agentComposerDraft";
import {
  EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE,
  navigateAgentComposerInputHistory,
  projectAgentComposerInputHistory,
  resolvePendingAgentComposerInputHistory
} from "./agentComposerInputHistory";

describe("agent composer input history", () => {
  it("projects structured user input and keeps the newest adjacent duplicate", () => {
    const conversation = conversationWithUserMessages([
      {
        id: "message-1",
        body: "expanded prompt",
        sourceTimelineItems: [
          {
            payload: {
              content: [
                { type: "text", text: "expanded prompt" },
                {
                  type: "image",
                  attachmentId: "attachment-1",
                  mimeType: "image/png",
                  name: "one.png"
                }
              ],
              displayPrompt: "original prompt"
            }
          }
        ]
      },
      {
        id: "message-2",
        body: "original prompt",
        sourceTimelineItems: [
          {
            payload: {
              content: [
                { type: "text", text: "expanded prompt" },
                {
                  type: "image",
                  attachmentId: "attachment-1",
                  mimeType: "image/png",
                  name: "one.png"
                }
              ],
              displayPrompt: "original prompt"
            }
          }
        ]
      }
    ]);

    const history = projectAgentComposerInputHistory(conversation);

    expect(history).toHaveLength(1);
    expect(history[0]!.id).toBe("turn-1:message-2");
    expect(agentComposerDraftPrompt(history[0]!.draft)).toBe("original prompt");
    expect(agentComposerDraftImages(history[0]!.draft)).toEqual([
      expect.objectContaining({
        attachmentId: "attachment-1",
        previewUrl: ""
      })
    ]);
  });

  it("does not restore a synthetic image-only display prompt as text", () => {
    const history = projectAgentComposerInputHistory(
      conversationWithUserMessages([
        {
          id: "image-message",
          body: "[Image]",
          sourceTimelineItems: [
            {
              payload: {
                content: [
                  {
                    type: "image",
                    path: "/tmp/image.png",
                    mimeType: "image/png"
                  }
                ],
                displayPrompt: "[Image]"
              }
            }
          ]
        }
      ])
    );

    expect(agentComposerDraftPrompt(history[0]!.draft)).toBe("");
    expect(agentComposerDraftImages(history[0]!.draft)).toHaveLength(1);
  });

  it("navigates from empty to older entries and clears past the newest", () => {
    const entries = [
      { id: "one", draft: buildAgentComposerDraft({ prompt: "one" }) },
      { id: "two", draft: buildAgentComposerDraft({ prompt: "two" }) }
    ];
    const latest = navigateAgentComposerInputHistory({
      currentDraft: emptyAgentComposerDraft(),
      direction: "older",
      entries,
      hasOlderPage: false,
      state: EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE
    });
    const older = navigateAgentComposerInputHistory({
      currentDraft: latest.draft!,
      direction: "older",
      entries,
      hasOlderPage: false,
      state: latest.state
    });
    const newer = navigateAgentComposerInputHistory({
      currentDraft: older.draft!,
      direction: "newer",
      entries,
      hasOlderPage: false,
      state: older.state
    });
    const cleared = navigateAgentComposerInputHistory({
      currentDraft: newer.draft!,
      direction: "newer",
      entries,
      hasOlderPage: false,
      state: newer.state
    });

    expect(agentComposerDraftPrompt(latest.draft!)).toBe("two");
    expect(agentComposerDraftPrompt(older.draft!)).toBe("one");
    expect(agentComposerDraftPrompt(newer.draft!)).toBe("two");
    expect(cleared.state.entryId).toBeNull();
    expect(agentComposerDraftPrompt(cleared.draft!)).toBe("");
  });

  it("leaves a typed or edited draft to normal arrow-key behavior", () => {
    const entries = [
      { id: "one", draft: buildAgentComposerDraft({ prompt: "one" }) }
    ];

    expect(
      navigateAgentComposerInputHistory({
        currentDraft: buildAgentComposerDraft({ prompt: "typed" }),
        direction: "older",
        entries,
        hasOlderPage: false,
        state: EMPTY_AGENT_COMPOSER_INPUT_HISTORY_STATE
      }).handled
    ).toBe(false);
    expect(
      navigateAgentComposerInputHistory({
        currentDraft: buildAgentComposerDraft({ prompt: "edited" }),
        direction: "older",
        entries,
        hasOlderPage: false,
        state: { entryId: "one", pendingOlderPage: false }
      })
    ).toMatchObject({
      handled: false,
      state: { entryId: null }
    });
  });

  it("starts again from the newest entry after a recalled draft is cleared", () => {
    const entries = [
      { id: "one", draft: buildAgentComposerDraft({ prompt: "one" }) },
      { id: "two", draft: buildAgentComposerDraft({ prompt: "two" }) }
    ];

    const navigation = navigateAgentComposerInputHistory({
      currentDraft: emptyAgentComposerDraft(),
      direction: "older",
      entries,
      hasOlderPage: false,
      state: { entryId: "one", pendingOlderPage: false }
    });

    expect(navigation.state.entryId).toBe("two");
    expect(agentComposerDraftPrompt(navigation.draft!)).toBe("two");
  });

  it("requests an older page and resolves to the prepended entry", () => {
    const current = {
      id: "current",
      draft: buildAgentComposerDraft({ prompt: "current" })
    };
    const pending = navigateAgentComposerInputHistory({
      currentDraft: current.draft,
      direction: "older",
      entries: [current],
      hasOlderPage: true,
      state: { entryId: current.id, pendingOlderPage: false }
    });
    const older = {
      id: "older",
      draft: buildAgentComposerDraft({ prompt: "older" })
    };
    const resolved = resolvePendingAgentComposerInputHistory({
      entries: [older, current],
      state: pending.state
    });

    expect(pending).toMatchObject({
      handled: true,
      requestOlderPage: true,
      state: { pendingOlderPage: true }
    });
    expect(resolved?.state.entryId).toBe("older");
    expect(agentComposerDraftPrompt(resolved!.draft!)).toBe("older");
  });
});

function conversationWithUserMessages(
  messages: Array<{
    id: string;
    body: string;
    sourceTimelineItems?: Array<{
      payload: Record<string, unknown>;
    }>;
  }>
): AgentConversationVM {
  return {
    sourceDetail: {
      turns: [
        {
          id: "turn-1",
          userMessages: messages
        }
      ]
    }
  } as unknown as AgentConversationVM;
}
