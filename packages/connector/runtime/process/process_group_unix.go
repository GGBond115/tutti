//go:build !windows

package process

import (
	"errors"
	"os/exec"
	"syscall"
)

func prepareProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessGroup(command *exec.Cmd) error {
	return signalProcessGroup(command, syscall.SIGTERM)
}

func killProcessGroup(command *exec.Cmd) error {
	return signalProcessGroup(command, syscall.SIGKILL)
}

func signalProcessGroup(command *exec.Cmd, signal syscall.Signal) error {
	if command == nil || command.Process == nil {
		return nil
	}
	if err := syscall.Kill(-command.Process.Pid, signal); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return command.Process.Signal(signal)
	}
	return nil
}
