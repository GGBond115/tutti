//go:build windows

package process

import (
	"errors"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

func prepareProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func terminateProcessGroup(command *exec.Cmd) error {
	return terminateWindowsProcessTree(command, false)
}

func killProcessGroup(command *exec.Cmd) error {
	return terminateWindowsProcessTree(command, true)
}

func terminateWindowsProcessTree(command *exec.Cmd, force bool) error {
	if command == nil || command.Process == nil {
		return nil
	}
	arguments := []string{"/PID", strconv.Itoa(command.Process.Pid), "/T"}
	if force {
		arguments = append(arguments, "/F")
	}
	err := exec.Command("taskkill.exe", arguments...).Run()
	if err == nil {
		return nil
	}
	if killErr := command.Process.Kill(); killErr != nil {
		return errors.Join(err, killErr)
	}
	return nil
}
