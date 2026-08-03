import type {
  AgentSessionRecording,
  TuttidClient
} from "@tutti-os/client-tuttid-ts";

export interface AgentSessionReplaySnapshot {
  activeRecording: AgentSessionRecording | null;
  error: unknown;
  loading: boolean;
  recordings: readonly AgentSessionRecording[];
}

export interface AgentSessionReplayImportAdapterResult {
  canceled: boolean;
  failedCount: number;
  importedCount: number;
}

export interface AgentSessionReplayImportResult {
  failedCount: number;
  importedCount: number;
  outcome: "canceled" | "complete" | "partial" | "failed";
}

export interface AgentSessionReplayPrerequisites {
  composerDefaults: {
    model: string;
    permissionModeId: string;
    reasoningEffort: string;
    speed: string;
  };
}

export interface AgentSessionReplayServiceDependencies {
  armNextSessionRecording(recordingId: string): void;
  clearNextSessionRecording(recordingId?: string): void;
  discardActivityEventRecording(recordingId: string): void;
  importCassettes(): Promise<AgentSessionReplayImportAdapterResult>;
  sealActivityEventRecording(recordingId: string): Promise<void>;
  startActivityEventRecording(recordingId: string): void;
  tuttidClient: Pick<
    TuttidClient,
    | "cancelAgentSessionRecording"
    | "completeAgentSessionRecording"
    | "deleteAgentSessionRecording"
    | "listAgentSessionRecordings"
    | "prepareAgentSessionReplayWorkspace"
    | "renameAgentSessionRecording"
    | "startAgentSessionRecording"
  >;
  workspaceId: string;
}

const activeStatuses = new Set<AgentSessionRecording["status"]>([
  "preparing",
  "ready",
  "recording",
  "finalizing"
]);

export class AgentSessionReplayService {
  private readonly dependencies: AgentSessionReplayServiceDependencies;
  private readonly listeners = new Set<() => void>();
  private snapshot: AgentSessionReplaySnapshot = {
    activeRecording: null,
    error: null,
    loading: false,
    recordings: []
  };
  private refreshPromise: Promise<void> | null = null;

  constructor(dependencies: AgentSessionReplayServiceDependencies) {
    this.dependencies = dependencies;
  }

  getSnapshot = (): AgentSessionReplaySnapshot => this.snapshot;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  async refresh(options: { background?: boolean } = {}): Promise<void> {
    if (this.refreshPromise) {
      return this.refreshPromise;
    }
    if (!options.background) {
      this.update({ loading: true });
    }
    this.refreshPromise = this.loadRecordings();
    try {
      await this.refreshPromise;
    } finally {
      this.refreshPromise = null;
    }
  }

  async startRecording(input: {
    agentSessionId?: string | null;
    agentTargetId: string;
    replayPrerequisites: AgentSessionReplayPrerequisites;
  }): Promise<void> {
    this.update({ error: null, loading: true });
    try {
      const recording =
        await this.dependencies.tuttidClient.startAgentSessionRecording(
          this.dependencies.workspaceId,
          {
            agentTargetId: input.agentTargetId,
            agentSessionId: input.agentSessionId ?? undefined,
            replayPrerequisites: input.replayPrerequisites
          }
        );
      this.dependencies.startActivityEventRecording(recording.id);
      if (!input.agentSessionId) {
        this.dependencies.armNextSessionRecording(recording.id);
      }
      this.replaceRecording(recording);
    } catch (error) {
      this.update({ error });
      throw error;
    } finally {
      this.update({ loading: false });
    }
  }

  async completeRecording(recordingId: string): Promise<void> {
    this.update({ error: null, loading: true });
    try {
      await this.dependencies.sealActivityEventRecording(recordingId);
      const recording =
        await this.dependencies.tuttidClient.completeAgentSessionRecording(
          this.dependencies.workspaceId,
          recordingId
        );
      this.dependencies.clearNextSessionRecording(recordingId);
      this.replaceRecording(recording);
    } catch (error) {
      this.update({ error });
      throw error;
    } finally {
      this.update({ loading: false });
    }
  }

  async cancelRecording(recordingId: string): Promise<void> {
    this.update({ error: null, loading: true });
    try {
      await this.dependencies.tuttidClient.cancelAgentSessionRecording(
        this.dependencies.workspaceId,
        recordingId
      );
      this.dependencies.discardActivityEventRecording(recordingId);
      this.dependencies.clearNextSessionRecording(recordingId);
      this.removeRecording(recordingId);
    } catch (error) {
      this.update({ error });
      throw error;
    } finally {
      this.update({ loading: false });
    }
  }

  async deleteRecording(recordingId: string): Promise<void> {
    this.update({ error: null, loading: true });
    try {
      await this.dependencies.tuttidClient.deleteAgentSessionRecording(
        this.dependencies.workspaceId,
        recordingId
      );
      this.removeRecording(recordingId);
    } catch (error) {
      this.update({ error });
      throw error;
    } finally {
      this.update({ loading: false });
    }
  }

  async renameRecording(recordingId: string, name: string): Promise<void> {
    this.update({ error: null, loading: true });
    try {
      const recording =
        await this.dependencies.tuttidClient.renameAgentSessionRecording(
          this.dependencies.workspaceId,
          recordingId,
          { name: name.trim() }
        );
      this.replaceRecording(recording);
    } catch (error) {
      this.update({ error });
      throw error;
    } finally {
      this.update({ loading: false });
    }
  }

  async importCassettes(): Promise<AgentSessionReplayImportResult> {
    this.update({ error: null, loading: true });
    try {
      const result = await this.dependencies.importCassettes();
      const importedCount = result.importedCount;
      const failedCount = result.failedCount;
      if (result.canceled) {
        return {
          failedCount,
          importedCount,
          outcome: "canceled"
        };
      }
      if (importedCount > 0) {
        await this.loadRecordings({
          preserveLoading: true,
          throwOnError: true
        });
      }
      return {
        failedCount,
        importedCount,
        outcome:
          failedCount === 0
            ? "complete"
            : importedCount > 0
              ? "partial"
              : "failed"
      };
    } catch (error) {
      this.update({ error });
      throw error;
    } finally {
      this.update({ loading: false });
    }
  }

  prepareReplayWorkspace(cassetteIds: readonly string[]) {
    return this.dependencies.tuttidClient.prepareAgentSessionReplayWorkspace(
      this.dependencies.workspaceId,
      { cassetteIds: [...cassetteIds] }
    );
  }

  private async loadRecordings(
    options: {
      preserveLoading?: boolean;
      throwOnError?: boolean;
    } = {}
  ): Promise<void> {
    try {
      const recordings =
        await this.dependencies.tuttidClient.listAgentSessionRecordings(
          this.dependencies.workspaceId
        );
      const nextRecordings = recordingsEqual(
        this.snapshot.recordings,
        recordings
      )
        ? this.snapshot.recordings
        : recordings;
      const nextSnapshot: AgentSessionReplaySnapshot = {
        activeRecording:
          nextRecordings.find((recording) =>
            activeStatuses.has(recording.status)
          ) ?? null,
        error: null,
        loading: options.preserveLoading ? this.snapshot.loading : false,
        recordings: nextRecordings
      };
      if (
        nextSnapshot.activeRecording === this.snapshot.activeRecording &&
        nextSnapshot.error === this.snapshot.error &&
        nextSnapshot.loading === this.snapshot.loading &&
        nextSnapshot.recordings === this.snapshot.recordings
      ) {
        return;
      }
      this.snapshot = nextSnapshot;
      this.emit();
    } catch (error) {
      this.update({
        error,
        loading: options.preserveLoading ? this.snapshot.loading : false
      });
      if (options.throwOnError) {
        throw error;
      }
    }
  }

  private replaceRecording(recording: AgentSessionRecording): void {
    const recordings = [
      recording,
      ...this.snapshot.recordings.filter((item) => item.id !== recording.id)
    ];
    this.snapshot = {
      ...this.snapshot,
      activeRecording: activeStatuses.has(recording.status) ? recording : null,
      error: null,
      recordings
    };
    this.emit();
  }

  private removeRecording(recordingId: string): void {
    this.snapshot = {
      ...this.snapshot,
      activeRecording:
        this.snapshot.activeRecording?.id === recordingId
          ? null
          : this.snapshot.activeRecording,
      error: null,
      recordings: this.snapshot.recordings.filter(
        (recording) => recording.id !== recordingId
      )
    };
    this.emit();
  }

  private update(patch: Partial<AgentSessionReplaySnapshot>): void {
    this.snapshot = { ...this.snapshot, ...patch };
    this.emit();
  }

  private emit(): void {
    for (const listener of this.listeners) {
      listener();
    }
  }
}

function recordingsEqual(
  left: readonly AgentSessionRecording[],
  right: readonly AgentSessionRecording[]
): boolean {
  return (
    left.length === right.length &&
    left.every(
      (recording, index) =>
        recording.id === right[index]?.id &&
        recording.updatedAtUnixMs === right[index]?.updatedAtUnixMs
    )
  );
}
