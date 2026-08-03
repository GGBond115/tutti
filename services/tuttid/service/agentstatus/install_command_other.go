//go:build !windows

package agentstatus

import (
	"context"
	"os/exec"
)

func newInstallExecCommand(ctx context.Context, executable string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, executable, args...)
}
