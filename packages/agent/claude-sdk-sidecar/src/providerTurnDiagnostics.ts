import { createHash } from "node:crypto";
import { pid, stderr } from "node:process";

export const CLAUDE_CODE_PROVIDER_TURN_DIAGNOSTIC_PREFIX =
  "CLAUDE_CODE_PROVIDER_TURN_DIAGNOSTIC";

type ProviderTurnDiagnosticCandidate = {
  turnId: string;
  promptUuid: string;
  providerTurnId?: string;
  settled: boolean;
  synthetic?: boolean;
  awaitingProviderTurnIdentity?: boolean;
};

export function logClaudeProviderTurnDiagnostic(
  stage: string,
  payload: Record<string, unknown>
): void {
  try {
    stderr.write(
      `${CLAUDE_CODE_PROVIDER_TURN_DIAGNOSTIC_PREFIX} ${JSON.stringify({
        stage,
        sidecarPid: pid,
        ...payload
      })}\n`
    );
  } catch {
    // Diagnostics must never change provider execution semantics.
  }
}

export function providerTurnDiagnosticError(
  error: unknown
): Record<string, unknown> {
  try {
    const name = error instanceof Error ? error.name : typeof error;
    const message = error instanceof Error ? error.message : String(error);
    const withMetadata =
      error && typeof error === "object"
        ? (error as { code?: unknown; status?: unknown })
        : undefined;
    const code = diagnosticScalar(withMetadata?.code);
    const status = diagnosticScalar(withMetadata?.status);
    return {
      name,
      ...(code !== undefined ? { code } : {}),
      ...(status !== undefined ? { status } : {}),
      ...(message
        ? {
            messageLength: message.length,
            messageFingerprint: createHash("sha256")
              .update(message)
              .digest("hex")
              .slice(0, 12)
          }
        : {})
    };
  } catch {
    return { name: "UninspectableError" };
  }
}

export function logClaudeUnresolvedProviderTurns(
  stage: string,
  input: {
    providerSessionId: string;
    generationId: number;
    turns: readonly ProviderTurnDiagnosticCandidate[];
    phaseForTurn: (turnId: string) => string | undefined;
    error?: unknown;
  }
): void {
  for (const turn of input.turns) {
    if (
      turn.settled ||
      turn.synthetic ||
      turn.providerTurnId?.trim() ||
      !turn.turnId.trim()
    ) {
      continue;
    }
    try {
      logClaudeProviderTurnDiagnostic(stage, {
        turnId: turn.turnId.trim(),
        providerSessionId: input.providerSessionId.trim(),
        promptCorrelationId: turn.promptUuid.trim(),
        generationId: input.generationId,
        providerTurnPhase: input.phaseForTurn(turn.turnId) ?? "unknown",
        awaitingProviderTurnIdentity:
          turn.awaitingProviderTurnIdentity === true,
        ...(input.error
          ? { error: providerTurnDiagnosticError(input.error) }
          : {})
      });
    } catch {
      // Diagnostics must never change query settlement semantics.
    }
  }
}

function diagnosticScalar(value: unknown): string | number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string" && value.trim()) {
    const normalized = value.trim();
    return /^[a-z0-9_.:-]{1,64}$/iu.test(normalized) ? normalized : undefined;
  }
  return undefined;
}
