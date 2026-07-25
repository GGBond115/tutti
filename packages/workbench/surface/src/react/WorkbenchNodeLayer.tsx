import { Fragment, memo, useMemo } from "react";
import { createPortal } from "react-dom";
import {
  selectFocusedWorkbenchNode,
  selectWorkbenchNodeZIndex,
  selectWorkbenchSnapPreviewRect
} from "../core/selectors.ts";
import type { WorkbenchNode } from "../core/types.ts";
import type {
  WorkbenchKeepMinimizedNodeMounted,
  WorkbenchRenderNode,
  WorkbenchSurfacePresentation,
  WorkbenchRenderWindowActions,
  WorkbenchRenderWindowHeader,
  WorkbenchResolveFullscreenHeaderMode,
  WorkbenchResolveWindowSurfaceLayer,
  WorkbenchResolveWindowZIndex,
  WorkbenchResolveWindowChromeMode,
  WorkbenchResolveWindowHeaderPresentation,
  WorkbenchWindowChromeMode
} from "./types.ts";
import type { WorkbenchGenieController } from "./useWorkbenchGenieAnimation.tsx";
import type { WorkbenchGenieNodeVisibility } from "./genieNodeVisibility.ts";
import { useWorkbenchController } from "./WorkbenchProvider.tsx";
import { WorkbenchWindowFrame } from "./WorkbenchWindowFrame.tsx";
import { useWorkbenchSelector } from "./hooks/useWorkbenchSelector.ts";
import { createWorkbenchNodeLayerNodeIDsSelector } from "./renderedNodeIds.ts";
import type { WorkbenchWindowChromeI18nRuntime } from "./workbenchWindowI18n.ts";
import { resolveWorkbenchWindowChromeMode } from "./windowHeader.ts";

export interface WorkbenchNodeLayerProps<TData = unknown> {
  genie: WorkbenchGenieController<TData>;
  edgeSnapEnabled?: boolean;
  interactive?: boolean;
  presentation?: WorkbenchSurfacePresentation | null;
  renderNode: WorkbenchRenderNode<TData>;
  shouldKeepMinimizedNodeMounted?: WorkbenchKeepMinimizedNodeMounted<TData>;
  renderWindowActions?: WorkbenchRenderWindowActions<TData>;
  renderWindowHeader?: WorkbenchRenderWindowHeader<TData>;
  resolveFullscreenHeaderMode?: WorkbenchResolveFullscreenHeaderMode<TData>;
  resolveWindowHeaderPresentation?: WorkbenchResolveWindowHeaderPresentation<TData>;
  resolveWindowSurfaceLayer?: WorkbenchResolveWindowSurfaceLayer<TData>;
  resolveWindowZIndex?: WorkbenchResolveWindowZIndex<TData>;
  windowChromeMode?:
    | WorkbenchWindowChromeMode
    | WorkbenchResolveWindowChromeMode<TData>;
  windowChromeI18n?: WorkbenchWindowChromeI18nRuntime;
}

export function WorkbenchNodeLayer<TData>({
  genie,
  edgeSnapEnabled = false,
  interactive = true,
  presentation,
  renderNode,
  shouldKeepMinimizedNodeMounted,
  renderWindowActions,
  renderWindowHeader,
  resolveFullscreenHeaderMode,
  resolveWindowHeaderPresentation,
  resolveWindowSurfaceLayer,
  resolveWindowZIndex,
  windowChromeMode,
  windowChromeI18n
}: WorkbenchNodeLayerProps<TData>) {
  const selectNodeLayerNodeIDs = useMemo(
    () =>
      createWorkbenchNodeLayerNodeIDsSelector({
        missionControl: presentation?.mode === "mission-control",
        resolveWindowSurfaceLayer,
        shouldKeepMinimizedNodeMounted
      }),
    [
      presentation?.mode,
      resolveWindowSurfaceLayer,
      shouldKeepMinimizedNodeMounted
    ]
  );
  const { defaultNodeIDs, dialogPopoverNodeIDs } = useWorkbenchSelector(
    selectNodeLayerNodeIDs
  );
  const snapPreviewRect = useWorkbenchSelector(selectWorkbenchSnapPreviewRect);
  const presentationInteraction =
    interactive && presentation?.mode === "mission-control"
      ? (presentation.interaction ?? null)
      : null;
  const dialogPopoverLayer =
    dialogPopoverNodeIDs.length > 0 ? (
      <MemoizedWorkbenchNodeLayerGroup
        className="workbench-node-layer workbench-node-layer--dialog-popover"
        edgeSnapEnabled={edgeSnapEnabled}
        fullscreenHeaderMode={resolveFullscreenHeaderMode}
        genieNodeVisibility={genie.nodeVisibility}
        interactive={interactive}
        minimizeNodeToAnchor={genie.minimizeNodeToAnchor}
        nodeIDs={dialogPopoverNodeIDs}
        presentation={presentation}
        renderNode={renderNode}
        renderWindowActions={renderWindowActions}
        renderWindowHeader={renderWindowHeader}
        resolveWindowHeaderPresentation={resolveWindowHeaderPresentation}
        resolveWindowZIndex={resolveWindowZIndex}
        windowChromeI18n={windowChromeI18n}
        windowChromeMode={windowChromeMode}
      />
    ) : null;

  return (
    <Fragment>
      <MemoizedWorkbenchNodeLayerGroup
        className="workbench-node-layer"
        edgeSnapEnabled={edgeSnapEnabled}
        fullscreenHeaderMode={resolveFullscreenHeaderMode}
        genieNodeVisibility={genie.nodeVisibility}
        interactive={interactive}
        minimizeNodeToAnchor={genie.minimizeNodeToAnchor}
        nodeIDs={defaultNodeIDs}
        onBackdropPress={presentationInteraction?.onBackdropPress}
        presentation={presentation}
        renderNode={renderNode}
        renderWindowActions={renderWindowActions}
        renderWindowHeader={renderWindowHeader}
        resolveWindowHeaderPresentation={resolveWindowHeaderPresentation}
        resolveWindowZIndex={resolveWindowZIndex}
        snapPreviewRect={snapPreviewRect}
        windowChromeI18n={windowChromeI18n}
        windowChromeMode={windowChromeMode}
      />
      {typeof document === "undefined"
        ? dialogPopoverLayer
        : dialogPopoverLayer
          ? createPortal(dialogPopoverLayer, document.body)
          : null}
    </Fragment>
  );
}

interface WorkbenchNodeLayerGroupProps<TData = unknown> {
  className: string;
  edgeSnapEnabled: boolean;
  fullscreenHeaderMode?: WorkbenchResolveFullscreenHeaderMode<TData>;
  genieNodeVisibility: WorkbenchGenieNodeVisibility;
  interactive: boolean;
  minimizeNodeToAnchor: WorkbenchGenieController<TData>["minimizeNodeToAnchor"];
  nodeIDs: readonly string[];
  onBackdropPress?: () => void;
  presentation?: WorkbenchSurfacePresentation | null;
  renderNode: WorkbenchRenderNode<TData>;
  renderWindowActions?: WorkbenchRenderWindowActions<TData>;
  renderWindowHeader?: WorkbenchRenderWindowHeader<TData>;
  resolveWindowHeaderPresentation?: WorkbenchResolveWindowHeaderPresentation<TData>;
  resolveWindowZIndex?: WorkbenchResolveWindowZIndex<TData>;
  snapPreviewRect?: ReturnType<typeof selectWorkbenchSnapPreviewRect>;
  windowChromeMode?:
    | WorkbenchWindowChromeMode
    | WorkbenchResolveWindowChromeMode<TData>;
  windowChromeI18n?: WorkbenchWindowChromeI18nRuntime;
}

function WorkbenchNodeLayerGroup<TData>({
  className,
  edgeSnapEnabled,
  fullscreenHeaderMode,
  genieNodeVisibility,
  interactive,
  minimizeNodeToAnchor,
  nodeIDs,
  onBackdropPress,
  presentation,
  renderNode,
  renderWindowActions,
  renderWindowHeader,
  resolveWindowHeaderPresentation,
  resolveWindowZIndex,
  snapPreviewRect,
  windowChromeI18n,
  windowChromeMode
}: WorkbenchNodeLayerGroupProps<TData>) {
  return (
    <div
      className={className}
      data-workbench-interactive={interactive ? "true" : "false"}
      onClick={
        onBackdropPress
          ? (event) => {
              if (event.target !== event.currentTarget) {
                return;
              }
              onBackdropPress();
            }
          : undefined
      }
    >
      {snapPreviewRect ? (
        <div
          className="workbench-snap-preview"
          style={{
            height: snapPreviewRect.height,
            left: snapPreviewRect.x,
            top: snapPreviewRect.y,
            width: snapPreviewRect.width
          }}
        />
      ) : null}
      {nodeIDs.map((nodeID) => (
        <MemoizedWorkbenchNodeLayerItem
          key={nodeID}
          fullscreenHeaderMode={fullscreenHeaderMode}
          genieNodeVisibility={genieNodeVisibility}
          edgeSnapEnabled={edgeSnapEnabled}
          interactive={interactive}
          minimizeNodeToAnchor={minimizeNodeToAnchor}
          nodeID={nodeID}
          presentation={presentation}
          renderNode={renderNode}
          renderWindowActions={renderWindowActions}
          renderWindowHeader={renderWindowHeader}
          resolveWindowHeaderPresentation={resolveWindowHeaderPresentation}
          resolveWindowZIndex={resolveWindowZIndex}
          windowChromeI18n={windowChromeI18n}
          windowChromeMode={windowChromeMode}
        />
      ))}
    </div>
  );
}

const MemoizedWorkbenchNodeLayerGroup = memo(
  WorkbenchNodeLayerGroup
) as typeof WorkbenchNodeLayerGroup;

interface WorkbenchNodeLayerItemProps<TData = unknown> {
  fullscreenHeaderMode?: WorkbenchResolveFullscreenHeaderMode<TData>;
  genieNodeVisibility: WorkbenchGenieNodeVisibility;
  edgeSnapEnabled: boolean;
  interactive: boolean;
  minimizeNodeToAnchor: WorkbenchGenieController<TData>["minimizeNodeToAnchor"];
  nodeID: string;
  presentation?: WorkbenchSurfacePresentation | null;
  renderNode: WorkbenchRenderNode<TData>;
  renderWindowActions?: WorkbenchRenderWindowActions<TData>;
  renderWindowHeader?: WorkbenchRenderWindowHeader<TData>;
  resolveWindowHeaderPresentation?: WorkbenchResolveWindowHeaderPresentation<TData>;
  resolveWindowZIndex?: WorkbenchResolveWindowZIndex<TData>;
  windowChromeMode?:
    | WorkbenchWindowChromeMode
    | WorkbenchResolveWindowChromeMode<TData>;
  windowChromeI18n?: WorkbenchWindowChromeI18nRuntime;
}

function WorkbenchNodeLayerItem<TData>({
  fullscreenHeaderMode,
  genieNodeVisibility,
  edgeSnapEnabled,
  interactive,
  minimizeNodeToAnchor,
  nodeID,
  presentation,
  renderNode,
  renderWindowActions,
  renderWindowHeader,
  resolveWindowHeaderPresentation,
  resolveWindowZIndex,
  windowChromeI18n,
  windowChromeMode
}: WorkbenchNodeLayerItemProps<TData>) {
  const controller = useWorkbenchController<TData>();
  const node = useWorkbenchSelector<TData, WorkbenchNode<TData> | null>(
    (state) => state.nodes.find((candidate) => candidate.id === nodeID) ?? null
  );
  const isFocused = useWorkbenchSelector(
    (state) => selectFocusedWorkbenchNode(state)?.id === nodeID
  );
  const isDragging = useWorkbenchSelector(
    (state) => state.activeDragNodeId === nodeID
  );
  const isResizing = useWorkbenchSelector(
    (state) => state.activeResizeNodeId === nodeID
  );
  const zIndex = useWorkbenchSelector((state) =>
    selectWorkbenchNodeZIndex(state, nodeID)
  );

  if (!node) {
    return null;
  }

  return (
    <WorkbenchWindowFrame
      edgeSnapEnabled={edgeSnapEnabled}
      hiddenMounted={node.isMinimized}
      interactive={interactive}
      presentation={presentation}
      node={node}
      genieNodeVisibility={genieNodeVisibility}
      minimizeNodeToAnchor={minimizeNodeToAnchor}
      resolveWindowZIndex={resolveWindowZIndex}
      fullscreenHeaderMode={fullscreenHeaderMode?.({
        controller,
        node
      })}
      renderActions={renderWindowActions}
      renderHeader={renderWindowHeader}
      windowHeaderPresentation={resolveWindowHeaderPresentation?.({
        controller,
        node
      })}
      windowChromeI18n={windowChromeI18n}
      windowChromeMode={resolveWorkbenchWindowChromeMode({
        controller,
        node,
        windowChromeMode
      })}
    >
      {renderNode({
        node,
        isDragging,
        isResizing,
        layout: {
          frame: node.frame,
          presentation,
          zIndex,
          isFocused
        },
        controller
      })}
    </WorkbenchWindowFrame>
  );
}

const MemoizedWorkbenchNodeLayerItem = memo(
  WorkbenchNodeLayerItem
) as typeof WorkbenchNodeLayerItem;
