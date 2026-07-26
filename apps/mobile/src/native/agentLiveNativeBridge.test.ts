import { parseAgentLiveDeliveries } from "./agentLiveNativeBridge";

describe("parseAgentLiveDeliveries", () => {
  test("maps ready, event, and scoped discontinuity deliveries", () => {
    expect(
      parseAgentLiveDeliveries(
        "workspace-1",
        JSON.stringify({
          result: {
            accepted: [
              { kind: "stream_ready" },
              {
                event: {
                  agentSessionId: "session-1",
                  data: {
                    agentSessionId: "session-1",
                    eventType: "session_audit",
                    workspaceId: "workspace-1"
                  },
                  eventType: "session_audit",
                  workspaceId: "workspace-1"
                },
                kind: "event"
              },
              {
                discontinuity: {
                  reason: "sequence_gap",
                  reconcileKeys: [
                    {
                      agentSessionId: "session-1",
                      kind: "session",
                      workspaceId: "workspace-1"
                    }
                  ]
                },
                kind: "discontinuity"
              }
            ]
          },
          workspaceId: "workspace-1"
        })
      )
    ).toEqual([
      { kind: "connection", status: "connected" },
      {
        event: expect.objectContaining({
          agentSessionId: "session-1",
          eventType: "session_audit",
          workspaceId: "workspace-1"
        }),
        kind: "event"
      },
      {
        kind: "discontinuity",
        reason: "sequence_gap",
        reconcileKeys: [
          {
            agentSessionId: "session-1",
            kind: "session",
            workspaceId: "workspace-1"
          }
        ]
      }
    ]);
  });

  test("maps disconnect and rejects deliveries for another workspace", () => {
    expect(
      parseAgentLiveDeliveries(
        "workspace-1",
        JSON.stringify({
          reason: "stream_closed",
          status: "disconnected",
          workspaceId: "workspace-1"
        })
      )
    ).toEqual([
      {
        kind: "connection",
        reason: "stream_closed",
        status: "disconnected"
      }
    ]);
    expect(
      parseAgentLiveDeliveries(
        "workspace-1",
        JSON.stringify({
          result: { accepted: [{ kind: "stream_ready" }] },
          workspaceId: "workspace-2"
        })
      )
    ).toEqual([]);
  });

  test("turns invalid or unknown native deliveries into reconciliation", () => {
    expect(parseAgentLiveDeliveries("workspace-1", "{")).toEqual([
      {
        kind: "discontinuity",
        reason: "invalid_native_delivery",
        reconcileKeys: []
      }
    ]);
    expect(
      parseAgentLiveDeliveries(
        "workspace-1",
        JSON.stringify({
          result: { accepted: [{ kind: "future_delivery" }] },
          workspaceId: "workspace-1"
        })
      )
    ).toEqual([
      {
        kind: "discontinuity",
        reason: "unknown_delivery",
        reconcileKeys: []
      }
    ]);
  });
});
