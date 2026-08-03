package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
)

func TestResolveAppEntrypointUsesDeclaredPlatformExecutable(t *testing.T) {
	config := workspacebiz.AppManifestRuntime{
		Bootstrap: "bootstrap.sh",
		Entrypoints: map[string]workspacebiz.AppManifestRuntimeEntrypoint{
			"windows-amd64": {Executable: "bin/windows-amd64/server.exe"},
		},
	}

	got, err := resolveAppEntrypoint(config, "windows-amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got != "bin/windows-amd64/server.exe" {
		t.Fatalf("resolveAppEntrypoint() = %q", got)
	}
}

func TestResolveAppEntrypointFallsBackToLegacyBootstrap(t *testing.T) {
	got, err := resolveAppEntrypoint(workspacebiz.AppManifestRuntime{Bootstrap: "bootstrap.sh"}, "darwin-arm64")
	if err != nil {
		t.Fatal(err)
	}
	if got != "bootstrap.sh" {
		t.Fatalf("resolveAppEntrypoint() = %q", got)
	}
}

func TestResolveAppEntrypointRejectsMissingPlatformAndBootstrap(t *testing.T) {
	_, err := resolveAppEntrypoint(workspacebiz.AppManifestRuntime{}, "windows-amd64")
	if !errors.Is(err, errAppPlatformUnsupported) {
		t.Fatalf("resolveAppEntrypoint() error = %v", err)
	}
}

func TestResolveAppEntrypointRejectsLegacyShellBootstrapOnWindows(t *testing.T) {
	_, err := resolveAppEntrypoint(workspacebiz.AppManifestRuntime{Bootstrap: "bootstrap.sh"}, "windows-amd64")
	if !errors.Is(err, errAppPlatformUnsupported) {
		t.Fatalf("resolveAppEntrypoint() error = %v", err)
	}
}

func TestResolveAppEntrypointRejectsMissingDeclaredPlatform(t *testing.T) {
	config := workspacebiz.AppManifestRuntime{
		Bootstrap: "bootstrap.sh",
		Entrypoints: map[string]workspacebiz.AppManifestRuntimeEntrypoint{
			"darwin-arm64": {Executable: "bin/darwin-arm64/server"},
		},
	}
	_, err := resolveAppEntrypoint(config, "windows-amd64")
	if !errors.Is(err, errAppPlatformUnsupported) {
		t.Fatalf("resolveAppEntrypoint() error = %v", err)
	}
}

func TestValidateAppEntrypointFilesIsIndependentOfHostPlatform(t *testing.T) {
	packageDir := t.TempDir()
	writeTestEntrypoint(t, packageDir, "bin/darwin-arm64/server", 0o755)
	writeTestEntrypoint(t, packageDir, "bin/windows-amd64/server.exe", 0o644)
	config := workspacebiz.AppManifestRuntime{
		Entrypoints: map[string]workspacebiz.AppManifestRuntimeEntrypoint{
			"darwin-arm64":  {Executable: "bin/darwin-arm64/server"},
			"windows-amd64": {Executable: "bin/windows-amd64/server.exe"},
		},
	}

	if err := validateAppEntrypointFiles(packageDir, config); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveAppEntrypoint(config, "linux-amd64"); !errors.Is(err, errAppPlatformUnsupported) {
		t.Fatalf("resolveAppEntrypoint() error = %v", err)
	}
}

func TestValidateAppEntrypointFilesRejectsMissingDeclaredFile(t *testing.T) {
	config := workspacebiz.AppManifestRuntime{
		Entrypoints: map[string]workspacebiz.AppManifestRuntimeEntrypoint{
			"windows-amd64": {Executable: "bin/windows-amd64/missing.exe"},
		},
	}

	if err := validateAppEntrypointFiles(t.TempDir(), config); err == nil {
		t.Fatal("validateAppEntrypointFiles() error = nil")
	}
}

func writeTestEntrypoint(t *testing.T, packageDir string, name string, mode os.FileMode) {
	t.Helper()
	filePath := filepath.Join(packageDir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("test"), mode); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filePath, mode); err != nil {
			t.Fatal(err)
		}
	}
}
