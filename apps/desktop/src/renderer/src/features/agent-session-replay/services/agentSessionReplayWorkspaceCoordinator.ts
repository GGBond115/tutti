export interface AgentSessionReplayWorkspaceCassette {
  agentTargetId: string;
  rootAgentSessionId: string;
  cassetteId: string;
  mode: "continue-session" | "create-session";
}

export interface AgentSessionReplayWorkspaceCassetteBinding extends AgentSessionReplayWorkspaceCassette {
  activationRevision: number;
  canonicalMessageVersion: number;
  canonicalSessionUpdatedAtUnixMs: number;
  detailHydrated: boolean;
  mounted: boolean;
  nodeId: string;
  ready: boolean;
  selectedAgentSessionId: string | null;
}

export interface AgentSessionReplayWorkspaceSnapshot {
  ready: boolean;
  cassettes: readonly AgentSessionReplayWorkspaceCassetteBinding[];
}

export interface AgentSessionReplayNodeLaunchRequest {
  agentTargetId: string;
  agentSessionId?: string;
  cassetteId: string;
  nodeId?: string;
  workspaceId: string;
}

export class AgentSessionReplayWorkspaceCoordinator {
  private readonly bindingsByCassetteId = new Map<
    string,
    AgentSessionReplayWorkspaceCassetteBinding
  >();
  private readonly cassetteIdsByNodeId = new Map<string, string>();
  private readonly listeners = new Set<() => void>();
  private readonly workspaceId: string;

  constructor(workspaceId: string) {
    if (!workspaceId.trim()) {
      throw new Error("Replay Workspace requires a workspace id");
    }
    this.workspaceId = workspaceId;
  }

  getSnapshot = (): AgentSessionReplayWorkspaceSnapshot => {
    const cassettes = [...this.bindingsByCassetteId.values()];
    return {
      ready:
        cassettes.length > 0 && cassettes.every((cassette) => cassette.ready),
      cassettes
    };
  };

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  async bootstrap(
    cassettes: readonly AgentSessionReplayWorkspaceCassette[],
    launchNode: (
      request: AgentSessionReplayNodeLaunchRequest
    ) => Promise<string | null>
  ): Promise<readonly AgentSessionReplayWorkspaceCassetteBinding[]> {
    this.validateBootstrap(cassettes);
    for (const cassette of cassettes) {
      const nodeId = (
        await launchNode({
          agentTargetId: cassette.agentTargetId,
          ...(cassette.mode === "continue-session"
            ? { agentSessionId: cassette.rootAgentSessionId }
            : {}),
          cassetteId: cassette.cassetteId,
          workspaceId: this.workspaceId
        })
      )?.trim();
      if (!nodeId) {
        throw new Error(
          `Replay Cassette ${cassette.cassetteId} failed to create an Agent Node`
        );
      }
      if (this.cassetteIdsByNodeId.has(nodeId)) {
        throw new Error(
          `Agent Node ${nodeId} is already bound to a Replay Cassette`
        );
      }
      const binding: AgentSessionReplayWorkspaceCassetteBinding = {
        ...cassette,
        activationRevision: 0,
        canonicalMessageVersion: 0,
        canonicalSessionUpdatedAtUnixMs: 0,
        detailHydrated: false,
        mounted: false,
        nodeId,
        ready: false,
        selectedAgentSessionId: null
      };
      this.bindingsByCassetteId.set(cassette.cassetteId, binding);
      this.cassetteIdsByNodeId.set(nodeId, cassette.cassetteId);
      this.emit();
    }
    return this.getSnapshot().cassettes;
  }

  async activateCassette(
    cassetteId: string,
    launchNode: (
      request: AgentSessionReplayNodeLaunchRequest
    ) => Promise<string | null>
  ): Promise<AgentSessionReplayWorkspaceCassetteBinding> {
    const cassette = this.bindingsByCassetteId.get(cassetteId.trim());
    if (!cassette) {
      throw new Error(`Replay Cassette ${cassetteId} is not registered`);
    }
    const nodeId = (
      await launchNode({
        agentTargetId: cassette.agentTargetId,
        agentSessionId: cassette.rootAgentSessionId,
        cassetteId: cassette.cassetteId,
        nodeId: cassette.nodeId,
        workspaceId: this.workspaceId
      })
    )?.trim();
    if (nodeId !== cassette.nodeId) {
      throw new Error(
        `Replay Cassette ${cassette.cassetteId} activated an unexpected Agent Node`
      );
    }
    this.replaceBinding(cassette, {
      activationRevision: cassette.activationRevision + 1
    });
    return this.bindingsByCassetteId.get(cassette.cassetteId)!;
  }

  reportNodeMounted(nodeId: string, mounted: boolean): void {
    this.updateNode(nodeId, { mounted });
  }

  reportSelectedAgentSession(
    nodeId: string,
    agentSessionId: string | null
  ): void {
    this.updateNode(nodeId, {
      selectedAgentSessionId: agentSessionId?.trim() || null
    });
  }

  reportSessionDetailHydrated(
    rootAgentSessionId: string,
    hydrated: boolean
  ): void {
    const normalizedSessionId = rootAgentSessionId.trim();
    const binding = [...this.bindingsByCassetteId.values()].find(
      (candidate) => candidate.rootAgentSessionId === normalizedSessionId
    );
    if (!binding) {
      throw new Error(
        `Replay session ${rootAgentSessionId} is not registered in this Workspace`
      );
    }
    this.replaceBinding(binding, { detailHydrated: hydrated });
  }

  reportSessionCanonicalObservation(
    rootAgentSessionId: string,
    observation: {
      messageVersion: number;
      updatedAtUnixMs: number;
    }
  ): void {
    if (
      !Number.isSafeInteger(observation.messageVersion) ||
      observation.messageVersion < 0 ||
      !Number.isSafeInteger(observation.updatedAtUnixMs) ||
      observation.updatedAtUnixMs < 0
    ) {
      throw new Error("Replay canonical Session observation is invalid");
    }
    const normalizedSessionId = rootAgentSessionId.trim();
    const binding = [...this.bindingsByCassetteId.values()].find(
      (candidate) => candidate.rootAgentSessionId === normalizedSessionId
    );
    if (!binding) {
      throw new Error(
        `Replay session ${rootAgentSessionId} is not registered in this Workspace`
      );
    }
    this.replaceBinding(binding, {
      canonicalMessageVersion: Math.max(
        binding.canonicalMessageVersion,
        observation.messageVersion
      ),
      canonicalSessionUpdatedAtUnixMs: Math.max(
        binding.canonicalSessionUpdatedAtUnixMs,
        observation.updatedAtUnixMs
      ),
      detailHydrated: true
    });
  }

  resolveCassetteIdForNode(nodeId: string): string {
    const cassetteId = this.cassetteIdsByNodeId.get(nodeId.trim());
    if (!cassetteId) {
      throw new Error(`Agent Node ${nodeId} is not bound to a Replay Cassette`);
    }
    return cassetteId;
  }

  getCassetteForNode(
    nodeId: string
  ): AgentSessionReplayWorkspaceCassetteBinding | null {
    const cassetteId = this.cassetteIdsByNodeId.get(nodeId.trim());
    return cassetteId
      ? (this.bindingsByCassetteId.get(cassetteId) ?? null)
      : null;
  }

  getCassetteForSession(
    rootAgentSessionId: string
  ): AgentSessionReplayWorkspaceCassetteBinding | null {
    const normalizedSessionId = rootAgentSessionId.trim();
    return (
      [...this.bindingsByCassetteId.values()].find(
        (candidate) => candidate.rootAgentSessionId === normalizedSessionId
      ) ?? null
    );
  }

  reset(): void {
    if (
      this.bindingsByCassetteId.size === 0 &&
      this.cassetteIdsByNodeId.size === 0
    ) {
      return;
    }
    this.bindingsByCassetteId.clear();
    this.cassetteIdsByNodeId.clear();
    this.emit();
  }

  createCassetteScopedControl<TControl extends { command: string }>(
    nodeId: string,
    control: TControl
  ): TControl & { cassetteId: string } {
    return {
      ...control,
      cassetteId: this.resolveCassetteIdForNode(nodeId)
    };
  }

  private validateBootstrap(
    cassettes: readonly AgentSessionReplayWorkspaceCassette[]
  ): void {
    if (this.bindingsByCassetteId.size > 0) {
      throw new Error("Replay Workspace has already been bootstrapped");
    }
    if (cassettes.length === 0) {
      throw new Error("Replay Workspace requires at least one Cassette");
    }
    const cassetteIds = new Set<string>();
    const sessionIds = new Set<string>();
    for (const cassette of cassettes) {
      const cassetteId = cassette.cassetteId.trim();
      const agentTargetId = cassette.agentTargetId.trim();
      const sessionId = cassette.rootAgentSessionId.trim();
      if (
        !agentTargetId ||
        !cassetteId ||
        !sessionId ||
        (cassette.mode !== "continue-session" &&
          cassette.mode !== "create-session")
      ) {
        throw new Error(
          "Replay Workspace Cassette identities and mode must be valid"
        );
      }
      if (cassetteIds.has(cassetteId) || sessionIds.has(sessionId)) {
        throw new Error("Replay Workspace Cassette identities must be unique");
      }
      cassetteIds.add(cassetteId);
      sessionIds.add(sessionId);
    }
  }

  private updateNode(
    nodeId: string,
    patch: Partial<AgentSessionReplayWorkspaceCassetteBinding>
  ): void {
    const cassetteId = this.resolveCassetteIdForNode(nodeId);
    const binding = this.bindingsByCassetteId.get(cassetteId);
    if (!binding) {
      throw new Error(`Replay Cassette ${cassetteId} is not registered`);
    }
    this.replaceBinding(binding, patch);
  }

  private replaceBinding(
    binding: AgentSessionReplayWorkspaceCassetteBinding,
    patch: Partial<AgentSessionReplayWorkspaceCassetteBinding>
  ): void {
    const next = { ...binding, ...patch };
    next.ready =
      next.mounted &&
      next.detailHydrated &&
      next.selectedAgentSessionId === next.rootAgentSessionId;
    if (
      next.mounted === binding.mounted &&
      next.activationRevision === binding.activationRevision &&
      next.canonicalMessageVersion === binding.canonicalMessageVersion &&
      next.canonicalSessionUpdatedAtUnixMs ===
        binding.canonicalSessionUpdatedAtUnixMs &&
      next.detailHydrated === binding.detailHydrated &&
      next.selectedAgentSessionId === binding.selectedAgentSessionId &&
      next.ready === binding.ready
    ) {
      return;
    }
    this.bindingsByCassetteId.set(next.cassetteId, next);
    this.emit();
  }

  private emit(): void {
    for (const listener of this.listeners) {
      listener();
    }
  }
}
