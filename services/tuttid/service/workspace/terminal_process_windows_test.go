//go:build windows

package workspace

import "testing"

func TestNormalizeWindowsTerminalInputExpandsBareCarriageReturns(t *testing.T) {
	got := string(normalizeWindowsTerminalInput([]byte("first\rsecond\r\n")))
	if got != "first\r\nsecond\r\n" {
		t.Fatalf("normalizeWindowsTerminalInput() = %q", got)
	}
}
