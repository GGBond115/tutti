package process

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

func TestConnectorProcessFixture(_ *testing.T) {
	if os.Getenv("TUTTI_CONNECTOR_PROCESS_FIXTURE") != "1" {
		return
	}
	if os.Getenv("TUTTI_CONNECTOR_WAIT_INPUT") != "" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	if os.Getenv("TUTTI_CONNECTOR_OUTPUT_BYTES") != "" {
		fmt.Print(strings.Repeat("x", 1024))
		return
	}
	fmt.Printf("allowed=%s leaked=%s", os.Getenv("ALLOWED_VALUE"), os.Getenv("SECRET_SHOULD_NOT_LEAK"))
	if fd := os.Getenv("TUTTI_CONNECTOR_FD_CREDENTIAL"); fd != "" {
		var descriptor int
		_, _ = fmt.Sscanf(fd, "%d", &descriptor)
		secret, _ := io.ReadAll(os.NewFile(uintptr(descriptor), "credential"))
		fmt.Printf(" credential=%s", secret)
	}
}

func TestTransportUsesConnectorValidation(t *testing.T) {
	transport, err := NewTransport()
	if err != nil || transport == nil {
		t.Fatalf("NewTransport() = %#v, %v", transport, err)
	}
	connection, err := transport.Start(context.Background(), Spec{})
	if connection != nil || err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("Start() = %#v, %v", connection, err)
	}
}

func TestTransportRequiresAbsoluteVerifiedExecutable(t *testing.T) {
	transport := newTransport(1024, 1024)
	if _, err := transport.Start(context.Background(), Spec{Command: []string{"node"}}); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative command error = %v", err)
	}
	path, _ := copyCurrentExecutableWithIdentity(t)
	if _, err := transport.Start(context.Background(), Spec{Command: []string{path}}); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("missing identity error = %v", err)
	}
}

func TestTransportRejectsReservedMalformedOrDuplicateEnvironmentKeys(t *testing.T) {
	path, identity := copyCurrentExecutableWithIdentity(t)
	transport := newTransport(1024, 1024)
	for _, environment := range [][]string{
		{"TUTTI_CONNECTOR_FD_CREDENTIAL=3"},
		{" BAD=value"},
		{"BAD-NAME=value"},
		{"9BAD=value"},
		{"PATH=/trusted", "path=/untrusted"},
	} {
		if _, err := transport.Start(context.Background(), Spec{
			Command: []string{path}, ExecutableIdentity: identity, Env: environment,
		}); err == nil {
			t.Fatalf("Start(Env=%#v) error = nil, want rejection", environment)
		}
	}
}

func TestTransportUsesExplicitEnvironmentAndExposesLifecycleCapabilities(t *testing.T) {
	t.Setenv("SECRET_SHOULD_NOT_LEAK", "daemon-secret")
	path, identity := copyCurrentExecutableWithIdentity(t)
	transport := newTransport(4096, 4096)
	connection, err := transport.Start(context.Background(), Spec{
		ConnectorKey:       "calendar",
		ConnectionID:       "connection-1",
		Command:            []string{path, "-test.run=TestConnectorProcessFixture"},
		ExecutableIdentity: identity,
		Env: []string{
			"TUTTI_CONNECTOR_PROCESS_FIXTURE=1",
			"ALLOWED_VALUE=visible",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if _, ok := connection.(ContextConnection); !ok {
		t.Fatal("transport connection does not expose contextual receive")
	}
	if _, ok := connection.(GracefulConnection); !ok {
		t.Fatal("transport connection does not expose graceful shutdown")
	}

	var stdout strings.Builder
	terminalCode := -1
	for {
		frame, err := connection.Recv()
		if err != nil {
			t.Fatal(err)
		}
		stdout.Write(frame.Stdout)
		if frame.ExitCode != nil {
			terminalCode = *frame.ExitCode
			break
		}
	}
	if got := stdout.String(); !strings.HasPrefix(got, "allowed=visible leaked=") || strings.Contains(got, "daemon-secret") {
		t.Fatalf("stdout = %q", got)
	}
	if terminalCode != 0 {
		t.Fatalf("terminal exit code = %d, want 0", terminalCode)
	}
}

func TestTransportRecvContextObservesCancellationWithoutClosingProcess(t *testing.T) {
	path, identity := copyCurrentExecutableWithIdentity(t)
	connection, err := newTransport(4096, 4096).Start(context.Background(), Spec{
		Command:            []string{path, "-test.run=TestConnectorProcessFixture"},
		ExecutableIdentity: identity,
		Env: []string{
			"TUTTI_CONNECTOR_PROCESS_FIXTURE=1",
			"TUTTI_CONNECTOR_WAIT_INPUT=1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	contextual := connection.(ContextConnection)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := contextual.RecvContext(ctx); !strings.Contains(fmt.Sprint(err), "context canceled") {
		t.Fatalf("RecvContext() error = %v, want context cancellation", err)
	}
}

func TestTransportGracefulCloseInputLetsProcessExitNormally(t *testing.T) {
	path, identity := copyCurrentExecutableWithIdentity(t)
	connection, err := newTransport(4096, 4096).Start(context.Background(), Spec{
		Command:            []string{path, "-test.run=TestConnectorProcessFixture"},
		ExecutableIdentity: identity,
		Env: []string{
			"TUTTI_CONNECTOR_PROCESS_FIXTURE=1",
			"TUTTI_CONNECTOR_WAIT_INPUT=1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	graceful := connection.(GracefulConnection)
	if err := graceful.CloseInput(); err != nil {
		t.Fatal(err)
	}
	for {
		frame, err := connection.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if frame.ExitCode == nil {
			continue
		}
		if *frame.ExitCode != 0 {
			t.Fatalf("exit code = %d, want 0", *frame.ExitCode)
		}
		break
	}
}

func TestTransportEnforcesOutputLimit(t *testing.T) {
	path, identity := copyCurrentExecutableWithIdentity(t)
	connection, err := newTransport(32, 4096).Start(context.Background(), Spec{
		Command:            []string{path, "-test.run=TestConnectorProcessFixture"},
		ExecutableIdentity: identity,
		Env: []string{
			"TUTTI_CONNECTOR_PROCESS_FIXTURE=1",
			"TUTTI_CONNECTOR_OUTPUT_BYTES=1024",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	for {
		_, err := connection.Recv()
		if err == nil {
			continue
		}
		if !strings.Contains(err.Error(), "stdout exceeds limit") {
			t.Fatalf("Recv() error = %v", err)
		}
		break
	}
}

func TestTransportRejectsChangedExecutableIdentity(t *testing.T) {
	path, identity := copyCurrentExecutableWithIdentity(t)
	if err := os.WriteFile(path, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	connection, err := newTransport(4096, 4096).Start(context.Background(), Spec{
		Command: []string{path}, ExecutableIdentity: identity,
	})
	if connection != nil || err == nil || !strings.Contains(err.Error(), "does not match expected identity") {
		t.Fatalf("Start() = %#v, %v, want executable identity rejection", connection, err)
	}
}

func TestTransportRejectsPreparedTreeMutationAtLaunch(t *testing.T) {
	path, identity := copyCurrentExecutableWithIdentity(t)
	artifactRoot := t.TempDir()
	entrypoint := filepath.Join(artifactRoot, "connector.js")
	if err := os.WriteFile(entrypoint, []byte("trusted"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := treeInventoryDigest(artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entrypoint, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	connection, err := newTransport(4096, 4096).Start(context.Background(), Spec{
		Command:            []string{path},
		ExecutableIdentity: identity,
		ArtifactTrees:      []ArtifactTreeIdentity{{Root: artifactRoot, SHA256: inventory}},
	})
	if connection != nil || err == nil || !strings.Contains(err.Error(), "changed before launch") {
		t.Fatalf("Start() = %#v, %v, want prepared-tree identity rejection", connection, err)
	}
}

func copyCurrentExecutableWithIdentity(t *testing.T) (string, *ExecutableIdentity) {
	t.Helper()
	sourcePath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	name := "verified-runtime"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	targetPath := filepath.Join(t.TempDir(), name)
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(target, hash), source)
	closeErr := target.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		t.Fatal(err)
	}
	return targetPath, &ExecutableIdentity{SHA256: hex.EncodeToString(hash.Sum(nil)), SizeBytes: size}
}
