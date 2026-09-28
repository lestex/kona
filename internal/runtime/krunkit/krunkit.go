// Package krunkit runs GPU node VMs with krunkit (libkrun + Venus) on a
// vmnet-helper network. Each node gets its own vmnet-helper; helpers with
// the same subnet join the same host bridge, so GPU nodes are L2 neighbours.
//
// Processes are started in their own session with stdio redirected to files
// so they outlive kona. Unix sockets live under /tmp/kona-<uid>/<cluster>/
// because socket paths are limited to 103 bytes.
package krunkit

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Node describes one krunkit VM.
type Node struct {
	Name string
	// Dir holds disk.raw, seed.iso, logs and pidfiles.
	Dir string
	// RunDir holds the node's unix sockets (short path).
	RunDir    string
	MAC       string
	CPUs      int
	MemoryMiB int64
	// Gateway, SubnetEnd and Netmask configure the shared vmnet network.
	Gateway   string
	SubnetEnd string
	Netmask   string
}

func (n Node) path(f string) string { return filepath.Join(n.Dir, f) }
func (n Node) sock() string         { return filepath.Join(n.RunDir, n.Name+".sock") }
func (n Node) api() string          { return filepath.Join(n.RunDir, n.Name+".api") }

// Disk and seed paths.
func (n Node) Disk() string    { return n.path("disk.raw") }
func (n Node) Seed() string    { return n.path("seed.iso") }
func (n Node) Console() string { return n.path("console.log") }

// Runtime holds tool paths.
type Runtime struct {
	Krunkit     string
	VmnetHelper string
	// HelperTimeout bounds how long vmnet-helper may take to start; a helper
	// asking for a colliding subnet can hang forever and ignores SIGTERM.
	HelperTimeout time.Duration
	// StopGrace is how long a VM gets to shut down after the API stop.
	StopGrace time.Duration
}

// New returns a Runtime using krunkit from PATH and the given vmnet-helper.
func New(vmnetHelper string) *Runtime {
	return &Runtime{Krunkit: "krunkit", VmnetHelper: vmnetHelper, HelperTimeout: 10 * time.Second,
		StopGrace: 15 * time.Second}
}

// CloneDisk makes dst an APFS clone of src (instant, copy-on-write),
// falling back to a regular copy on other filesystems.
func CloneDisk(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := unix.Clonefile(src, dst, 0); err == nil {
		return nil
	} else if !errors.Is(err, unix.ENOTSUP) && !errors.Is(err, unix.EXDEV) {
		return fmt.Errorf("clone %s: %w", src, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// MakeSeed writes a cloud-init NoCloud ISO (volume label "cidata") from
// files (name -> content) using macOS hdiutil.
func MakeSeed(ctx context.Context, iso string, files map[string]string) error {
	dir, err := os.MkdirTemp(filepath.Dir(iso), ".seed-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return err
		}
	}
	os.Remove(iso)
	out, err := exec.CommandContext(ctx, "hdiutil", "makehybrid", "-quiet", "-iso", "-joliet",
		"-default-volume-name", "cidata", "-o", iso, dir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("hdiutil makehybrid: %v: %s", err, out)
	}
	return os.Chmod(iso, 0o600)
}

// Start boots node n. The disk and seed must already exist.
func (r *Runtime) Start(ctx context.Context, n Node) error {
	if r.Running(n) {
		return nil
	}
	if err := os.MkdirAll(n.RunDir, 0o700); err != nil {
		return err
	}
	_ = os.Remove(n.sock())
	_ = os.Remove(n.api())
	r.killPidfile(n.path("helper.pid"))

	// A fresh interface ID every start: reusing one held by a hung helper
	// wedges the new helper too.
	helper, err := spawn(n.path("helper.json"), n.path("helper.log"), r.VmnetHelper,
		"--socket", n.sock(), "--interface-id", uuid(), "--operation-mode", "shared",
		"--start-address", n.Gateway, "--end-address", n.SubnetEnd, "--subnet-mask", n.Netmask)
	if err != nil {
		return fmt.Errorf("start vmnet-helper: %w", err)
	}
	if err := writePid(n.path("helper.pid"), helper); err != nil {
		return err
	}
	deadline := time.Now().Add(r.HelperTimeout)
	for {
		if fi, err := os.Stat(n.path("helper.json")); err == nil && fi.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			syscall.Kill(helper, syscall.SIGKILL)
			log, _ := os.ReadFile(n.path("helper.log"))
			return fmt.Errorf("vmnet-helper did not start within %s (subnet %s/%s in use by another vmnet network?): %s",
				r.HelperTimeout, n.Gateway, n.Netmask, strings.TrimSpace(string(log)))
		}
		time.Sleep(100 * time.Millisecond)
	}

	vm, err := spawn(n.path("krunkit.out"), n.path("krunkit.out"), r.Krunkit,
		"--cpus", strconv.Itoa(n.CPUs), "--memory", strconv.FormatInt(n.MemoryMiB, 10),
		"--device", "virtio-blk,path="+n.Disk()+",format=raw",
		"--device", "virtio-blk,path="+n.Seed()+",format=raw",
		"--device", "virtio-net,type=unixgram,path="+n.sock()+",mac="+n.MAC,
		"--device", "virtio-serial,logFilePath="+n.Console(),
		"--restful-uri", "unix://"+n.api(),
		// libkrun logs why the VMM stopped (guest shutdown vs error).
		"--log-file", n.path("krunkit.log"), "--krun-log-level", "3",
	)
	if err != nil {
		syscall.Kill(helper, syscall.SIGKILL)
		return fmt.Errorf("start krunkit: %w", err)
	}
	return writePid(n.path("krunkit.pid"), vm)
}

// Running reports whether node n's krunkit process is alive.
func (r *Runtime) Running(n Node) bool {
	pid, ok := readPid(n.path("krunkit.pid"))
	return ok && alive(pid, "krunkit")
}

// Stop shuts node n down: an ACPI-style stop through the REST API first,
// then SIGTERM/SIGKILL, then its vmnet-helper. Stopped nodes are not an error.
func (r *Runtime) Stop(ctx context.Context, n Node) error {
	if pid, ok := readPid(n.path("krunkit.pid")); ok && alive(pid, "krunkit") {
		stopViaAPI(ctx, n.api())
		if !waitGone(pid, r.StopGrace) {
			syscall.Kill(pid, syscall.SIGTERM)
			if !waitGone(pid, 5*time.Second) {
				syscall.Kill(pid, syscall.SIGKILL)
				waitGone(pid, 5*time.Second)
			}
		}
	}
	os.Remove(n.path("krunkit.pid"))
	r.killPidfile(n.path("helper.pid"))
	os.Remove(n.sock())
	os.Remove(n.api())
	return nil
}

// Delete stops node n and removes its directory (disk included).
func (r *Runtime) Delete(ctx context.Context, n Node) error {
	if err := r.Stop(ctx, n); err != nil {
		return err
	}
	return os.RemoveAll(n.Dir)
}

// KillMatching SIGKILLs every process whose command line contains substr
// (krunkit and vmnet-helper of a cluster whose state is gone). It returns
// the pids it killed.
func KillMatching(substr string) []int {
	out, _ := exec.Command("pgrep", "-f", regexp.QuoteMeta(substr)).Output()
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(f)
		if err != nil || pid == os.Getpid() {
			continue
		}
		if syscall.Kill(pid, syscall.SIGKILL) == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

func (r *Runtime) killPidfile(path string) {
	pid, ok := readPid(path)
	if ok && alive(pid, "") {
		syscall.Kill(pid, syscall.SIGTERM)
		if !waitGone(pid, 2*time.Second) {
			syscall.Kill(pid, syscall.SIGKILL) // hung helpers ignore SIGTERM
			waitGone(pid, 2*time.Second)
		}
	}
	os.Remove(path)
}

func stopViaAPI(ctx context.Context, sock string) {
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://krunkit/vm/state",
		strings.NewReader(`{"state":"Stop"}`))
	if err != nil {
		return
	}
	if resp, err := c.Do(req); err == nil {
		resp.Body.Close()
	}
}

// spawn starts a detached process (own session, stdin from /dev/null).
func spawn(stdout, stderr, name string, args ...string) (int, error) {
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

func writePid(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

func readPid(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil && pid > 0
}

// alive reports whether pid exists and, if comm is set, runs that program
// (guards against pid reuse after a reboot).
func alive(pid int, comm string) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=,command=").Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(string(out))
	// A killed process that nobody reaped yet (zombie) is gone for us.
	if len(fields) == 0 || strings.HasPrefix(fields[0], "Z") {
		return false
	}
	return comm == "" || strings.Contains(string(out), comm)
}

func waitGone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !alive(pid, "") {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !alive(pid, "")
}

func uuid() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
