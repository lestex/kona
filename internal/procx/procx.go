// Package procx starts and tracks detached helper processes (krunkit,
// vmnet-helper, the WireGuard relay) through pidfiles.
package procx

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Spawn starts a detached process: its own session, stdin from /dev/null,
// stdout/stderr to files, so it outlives kona.
func Spawn(stdout, stderr, name string, args ...string) (int, error) {
	out, err := os.OpenFile(stdout, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	errf := out
	if stderr != stdout {
		if errf, err = os.OpenFile(stderr, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err != nil {
			return 0, err
		}
		defer errf.Close()
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = out, errf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	return pid, cmd.Process.Release()
}

// WritePid records pid in path.
func WritePid(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

// ReadPid reads a pidfile.
func ReadPid(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil && pid > 0
}

// Alive reports whether pid exists and, if match is set, its command line
// contains match (guards against pid reuse after a reboot). Zombies count
// as gone.
func Alive(pid int, match string) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=,command=").Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || strings.HasPrefix(fields[0], "Z") {
		return false
	}
	return match == "" || strings.Contains(string(out), match)
}

// WaitGone waits up to d for pid to exit.
func WaitGone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !Alive(pid, "") {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !Alive(pid, "")
}

// KillPidfile stops the process in a pidfile (SIGTERM, then SIGKILL) and
// removes the pidfile.
func KillPidfile(path string) {
	if pid, ok := ReadPid(path); ok && Alive(pid, "") {
		syscall.Kill(pid, syscall.SIGTERM)
		if !WaitGone(pid, 2*time.Second) {
			syscall.Kill(pid, syscall.SIGKILL)
			WaitGone(pid, 2*time.Second)
		}
	}
	os.Remove(path)
}
