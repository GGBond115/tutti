import type { WorkbenchFrame, WorkbenchState } from "./types.ts";

const exposedNodeIDsByState = new WeakMap<object, ReadonlySet<string>>();

export function selectVisuallyExposedWorkbenchNodeIDs<TData>(
  state: WorkbenchState<TData>
): ReadonlySet<string> {
  const cached = exposedNodeIDsByState.get(state);
  if (cached) {
    return cached;
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
    if (node.isMinimized) {
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
      if (!occluder || occluder.isMinimized) {
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

  exposedNodeIDsByState.set(state, exposedNodeIDs);
  return exposedNodeIDs;
}

export function selectWorkbenchNodeIsVisuallyExposed<TData>(
  state: WorkbenchState<TData>,
  nodeID: string
): boolean {
  return selectVisuallyExposedWorkbenchNodeIDs(state).has(nodeID);
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
