package relay

import (
	"context"
	"net"
	"testing"
	"time"
)

// echo replies with "re:" + payload.
func echo(t *testing.T) *net.UDPAddr {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			c.WriteToUDP(append([]byte("re:"), buf[:n]...), from)
		}
	}()
	return c.LocalAddr().(*net.UDPAddr)
}

func freePort(t *testing.T) *net.UDPAddr {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	a := c.LocalAddr().(*net.UDPAddr)
	c.Close()
	return a
}

func TestRelayRoundTripPerClient(t *testing.T) {
	target := echo(t)
	listen := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &Relay{Listen: listen, Target: target}
	go r.Run(ctx)
	time.Sleep(100 * time.Millisecond)

	// Two clients (two GPU nodes) must each get their own replies.
	for _, msg := range []string{"gpu-1", "gpu-2"} {
		c, err := net.DialUDP("udp4", nil, listen)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := c.Write([]byte(msg)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		n, err := c.Read(buf)
		if err != nil || string(buf[:n]) != "re:"+msg {
			t.Fatalf("client %s got %q, %v", msg, buf[:n], err)
		}
	}
}
