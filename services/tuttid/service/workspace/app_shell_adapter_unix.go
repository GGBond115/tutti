//go:build !windows

package workspace

import (
	"context"
	"os/exec"
)

func (platformAppShellAdapter) Command(ctx context.Context, scriptPath string) (*exec.Cmd, []string, error) {
	return exec.CommandContext(ctx, scriptPath), nil, nil
}
