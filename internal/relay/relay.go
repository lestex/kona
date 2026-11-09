// Package relay is a host-side UDP relay that carries WireGuard between
// krunkit GPU nodes and `container` CPU nodes.
//
// Separate vmnet networks cannot reach each other, but the macOS host sits
// on both. GPU nodes send to <gpu gateway>:<port>; the relay forwards each
// client's datagrams to the CPU node's static IP from a dedicated upstream
// socket, so replies map back to the right client.
//
// kona used `container run -p` for this until container 1.5.0, whose
// forwarder failed with EHOSTUNREACH after the upgrade (docs/failure-modes.md).
// The relay runs as a detached child of kona, like vmnet-helper.
package relay

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// IdleTimeout drops a client session after no traffic in either direction.
// WireGuard keepalives (25 s) keep live sessions open.
const IdleTimeout = 3 * time.Minute

// Relay forwards UDP from Listen to Target.
type Relay struct {
	Listen *net.UDPAddr
	Target *net.UDPAddr
	// Logf reports session events (may be nil).
	Logf func(format string, args ...any)
}

type session struct {
	up   *net.UDPConn
	last time.Time
}

// Run serves until ctx is done.
func (r *Relay) Run(ctx context.Context) error {
	ln, err := net.ListenUDP("udp4", r.Listen)
	if err != nil {
		return err
	}
	defer ln.Close()
	go func() { <-ctx.Done(); ln.Close() }()

	var mu sync.Mutex
	sessions := map[string]*session{}
	go r.reap(ctx, &mu, sessions)

	buf := make([]byte, 65535)
	for {
		n, client, err := ln.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		key := client.String()
		mu.Lock()
		s := sessions[key]
		if s == nil {
			up, err := net.DialUDP("udp4", nil, r.Target)
			if err != nil {
				mu.Unlock()
				r.logf("dial %s: %v", r.Target, err)
				continue
			}
			s = &session{up: up}
			sessions[key] = s
			r.logf("session %s -> %s", key, r.Target)
			go r.back(ln, client, s, &mu)
		}
		s.last = time.Now()
		mu.Unlock()
		s.up.Write(buf[:n])
	}
}

// back copies replies from the target to the client until the upstream
// socket is closed.
func (r *Relay) back(ln *net.UDPConn, client *net.UDPAddr, s *session, mu *sync.Mutex) {
	buf := make([]byte, 65535)
	for {
		n, err := s.up.Read(buf)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				// ICMP unreachable while the CPU node restarts: keep going.
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return
		}
		mu.Lock()
		s.last = time.Now()
		mu.Unlock()
		ln.WriteToUDP(buf[:n], client)
	}
}

func (r *Relay) reap(ctx context.Context, mu *sync.Mutex, sessions map[string]*session) {
	t := time.NewTicker(IdleTimeout / 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			for _, s := range sessions {
				s.up.Close()
			}
			mu.Unlock()
			return
		case <-t.C:
			mu.Lock()
			for k, s := range sessions {
				if time.Since(s.last) > IdleTimeout {
					s.up.Close()
					delete(sessions, k)
					r.logf("session %s idle, closed", k)
				}
			}
			mu.Unlock()
		}
	}
}

func (r *Relay) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}
