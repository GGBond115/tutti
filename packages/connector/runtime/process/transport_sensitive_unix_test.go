//go:build !windows

package process

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestTransportPassesSensitiveFileByDescriptorWithoutLeakingSecret(t *testing.T) {
	t.Setenv("SECRET_SHOULD_NOT_LEAK", "daemon-secret")
	path, identity := copyCurrentExecutableWithIdentity(t)
	credential, err := os.CreateTemp(t.TempDir(), "credential")
	if err != nil {
		t.Fatal(err)
	}
	defer credential.Close()
	if _, err := credential.WriteString("fd-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := credential.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	connection, err := newTransport(4096, 4096).Start(context.Background(), Spec{
		Command:            []string{path, "-test.run=TestConnectorProcessFixture"},
		ExecutableIdentity: identity,
		Env: []string{
			"TUTTI_CONNECTOR_PROCESS_FIXTURE=1",
			"ALLOWED_VALUE=visible",
		},
		SensitiveInheritedFiles: []SensitiveInheritedFile{{
			File: credential, DescriptorEnvKey: "TUTTI_CONNECTOR_FD_CREDENTIAL", Purpose: "test credential",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	var stdout strings.Builder
	for {
		frame, err := connection.Recv()
		if err != nil {
			t.Fatal(err)
		}
		stdout.Write(frame.Stdout)
		if frame.ExitCode != nil {
			break
		}
	}
	if got := stdout.String(); !strings.HasPrefix(got, "allowed=visible leaked= credential=fd-secret") || strings.Contains(got, "daemon-secret") {
		t.Fatalf("stdout = %q", got)
	}
}
