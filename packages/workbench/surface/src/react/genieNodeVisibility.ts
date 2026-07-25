export interface WorkbenchGenieNodeVisibility {
  getSnapshot(nodeID: string): boolean;
  subscribe(nodeID: string, listener: () => void): () => void;
}

export interface WorkbenchGenieNodeVisibilityStore extends WorkbenchGenieNodeVisibility {
  dispose(): void;
  setHidden(nodeID: string, hidden: boolean): void;
}

export function createWorkbenchGenieNodeVisibilityStore(): WorkbenchGenieNodeVisibilityStore {
  const hiddenNodeIDs = new Set<string>();
  const listenersByNodeID = new Map<string, Set<() => void>>();

  return {
    dispose() {
      hiddenNodeIDs.clear();
      listenersByNodeID.clear();
    },
    getSnapshot(nodeID) {
      return hiddenNodeIDs.has(nodeID);
    },
    setHidden(nodeID, hidden) {
      if (hiddenNodeIDs.has(nodeID) === hidden) {
        return;
      }
      if (hidden) {
        hiddenNodeIDs.add(nodeID);
      } else {
        hiddenNodeIDs.delete(nodeID);
      }
      for (const listener of listenersByNodeID.get(nodeID) ?? []) {
        listener();
      }
    },
    subscribe(nodeID, listener) {
      const listeners = listenersByNodeID.get(nodeID) ?? new Set<() => void>();
      listeners.add(listener);
      listenersByNodeID.set(nodeID, listeners);
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) {
          listenersByNodeID.delete(nodeID);
        }
      };
    }
  };
}
