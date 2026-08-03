import type { TuttidClient, UserProject } from "@tutti-os/client-tuttid-ts";
import { ObservableService } from "./observableService";

export interface MobileUserProjectDirectorySnapshot {
  errorCode: "request_failed" | null;
  projects: readonly UserProject[];
  status: "idle" | "loading" | "ready";
}

export class MobileUserProjectDirectoryService extends ObservableService<MobileUserProjectDirectorySnapshot> {
  readonly _serviceBrand: undefined;
  private disposed = false;
  private loadPromise: Promise<void> | null = null;
  private snapshot: MobileUserProjectDirectorySnapshot = {
    errorCode: null,
    projects: [],
    status: "idle"
  };

  constructor(private readonly client: TuttidClient) {
    super();
  }

  getSnapshot = (): MobileUserProjectDirectorySnapshot => this.snapshot;

  load(): Promise<void> {
    if (this.loadPromise) return this.loadPromise;
    if (this.disposed) return Promise.resolve();
    this.publish({ ...this.snapshot, errorCode: null, status: "loading" });
    const loadPromise = this.client
      .listUserProjects()
      .then(({ projects }) => {
        if (this.disposed) return;
        this.publish({ errorCode: null, projects, status: "ready" });
      })
      .catch(() => {
        if (this.disposed) return;
        this.publish({
          ...this.snapshot,
          errorCode: "request_failed",
          status: "ready"
        });
      })
      .finally(() => {
        if (this.loadPromise === loadPromise) this.loadPromise = null;
      });
    this.loadPromise = loadPromise;
    return loadPromise;
  }

  reset(): void {
    if (this.disposed) return;
    this.loadPromise = null;
    this.publish({ errorCode: null, projects: [], status: "idle" });
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.loadPromise = null;
    this.clearListeners();
  }

  private publish(snapshot: MobileUserProjectDirectorySnapshot): void {
    if (this.disposed) return;
    this.snapshot = snapshot;
    this.emitChange();
  }
}
