package workspace

import (
	"context"
	"os/exec"
)

// AppShellAdapter adapts the stable POSIX script contract to the host
// platform. Callers own lifecycle and environment setup; implementations only
// decide how a package script is invoked.
type AppShellAdapter interface {
	Command(context.Context, string) (command *exec.Cmd, binDirs []string, err error)
}

type platformAppShellAdapter struct{}

func appShellAdapterOrDefault(adapter AppShellAdapter) AppShellAdapter {
	if adapter != nil {
		return adapter
	}
	return platformAppShellAdapter{}
}
