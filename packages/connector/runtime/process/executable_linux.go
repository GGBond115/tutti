//go:build linux

package process

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

type preparedExecutable struct {
	path string
	file *os.File
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
	file := os.NewFile(uintptr(fd), "verified-connector-executable")
	closeWithError := func(err error) (preparedExecutable, error) {
		_ = file.Close()
		return preparedExecutable{}, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return closeWithError(errors.New("verified process executable is not an executable ordinary file"))
	}
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return closeWithError(fmt.Errorf("fingerprint process executable descriptor: %w", err))
	}
	if size != expected.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		return closeWithError(errors.New("process executable does not match expected identity"))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return closeWithError(fmt.Errorf("rewind process executable descriptor: %w", err))
	}
	return preparedExecutable{path: "/proc/self/fd/3", file: file}, nil
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
	if executable == nil || executable.file == nil {
		return nil
	}
	err := executable.file.Close()
	executable.file = nil
	return err
}
