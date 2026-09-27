// Package execx runs external commands behind an interface so callers can
// be tested without the real tools.
package execx

import (
	"bytes"
	"context"
	"fmt"
	"os"
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

// WithEnv returns r with env added when r is an OS runner; other runners
// (fakes) are returned unchanged.
func WithEnv(r Runner, env ...string) Runner {
	if o, ok := r.(OS); ok {
		o.Env = append(append([]string{}, o.Env...), env...)
		return o
	}
	return r
}

// OS runs commands on the host.
type OS struct {
	// Env is appended to the inherited environment.
	Env []string
}

// Run implements Runner.
func (o OS) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	if len(o.Env) > 0 {
		cmd.Env = append(os.Environ(), o.Env...)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), &Error{Cmd: name + " " + strings.Join(args, " "), Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}
