import type { EventEmitter } from "node:events";
import type { Readable } from "node:stream";
import type { DesktopLogger } from "./logging.ts";

const readyPrefix = "[tutti-agent-session-replay-ready] ";
const completePrefix = "[tutti-agent-session-replay-complete] ";
const failedPrefix = "[tutti-agent-session-replay-failed] ";
const checkpointPrefix = "[tutti-agent-session-replay-checkpoint] ";
const replacePrefix = "[tutti-agent-session-replay-replace] ";
const maxDiagnosticCharacters = 12_000;

export interface ManagedReplayChild extends EventEmitter {
  exitCode: number | null;
  kill(signal?: NodeJS.Signals): boolean;
  signalCode: NodeJS.Signals | null;
  stderr: Readable;
  stdout: Readable;
}

export function monitorManagedReplayWorkspace(
  child: ManagedReplayChild,
  input: {
    expectedCassetteIds: ReadonlySet<string>;
    logger: DesktopLogger;
    onCheckpoint(
      cassetteId: string,
      checkpoint: number,
      totalCheckpoints: number,
      totalDurationMs: number
    ): void;
    onComplete(cassetteId: string): void;
    onFailed(cassetteId: string, error: Error): void;
    onReady(cassetteId: string): void;
    onTerminated(error: Error | null): Promise<void>;
    timeoutMs: number;
  }
): Promise<void> {
  let diagnostics = "";
  let stdoutBuffer = "";
  let terminated = false;
  const ready = new Set<string>();
  const terminal = new Set<string>();
  let readyTimeout: ReturnType<typeof setTimeout> | null = null;
  let resolveClosed!: () => void;
  const closed = new Promise<void>((resolve) => {
    resolveClosed = resolve;
  });
  const cleanup = () => {
    if (readyTimeout) clearTimeout(readyTimeout);
    child.off("error", onError);
    child.off("close", onClose);
    child.stdout.off("data", onStdout);
    child.stderr.off("data", onStderr);
  };
  const terminate = (error: Error | null) => {
    if (terminated) return;
    terminated = true;
    cleanup();
    void input.onTerminated(error).finally(resolveClosed);
  };
  const fatal = (error: Error) => {
    child.kill("SIGTERM");
    terminate(error);
  };
  const appendDiagnostic = (chunk: unknown) => {
    diagnostics = `${diagnostics}${String(chunk)}`.slice(
      -maxDiagnosticCharacters
    );
  };
  const armReadyTimeout = () => {
    if (readyTimeout) clearTimeout(readyTimeout);
    readyTimeout = setTimeout(
      () =>
        fatal(
          new Error(
            `Replay Workspace did not become ready within ${input.timeoutMs} ms: ${diagnostics.trim()}`
          )
        ),
      input.timeoutMs
    );
  };
  const onError = (error: Error) => fatal(error);
  const onClose = (code: number | null, signal: NodeJS.Signals | null) => {
    const readyOrTerminal = [...input.expectedCassetteIds].every(
      (cassetteId) => ready.has(cassetteId) || terminal.has(cassetteId)
    );
    if (code === 0 && readyOrTerminal) {
      terminate(null);
      return;
    }
    const error = new Error(
      `Replay Workspace exited${readyOrTerminal ? "" : " before it became ready"} (${code ?? signal ?? "unknown"}): ${diagnostics.trim()}`
    );
    input.logger.error("managed Agent Session Replay Workspace failed", {
      diagnostics: diagnostics.trim(),
      error_message: error.message,
      exit_code: code,
      expected_cassette_ids: [...input.expectedCassetteIds],
      ready_cassette_ids: [...ready],
      signal,
      terminal_cassette_ids: [...terminal]
    });
    terminate(error);
  };
  const onStderr = (chunk: unknown) => {
    appendDiagnostic(chunk);
    input.logger.debug("managed Agent Session Replay output", {
      output: String(chunk).trim()
    });
  };
  const clearReadyTimeoutIfSettled = () => {
    if (
      [...input.expectedCassetteIds].every(
        (cassetteId) => ready.has(cassetteId) || terminal.has(cassetteId)
      )
    ) {
      if (readyTimeout) {
        clearTimeout(readyTimeout);
        readyTimeout = null;
      }
    }
  };
  const parseEvent = (
    line: string,
    prefix: string,
    event: string
  ): { payload: Record<string, unknown>; cassetteId: string } | null => {
    try {
      const payload = JSON.parse(line.slice(prefix.length)) as Record<
        string,
        unknown
      >;
      const cassetteId =
        typeof payload.cassetteId === "string" ? payload.cassetteId : "";
      if (!input.expectedCassetteIds.has(cassetteId)) {
        fatal(
          new Error(
            `Replay Workspace reported ${event} for an unknown Cassette id: ${cassetteId}`
          )
        );
        return null;
      }
      return { payload, cassetteId };
    } catch (error) {
      fatal(
        new Error(
          `Replay Workspace reported an invalid ${event} event: ${toError(error).message}`
        )
      );
      return null;
    }
  };
  const onStdout = (chunk: unknown) => {
    appendDiagnostic(chunk);
    stdoutBuffer += String(chunk);
    for (;;) {
      const newline = stdoutBuffer.indexOf("\n");
      if (newline < 0) break;
      const line = stdoutBuffer.slice(0, newline).trim();
      stdoutBuffer = stdoutBuffer.slice(newline + 1);
      if (line.startsWith(readyPrefix)) {
        const event = parseEvent(line, readyPrefix, "ready");
        if (event && !terminal.has(event.cassetteId)) {
          ready.add(event.cassetteId);
          input.onReady(event.cassetteId);
          clearReadyTimeoutIfSettled();
        }
        continue;
      }
      if (line.startsWith(checkpointPrefix)) {
        const event = parseEvent(line, checkpointPrefix, "checkpoint");
        const checkpoint = event?.payload.checkpoint;
        const totalDurationMs = event?.payload.totalDurationMs;
        const totalCheckpoints = event?.payload.totalCheckpoints;
        if (
          event &&
          !terminal.has(event.cassetteId) &&
          Number.isSafeInteger(checkpoint) &&
          (checkpoint as number) >= 0 &&
          Number.isSafeInteger(totalCheckpoints) &&
          (totalCheckpoints as number) > (checkpoint as number) &&
          Number.isSafeInteger(totalDurationMs) &&
          (totalDurationMs as number) >= 0
        ) {
          input.onCheckpoint(
            event.cassetteId,
            checkpoint as number,
            totalCheckpoints as number,
            totalDurationMs as number
          );
        } else if (event && !terminal.has(event.cassetteId)) {
          fatal(new Error("Replay Workspace reported an invalid checkpoint"));
        }
        continue;
      }
      if (line.startsWith(completePrefix)) {
        const event = parseEvent(line, completePrefix, "complete");
        if (!event || terminal.has(event.cassetteId)) continue;
        if (!ready.has(event.cassetteId)) {
          fatal(
            new Error(
              `Replay Workspace Cassette completed before it became ready: ${event.cassetteId}`
            )
          );
          continue;
        }
        terminal.add(event.cassetteId);
        input.onComplete(event.cassetteId);
        clearReadyTimeoutIfSettled();
        continue;
      }
      if (line.startsWith(failedPrefix)) {
        const event = parseEvent(line, failedPrefix, "failed");
        if (!event || terminal.has(event.cassetteId)) continue;
        terminal.add(event.cassetteId);
        const cause = replayFailureCause(event.payload.cause);
        input.onFailed(
          event.cassetteId,
          new Error(
            (typeof event.payload.error === "string" &&
              event.payload.error.trim()) ||
              "Agent Session Replay failed",
            cause ? { cause } : undefined
          )
        );
        clearReadyTimeoutIfSettled();
        continue;
      }
      if (line.startsWith(replacePrefix)) {
        fatal(
          new Error(
            "Replay Workspace replacement controls are not supported in the fixed-batch version"
          )
        );
      }
    }
  };
  armReadyTimeout();
  child.once("error", onError);
  child.once("close", onClose);
  child.stdout.on("data", onStdout);
  child.stderr.on("data", onStderr);
  return closed;
}

function replayFailureCause(
  value: unknown
): { code: string; message: string } | null {
  const cause = value as { code?: unknown; message?: unknown };
  if (
    !cause ||
    typeof cause !== "object" ||
    typeof cause.code !== "string" ||
    !cause.code.trim() ||
    typeof cause.message !== "string" ||
    !cause.message.trim()
  ) {
    return null;
  }
  return {
    code: cause.code.trim(),
    message: cause.message.trim()
  };
}

function toError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}
