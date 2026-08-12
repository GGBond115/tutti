import { useEffect, useMemo } from "react";
import type { WorkbenchDockContext } from "../react/types.ts";
import type { WorkbenchDockPreviewCache } from "../react/dockPreviewCache.ts";
import { captureWorkbenchNodePreviewImage } from "../react/useWorkbenchGenieAnimation.tsx";
import {
  workbenchDockPreviewIdentity,
  workbenchNodePreviewRuntime
} from "../preview/workbenchNodePreviewRuntime.ts";
import { readWorkbenchHostExternalState } from "./externalState.ts";
import type { ResolvedWorkbenchHostDockEntry } from "./dockEntries.ts";
import { workbenchHostDockPopupPreviewViewport } from "./WorkbenchHostDockPopup.tsx";
import type {
  WorkbenchHostExternalStateSource,
  WorkbenchHostHandle,
  WorkbenchHostNodeData
} from "./types.ts";

export function useWorkbenchHostFocusedDockPreviewCapture(input: {
  context: WorkbenchDockContext<WorkbenchHostNodeData>;
  dockPreviewCache?: WorkbenchDockPreviewCache;
  enabled: boolean;
  externalStateRevision: number;
  externalStateSource?: WorkbenchHostExternalStateSource;
  host: WorkbenchHostHandle;
  resolvedEntries: readonly ResolvedWorkbenchHostDockEntry[];
  workspaceId: string;
}): void {
  const request = useMemo(() => {
    if (!input.enabled || !input.context.focusedNodeId) {
      return null;
    }
    const node = input.context.nodes.find(
      (candidate) => candidate.id === input.context.focusedNodeId
    );
    if (!node || node.isMinimized) {
      return null;
    }
    const resolvedEntry = input.resolvedEntries.find((entry) =>
      entry.matchedNodes.some((candidate) => candidate.id === node.id)
    );
    if (!resolvedEntry) {
      return null;
    }

    const externalState = readWorkbenchHostExternalState({
      externalStateSource: input.externalStateSource,
      node,
      workspaceId: input.workspaceId
    });
    const popupItem = {
      externalNodeState: externalState.externalNodeState,
      externalWorkspaceState: externalState.externalWorkspaceState,
      host: input.host,
      isFocused: true,
      isMinimized: false,
      node,
      previewViewport: workbenchHostDockPopupPreviewViewport
    };
    const descriptor = resolvedEntry.entry.resolvePopupItem?.(popupItem) ?? {};
    const providedPreview =
      descriptor.preview ??
      resolvedEntry.entry.providePopupItemPreview?.(popupItem) ??
      null;
    if (providedPreview) {
      return null;
    }
    const capture = resolvedEntry.entry.capturePopupItemPreview
      ? () => resolvedEntry.entry.capturePopupItemPreview?.(popupItem) ?? null
      : () => captureWorkbenchNodePreviewImage(node.id, { bypassCache: true });

    const key = {
      instanceId: node.data.instanceId,
      instanceKey: node.data.instanceKey ?? null,
      nodeId: node.id,
      revision: descriptor.revision ?? null,
      typeId: node.data.typeId,
      workspaceId: input.workspaceId
    };
    return { capture, key, nodeId: node.id };
  }, [
    input.context.focusedNodeId,
    input.context.nodes,
    input.enabled,
    input.externalStateRevision,
    input.externalStateSource,
    input.host,
    input.resolvedEntries,
    input.workspaceId
  ]);

  useEffect(() => {
    if (!request) {
      return;
    }
    void workbenchNodePreviewRuntime.ensure({
      capture: request.capture,
      identity: workbenchDockPreviewIdentity(request.key),
      nodeId: request.nodeId,
      writePersisted: input.dockPreviewCache
        ? (previewImageUrl) =>
            input.dockPreviewCache?.write({
              key: request.key,
              previewImageUrl
            })
        : undefined
    });
  }, [input.dockPreviewCache, request]);
}
