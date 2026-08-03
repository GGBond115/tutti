import type { AgentActivityCapabilityReference } from "@tutti-os/agent-activity-core";
import type { WorkspaceAgentCapabilityReference } from "@tutti-os/client-tuttid-ts";

export function tuttiCapabilityReferencesFromActivity(
  references: readonly AgentActivityCapabilityReference[] | null | undefined
): WorkspaceAgentCapabilityReference[] {
  return (
    references?.map((reference) => {
      if (
        reference.capability !== "tutti" ||
        reference.source !== "slash_command"
      ) {
        throw new Error(
          "Unsupported workspace agent capability reference contract"
        );
      }
      return {
        capability: reference.capability,
        source: reference.source
      };
    }) ?? []
  );
}

export function agentActivityCapabilityReferencesFromTuttid(
  references: WorkspaceAgentCapabilityReference[] | undefined
): AgentActivityCapabilityReference[] {
  if (references === undefined) {
    return [];
  }
  if (!Array.isArray(references)) {
    throw new Error(
      "Protocol contract error: workspace agent capabilityRefs must be an array"
    );
  }
  return references.map((reference) => {
    if (
      reference?.capability !== "tutti" ||
      reference.source !== "slash_command"
    ) {
      throw new Error(
        "Protocol contract error: unsupported workspace agent capability reference"
      );
    }
    return {
      capability: reference.capability,
      source: reference.source
    };
  });
}
