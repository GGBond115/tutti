import type { WorkbenchFrame, WorkbenchState } from "./types.ts";

const noNonOccludingNodeIDs: ReadonlySet<string> = new Set();
const exposedNodeIDsByState = new WeakMap<
  object,
  {
    exposedNodeIDs: ReadonlySet<string>;
    nonOccludingNodeIDs: ReadonlySet<string>;
  }
>();
const exposedNodeIDsByNodeStack = new WeakMap<
  object,
  {
    exposedNodeIDs: ReadonlySet<string>;
    nodes: WorkbenchState<unknown>["nodes"];
    nonOccludingNodeIDs: ReadonlySet<string>;
    surfaceSize: WorkbenchState<unknown>["surfaceSize"];
  }
>();

export function selectVisuallyExposedWorkbenchNodeIDs<TData>(
  state: WorkbenchState<TData>,
  nonOccludingNodeIDs: ReadonlySet<string> = noNonOccludingNodeIDs
): ReadonlySet<string> {
  const cachedByState = exposedNodeIDsByState.get(state);
  if (cachedByState?.nonOccludingNodeIDs === nonOccludingNodeIDs) {
    return cachedByState.exposedNodeIDs;
  }
  const cachedByGeometry = exposedNodeIDsByNodeStack.get(state.nodeStack);
  if (
    cachedByGeometry &&
    cachedByGeometry.nonOccludingNodeIDs === nonOccludingNodeIDs &&
    hasEquivalentVisualOcclusionGeometry(cachedByGeometry, state)
  ) {
    cachedByGeometry.nodes = state.nodes;
    cachedByGeometry.surfaceSize = state.surfaceSize;
    exposedNodeIDsByState.set(state, {
      exposedNodeIDs: cachedByGeometry.exposedNodeIDs,
      nonOccludingNodeIDs
    });
    return cachedByGeometry.exposedNodeIDs;
  }

  const nodeByID = new Map(state.nodes.map((node) => [node.id, node]));
  const stackedIDs = new Set(state.nodeStack);
  const orderedNodes = [
    ...state.nodes.filter((node) => !stackedIDs.has(node.id)),
    ...state.nodeStack.flatMap((nodeID) => {
      const node = nodeByID.get(nodeID);
      return node ? [node] : [];
    })
  ];
  const surfaceFrame: WorkbenchFrame = {
    x: 0,
    y: 0,
    width: state.surfaceSize.width,
    height: state.surfaceSize.height
  };
  const exposedNodeIDs = new Set<string>();

  orderedNodes.forEach((node, nodeIndex) => {
    if (node.isMinimized || nonOccludingNodeIDs.has(node.id)) {
      return;
    }
    const visibleFrame = intersectFrames(node.frame, surfaceFrame);
    if (!visibleFrame) {
      return;
    }

    let uncoveredFrames = [visibleFrame];
    for (
      let occluderIndex = nodeIndex + 1;
      occluderIndex < orderedNodes.length && uncoveredFrames.length > 0;
      occluderIndex += 1
    ) {
      const occluder = orderedNodes[occluderIndex];
      if (
        !occluder ||
        occluder.isMinimized ||
        nonOccludingNodeIDs.has(occluder.id)
      ) {
        continue;
      }
      uncoveredFrames = uncoveredFrames.flatMap((frame) =>
        subtractFrame(frame, occluder.frame)
      );
    }
    if (uncoveredFrames.length > 0) {
      exposedNodeIDs.add(node.id);
    }
  });

  exposedNodeIDsByState.set(state, {
    exposedNodeIDs,
    nonOccludingNodeIDs
  });
  exposedNodeIDsByNodeStack.set(state.nodeStack, {
    exposedNodeIDs,
    nodes: state.nodes,
    nonOccludingNodeIDs,
    surfaceSize: state.surfaceSize
  });
  return exposedNodeIDs;
}

export function selectWorkbenchNodeIsVisuallyExposed<TData>(
  state: WorkbenchState<TData>,
  nodeID: string,
  nonOccludingNodeIDs?: ReadonlySet<string>
): boolean {
  return selectVisuallyExposedWorkbenchNodeIDs(state, nonOccludingNodeIDs).has(
    nodeID
  );
}

function intersectFrames(
  left: WorkbenchFrame,
  right: WorkbenchFrame
): WorkbenchFrame | null {
  const x = Math.max(left.x, right.x);
  const y = Math.max(left.y, right.y);
  const rightEdge = Math.min(
    left.x + Math.max(0, left.width),
    right.x + Math.max(0, right.width)
  );
  const bottomEdge = Math.min(
    left.y + Math.max(0, left.height),
    right.y + Math.max(0, right.height)
  );
  if (rightEdge <= x || bottomEdge <= y) {
    return null;
  }
  return {
    x,
    y,
    width: rightEdge - x,
    height: bottomEdge - y
  };
}

function subtractFrame(
  source: WorkbenchFrame,
  occluder: WorkbenchFrame
): WorkbenchFrame[] {
  const covered = intersectFrames(source, occluder);
  if (!covered) {
    return [source];
  }

  const sourceRight = source.x + source.width;
  const sourceBottom = source.y + source.height;
  const coveredRight = covered.x + covered.width;
  const coveredBottom = covered.y + covered.height;
  const remaining: WorkbenchFrame[] = [];

  pushFrame(remaining, {
    x: source.x,
    y: source.y,
    width: source.width,
    height: covered.y - source.y
  });
  pushFrame(remaining, {
    x: source.x,
    y: coveredBottom,
    width: source.width,
    height: sourceBottom - coveredBottom
  });
  pushFrame(remaining, {
    x: source.x,
    y: covered.y,
    width: covered.x - source.x,
    height: covered.height
  });
  pushFrame(remaining, {
    x: coveredRight,
    y: covered.y,
    width: sourceRight - coveredRight,
    height: covered.height
  });
  return remaining;
}

function pushFrame(frames: WorkbenchFrame[], frame: WorkbenchFrame): void {
  if (frame.width > 0 && frame.height > 0) {
    frames.push(frame);
  }
}

function hasEquivalentVisualOcclusionGeometry<TData>(
  cached: {
    nodes: WorkbenchState<unknown>["nodes"];
    surfaceSize: WorkbenchState<unknown>["surfaceSize"];
  },
  state: WorkbenchState<TData>
): boolean {
  if (
    cached.surfaceSize.width !== state.surfaceSize.width ||
    cached.surfaceSize.height !== state.surfaceSize.height ||
    cached.nodes.length !== state.nodes.length
  ) {
    return false;
  }
  if (cached.nodes === state.nodes) {
    return true;
  }
  return cached.nodes.every((cachedNode, index) => {
    const node = state.nodes[index];
    return (
      node !== undefined &&
      cachedNode.id === node.id &&
      cachedNode.isMinimized === node.isMinimized &&
      cachedNode.frame.x === node.frame.x &&
      cachedNode.frame.y === node.frame.y &&
      cachedNode.frame.width === node.frame.width &&
      cachedNode.frame.height === node.frame.height
    );
  });
}
