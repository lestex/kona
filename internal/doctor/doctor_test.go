package doctor

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/lestex/kona/internal/execx"
)

func healthy() *execx.Fake {
	return &execx.Fake{Responses: map[string]execx.FakeResponse{
		"sw_vers -productVersion": {Out: []byte("26.6.2\n")},
		"container --version":     {Out: []byte("container CLI version 1.4.1 (build: release)\n")},
		"container system status": {Out: []byte("status              running\npaths.installRoot   /opt/homebrew/Cellar/container/1.5.0/\n")},
		"sh -c codesign -dv --verbose=2 \"$1\" 2>&1 sh /opt/homebrew/Cellar/container/1.5.0/libexec/container-plugins/container-runtime-linux/bin/container-runtime-linux": {
			Out: []byte("Identifier=com.apple.container.container-runtime-linux\nSignature=adhoc\nTeamIdentifier=not set\n")},
		"scutil --dns": {Out: []byte("resolver #1\n  nameserver[0] : 8.8.8.8\n")},
	}}
}

func newDoctor(f *execx.Fake) *Doctor {
	return &Doctor{Runner: f, GOOS: "darwin", GOARCH: "arm64",
		Stat: func(string) (os.FileInfo, error) { return nil, nil }}
}

func find(rs []Result, name string) Result {
	for _, r := range rs {
		if r.Name == name {
			return r
		}
	}
	return Result{}
}

func TestHealthyHost(t *testing.T) {
	rs := newDoctor(healthy()).Run(context.Background(), Options{Kernel: "/k"})
	if Failed(rs) {
		t.Fatalf("healthy host failed: %+v", rs)
	}
}

func TestMacOS15FailsWithReason(t *testing.T) {
	f := healthy()
	f.Responses["sw_vers -productVersion"] = execx.FakeResponse{Out: []byte("15.5\n")}
	r := find(newDoctor(f).Run(context.Background(), Options{}), "macos")
	if r.Status != Fail || !strings.Contains(r.Message, "macOS 26") {
		t.Fatalf("macos result = %+v", r)
	}
}

func TestMissingContainerAndKernel(t *testing.T) {
	f := healthy()
	f.Responses["container --version"] = execx.FakeResponse{Err: errors.New("not found")}
	d := newDoctor(f)
	d.Stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	rs := d.Run(context.Background(), Options{Kernel: "/missing"})
	if c := find(rs, "container"); c.Status != Fail || c.Fix != "brew install container && container system start" {
		t.Fatalf("container result = %+v", c)
	}
	if k := find(rs, "kernel"); k.Status != Fail || !strings.Contains(k.Fix, "kona kernel install") {
		t.Fatalf("kernel result = %+v", k)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"26.6.2", "26", 1}, {"15.5", "26", -1}, {"1.4.1", "1.4.1", 0}, {"1.10", "1.9", 1}}
	for _, c := range cases {
		if got := compare(c.a, c.b); got != c.want {
			t.Errorf("compare(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestContainerBuildSigning(t *testing.T) {
	f := healthy()
	if r := find(newDoctor(f).Run(context.Background(), Options{}), "container-build"); r.Status != OK ||
		!strings.Contains(r.Message, "ad-hoc signed (Homebrew build)") {
		t.Fatalf("homebrew: %+v", r)
	}
	f.Responses["container system status"] = execx.FakeResponse{Out: []byte("paths.installRoot   /usr/local/\n")}
	f.Responses["sh -c codesign -dv --verbose=2 \"$1\" 2>&1 sh /usr/local/libexec/container-plugins/container-runtime-linux/bin/container-runtime-linux"] =
		execx.FakeResponse{Out: []byte("Signature size=9000\nTeamIdentifier=UPBK2H6LZM\n")}
	if r := find(newDoctor(f).Run(context.Background(), Options{}), "container-build"); r.Message != "runtime signed by team UPBK2H6LZM" {
		t.Fatalf("pkg: %+v", r)
	}
}

const scutilIPv6First = `DNS configuration

resolver #1
  search domain[0] : attlocal.net
  nameserver[0] : 2600:1700:1964:80b0::1
  nameserver[1] : 192.168.1.254
  if_index : 14 (en0)

resolver #2
  domain   : local
  options  : mdns

resolver #3
  domain   : example.vpn
  nameserver[0] : 10.8.0.1

DNS configuration (for scoped queries)

resolver #1
  nameserver[0] : 172.16.0.53
`

func TestHostDNSPicksFirstIPv4(t *testing.T) {
	cases := map[string]string{
		// IPv6 first (this host on 2026-11-08): the IPv4 one is used, not
		// the per-domain VPN resolver or the scoped section.
		scutilIPv6First: "192.168.1.254",
		"resolver #1\n  nameserver[0] : 8.8.8.8\n": "8.8.8.8",
		// IPv6 only: nothing usable.
		"resolver #1\n  nameserver[0] : 2001:db8::53\n": "",
		// Only a per-domain resolver has IPv4.
		"resolver #1\n  nameserver[0] : 2001:db8::53\nresolver #2\n  domain : corp\n  nameserver[0] : 10.0.0.1\n": "",
	}
	for out, want := range cases {
		f := &execx.Fake{Responses: map[string]execx.FakeResponse{"scutil --dns": {Out: []byte(out)}}}
		if got, err := HostDNS(context.Background(), f); err != nil || got != want {
			t.Errorf("HostDNS = %q, %v; want %q for:\n%s", got, err, want, out)
		}
	}
}

func TestDNSWarnsWithoutIPv4(t *testing.T) {
	f := healthy()
	f.Responses["scutil --dns"] = execx.FakeResponse{Out: []byte("resolver #1\n  nameserver[0] : 2001:db8::53\n")}
	if r := find(newDoctor(f).Run(context.Background(), Options{}), "dns"); r.Status != Warn || !strings.Contains(r.Fix, "IPv4") {
		t.Fatalf("dns = %+v", r)
	}
}
