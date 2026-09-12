// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// participantTCPProxy never terminates TLS or interprets naming traffic. It
// bounds concurrent sockets and fragments/delays encrypted bytes, and can cut
// established connections while refusing reconnects. It models a transport
// partition, not packet reordering (which TCP hides from applications).
type participantTCPProxy struct {
	ctx        context.Context
	cancel     context.CancelFunc
	listener   net.Listener
	target     string
	fragment   int
	delay      time.Duration
	mu         sync.Mutex
	blocked    bool
	stalled    chan struct{}
	sockets    map[net.Conn]struct{}
	acceptDone chan struct{}
	workers    sync.WaitGroup
}

func newParticipantTCPProxy(t *testing.T, ctx context.Context, target string, fragment int, delay time.Duration) *participantTCPProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(ctx)
	p := &participantTCPProxy{ctx: ctx, cancel: cancel, listener: listener, target: target, fragment: fragment, delay: delay, sockets: make(map[net.Conn]struct{}), acceptDone: make(chan struct{})}
	go func() {
		defer close(p.acceptDone)
		for {
			incoming, err := listener.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			if p.blocked || ctx.Err() != nil || len(p.sockets) >= 8 {
				p.mu.Unlock()
				_ = incoming.Close()
				continue
			}
			p.sockets[incoming] = struct{}{}
			p.workers.Add(1)
			p.mu.Unlock()
			go p.forward(incoming)
		}
	}()
	t.Cleanup(func() {
		p.cancel()
		_ = p.listener.Close()
		p.setBlocked(true)
		<-p.acceptDone
		p.workers.Wait()
	})
	return p
}

func (p *participantTCPProxy) port() int { return p.listener.Addr().(*net.TCPAddr).Port }
func (p *participantTCPProxy) setBlocked(blocked bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blocked = blocked
	if blocked {
		for socket := range p.sockets {
			_ = socket.Close()
		}
	}
}
func (p *participantTCPProxy) setStalled(stalled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if stalled && p.stalled == nil {
		p.stalled = make(chan struct{})
	}
	if !stalled && p.stalled != nil {
		close(p.stalled)
		p.stalled = nil
	}
}

func (p *participantTCPProxy) forward(incoming net.Conn) {
	defer p.workers.Done()
	defer func() { _ = incoming.Close(); p.mu.Lock(); delete(p.sockets, incoming); p.mu.Unlock() }()
	outgoing, err := (&net.Dialer{}).DialContext(p.ctx, "tcp", p.target)
	if err != nil {
		return
	}
	defer outgoing.Close()
	p.mu.Lock()
	if p.blocked || p.ctx.Err() != nil {
		p.mu.Unlock()
		return
	}
	p.sockets[outgoing] = struct{}{}
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.sockets, outgoing); p.mu.Unlock() }()
	copied := make(chan struct{})
	go func() { p.copy(outgoing, incoming); _ = outgoing.Close(); _ = incoming.Close(); close(copied) }()
	p.copy(incoming, outgoing)
	_ = outgoing.Close()
	_ = incoming.Close()
	<-copied
}
func (p *participantTCPProxy) copy(dst, src net.Conn) {
	buffer := make([]byte, p.fragment)
	for {
		n, err := src.Read(buffer)
		if n > 0 {
			p.mu.Lock()
			stalled := p.stalled
			p.mu.Unlock()
			if stalled != nil {
				select {
				case <-stalled:
				case <-p.ctx.Done():
					return
				}
			}

			if p.delay > 0 {
				timer := time.NewTimer(p.delay)
				select {
				case <-timer.C:
				case <-p.ctx.Done():
					timer.Stop()
					return
				}
			}
			written, writeErr := dst.Write(buffer[:n])
			if writeErr != nil || written != n {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
