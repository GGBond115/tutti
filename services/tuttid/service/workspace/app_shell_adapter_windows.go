//go:build windows

package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const workspaceAppShellEnv = "TUTTI_WORKSPACE_APP_SHELL"

func (platformAppShellAdapter) Command(ctx context.Context, scriptPath string) (*exec.Cmd, []string, error) {
	shellPath := strings.TrimSpace(os.Getenv(workspaceAppShellEnv))
	if shellPath == "" {
		return nil, nil, fmt.Errorf("Windows workspace app shell is unavailable: %s is not configured", workspaceAppShellEnv)
	}
	if !filepath.IsAbs(shellPath) {
		return nil, nil, fmt.Errorf("Windows workspace app shell must be an absolute path: %s", shellPath)
	}
	info, err := os.Stat(shellPath)
	if err != nil {
		return nil, nil, fmt.Errorf("stat Windows workspace app shell: %w", err)
	}
	if info.IsDir() {
		return nil, nil, fmt.Errorf("Windows workspace app shell must be a file: %s", shellPath)
	}
	return exec.CommandContext(ctx, shellPath, "--noprofile", "--norc", scriptPath), []string{filepath.Dir(shellPath)}, nil
}
