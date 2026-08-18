//go:build darwin

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

	"golang.org/x/sys/unix"
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
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return preparedExecutable{}, fmt.Errorf("open verified process executable: %w", err)
	}
	source := os.NewFile(uintptr(fd), "verified-connector-executable")
	defer func() { _ = source.Close() }()
	if err := verifyExecutable(source, expected); err != nil {
		return preparedExecutable{}, err
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return preparedExecutable{}, err
	}
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return preparedExecutable{}, err
	}
	privateDir, err := os.MkdirTemp(tempRoot, ".tutti-verified-connector-")
	if err != nil {
		return preparedExecutable{}, err
	}
	snapshotPath := filepath.Join(privateDir, "runtime")
	cleanup := func() {
		_ = unix.Chflags(snapshotPath, 0)
		_ = unix.Chflags(privateDir, 0)
		_ = os.Chmod(privateDir, 0o700)
		_ = os.RemoveAll(privateDir)
	}
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
	if err := os.Chmod(snapshotPath, 0o500); err != nil {
		cleanup()
		return preparedExecutable{}, err
	}
	if err := unix.Chflags(snapshotPath, unix.UF_IMMUTABLE); err != nil {
		cleanup()
		return preparedExecutable{}, err
	}
	if err := os.Chmod(privateDir, 0o500); err != nil {
		cleanup()
		return preparedExecutable{}, err
	}
	if err := unix.Chflags(privateDir, unix.UF_IMMUTABLE); err != nil {
		cleanup()
		return preparedExecutable{}, err
	}
	snapshotFD, err := unix.Open(snapshotPath, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		cleanup()
		return preparedExecutable{}, err
	}
	snapshot := os.NewFile(uintptr(snapshotFD), "verified-connector-snapshot")
	if err := verifyExecutable(snapshot, expected); err != nil {
		_ = snapshot.Close()
		cleanup()
		return preparedExecutable{}, err
	}
	if err := snapshot.Close(); err != nil {
		cleanup()
		return preparedExecutable{}, err
	}
	return preparedExecutable{path: snapshotPath, privateDir: privateDir}, nil
}

func verifyExecutable(file *os.File, expected *ExecutableIdentity) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return errors.New("verified process executable is not an executable ordinary file")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return fmt.Errorf("fingerprint process executable descriptor: %w", err)
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
		_ = unix.Chflags(executable.path, 0)
		_ = unix.Chflags(executable.privateDir, 0)
		if err := os.Chmod(executable.privateDir, 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
		if err := os.RemoveAll(executable.privateDir); err != nil {
			result = errors.Join(result, err)
		}
		executable.privateDir = ""
	}
	return result
}
