package workspace

import (
	"errors"
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
