package krunkit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeTool writes a shell script standing in for krunkit or vmnet-helper.
func fakeTool(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func testNode(t *testing.T) Node {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "disk.raw"), nil, 0o600)
	os.WriteFile(filepath.Join(d, "seed.iso"), nil, 0o600)
	return Node{Name: "gpu-1", Dir: d, RunDir: t.TempDir(), MAC: "52:54:00:00:00:01", CPUs: 2, MemoryMiB: 1024,
		Gateway: "192.168.71.1", SubnetEnd: "192.168.71.254", Netmask: "255.255.255.0"}
}

func TestHelperTimeoutKillsHungHelper(t *testing.T) {
	// A helper that never prints its JSON and ignores SIGTERM.
	r := &Runtime{Krunkit: "true", VmnetHelper: fakeTool(t, "vmnet-helper", "trap '' TERM; exec sleep 30"),
		HelperTimeout: 300 * time.Millisecond}
	n := testNode(t)
	err := r.Start(context.Background(), n)
	if err == nil || !strings.Contains(err.Error(), "did not start") {
		t.Fatalf("want helper timeout, got %v", err)
	}
	pid, _ := readPid(filepath.Join(n.Dir, "helper.pid"))
	if !waitGone(pid, 2*time.Second) {
		t.Fatalf("hung helper %d still alive", pid)
	}
}

func TestStartStopLifecycle(t *testing.T) {
	helper := fakeTool(t, "vmnet-helper", `echo '{"vmnet_mtu":1500}'; exec sleep 30`)
	// Record args so the command line can be checked; stay up like a VM.
	argsFile := filepath.Join(t.TempDir(), "args")
	// No exec: the process must keep "krunkit" in its command line.
	vm := fakeTool(t, "krunkit", `echo "$@" > `+argsFile+`; sleep 30`)
	r := &Runtime{Krunkit: vm, VmnetHelper: helper, HelperTimeout: 5 * time.Second, StopGrace: 300 * time.Millisecond}
	n := testNode(t)
	if err := r.Start(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	args, _ := os.ReadFile(argsFile)
	for _, want := range []string{"--cpus 2 --memory 1024", "virtio-blk,path=" + n.Disk() + ",format=raw",
		"virtio-net,type=unixgram,path=" + n.sock() + ",mac=52:54:00:00:00:01", "--restful-uri unix://" + n.api()} {
		if !strings.Contains(string(args), want) {
			t.Errorf("krunkit args missing %q: %s", want, args)
		}
	}
	kpid, _ := readPid(filepath.Join(n.Dir, "krunkit.pid"))
	hpid, _ := readPid(filepath.Join(n.Dir, "helper.pid"))
	if err := r.Stop(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	if !waitGone(kpid, 2*time.Second) || !waitGone(hpid, 2*time.Second) {
		t.Fatal("processes survived Stop")
	}
	if _, err := os.Stat(filepath.Join(n.Dir, "krunkit.pid")); !os.IsNotExist(err) {
		t.Fatal("pidfile left behind")
	}
}

func TestCloneDisk(t *testing.T) {
	src := filepath.Join(t.TempDir(), "base.raw")
	os.WriteFile(src, []byte("disk"), 0o644)
	dst := filepath.Join(t.TempDir(), "n", "disk.raw")
	if err := CloneDisk(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "disk" {
		t.Fatalf("clone content %q", b)
	}
}

func TestUUIDFormat(t *testing.T) {
	u := uuid()
	if len(u) != 36 || u[14] != '4' {
		t.Fatalf("uuid %s", u)
	}
}
