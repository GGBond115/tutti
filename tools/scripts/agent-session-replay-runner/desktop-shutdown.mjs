import { stopProcessTree } from "../run-agent-gui-performance.mjs";

function defaultIsProcessAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error?.code !== "ESRCH";
  }
}

/**
 * Stop a detached Desktop/tuttid tree when this process is interrupted, when
 * stdout/stderr break (owner gone), or when an explicit parent PID exits.
 * Desktop is spawned detached, so killing only the runner leaves Dock orphans.
 */
export function bindManagedReplayShutdown(
  desktop,
  {
    clearInterval: clearIntervalFn = clearInterval,
    isProcessAlive = defaultIsProcessAlive,
    parentPid = process.env.TUTTI_AGENT_SESSION_REPLAY_PARENT_PID,
    processRuntime = process,
    setInterval: setIntervalFn = setInterval,
    stopDesktop = stopProcessTree
  } = {}
) {
  let stopping = false;
  const stop = () => {
    if (stopping) return;
    stopping = true;
    void Promise.resolve(stopDesktop(desktop)).catch(() => undefined);
  };
  const onOutputError = (error) => {
    if (error?.code === "EPIPE") {
      stop();
    }
  };
  const parsedParentPid = Number.parseInt(parentPid?.trim() ?? "", 10);
  const parentCheckInterval =
    Number.isSafeInteger(parsedParentPid) && parsedParentPid > 0
      ? setIntervalFn(() => {
          if (!isProcessAlive(parsedParentPid)) {
            stop();
          }
        }, 500)
      : null;
  parentCheckInterval?.unref?.();
  processRuntime.once("SIGINT", stop);
  processRuntime.once("SIGTERM", stop);
  processRuntime.stdout?.on("error", onOutputError);
  processRuntime.stderr?.on("error", onOutputError);
  return () => {
    if (parentCheckInterval) {
      clearIntervalFn(parentCheckInterval);
    }
    processRuntime.off("SIGINT", stop);
    processRuntime.off("SIGTERM", stop);
    processRuntime.stdout?.off("error", onOutputError);
    processRuntime.stderr?.off("error", onOutputError);
  };
}
