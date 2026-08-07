import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentGUIQuickComposer } from "./AgentGUIQuickComposer";

describe("AgentGUIQuickComposer", () => {
  it("uses the in-flow embedded layout for image drafts", () => {
    const { container } = render(
      <AgentGUIQuickComposer
        agentTargets={[
          {
            agentTargetId: "agent:codex",
            iconUrl: "/codex.png",
            label: "Codex",
            provider: "codex",
            ref: { kind: "test", provider: "codex" },
            targetId: "agent:codex"
          }
        ]}
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
});
