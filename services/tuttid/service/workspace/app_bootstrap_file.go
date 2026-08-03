package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func validateAppBootstrapFile(packageDir string, bootstrap string) error {
	bootstrap = strings.TrimSpace(bootstrap)
	bootstrapPath := filepath.Join(packageDir, filepath.FromSlash(bootstrap))
	info, err := os.Stat(bootstrapPath)
	if err != nil {
		return fmt.Errorf("stat runtime bootstrap %q: %w", bootstrap, err)
	}
	if info.IsDir() {
		return fmt.Errorf("runtime bootstrap %q must be a file", bootstrap)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return fmt.Errorf("runtime bootstrap %q must be executable", bootstrap)
	}
	return nil
}
