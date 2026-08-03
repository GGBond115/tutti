package workspace

import (
	"fmt"
	"io"
)

// TerminalProcessFactory is the platform boundary owned by TerminalService.
// Implementations must provide equivalent PTY semantics without exposing the
// underlying Unix PTY or Windows ConPTY library to the service.
type TerminalProcessFactory interface {
	Start(shell string, args []string, cwd string, env []string, cols int, rows int) (TerminalProcess, error)
}

type TerminalProcess interface {
	io.ReadWriteCloser
	FD() uintptr
	Kill() error
	PID() int
	Resize(cols int, rows int) error
	Wait() error
}

type terminalProcessExitError struct {
	code int
}

func (e terminalProcessExitError) Error() string {
	return fmt.Sprintf("terminal process exited with code %d", e.code)
}
