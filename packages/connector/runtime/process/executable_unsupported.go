//go:build !darwin && !linux && !windows

package process

import (
	"errors"
	"os"
)

type preparedExecutable struct {
	path string
	file *os.File
}

func prepareExecutable(path string, expected *ExecutableIdentity) (preparedExecutable, error) {
	if expected != nil {
		return preparedExecutable{}, errors.New("verified descriptor process start is unavailable on this platform")
	}
	return preparedExecutable{path: path}, nil
}

func (executable *preparedExecutable) Close() error { return nil }
