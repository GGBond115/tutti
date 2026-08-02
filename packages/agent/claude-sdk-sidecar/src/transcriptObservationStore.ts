import type {
  SessionKey,
  SessionStore,
  SessionStoreEntry
} from "@anthropic-ai/claude-agent-sdk";
import { importSessionToStore } from "@anthropic-ai/claude-agent-sdk";

type TranscriptEntryObserver = (
  key: SessionKey,
  entries: readonly SessionStoreEntry[]
) => void;

/**
 * Receives the SDK's official transcript mirror without becoming a second
 * persistence owner. Returning no stored history keeps explicit resume on
 * Claude's existing local store; append is only a live observation boundary.
 */
export class TranscriptObservationStore implements SessionStore {
  private readonly projectDirectory: string;
  private readonly observe: TranscriptEntryObserver;
  private readonly replayNativeSession: typeof importSessionToStore;

  constructor(
    projectDirectory: string,
    observe: TranscriptEntryObserver,
    replayNativeSession: typeof importSessionToStore = importSessionToStore
  ) {
    this.projectDirectory = projectDirectory;
    this.observe = observe;
    this.replayNativeSession = replayNativeSession;
  }

  async append(key: SessionKey, entries: SessionStoreEntry[]): Promise<void> {
    this.observe(key, entries);
  }

  async load(key: SessionKey): Promise<SessionStoreEntry[] | null> {
    if (!key.subpath && key.sessionId.trim()) {
      try {
        await this.replayNativeSession(key.sessionId, this, {
          dir: this.projectDirectory,
          includeSubagents: false
        });
      } catch {
        // Observation is best effort. Native Claude resume remains the source
        // of truth and must not be blocked by a replay/import failure.
      }
    }
    // This store is an observer, not the resume persistence owner. Returning
    // null keeps the SDK attached to Claude's original local JSONL path.
    return null;
  }
}
