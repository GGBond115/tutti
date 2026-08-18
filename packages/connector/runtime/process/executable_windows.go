//go:build windows

package process

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type preparedExecutable struct {
	path       string
	file       *os.File
	privateDir string
}

func prepareExecutable(path string, expected *ExecutableIdentity) (preparedExecutable, error) {
	if expected == nil {
		return preparedExecutable{path: path}, nil
	}
	if !validExecutableIdentity(expected) {
		return preparedExecutable{}, errors.New("process executable identity is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return preparedExecutable{}, errors.New("verified process executable is not an ordinary file")
	}
	source, err := os.Open(path)
	if err != nil {
		return preparedExecutable{}, fmt.Errorf("open verified process executable: %w", err)
	}
	defer func() { _ = source.Close() }()
	if err := verifyExecutable(source, expected); err != nil {
		return preparedExecutable{}, err
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return preparedExecutable{}, fmt.Errorf("rewind verified process executable: %w", err)
	}

	privateDir, err := os.MkdirTemp("", ".tutti-verified-connector-")
	if err != nil {
		return preparedExecutable{}, err
	}
	snapshotPath := filepath.Join(privateDir, "runtime.exe")
	cleanup := func() { _ = os.RemoveAll(privateDir) }
	target, err := os.OpenFile(snapshotPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o500)
	if err != nil {
		cleanup()
		return preparedExecutable{}, err
	}
	_, copyErr := io.Copy(target, source)
	syncErr := target.Sync()
	closeErr := target.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		cleanup()
		return preparedExecutable{}, errors.Join(copyErr, syncErr, closeErr)
	}
	snapshot, err := os.Open(snapshotPath)
	if err != nil {
		cleanup()
		return preparedExecutable{}, err
	}
	verifyErr := verifyExecutable(snapshot, expected)
	closeErr = snapshot.Close()
	if verifyErr != nil || closeErr != nil {
		cleanup()
		return preparedExecutable{}, errors.Join(verifyErr, closeErr)
	}
	return preparedExecutable{path: snapshotPath, privateDir: privateDir}, nil
}

func verifyExecutable(file *os.File, expected *ExecutableIdentity) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("verified process executable is not an ordinary file")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return fmt.Errorf("fingerprint process executable: %w", err)
	}
	if size != expected.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		return errors.New("process executable does not match expected identity")
	}
	return nil
}

func validExecutableIdentity(identity *ExecutableIdentity) bool {
	if identity == nil || identity.SizeBytes <= 0 || len(identity.SHA256) != sha256.Size*2 ||
		identity.SHA256 != strings.ToLower(identity.SHA256) {
		return false
	}
	_, err := hex.DecodeString(identity.SHA256)
	return err == nil
}

func (executable *preparedExecutable) Close() error {
	if executable == nil {
		return nil
	}
	var result error
	if executable.file != nil {
		result = executable.file.Close()
		executable.file = nil
	}
	if executable.privateDir != "" {
		result = errors.Join(result, os.RemoveAll(executable.privateDir))
		executable.privateDir = ""
	}
	return result
}
