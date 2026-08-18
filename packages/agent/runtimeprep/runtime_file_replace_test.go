package runtimeprep

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeRolloutReplaceInstallsExactFile(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "rollout.tmp")
	destination := filepath.Join(directory, "rollout.jsonl")
	if err := os.WriteFile(source, []byte("new rollout\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old rollout\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceRuntimeFile(source, destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new rollout\n" {
		t.Fatalf("destination = %q, want new rollout", content)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists, err=%v", err)
	}
}
