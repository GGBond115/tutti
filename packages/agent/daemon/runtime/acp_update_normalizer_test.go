package agentruntime

import (
	"encoding/json"
	"testing"
)

func TestACPModeValueReadsCurrentModeID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		update map[string]any
		want   string
	}{
		{name: "acp canonical currentModeId", update: map[string]any{"currentModeId": "acceptEdits"}, want: "acceptEdits"},
		{name: "snake current_mode_id", update: map[string]any{"current_mode_id": "plan"}, want: "plan"},
		{name: "legacy modeId fallback", update: map[string]any{"modeId": "default"}, want: "default"},
		{name: "empty", update: map[string]any{}, want: ""},
	}
	for _, tc := range cases {
		if got := acpModeValue(tc.update); got != tc.want {
			t.Fatalf("%s: acpModeValue = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestApplyACPUpdateToLiveStateCapturesCurrentModeID(t *testing.T) {
	t.Parallel()

	state := newACPLiveState()
	raw, err := json.Marshal(map[string]any{
		"update": map[string]any{
			"sessionUpdate": "current_mode_update",
			"currentModeId": "auto",
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	applyACPUpdateToLiveState(&state, "agent-session-1", raw)
	if state.currentMode != "auto" {
		t.Fatalf("state.currentMode = %q, want auto", state.currentMode)
	}
}

func TestACPInferTerminalToolStatusUnderstandsGitDiffNoIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		update   map[string]any
		output   map[string]any
		expected string
	}{
		{
			name: "differences are a completed command result",
			update: map[string]any{
				"rawInput": map[string]any{
					"command": []any{"git", "diff", "--no-index", "old", "new"},
				},
			},
			output:   map[string]any{"exitCode": 1, "stdout": "diff --git a/old b/new"},
			expected: messageStreamStateCompleted,
		},
		{
			name: "git diff execution errors still fail",
			update: map[string]any{
				"rawInput": map[string]any{"command": "git diff --no-index old new"},
			},
			output:   map[string]any{"exitCode": 2},
			expected: messageStreamStateFailed,
		},
		{
			name:     "unknown exit one remains failed",
			update:   map[string]any{"rawInput": map[string]any{"command": "rg missing"}},
			output:   map[string]any{"exitCode": 1},
			expected: messageStreamStateFailed,
		},
		{
			name: "compound shell command remains failed",
			update: map[string]any{
				"rawInput": map[string]any{"command": "git diff --no-index old new; exit 1"},
			},
			output:   map[string]any{"exitCode": 1},
			expected: messageStreamStateFailed,
		},
		{
			name: "multiple parsed commands remain failed",
			update: map[string]any{
				"rawInput": map[string]any{
					"parsed_cmd": []any{
						[]any{"git", "diff", "--no-index", "old", "new"},
						[]any{"rg", "missing"},
					},
				},
			},
			output:   map[string]any{"exitCode": 1},
			expected: messageStreamStateFailed,
		},
		{
			name: "conflicting command representations remain failed",
			update: map[string]any{
				"rawInput": map[string]any{
					"parsed_cmd": []any{
						[]any{"git", "diff", "--no-index", "old", "new"},
					},
					"command": "git diff --no-index old new; exit 1",
				},
			},
			output:   map[string]any{"exitCode": 1},
			expected: messageStreamStateFailed,
		},
		{
			name: "windows git executable is recognized from parsed command",
			update: map[string]any{
				"rawInput": map[string]any{
					"parsed_cmd": []any{[]any{"C:\\Program Files\\Git\\cmd\\git.exe", "diff", "--no-index", "old", "new"}},
				},
			},
			output:   map[string]any{"exit_code": 1},
			expected: messageStreamStateCompleted,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := acpInferTerminalToolStatus(test.update, test.output); got != test.expected {
				t.Fatalf("acpInferTerminalToolStatus() = %q, want %q", got, test.expected)
			}
		})
	}
}
