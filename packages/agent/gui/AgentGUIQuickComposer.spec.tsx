import { render } from "@testing-library/react";
import { createRichTextMentionService } from "@tutti-os/ui-rich-text/service";
import type { RichTextTriggerProvider } from "@tutti-os/ui-rich-text/types";
import { describe, expect, it, vi } from "vitest";
import { AgentGUIQuickComposer } from "./AgentGUIQuickComposer";
import type { AgentGUIAgentTarget } from "./types";

const agentTargets = [
  {
    agentTargetId: "agent:codex",
    iconUrl: "/codex.png",
    label: "Codex",
    provider: "codex",
    ref: { kind: "test", provider: "codex" },
    targetId: "agent:codex"
  }
] satisfies AgentGUIAgentTarget[];

describe("AgentGUIQuickComposer", () => {
  it("fills host-owned height only when requested", () => {
    const { container } = render(
      <AgentGUIQuickComposer
        agentTargets={agentTargets}
        content={[{ text: "", type: "text" }]}
        fillAvailableHeight={true}
        selectedAgentTargetId="agent:codex"
        workspaceId="workspace:test"
        onAgentTargetChange={vi.fn()}
        onContentChange={vi.fn()}
        onSubmit={vi.fn()}
      />
    );

    const composer = container.querySelector(
      'form[data-layout="embedded"][data-fill-available-height="true"]'
    );

    expect(composer).not.toBeNull();
    expect(
      composer?.querySelector(".agent-gui-node__rich-text-editor-surface")
    ).not.toBeNull();
    expect(
      composer?.querySelector(".agent-gui-node__rich-text-editor-content")
    ).not.toBeNull();
  });

  it("uses the in-flow embedded layout for image drafts", () => {
    const { container } = render(
      <AgentGUIQuickComposer
        agentTargets={agentTargets}
        content={[
          { text: "Inspect this screenshot", type: "text" },
          {
            data: "iVBORw0KGgo=",
            mimeType: "image/png",
            name: "screenshot.png",
            type: "image"
          }
        ]}
        selectedAgentTargetId="agent:codex"
        workspaceId="workspace:test"
        onAgentTargetChange={vi.fn()}
        onContentChange={vi.fn()}
        onSubmit={vi.fn()}
      />
    );

    const composer = container.querySelector<HTMLFormElement>(
      'form[data-layout="embedded"]'
    );
    const promptInputArea = composer?.querySelector(
      ".agent-gui-node__composer-prompt-input-area"
    );

    expect(composer).not.toBeNull();
    expect(
      promptInputArea?.querySelector(
        '[data-testid="agent-gui-composer-image-draft"]'
      )
    ).not.toBeNull();
  });

  it("enables workspace references when the embedding host supplies the picker", () => {
    const { container } = render(
      <AgentGUIQuickComposer
        agentTargets={agentTargets}
        content={[{ text: "", type: "text" }]}
        selectedAgentTargetId="agent:codex"
        workspaceId="workspace:test"
        onAgentTargetChange={vi.fn()}
        onContentChange={vi.fn()}
        onRequestWorkspaceReferences={vi.fn().mockResolvedValue({
          files: [],
          mentionItems: []
        })}
        onSubmit={vi.fn()}
      />
    );

    const addIcon = container.querySelector(
      '[data-agent-reference-add-icon="true"]'
    );
    expect(addIcon?.closest("button")?.hasAttribute("disabled")).toBe(false);
  });

  it("renders a host action accessory beside send inside the AgentGUI token scope", () => {
    const { container } = render(
      <AgentGUIQuickComposer
        agentTargets={agentTargets}
        composerActionAccessory={
          <span data-testid="quick-composer-action-accessory">Track Task</span>
        }
        content={[{ text: "Inspect this", type: "text" }]}
        selectedAgentTargetId="agent:codex"
        workspaceId="workspace:test"
        onAgentTargetChange={vi.fn()}
        onContentChange={vi.fn()}
        onSubmit={vi.fn()}
      />
    );

    const scope = container.querySelector(".agent-gui-node__shell");
    const accessory = container.querySelector(
      '[data-testid="quick-composer-action-accessory"]'
    );
    const send = container.querySelector(
      '[data-testid="agent-gui-composer-send"]'
    );

    expect(scope).not.toBeNull();
    expect(scope?.contains(accessory)).toBe(true);
    expect(accessory?.parentElement).toBe(send?.parentElement);
  });

  it("installs the mention service supplied by the embedding host", () => {
    const query = vi.fn().mockResolvedValue([]);
    const provider: RichTextTriggerProvider<{ id: string; label: string }> = {
      id: "file",
      trigger: "@",
      query,
      getItemKey: (item) => item.id,
      getItemLabel: (item) => item.label,
      toInsertResult: (item) => ({
        href: `/workspace/${item.id}`,
        kind: "markdown-link",
        label: item.label
      })
    };
    const mentionService = createRichTextMentionService({
      providers: [provider]
    });
    const listProviders = vi.spyOn(mentionService, "listProviders");
    const { unmount } = render(
      <AgentGUIQuickComposer
        agentTargets={agentTargets}
        content={[{ text: "", type: "text" }]}
        mentionService={mentionService}
        selectedAgentTargetId="agent:codex"
        workspaceId="workspace:test"
        onAgentTargetChange={vi.fn()}
        onContentChange={vi.fn()}
        onSubmit={vi.fn()}
      />
    );

    expect(listProviders).toHaveBeenCalled();

    unmount();
    mentionService.dispose();
  });
});
