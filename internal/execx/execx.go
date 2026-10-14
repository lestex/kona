// Package execx runs external commands behind an interface so callers can
// be tested without the real tools.
package execx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs a command and returns its stdout.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Error is returned when a command exits non-zero. It keeps stderr so
// callers can surface the tool's own message.
type Error struct {
	Cmd    string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		return fmt.Sprintf("%s: %v", e.Cmd, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Cmd, msg)
}

func (e *Error) Unwrap() error { return e.Err }

// OS runs commands on the host.
type OS struct{}

// Run implements Runner.
func (OS) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), &Error{Cmd: name + " " + strings.Join(args, " "), Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}
