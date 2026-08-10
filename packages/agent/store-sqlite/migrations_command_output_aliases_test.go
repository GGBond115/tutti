package storesqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
)

func TestCommandOutputAliasMigrationCompactsOnlyOversizedTerminalCommands(
	t *testing.T,
) {
	t.Parallel()

	store := openTestStore(t, testOptions(&staticProjectPaths{}))
	ctx := context.Background()
	if _, err := store.ReportSessionState(ctx, SessionStateReport{
		WorkspaceID:      "ws-command-alias",
		AgentSessionID:   "session-command-alias",
		Provider:         "codex",
		OccurredAtUnixMS: 10,
	}); err != nil {
		t.Fatalf("ReportSessionState() error = %v", err)
	}

	large := strings.Repeat("x", canonical.ToolOutputTextMaxBytes/2+256)
	tests := []struct {
		messageID string
		status    string
		payload   map[string]any
		wantText  bool
	}{
		{
			messageID: "oversized-terminal-alias",
			status:    "completed",
			payload: map[string]any{
				"toolName": "Bash",
				"input":    map[string]any{"command": "print output"},
				"output":   map[string]any{"text": large, "stdout": large + "\n"},
				"seq":      json.Number("9007199254740993"),
			},
			wantText: false,
		},
		{
			messageID: "small-terminal-alias",
			status:    "completed",
			payload: map[string]any{
				"toolName": "Bash",
				"input":    map[string]any{"command": "print output"},
				"output":   map[string]any{"text": "small", "stdout": "small\n"},
			},
			wantText: true,
		},
		{
			messageID: "oversized-non-command",
			status:    "completed",
			payload: map[string]any{
				"toolName": "AskUserQuestion",
				"output":   map[string]any{"text": large, "stdout": large + "\n"},
			},
			wantText: true,
		},
		{
			messageID: "oversized-running-command",
			status:    "running",
			payload: map[string]any{
				"toolName": "exec_command",
				"input":    map[string]any{"cmd": "print output"},
				"output":   map[string]any{"text": large, "stdout": large + "\n"},
			},
			wantText: true,
		},
		{
			messageID: "oversized-distinct-command-text",
			status:    "completed",
			payload: map[string]any{
				"toolName": "Bash",
				"input":    map[string]any{"command": "print output"},
				"output": map[string]any{
					"text":   strings.Repeat("y", len(large)),
					"stdout": large + "\n",
				},
			},
			wantText: true,
		},
	}

	for index, test := range tests {
		payloadJSON, err := json.Marshal(test.payload)
		if err != nil {
			t.Fatalf("marshal %s payload: %v", test.messageID, err)
		}
		if _, err := store.db.ExecContext(ctx, `
INSERT INTO workspace_agent_messages (
  workspace_id, agent_session_id, message_id, version, role, kind, status,
  payload_json, occurred_at_unix_ms, started_at_unix_ms, completed_at_unix_ms,
  created_at_unix_ms, updated_at_unix_ms
) VALUES (?, ?, ?, ?, 'assistant', 'tool_call', ?, ?, 101, 102, 103, 104, 105)
`, "ws-command-alias", "session-command-alias", test.messageID, index+1,
			test.status, string(payloadJSON)); err != nil {
			t.Fatalf("insert %s: %v", test.messageID, err)
		}
	}

	if _, err := store.db.ExecContext(ctx, `
DELETE FROM agent_store_schema_migrations WHERE id = ?
`, schemaMigrationWorkspaceAgentCommandOutputAliasesV1); err != nil {
		t.Fatalf("reset command output alias migration marker: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("rerun command output alias migration: %v", err)
	}

	for index, test := range tests {
		var (
			payloadJSON                        string
			version                            int
			occurredAt, startedAt, completedAt int64
			createdAt, updatedAt               int64
		)
		if err := store.db.QueryRowContext(ctx, `
SELECT payload_json, version, occurred_at_unix_ms, started_at_unix_ms,
       completed_at_unix_ms, created_at_unix_ms, updated_at_unix_ms
FROM workspace_agent_messages
WHERE workspace_id = ? AND agent_session_id = ? AND message_id = ?
`, "ws-command-alias", "session-command-alias", test.messageID).Scan(
			&payloadJSON,
			&version,
			&occurredAt,
			&startedAt,
			&completedAt,
			&createdAt,
			&updatedAt,
		); err != nil {
			t.Fatalf("read %s: %v", test.messageID, err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			t.Fatalf("decode %s: %v", test.messageID, err)
		}
		output := payload["output"].(map[string]any)
		_, hasText := output["text"]
		if hasText != test.wantText {
			t.Fatalf("%s output = %#v, want text present %t", test.messageID, output, test.wantText)
		}
		if test.messageID == "oversized-terminal-alias" &&
			!strings.Contains(payloadJSON, `"seq":9007199254740993`) {
			t.Fatalf("%s lost exact JSON integer: %s", test.messageID, payloadJSON[:128])
		}
		if test.messageID == "oversized-terminal-alias" && output["stdout"] != large+"\n" {
			t.Fatal("oversized terminal alias migration changed raw stdout")
		}
		if version != index+1 || occurredAt != 101 || startedAt != 102 ||
			completedAt != 103 || createdAt != 104 || updatedAt != 105 {
			t.Fatalf(
				"%s metadata changed: version=%d times=%d/%d/%d/%d/%d",
				test.messageID,
				version,
				occurredAt,
				startedAt,
				completedAt,
				createdAt,
				updatedAt,
			)
		}
	}

	var compactedJSON string
	if err := store.db.QueryRowContext(ctx, `
SELECT payload_json FROM workspace_agent_messages WHERE message_id = ?
`, "oversized-terminal-alias").Scan(&compactedJSON); err != nil {
		t.Fatalf("read compacted payload: %v", err)
	}
	if len(compactedJSON) >= canonical.ToolOutputTextMaxBytes {
		t.Fatalf("compacted payload has %d bytes, want below %d", len(compactedJSON), canonical.ToolOutputTextMaxBytes)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("repeat command output alias migration: %v", err)
	}
	var repeatedJSON string
	if err := store.db.QueryRowContext(ctx, `
SELECT payload_json FROM workspace_agent_messages WHERE message_id = ?
`, "oversized-terminal-alias").Scan(&repeatedJSON); err != nil {
		t.Fatalf("read repeated payload: %v", err)
	}
	if repeatedJSON != compactedJSON {
		t.Fatal("repeated migration changed the compacted payload")
	}
}
