package workspace

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
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

func validateAppEntrypointFiles(packageDir string, config workspacebiz.AppManifestRuntime) error {
	if len(config.Entrypoints) == 0 {
		return validateAppEntrypointFile(packageDir, config.Bootstrap, runtime.GOOS != "windows")
	}

	platforms := make([]string, 0, len(config.Entrypoints))
	for platform := range config.Entrypoints {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	for _, platform := range platforms {
		entrypoint := strings.TrimSpace(config.Entrypoints[platform].Executable)
		if entrypoint == "" {
			return fmt.Errorf("runtime entrypoint for %s is empty", platform)
		}
		requireExecutable := runtime.GOOS != "windows" && !strings.HasPrefix(platform, "windows-")
		if err := validateAppEntrypointFile(packageDir, entrypoint, requireExecutable); err != nil {
			return fmt.Errorf("validate runtime entrypoint for %s: %w", platform, err)
		}
	}
	return nil
}

func validateAppEntrypointFile(packageDir string, entrypoint string, requireExecutable bool) error {
	entrypoint = strings.TrimSpace(entrypoint)
	cleanEntrypoint := path.Clean(entrypoint)
	if entrypoint == "" || cleanEntrypoint == "." || strings.Contains(entrypoint, "\\") || path.IsAbs(entrypoint) || filepath.IsAbs(entrypoint) || filepath.VolumeName(entrypoint) != "" {
		return fmt.Errorf("runtime entrypoint %q must be a relative package path", entrypoint)
	}
	if cleanEntrypoint == ".." || strings.HasPrefix(cleanEntrypoint, "../") {
		return fmt.Errorf("runtime entrypoint %q escapes the package directory", entrypoint)
	}
	entrypointPath := filepath.Join(packageDir, filepath.FromSlash(cleanEntrypoint))
	info, err := os.Stat(entrypointPath)
	if err != nil {
		return fmt.Errorf("stat runtime entrypoint %q: %w", entrypoint, err)
	}
	if info.IsDir() {
		return fmt.Errorf("runtime entrypoint %q must be a file", entrypoint)
	}
	if requireExecutable && info.Mode()&0o111 == 0 {
		return fmt.Errorf("runtime entrypoint %q must be executable", entrypoint)
	}
	return nil
}
