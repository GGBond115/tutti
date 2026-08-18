package process

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultStdoutLimit  = int64(64 * 1024 * 1024)
	defaultStderrLimit  = int64(16 * 1024 * 1024)
	reservedFDEnvPrefix = "TUTTI_CONNECTOR_FD_"
)

type localTransport struct {
	stdoutLimit int64
	stderrLimit int64
}

// NewTransport returns the bounded, receipt-verifying process transport used
// by all locally executed Connector operations.
func NewTransport() (Transport, error) {
	return newTransport(defaultStdoutLimit, defaultStderrLimit), nil
}

func newTransport(stdoutLimit, stderrLimit int64) Transport {
	return localTransport{stdoutLimit: stdoutLimit, stderrLimit: stderrLimit}
}

func (transport localTransport) Start(ctx context.Context, spec Spec) (Connection, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	preparedExecutable, err := prepareExecutable(spec.Command[0], spec.ExecutableIdentity)
	if err != nil {
		return nil, err
	}
	started := false
	defer func() {
		if !started {
			_ = preparedExecutable.Close()
		}
	}()

	processContext, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(processContext, preparedExecutable.path, spec.Command[1:]...)
	if preparedExecutable.file != nil {
		command.ExtraFiles = append(command.ExtraFiles, preparedExecutable.file)
	}
	// A non-nil empty slice is intentional: Connector processes inherit no
	// daemon environment. The caller must pass every allowed value explicitly.
	command.Env = append([]string{}, spec.Env...)
	if cwd := strings.TrimSpace(spec.CWD); cwd != "" {
		command.Dir = cwd
	}
	if err := addSensitiveInheritedFiles(command, &spec); err != nil {
		cancel()
		return nil, err
	}
	prepareProcessGroup(command)

	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	connection := &localConnection{
		cancel:             cancel,
		command:            command,
		preparedExecutable: &preparedExecutable,
		done:               make(chan struct{}),
		closing:            make(chan struct{}),
		frames:             make(chan Frame, 16),
		stdin:              stdin,
		stdoutLimit:        transport.stdoutLimit,
		stderrLimit:        transport.stderrLimit,
	}
	command.Stdout = frameWriter{connection: connection, stdout: true}
	command.Stderr = frameWriter{connection: connection}
	// Keep this immediately adjacent to Start: the signed receipt is rechecked
	// after every other launch preparation step and before the child can observe
	// its entrypoint or modules.
	if err := verifyArtifactTrees(spec.ArtifactTrees); err != nil {
		_ = stdin.Close()
		cancel()
		return nil, err
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		return nil, err
	}
	started = true
	go connection.wait()
	return connection, nil
}

func verifyArtifactTrees(identities []ArtifactTreeIdentity) error {
	for _, identity := range identities {
		if !filepath.IsAbs(identity.Root) || len(identity.SHA256) != sha256.Size*2 ||
			strings.ToLower(identity.SHA256) != identity.SHA256 {
			return errors.New("connector artifact tree identity is invalid")
		}
		if _, err := hex.DecodeString(identity.SHA256); err != nil {
			return errors.New("connector artifact tree identity is invalid")
		}
		actual, err := treeInventoryDigest(identity.Root)
		if err != nil {
			return fmt.Errorf("verify connector artifact tree: %w", err)
		}
		if actual != identity.SHA256 {
			return errors.New("connector artifact tree changed before launch")
		}
	}
	return nil
}

func treeInventoryDigest(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." || relative == ".tutti-connector-receipt.json" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("connector artifact tree contains an unsupported file type")
		}
		_, _ = io.WriteString(hash, filepath.ToSlash(relative))
		_, _ = hash.Write([]byte{0})
		if entry.IsDir() {
			_, _ = hash.Write([]byte("dir\x00"))
			return nil
		}
		_, _ = io.WriteString(hash, fmt.Sprintf("file\x00%d\x00", info.Size()))
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		return errors.Join(copyErr, closeErr)
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validateSpec(spec Spec) error {
	if len(spec.Command) == 0 || strings.TrimSpace(spec.Command[0]) == "" {
		return errors.New("connector process command is required")
	}
	if !filepath.IsAbs(spec.Command[0]) {
		return errors.New("connector process executable must be absolute")
	}
	if spec.ExecutableIdentity == nil || strings.TrimSpace(spec.ExecutableIdentity.SHA256) == "" ||
		spec.ExecutableIdentity.SizeBytes <= 0 {
		return errors.New("connector process executable identity is required")
	}
	environmentKeys := make(map[string]struct{}, len(spec.Env))
	for _, item := range spec.Env {
		key, _, ok := strings.Cut(item, "=")
		if !ok || !validEnvironmentKey(key) {
			return errors.New("connector process environment entries must be explicit key=value pairs")
		}
		if strings.HasPrefix(strings.ToUpper(key), reservedFDEnvPrefix) {
			return fmt.Errorf("connector process environment key %q uses a host-reserved prefix", key)
		}
		normalizedKey := strings.ToUpper(key)
		if _, exists := environmentKeys[normalizedKey]; exists {
			return fmt.Errorf("connector process environment key %q is duplicated", key)
		}
		environmentKeys[normalizedKey] = struct{}{}
	}
	return nil
}

func validEnvironmentKey(key string) bool {
	if key == "" || key != strings.TrimSpace(key) {
		return false
	}
	for index, character := range key {
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') ||
			character == '_' || (index > 0 && character >= '0' && character <= '9') {
			continue
		}
		return false
	}
	return true
}

func addSensitiveInheritedFiles(command *exec.Cmd, spec *Spec) error {
	seen := make(map[string]struct{}, len(spec.Env)+len(spec.SensitiveInheritedFiles))
	for _, item := range spec.Env {
		key, _, _ := strings.Cut(item, "=")
		seen[strings.ToUpper(key)] = struct{}{}
	}
	for _, inherited := range spec.SensitiveInheritedFiles {
		key := strings.ToUpper(strings.TrimSpace(inherited.DescriptorEnvKey))
		if inherited.File == nil || strings.TrimSpace(inherited.Purpose) == "" {
			return errors.New("connector sensitive inherited file and purpose are required")
		}
		if !strings.HasPrefix(key, reservedFDEnvPrefix) || strings.ContainsAny(key, "=\x00") {
			return fmt.Errorf("connector sensitive descriptor environment key %q is invalid", inherited.DescriptorEnvKey)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("connector sensitive descriptor environment key %q is duplicated", key)
		}
		seen[key] = struct{}{}
		command.ExtraFiles = append(command.ExtraFiles, inherited.File)
		// ExtraFiles begin at fd 3. A verified executable descriptor occupies the
		// first slot when the platform uses one.
		fd := 3 + len(command.ExtraFiles) - 1
		command.Env = append(command.Env, fmt.Sprintf("%s=%d", key, fd))
	}
	return nil
}

type localConnection struct {
	cancel             context.CancelFunc
	command            *exec.Cmd
	preparedExecutable *preparedExecutable
	done               chan struct{}
	closing            chan struct{}
	frames             chan Frame
	stdin              io.WriteCloser
	stdoutLimit        int64
	stderrLimit        int64

	closeMu     sync.Mutex
	closingOnce sync.Once
	inputOnce   sync.Once
	sendMu      sync.Mutex
	outputMu    sync.Mutex
	stdout      int64
	stderr      int64
	limitErr    error
}

func (connection *localConnection) Send(data []byte) error {
	if connection == nil || connection.stdin == nil {
		return io.ErrClosedPipe
	}
	connection.sendMu.Lock()
	defer connection.sendMu.Unlock()
	_, err := connection.stdin.Write(data)
	return err
}

func (connection *localConnection) Recv() (Frame, error) {
	if connection == nil {
		return Frame{}, io.EOF
	}
	frame, ok := <-connection.frames
	if ok {
		return frame, nil
	}
	connection.outputMu.Lock()
	err := connection.limitErr
	connection.outputMu.Unlock()
	if err != nil {
		return Frame{}, err
	}
	return Frame{}, io.EOF
}

func (connection *localConnection) RecvContext(ctx context.Context) (Frame, error) {
	if connection == nil {
		return Frame{}, io.EOF
	}
	select {
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case frame, ok := <-connection.frames:
		if ok {
			return frame, nil
		}
		connection.outputMu.Lock()
		err := connection.limitErr
		connection.outputMu.Unlock()
		if err != nil {
			return Frame{}, err
		}
		return Frame{}, io.EOF
	}
}

func (connection *localConnection) Close() error {
	if connection == nil {
		return nil
	}
	connection.closeMu.Lock()
	defer connection.closeMu.Unlock()
	if connection.waitDone(0) {
		return nil
	}
	connection.closingOnce.Do(func() { close(connection.closing) })
	_ = connection.CloseInput()
	if connection.waitDone(250 * time.Millisecond) {
		return nil
	}
	_ = connection.Terminate()
	if connection.waitDone(750 * time.Millisecond) {
		return nil
	}
	killErr := connection.Kill()
	if connection.waitDone(2 * time.Second) {
		return nil
	}
	return errors.Join(killErr, errors.New("connector process did not exit after kill"))
}

func (connection *localConnection) CloseInput() error {
	if connection == nil || connection.stdin == nil {
		return nil
	}
	var err error
	connection.inputOnce.Do(func() { err = connection.stdin.Close() })
	return err
}

func (connection *localConnection) Terminate() error {
	if connection == nil {
		return nil
	}
	return terminateProcessGroup(connection.command)
}

func (connection *localConnection) Kill() error {
	if connection == nil {
		return nil
	}
	connection.cancel()
	return killProcessGroup(connection.command)
}

func (connection *localConnection) waitDone(timeout time.Duration) bool {
	if timeout <= 0 {
		select {
		case <-connection.done:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-connection.done:
		return true
	case <-timer.C:
		return false
	}
}

func (connection *localConnection) acceptOutput(stdout bool, size int) error {
	connection.outputMu.Lock()
	defer connection.outputMu.Unlock()
	if stdout {
		connection.stdout += int64(size)
		if connection.stdoutLimit > 0 && connection.stdout > connection.stdoutLimit {
			connection.limitErr = fmt.Errorf("connector process stdout exceeds limit %d", connection.stdoutLimit)
		}
	} else {
		connection.stderr += int64(size)
		if connection.stderrLimit > 0 && connection.stderr > connection.stderrLimit {
			connection.limitErr = fmt.Errorf("connector process stderr exceeds limit %d", connection.stderrLimit)
		}
	}
	return connection.limitErr
}

func (connection *localConnection) wait() {
	err := connection.command.Wait()
	if connection.preparedExecutable != nil {
		_ = connection.preparedExecutable.Close()
		connection.preparedExecutable = nil
	}
	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	select {
	case connection.frames <- Frame{ExitCode: &exitCode}:
	case <-connection.closing:
	}
	close(connection.frames)
	close(connection.done)
}

type frameWriter struct {
	connection *localConnection
	stdout     bool
}

func (writer frameWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if err := writer.connection.acceptOutput(writer.stdout, len(data)); err != nil {
		_ = killProcessGroup(writer.connection.command)
		return len(data), nil
	}
	frame := Frame{}
	if writer.stdout {
		frame.Stdout = append([]byte(nil), data...)
	} else {
		frame.Stderr = append([]byte(nil), data...)
	}
	select {
	case writer.connection.frames <- frame:
		return len(data), nil
	case <-writer.connection.closing:
		return len(data), nil
	}
}

var _ Transport = localTransport{}
var _ GracefulConnection = (*localConnection)(nil)
var _ ContextConnection = (*localConnection)(nil)
