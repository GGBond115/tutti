export interface AgentProcessingRowVM {
  kind: "processing";
  id: string;
  turnId: string | null;
  label?: string | null;
  reason?: "provider-continuation" | null;
  occurredAtUnixMs: number | null;
}
