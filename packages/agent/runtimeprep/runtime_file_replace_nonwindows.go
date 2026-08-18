//go:build !windows

package runtimeprep

import "os"

// replaceRuntimeFile is the narrow owner for provider-state file replacement.
func replaceRuntimeFile(source, destination string) error {
	return os.Rename(source, destination)
}
