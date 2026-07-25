import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createI18nRuntime } from "@tutti-os/ui-i18n-runtime";
import {
  createWorkspaceFileManagerI18nRuntime,
  workspaceFileManagerI18nResources
} from "../i18n/workspaceFileManagerI18n.ts";
import type { WorkspaceFileManagerSession } from "../services/workspaceFileManagerService.interface.ts";

const panelProps = vi.hoisted(() => [] as Record<string, unknown>[]);
const iconViewportEnter = vi.hoisted(() => vi.fn());
const iconViewportLeave = vi.hoisted(() => vi.fn());
const panelsSnapshot = vi.hoisted(() => ({
  state: {
    directoryExpansionByPath: {},
    entries: [],
    error: null,
    expandedDirectoryPaths: {},
    isLoading: false
  },
  view: {
    canMove: true,
    contextMenuEntryPath: null,
    inlineRenameEntryPath: null,
    inlineRenameValidation: null,
    isRenaming: false,
    isSearchMode: false,
    isSearching: false,
    pendingDirectoryPath: null,
    previewState: { status: "empty" },
    searchEntries: [],
    searchError: null,
    selectedEntry: null,
    selectedPath: null
  }
}));

vi.mock("./WorkspaceFileManagerPanels.tsx", () => ({
  WorkspaceFileManagerPanels: (props: Record<string, unknown>) => {
    panelProps.push(props);
    return null;
  }
}));

vi.mock("./useWorkspaceFileEntryIconUrls.ts", () => ({
  useWorkspaceFileEntryIconUrls: () => ({
    iconUrlByCacheKey: new Map(),
    reportEntryIconViewportEnter: iconViewportEnter,
    reportEntryIconViewportLeave: iconViewportLeave
  })
}));

vi.mock("./useWorkspaceFileManagerService.ts", () => ({
  useWorkspaceFileManagerPanelsView: () => panelsSnapshot
}));

import { WorkspaceFileManagerPanelsContainer } from "./WorkspaceFileManagerPanelsContainer.tsx";

afterEach(() => {
  panelProps.length = 0;
  vi.restoreAllMocks();
});

describe("WorkspaceFileManagerPanelsContainer", () => {
  it("keeps entry callbacks stable across parent renders", async () => {
    const session = {
      cancelInlineRename: vi.fn(),
      clearInlineRenameValidation: vi.fn(),
      confirmInlineRename: vi.fn(),
      moveEntry: vi.fn(),
      openEntry: vi.fn(),
      select: vi.fn(),
      toggleDirectoryExpanded: vi.fn()
    } as unknown as WorkspaceFileManagerSession;
    const i18n = createWorkspaceFileManagerI18nRuntime(
      createI18nRuntime({
        dictionaries: [workspaceFileManagerI18nResources.en]
      })
    );
    const onOpenContextMenu = vi.fn();
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);
    const previousActEnvironment = (
      globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
    ).IS_REACT_ACT_ENVIRONMENT;
    (
      globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
    ).IS_REACT_ACT_ENVIRONMENT = true;
    const renderPanels = () => (
      <WorkspaceFileManagerPanelsContainer
        arrangeMode="none"
        i18n={i18n}
        layoutMode="list"
        onOpenContextMenu={onOpenContextMenu}
        session={session}
        showPreviewPanel={false}
      />
    );

    try {
      await act(async () => {
        root.render(renderPanels());
      });
      await act(async () => {
        root.render(renderPanels());
      });

      expect(panelProps).toHaveLength(2);
      for (const key of [
        "onBlankContextMenu",
        "onCancelInlineRename",
        "onClearInlineRenameValidation",
        "onConfirmInlineRename",
        "onMoveEntry",
        "onOpenEntry",
        "onSelect",
        "onToggleDirectoryExpanded",
        "state"
      ]) {
        expect(panelProps[1]?.[key]).toBe(panelProps[0]?.[key]);
      }
    } finally {
      await act(async () => {
        root.unmount();
      });
      container.remove();
      (
        globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
      ).IS_REACT_ACT_ENVIRONMENT = previousActEnvironment;
    }
  });
});
