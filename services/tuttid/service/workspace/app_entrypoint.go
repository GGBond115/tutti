package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
)

var errAppPlatformUnsupported = errors.New("workspace app does not support this platform")

func currentAppPlatformKey() string {
	return appPlatformKey(runtime.GOOS, runtime.GOARCH)
}

func appPlatformKey(goos string, goarch string) string {
	return strings.TrimSpace(goos) + "-" + strings.TrimSpace(goarch)
}

func resolveAppEntrypoint(config workspacebiz.AppManifestRuntime, platformKey string) (string, error) {
	platformKey = strings.TrimSpace(platformKey)
	if entrypoint, ok := config.Entrypoints[platformKey]; ok {
		executable := strings.TrimSpace(entrypoint.Executable)
		if executable == "" {
			return "", fmt.Errorf("%w: entrypoint for %s is empty", errAppPlatformUnsupported, platformKey)
		}
		return executable, nil
	}
	if len(config.Entrypoints) > 0 || strings.HasPrefix(platformKey, "windows-") {
		return "", fmt.Errorf("%w: no entrypoint for %s", errAppPlatformUnsupported, platformKey)
	}
	bootstrap := strings.TrimSpace(config.Bootstrap)
	if bootstrap == "" {
		return "", fmt.Errorf("%w: no entrypoint for %s", errAppPlatformUnsupported, platformKey)
	}
	return bootstrap, nil
}

func validateAppEntrypointFile(packageDir string, config workspacebiz.AppManifestRuntime) (string, error) {
	entrypoint, err := resolveAppEntrypoint(config, currentAppPlatformKey())
	if err != nil {
		return "", err
	}
	entrypointPath := filepath.Join(packageDir, filepath.FromSlash(entrypoint))
	info, err := os.Stat(entrypointPath)
	if err != nil {
		return "", fmt.Errorf("stat runtime entrypoint %q: %w", entrypoint, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("runtime entrypoint %q must be a file", entrypoint)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("runtime entrypoint %q must be executable", entrypoint)
	}
	return entrypoint, nil
}
