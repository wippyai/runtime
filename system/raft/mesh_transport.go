// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	hraft "github.com/hashicorp/raft"
	"github.com/hashicorp/yamux"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/cluster/internode"
)

// meshAddr is the net.Addr implementation used by classConn. The host
// field is the peer's NodeID; net.Addr is opaque to hashicorp/raft's
// NetworkTransport so this is enough to drive the per-peer pool.
type meshAddr struct{ id cluster.NodeID }

func (a meshAddr) Network() string { return "mesh" }
func (a meshAddr) String() string  { return a.id }

// classConn is a byte-stream net.Conn that rides on top of a single
// peer's ClassRaftMesh internode frames. There's exactly one classConn
// per peer pair; it backs that pair's yamux session, which in turn
// multiplexes the per-stream net.Conns that hraft.NetworkTransport
// opens for its connection pool.
type classConn struct {
	connMgr       internode.ConnectionManager
	readDeadline  atomic.Pointer[time.Time]
	writeDeadline atomic.Pointer[time.Time]
	incoming      chan []byte
	closeCh       chan struct{}
	local         cluster.NodeID
	remote        cluster.NodeID
	readBuf       []byte
	closeOnce     sync.Once
	readBufMu     sync.Mutex
	closed        atomic.Bool
}

func newClassConn(local, remote cluster.NodeID, connMgr internode.ConnectionManager) *classConn {
	return &classConn{
		local:   local,
		remote:  remote,
		connMgr: connMgr,
		// Buffered so a small writer burst from the peer does not block
		// the manager's read loop. Bounded so partition surfaces as
		// backpressure (drop-oldest at the internode queue) rather than
		// unbounded growth.
		incoming: make(chan []byte, 256),
		closeCh:  make(chan struct{}),
	}
}

// injectInbound delivers one inbound ClassRaftMesh frame to this conn.
// Called by meshStreamLayer.onInbound for every frame received for the
// associated peer. Drops on full channel record a metric and return.
func (c *classConn) injectInbound(data []byte) {
	if c.closed.Load() {
		return
	}
	select {
	case c.incoming <- data:
	case <-c.closeCh:
	default:
		if c.connMgr != nil {
			c.connMgr.RecordDropReason("raft_mesh_inbound_full")
		}
	}
}

func (c *classConn) Read(p []byte) (int, error) {
	c.readBufMu.Lock()
	if len(c.readBuf) > 0 {
		n := copy(p, c.readBuf)
		c.readBuf = c.readBuf[n:]
		c.readBufMu.Unlock()
		return n, nil
	}
	c.readBufMu.Unlock()

	var timerCh <-chan time.Time
	if dl := c.readDeadline.Load(); dl != nil && !dl.IsZero() {
		d := time.Until(*dl)
		if d <= 0 {
			return 0, os.ErrDeadlineExceeded
		}
		t := time.NewTimer(d)
		defer t.Stop()
		timerCh = t.C
	}

	select {
	case data, ok := <-c.incoming:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, data)
		if n < len(data) {
			c.readBufMu.Lock()
			c.readBuf = append(c.readBuf, data[n:]...)
			c.readBufMu.Unlock()
		}
		return n, nil
	case <-timerCh:
		return 0, os.ErrDeadlineExceeded
	case <-c.closeCh:
		return 0, io.EOF
	}
}

func (c *classConn) Write(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	if dl := c.writeDeadline.Load(); dl != nil && !dl.IsZero() && time.Now().After(*dl) {
		return 0, os.ErrDeadlineExceeded
	}
	cp := make([]byte, len(p))
	copy(cp, p)
	if err := c.connMgr.SendToNode(c.remote, cp, internode.ClassRaftMesh); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *classConn) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.closeCh)
	})
	return nil
}

func (c *classConn) LocalAddr() net.Addr  { return meshAddr{id: c.local} }
func (c *classConn) RemoteAddr() net.Addr { return meshAddr{id: c.remote} }

func (c *classConn) SetDeadline(t time.Time) error {
	c.readDeadline.Store(&t)
	c.writeDeadline.Store(&t)
	return nil
}

func (c *classConn) SetReadDeadline(t time.Time) error {
	c.readDeadline.Store(&t)
	return nil
}

func (c *classConn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline.Store(&t)
	return nil
}

// meshStreamLayer implements hraft.StreamLayer over the wippy internode
// mesh. One yamux session per peer pair backs hraft's per-peer
// net.Conn pool: Dial opens a new yamux stream to the peer; Accept
// hands hraft inbound streams arriving on any peer session. There is
// no separate Raft listener — every frame multiplexes onto the same
// internode connection that already carries gossip and PG broadcast
// traffic.
type meshStreamLayer struct {
	connMgr   internode.ConnectionManager
	logger    *zap.Logger
	acceptCh  chan net.Conn
	closeCh   chan struct{}
	sessions  map[cluster.NodeID]*peerSession
	self      cluster.NodeID
	mu        sync.Mutex
	closeOnce sync.Once
}

type peerSession struct {
	conn    *classConn
	session *yamux.Session
}

// newMeshStreamLayer constructs the StreamLayer. The caller MUST invoke
// register() before any Raft instance touches the transport, because
// the inbound dispatcher pipes ClassRaftMesh frames into the per-peer
// classConns.
func newMeshStreamLayer(self cluster.NodeID, connMgr internode.ConnectionManager, logger *zap.Logger) *meshStreamLayer {
	return &meshStreamLayer{
		self:    self,
		connMgr: connMgr,
		logger:  logger,
		// AcceptBacklog matches yamux's default so a fast leader doesn't
		// stall its outbound RPC pool waiting for accepts to drain.
		acceptCh: make(chan net.Conn, 256),
		closeCh:  make(chan struct{}),
		sessions: make(map[cluster.NodeID]*peerSession),
	}
}

// register installs the ClassRaftMesh receiver on the connection
// manager. Returns an error if some other subsystem already claimed
// the class (would mean a misconfiguration).
func (l *meshStreamLayer) register() error {
	if !l.connMgr.RegisterClassReceiver(internode.ClassRaftMesh, l.onInbound) {
		return errors.New("raft mesh: ClassRaftMesh receiver already registered")
	}
	return nil
}

func (l *meshStreamLayer) onInbound(nodeID cluster.NodeID, data []byte) {
	sess := l.getOrCreateSession(nodeID)
	if sess == nil {
		return
	}
	sess.conn.injectInbound(data)
}

// getOrCreateSession returns the yamux session for `peer`, creating it
// (and its backing classConn) on first use. The tie-breaker uses the
// lexically smaller NodeID as the yamux Client so both ends agree on
// roles regardless of which side opened first.
func (l *meshStreamLayer) getOrCreateSession(peer cluster.NodeID) *peerSession {
	select {
	case <-l.closeCh:
		return nil
	default:
	}

	l.mu.Lock()
	if l.sessions == nil {
		l.mu.Unlock()
		return nil
	}
	if sess, ok := l.sessions[peer]; ok {
		l.mu.Unlock()
		return sess
	}
	l.mu.Unlock()

	conn := newClassConn(l.self, peer, l.connMgr)

	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	// Disable yamux keepalive: the internode layer already runs its own
	// heartbeat path. Two redundant keepalives just doubled the chance
	// of a spurious "no contact" close under partition.
	cfg.EnableKeepAlive = false

	var (
		ys  *yamux.Session
		err error
	)
	if l.self < peer {
		ys, err = yamux.Client(conn, cfg)
	} else {
		ys, err = yamux.Server(conn, cfg)
	}
	if err != nil {
		l.logger.Error("raft mesh: yamux session create failed",
			zap.String("peer", peer), zap.Error(err))
		_ = conn.Close()
		return nil
	}

	sess := &peerSession{conn: conn, session: ys}
	l.mu.Lock()
	if l.sessions == nil {
		l.mu.Unlock()
		_ = ys.Close()
		_ = conn.Close()
		return nil
	}
	if existing, ok := l.sessions[peer]; ok {
		l.mu.Unlock()
		_ = ys.Close()
		_ = conn.Close()
		return existing
	}
	l.sessions[peer] = sess
	l.mu.Unlock()

	go l.acceptLoop(peer, sess)
	return sess
}

// acceptLoop drains streams arriving on `sess` and pushes them into the
// global acceptCh. Returns when the session terminates.
func (l *meshStreamLayer) acceptLoop(peer cluster.NodeID, sess *peerSession) {
	for {
		stream, err := sess.session.AcceptStream()
		if err != nil {
			l.logger.Debug("raft mesh: session accept terminated",
				zap.String("peer", peer), zap.Error(err))
			return
		}
		select {
		case l.acceptCh <- stream:
		case <-l.closeCh:
			_ = stream.Close()
			return
		}
	}
}

// Accept yields the next inbound stream from any peer session. Blocks
// until a stream arrives or Close is called.
func (l *meshStreamLayer) Accept() (net.Conn, error) {
	select {
	case c, ok := <-l.acceptCh:
		if !ok {
			return nil, net.ErrClosed
		}
		return c, nil
	case <-l.closeCh:
		return nil, net.ErrClosed
	}
}

// Dial opens a new yamux stream to the peer identified by addr (treated
// as a NodeID — the mesh transport ignores host:port hints in
// ServerAddress).
func (l *meshStreamLayer) Dial(addr hraft.ServerAddress, timeout time.Duration) (net.Conn, error) {
	peer := cluster.NodeID(addr)
	sess := l.getOrCreateSession(peer)
	if sess == nil {
		return nil, fmt.Errorf("raft mesh: no session for peer %q", peer)
	}

	type dialResult struct {
		stream net.Conn
		err    error
	}
	resCh := make(chan dialResult, 1)
	go func() {
		stream, err := sess.session.OpenStream()
		resCh <- dialResult{stream: stream, err: err}
	}()

	var timerCh <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timerCh = t.C
	}

	select {
	case r := <-resCh:
		if r.err != nil {
			return nil, r.err
		}
		return r.stream, nil
	case <-timerCh:
		return nil, fmt.Errorf("raft mesh: dial timeout to %q after %s", peer, timeout)
	case <-l.closeCh:
		return nil, net.ErrClosed
	}
}

func (l *meshStreamLayer) Addr() net.Addr { return meshAddr{id: l.self} }

func (l *meshStreamLayer) Close() error {
	l.closeOnce.Do(func() {
		close(l.closeCh)
		l.mu.Lock()
		for _, s := range l.sessions {
			_ = s.session.Close()
			_ = s.conn.Close()
		}
		l.sessions = nil
		l.mu.Unlock()
	})
	return nil
}
