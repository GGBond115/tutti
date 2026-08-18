//go:build darwin || linux || windows

package process

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPreparedExecutableRunsVerifiedBytesAfterSourcePathReplacement(t *testing.T) {
	path, identity := copyCurrentExecutableWithIdentity(t)
	prepared, err := prepareExecutable(path, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Errorf("close prepared executable: %v", err)
		}
	}()
	if err := os.Rename(path, path+".verified"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unverified replacement"), 0o700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(prepared.path, "-test.run=TestConnectorProcessFixture")
	if prepared.file != nil {
		command.ExtraFiles = []*os.File{prepared.file}
	}
	command.Env = []string{
		"TUTTI_CONNECTOR_PROCESS_FIXTURE=1",
		"ALLOWED_VALUE=verified",
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run prepared executable: %v; output=%q", err, output)
	}
	if got := string(output); !strings.HasPrefix(got, "allowed=verified leaked=") {
		t.Fatalf("prepared executable output = %q", got)
	}
}
