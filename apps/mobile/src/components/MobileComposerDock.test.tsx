import { NativeSheet } from "@tutti-os/ui-system/native";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { Modal, TextInput } from "react-native";
import type { WorkspaceActivitySnapshot } from "../services/workspaceActivityService";
import { MobileComposerDock } from "./MobileComposerDock";

test("keeps settings and tools overlays mutually exclusive during rapid taps", () => {
  let renderer: ReactTestRenderer;
  act(() => {
    renderer = create(
      <MobileComposerDock
        model={createModel()}
        onDraftChange={() => undefined}
        onRefreshQuickPrompts={() => Promise.resolve()}
        onSend={() => undefined}
        onStop={() => undefined}
        onUpdate={() => undefined}
        quickPromptLibrary={{
          enabled: false,
          errorCode: null,
          prompts: [],
          status: "ready"
        }}
      />
    );
  });

  press(renderer!, "mobile-composer-model-settings");
  expect(visibleModals(renderer!)).toEqual([true, false]);

  press(renderer!, "mobile-composer-tools");
  expect(visibleModals(renderer!)).toEqual([false, true]);

  act(() => renderer!.root.findByType(NativeSheet).props.onOpenChange(false));
  expect(visibleModals(renderer!)).toEqual([false, true]);

  press(renderer!, "mobile-composer-model-settings");
  expect(visibleModals(renderer!)).toEqual([true, false]);

  act(() => renderer!.root.findAllByType(Modal)[1]?.props.onRequestClose());
  expect(visibleModals(renderer!)).toEqual([true, false]);
});

test("keeps the draft visible but disables editing and actions while unavailable", () => {
  let renderer: ReactTestRenderer;
  act(() => {
    renderer = create(
      <MobileComposerDock
        model={{
          ...createModel(),
          commandsAvailable: false,
          draft: "keep this draft"
        }}
        onDraftChange={() => undefined}
        onRefreshQuickPrompts={() => Promise.resolve()}
        onSend={() => undefined}
        onStop={() => undefined}
        onUpdate={() => undefined}
        quickPromptLibrary={{
          enabled: false,
          errorCode: null,
          prompts: [],
          status: "ready"
        }}
      />
    );
  });

  expect(renderer!.root.findByType(TextInput).props.editable).toBe(false);
  expect(
    renderer!.root.find((node) => node.props.testID === "mobile-composer-tools")
      .props.disabled
  ).toBe(true);
});

function press(renderer: ReactTestRenderer, testID: string): void {
  const target = renderer.root.find(
    (node) =>
      node.props.testID === testID && typeof node.props.onPress === "function"
  );
  act(() => target.props.onPress());
}

function visibleModals(renderer: ReactTestRenderer): boolean[] {
  return renderer.root
    .findAllByType(Modal)
    .map((modal) => modal.props.visible === true);
}

function createModel(): WorkspaceActivitySnapshot {
  return {
    activity: {
      presences: [],
      sessionMessagesById: {},
      sessions: [],
      workspaceId: "workspace"
    },
    ambiguousSubmission: false,
    composerOptions: {
      behavior: {
        collapseModelOptionsToLatest: false,
        modelOptionsAuthoritative: true,
        planModeExclusiveWithPermissionMode: false,
        prewarmDraftSession: false,
        refreshModelOptionsAfterSettings: false
      },
      capabilities: null,
      loadedAtUnixMs: 0,
      models: [{ label: "Test model", value: "test-model" }],
      modelConfigurable: true,
      provider: "test",
      reasoningEfforts: [],
      skills: [],
      speeds: []
    },
    composerOptionsLoadStatus: "ready",
    composerSettings: { model: "test-model" },
    composerSettingsSupport: {
      browser: false,
      computer: false,
      model: true,
      modelSwitch: true,
      permission: false,
      permissionModeChangeDeferred: false,
      permissionModeChangeDuringTurn: false,
      plan: false,
      planImplementation: false,
      reasoning: false,
      speed: false
    },
    commandsAvailable: true,
    conversation: null,
    creating: false,
    draft: "",
    errorCode: null,
    interactionStates: {},
    loading: false,
    pendingInteractions: [],
    pinningSessionIds: [],
    railErrorCode: null,
    railSections: [],
    railStatus: "ready",
    selectedAgentSessionId: null,
    selectedAgentTargetId: null,
    selectedSession: null,
    sending: false,
    targets: []
  };
}
