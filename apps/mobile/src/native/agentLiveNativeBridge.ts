import type {
  AgentLiveDelivery,
  AgentLiveReconcileKey
} from "../services/servicePorts";

export function parseAgentLiveDeliveries(
  workspaceId: string,
  payload: string
): AgentLiveDelivery[] {
  try {
    const envelope = JSON.parse(payload) as {
      reason?: unknown;
      result?: {
        accepted?: unknown;
        reason?: unknown;
        reconcileRequired?: unknown;
      };
      status?: unknown;
      workspaceId?: unknown;
    };
    if (envelope.workspaceId !== workspaceId) return [];
    if (envelope.status === "disconnected") {
      return [
        {
          kind: "connection",
          reason:
            typeof envelope.reason === "string"
              ? envelope.reason
              : "stream_closed",
          status: "disconnected"
        }
      ];
    }
    const result = envelope.result;
    if (!result || !Array.isArray(result.accepted)) return [];
    const deliveries: AgentLiveDelivery[] = [];
    let hasDiscontinuity = false;
    for (const accepted of result.accepted) {
      if (!isRecord(accepted)) continue;
      if (accepted.kind === "stream_ready") {
        deliveries.push({ kind: "connection", status: "connected" });
        continue;
      }
      if (accepted.kind === "event" && isRecord(accepted.event)) {
        deliveries.push({
          event: accepted.event as unknown as Extract<
            AgentLiveDelivery,
            { kind: "event" }
          >["event"],
          kind: "event"
        });
        continue;
      }
      if (accepted.kind === "discontinuity") {
        const discontinuity = isRecord(accepted.discontinuity)
          ? accepted.discontinuity
          : {};
        hasDiscontinuity = true;
        deliveries.push({
          kind: "discontinuity",
          reason:
            typeof discontinuity.reason === "string"
              ? discontinuity.reason
              : "stream_discontinuity",
          reconcileKeys: parseReconcileKeys(discontinuity.reconcileKeys)
        });
        continue;
      }
      if (accepted.kind === "rejected") {
        deliveries.push({
          kind: "connection",
          reason: "protocol_rejected",
          status: "disconnected"
        });
        continue;
      }
      deliveries.push({
        kind: "discontinuity",
        reason:
          accepted.kind === "goal_changed" ||
          accepted.kind === "attachment_changed"
            ? accepted.kind
            : "unknown_delivery",
        reconcileKeys: []
      });
    }
    if (result.reconcileRequired === true && !hasDiscontinuity) {
      deliveries.push({
        kind: "discontinuity",
        reason:
          typeof result.reason === "string"
            ? result.reason
            : "stream_discontinuity",
        reconcileKeys: []
      });
    }
    return deliveries;
  } catch {
    return [
      {
        kind: "discontinuity",
        reason: "invalid_native_delivery",
        reconcileKeys: []
      }
    ];
  }
}

function parseReconcileKeys(value: unknown): AgentLiveReconcileKey[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((candidate) => {
    if (
      !isRecord(candidate) ||
      typeof candidate.kind !== "string" ||
      typeof candidate.workspaceId !== "string"
    ) {
      return [];
    }
    return [
      {
        kind: candidate.kind,
        workspaceId: candidate.workspaceId,
        ...(typeof candidate.agentSessionId === "string"
          ? { agentSessionId: candidate.agentSessionId }
          : {}),
        ...(typeof candidate.messageId === "string"
          ? { messageId: candidate.messageId }
          : {}),
        ...(typeof candidate.turnId === "string"
          ? { turnId: candidate.turnId }
          : {}),
        ...(typeof candidate.requestId === "string"
          ? { requestId: candidate.requestId }
          : {})
      }
    ];
  });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
