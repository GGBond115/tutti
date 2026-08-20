package agentruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexAppServerStartupTraceRecordsStructuredSpanClose(t *testing.T) {
	var observations []CodexAppServerSpanObservation
	trace := &codexAppServerStartupTrace{
		startedAt: time.Now(),
		session:   Session{Provider: ProviderCodex, RoomID: "room-1", AgentSessionID: "session-1"},
		path:      filepath.Join(t.TempDir(), "trace.jsonl"),
		spanObserver: func(observation CodexAppServerSpanObservation) {
			observations = append(observations, observation)
		},
		spanStartedAt: make(map[string][]time.Time),
	}
	firstChunk := []byte(`{"timestamp":"2026-08-20T02:00:00.000Z","level":"INFO","target":"codex_core","fields":{"message":"new"},"span":{"name":"session_init"}`)
	secondChunk := []byte(`}
{"timestamp":"2026-08-20T02:00:00.125Z","level":"INFO","target":"codex_core","fields":{"message":"close","time.busy":"120ms","time.idle":"5ms"},"span":{"name":"session_init"}}
{"timestamp":"2026-08-20T02:00:00.200Z","level":"INFO","target":"codex_core","fields":{"message":"close"},"span":{"name":"unrelated_span"}}
`)

	trace.LogStderr(firstChunk)
	trace.LogStderr(secondChunk)

	data, err := os.ReadFile(trace.path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var records []map[string]any
	for _, line := range splitJSONLines(data) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode trace line %q: %v", line, err)
		}
		records = append(records, record)
	}

	var closeRecord map[string]any
	spanRecords := 0
	for _, record := range records {
		if record["event"] != "app_server.span" {
			continue
		}
		spanRecords++
		if record["span_phase"] == "close" {
			closeRecord = record
		}
	}
	if spanRecords != 2 {
		t.Fatalf("span records = %d, want new and close only", spanRecords)
	}
	if closeRecord == nil {
		t.Fatal("missing session_init close span record")
	}
	if closeRecord["span_name"] != "session_init" {
		t.Fatalf("span_name = %#v, want session_init", closeRecord["span_name"])
	}
	if closeRecord["duration_ms"] != float64(125) {
		t.Fatalf("duration_ms = %#v, want 125", closeRecord["duration_ms"])
	}
	if closeRecord["span_busy"] != "120ms" || closeRecord["span_idle"] != "5ms" {
		t.Fatalf("span timing = %#v, want busy/idle values", closeRecord)
	}
	if len(observations) != 1 {
		t.Fatalf("span observations = %d, want one completed span", len(observations))
	}
	observation := observations[0]
	if observation.Provider != ProviderCodex || observation.RoomID != "room-1" || observation.AgentSessionID != "session-1" {
		t.Fatalf("observation scope = %#v, want Codex room/session scope", observation)
	}
	if observation.SpanName != "session_init" || observation.SpanPhase != "close" || observation.DurationMS != 125 {
		t.Fatalf("observation span = %#v, want session_init close at 125ms", observation)
	}
	if observation.SpanBusy != "120ms" || observation.SpanIdle != "5ms" {
		t.Fatalf("observation timing = %#v, want busy/idle values", observation)
	}
}

func TestCodexAppServerStartupTraceObserverPanicDoesNotEscape(t *testing.T) {
	trace := &codexAppServerStartupTrace{
		startedAt: time.Now(),
		session:   Session{Provider: ProviderCodex, AgentSessionID: "session-1"},
		path:      filepath.Join(t.TempDir(), "trace.jsonl"),
		spanObserver: func(CodexAppServerSpanObservation) {
			panic("analytics observer failure")
		},
		spanStartedAt: make(map[string][]time.Time),
	}

	trace.LogStderr([]byte(`{"timestamp":"2026-08-20T02:00:00.000Z","fields":{"message":"new"},"span":{"name":"session_init"}}
{"timestamp":"2026-08-20T02:00:00.125Z","fields":{"message":"close"},"span":{"name":"session_init"}}
`))
}

func TestWithCodexAppServerLoggingReplacesInheritedLogSettings(t *testing.T) {
	env := withCodexAppServerLogging([]string{
		"log_format=pretty",
		"rust_log=off",
		"SESSION_ENV=1",
	})
	if !containsString(env, "SESSION_ENV=1") {
		t.Fatalf("env = %#v, lost session env", env)
	}
	if !containsString(env, codexAppServerLogFormatEnv) || !containsString(env, codexAppServerRustLogEnv) {
		t.Fatalf("env = %#v, missing managed Codex log settings", env)
	}
	if countEnvironmentKeyFold(env, "LOG_FORMAT") != 1 || countEnvironmentKeyFold(env, "RUST_LOG") != 1 {
		t.Fatalf("env = %#v, expected one managed value per log key", env)
	}
}

func splitJSONLines(data []byte) [][]byte {
	lines := make([][]byte, 0)
	for len(data) > 0 {
		index := 0
		for index < len(data) && data[index] != '\n' {
			index++
		}
		line := data[:index]
		if len(line) > 0 {
			lines = append(lines, line)
		}
		if index == len(data) {
			break
		}
		data = data[index+1:]
	}
	return lines
}

func countEnvironmentKeyFold(env []string, key string) int {
	count := 0
	for _, entry := range env {
		candidateKey, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(candidateKey, key) {
			count++
		}
	}
	return count
}
