import { randomUUID } from "node:crypto";
import { resolveDesktopDefaultsFromEnv } from "./defaults.ts";
import {
  desktopErrorCodes,
  formatErrorMessage
} from "../shared/errors/desktopErrors.ts";
import {
  RotatingFileWriter,
  type RotatingFileWriterOptions
} from "./rotatingFileWriter.ts";

type DesktopLogLevel = "debug" | "info" | "warn" | "error";
type DesktopLogOutput = "file" | "stdout" | "tee";

export interface DesktopLogger {
  debug(message: string, fields?: Record<string, unknown>): void;
  info(message: string, fields?: Record<string, unknown>): void;
  warn(message: string, fields?: Record<string, unknown>): void;
  error(message: string, fields?: Record<string, unknown>): void;
  close(): Promise<void>;
}

interface DesktopLogRuntime {
  logger: DesktopLogger;
  filePath: string;
  sessionID: string;
  close(): Promise<void>;
  flush(): Promise<void>;
}

type DesktopLogSink = (content: string) => Promise<void> | void;

interface ProcessStreamState {
  available: boolean;
  pending: Set<() => void>;
}

const levelPriority: Record<DesktopLogLevel, number> = {
  debug: 10,
  info: 20,
  warn: 30,
  error: 40
};

const processStreamStates = new WeakMap<
  NodeJS.WritableStream,
  ProcessStreamState
>();
const desktopLogSessionID = resolveDesktopLogSessionID();
const writeToProcessStderr = createBestEffortProcessSink(process.stderr);

let currentRuntime: DesktopLogRuntime = createProcessRuntime({
  level: resolveDesktopDefaultsFromEnv().logging.defaultLevel
});

export async function setupDesktopLogger(): Promise<DesktopLogger> {
  const output = resolveDesktopLogOutput();
  const level = resolveDesktopLogLevel();
  const logPath = resolveDesktopLogPath();

  try {
    currentRuntime = await createFileAwareRuntime({
      level,
      output,
      logPath
    });
  } catch (error) {
    currentRuntime = createProcessRuntime({
      level
    });
    currentRuntime.logger.error("desktop logger fallback to stdout", {
      error: formatErrorMessage(error),
      error_code: desktopErrorCodes.loggerFallback,
      requestedOutput: output,
      requestedLogPath: logPath
    });
  }

  return currentRuntime.logger;
}

export function getDesktopLogger(): DesktopLogger {
  return currentRuntime.logger;
}

export function getDesktopLogSessionID(): string {
  return currentRuntime.sessionID;
}

export function flushDesktopLogger(): Promise<void> {
  return currentRuntime.flush();
}

async function createFileAwareRuntime(options: {
  level: DesktopLogLevel;
  output: DesktopLogOutput;
  logPath: string;
}): Promise<DesktopLogRuntime> {
  if (options.output === "stdout") {
    return createProcessRuntime({
      level: options.level
    });
  }

  const rotatingWriter = await RotatingFileWriter.create(
    options.logPath,
    rotatingFileWriterOptionsFromEnv()
  );
  const writeToFile: DesktopLogSink = (content) =>
    rotatingWriter.write(content);
  const sink =
    options.output === "tee"
      ? createTeeSink(writeToFile, createBestEffortProcessSink(process.stdout))
      : writeToFile;

  return createWriterRuntime({
    level: options.level,
    sessionID: desktopLogSessionID,
    sink,
    filePath: rotatingWriter.path(),
    onClose: () => rotatingWriter.close()
  });
}

function createProcessRuntime(options: {
  level: DesktopLogLevel;
}): DesktopLogRuntime {
  return createWriterRuntime({
    level: options.level,
    sessionID: desktopLogSessionID,
    sink: createBestEffortProcessSink(process.stdout),
    filePath: "",
    onClose: () => Promise.resolve()
  });
}

function createWriterRuntime(options: {
  level: DesktopLogLevel;
  sessionID: string;
  sink: DesktopLogSink;
  filePath: string;
  onClose: () => Promise<void>;
}): DesktopLogRuntime {
  const minPriority = levelPriority[options.level];
  let pendingWrite: Promise<void> = Promise.resolve();

  function log(
    level: DesktopLogLevel,
    message: string,
    fields?: Record<string, unknown>
  ): void {
    if (levelPriority[level] < minPriority) {
      return;
    }

    const line = formatLogLine(level, options.sessionID, message, fields);
    pendingWrite = pendingWrite
      .then(() => Promise.resolve(options.sink(line)))
      .catch((error) => {
        void writeToProcessStderr(
          `[desktop-logger] write failed: ${error instanceof Error ? (error.stack ?? error.message) : String(error)}\n`
        );
      });
  }

  async function flush(): Promise<void> {
    await pendingWrite;
  }

  return {
    logger: {
      debug(message, fields) {
        log("debug", message, fields);
      },
      info(message, fields) {
        log("info", message, fields);
      },
      warn(message, fields) {
        log("warn", message, fields);
      },
      error(message, fields) {
        log("error", message, fields);
      },
      async close() {
        await flush();
        await options.onClose();
      }
    },
    filePath: options.filePath,
    sessionID: options.sessionID,
    async close() {
      await flush();
      await options.onClose();
    },
    flush
  };
}

function formatLogLine(
  level: DesktopLogLevel,
  sessionID: string,
  message: string,
  fields?: Record<string, unknown>
): string {
  const base = [
    `time=${new Date().toISOString()}`,
    `level=${level}`,
    `component=${JSON.stringify("tutti-desktop")}`,
    `pid=${process.pid}`,
    `session_id=${JSON.stringify(sessionID)}`,
    `msg=${JSON.stringify(message)}`
  ];

  if (fields) {
    for (const [key, value] of Object.entries(fields)) {
      base.push(`${key}=${formatFieldValue(value)}`);
    }
  }

  return `${base.join(" ")}\n`;
}

function formatFieldValue(value: unknown): string {
  if (typeof value === "string") {
    return JSON.stringify(value);
  }

  if (value instanceof Error) {
    return JSON.stringify(value.message);
  }

  return JSON.stringify(value);
}

function resolveDesktopLogOutput(): DesktopLogOutput {
  const override = process.env.TUTTI_DESKTOP_LOG_OUTPUT?.trim().toLowerCase();
  if (override === "stdout" || override === "tee" || override === "file") {
    return override;
  }

  return resolveDesktopDefaultsFromEnv().logging.defaultOutput;
}

function resolveDesktopLogLevel(): DesktopLogLevel {
  const override = process.env.TUTTI_DESKTOP_LOG_LEVEL?.trim().toLowerCase();
  if (
    override === "debug" ||
    override === "info" ||
    override === "warn" ||
    override === "error"
  ) {
    return override;
  }

  return resolveDesktopDefaultsFromEnv().logging.defaultLevel;
}

function resolveDesktopLogPath(): string {
  const override = process.env.TUTTI_DESKTOP_LOG_PATH?.trim();
  if (override) {
    return override;
  }

  return resolveDesktopDefaultsFromEnv().state.desktopLogPath;
}

function resolveDesktopLogSessionID(): string {
  const existing = process.env.TUTTI_SESSION_ID?.trim();
  if (existing) {
    return existing;
  }

  const generated = randomUUID();
  process.env.TUTTI_SESSION_ID = generated;
  return generated;
}

function createTeeSink(
  fileSink: DesktopLogSink,
  processSink: DesktopLogSink
): DesktopLogSink {
  return async (content) => {
    await fileSink(content);
    await processSink(content);
  };
}

export function createBestEffortProcessSink(
  stream: NodeJS.WritableStream
): DesktopLogSink {
  let state = processStreamStates.get(stream);
  if (!state) {
    state = {
      available: true,
      pending: new Set()
    };
    processStreamStates.set(stream, state);
    stream.on("error", () => {
      state!.available = false;
      for (const resolve of state!.pending) {
        resolve();
      }
      state!.pending.clear();
    });
  }

  return (content) => {
    if (!state.available) {
      return;
    }
    return new Promise<void>((resolve) => {
      const settle = () => {
        state.pending.delete(settle);
        resolve();
      };
      state.pending.add(settle);
      try {
        stream.write(content, settle);
      } catch {
        state.available = false;
        for (const finish of state.pending) {
          finish();
        }
      }
    });
  };
}

function rotatingFileWriterOptionsFromEnv(): RotatingFileWriterOptions {
  const defaults = resolveDesktopDefaultsFromEnv().logging;
  return {
    maxSizeBytes:
      envIntOrDefault("TUTTI_LOG_MAX_SIZE_MB", defaults.maxSizeMB) *
      1024 *
      1024,
    maxBackups: envIntOrDefault("TUTTI_LOG_MAX_BACKUPS", defaults.maxBackups),
    maxAgeDays: envIntOrDefault("TUTTI_LOG_MAX_AGE_DAYS", defaults.maxAgeDays),
    maxTotalBytes:
      envIntOrDefault("TUTTI_LOG_MAX_TOTAL_MB", defaults.maxTotalMB) *
      1024 *
      1024
  };
}

function envIntOrDefault(key: string, fallback: number): number {
  const value = process.env[key]?.trim();
  if (!value) {
    return fallback;
  }

  const parsed = Number.parseInt(value, 10);
  return Number.isNaN(parsed) ? fallback : parsed;
}
