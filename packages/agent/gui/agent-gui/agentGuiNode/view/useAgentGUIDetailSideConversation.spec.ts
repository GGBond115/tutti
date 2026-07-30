import { describe, expect, it } from "vitest";
import {
  appendAgentSidePromptToDraft,
  parseAgentSideInvocation
} from "./useAgentGUIDetailSideConversation";

describe("parseAgentSideInvocation", () => {
  it("extracts a text-only Side prompt", () => {
    expect(
      parseAgentSideInvocation([{ type: "text", text: "/side inspect this" }])
    ).toEqual({ prompt: "inspect this", contentSupported: true });
  });

  it("rejects the whole invocation when any attachment would be lost", () => {
    expect(
      parseAgentSideInvocation([
        { type: "text", text: "/side inspect this" },
        { type: "file", path: "/tmp/context.txt" }
      ])
    ).toEqual({ prompt: "inspect this", contentSupported: false });
  });

  it("does not intercept ordinary main-conversation input", () => {
    expect(
      parseAgentSideInvocation([{ type: "text", text: "continue main" }])
    ).toBeNull();
  });
});

describe("appendAgentSidePromptToDraft", () => {
  it("moves a main /side prompt into an empty running Side draft", () => {
    expect(
      appendAgentSidePromptToDraft([{ type: "text", text: "" }], "inspect this")
    ).toEqual([{ type: "text", text: "inspect this" }]);
  });

  it("preserves an existing Side draft when another prompt is redirected", () => {
    expect(
      appendAgentSidePromptToDraft(
        [{ type: "text", text: "existing question" }],
        "additional context"
      )
    ).toEqual([
      {
        type: "text",
        text: "existing question\nadditional context"
      }
    ]);
  });
});
