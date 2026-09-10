// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

const (
	wireVersion    = 1
	maxWireBytes   = 512 << 10
	maxMeshPending = 128
	meshTimeout    = 5 * time.Second
)

const (
	opAttach uint8 = iota + 1
	opSnapshot
	opInput
	opResize
	opPing
	opRelease
	opNotify
	opClosed
	opReply
)

// Sequence numbers are per attachment, not per TCP connection. Internode can
// replay a partially flushed batch after reconnect; input must execute once.
type wireFrame struct {
	Caller   pid.PID
	Ref      string
	Error    string
	Snapshot ttyapi.Snapshot
	Event    ttyapi.Event
	ID       uint64
	Seq      uint64
	Width    int
	Height   int
	Rights   ttyapi.MountRights
	Version  uint8
	Op       uint8
}
type incomingFrame struct {
	peer  string
	frame wireFrame
}
type pendingFrame struct {
	reply chan wireFrame
	peer  string
	ref   string
}

type meshService struct {
	transport ttyapi.MeshTransport
	views     map[string]*remoteViewport
	service   *Service
	requests  chan incomingFrame
	done      chan struct{}
	pending   map[uint64]pendingFrame
	local     string
	codec     codec.MsgpackHandle
	wg        sync.WaitGroup
	next      atomic.Uint64
	once      sync.Once
	mu        sync.Mutex
	closed    bool
}

// SetMesh installs the optional transport before the broker is exposed to
// processes. Local-only boot retains the zero-goroutine service.
func (s *Service) SetMesh(local string, transport ttyapi.MeshTransport) error {
	if local == "" || transport == nil {
		return ttyapi.ErrServiceUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.mesh != nil {
		return ttyapi.ErrServiceUnavailable
	}
	m := &meshService{service: s, transport: transport, local: local, requests: make(chan incomingFrame, 128), done: make(chan struct{}), pending: make(map[uint64]pendingFrame), views: make(map[string]*remoteViewport)}
	m.codec.WriteExt = true
	m.codec.MaxInitLen = 1024
	if err := transport.Receive(m.receive); err != nil {
		return err
	}
	s.mesh = m
	for range 4 {
		m.wg.Add(1)
		go m.worker()
	}
	return nil
}
func (m *meshService) send(peer string, f wireFrame) error {
	f.Version = wireVersion
	var b bytes.Buffer
	if err := codec.NewEncoder(&b, &m.codec).Encode(f); err != nil {
		return err
	}
	if b.Len() > maxWireBytes {
		return ttyapi.ErrMeshProtocol
	}
	return m.transport.Send(peer, b.Bytes())
}
func (m *meshService) receive(peer string, data []byte) {
	if len(data) == 0 || len(data) > maxWireBytes {
		return
	}
	select {
	case <-m.done:
		return
	default:
	}
	var f wireFrame
	if err := codec.NewDecoderBytes(data, &m.codec).Decode(&f); err != nil || f.Version != wireVersion || len(f.Ref) > 512 {
		return
	}
	switch f.Op {
	case opReply:
		m.mu.Lock()
		p, ok := m.pending[f.ID]
		m.mu.Unlock()
		if ok && p.peer == peer && p.ref == f.Ref {
			select {
			case p.reply <- f:
			default:
			}
		}
	case opNotify, opClosed:
		m.mu.Lock()
		v := m.views[f.Ref]
		m.mu.Unlock()
		if v == nil || v.peer != peer {
			return
		}
		if f.Op == opClosed {
			v.finish(false)
			return
		}
		select {
		case v.dirty <- struct{}{}:
		default:
		}
	default:
		select {
		case m.requests <- incomingFrame{peer: peer, frame: f}:
		case <-m.done:
		default:
			// Bounded admission. A caller times out and closes its attachment; the
			// connection reader never waits for a slow actor or a terminal consumer.
		}
	}
}
func (m *meshService) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.done:
			return
		case request := <-m.requests:
			m.handle(request.peer, request.frame)
		}
	}
}
func wireError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ttyapi.ErrPermissionDenied):
		return "permission"
	case errors.Is(err, ttyapi.ErrInputInactive):
		return "inactive"
	case errors.Is(err, ttyapi.ErrInvalidViewportSize):
		return "size"
	default:
		return "closed"
	}
}
func fromWireError(code string) error {
	switch code {
	case "":
		return nil
	case "permission":
		return ttyapi.ErrPermissionDenied
	case "inactive":
		return ttyapi.ErrInputInactive
	case "size":
		return ttyapi.ErrInvalidViewportSize
	default:
		return ttyapi.ErrMountExpired
	}
}
func (m *meshService) handle(peer string, f wireFrame) {
	reply := wireFrame{Op: opReply, Ref: f.Ref, ID: f.ID, Seq: f.Seq}
	s := m.service
	s.mu.Lock()
	record := s.mounts[f.Ref]
	s.mu.Unlock()
	if record == nil || peer != record.recipient.Node || !samePID(f.Caller, record.recipient) {
		reply.Error = "permission"
		_ = m.send(peer, reply)
		return
	}
	if f.Op == opRelease {
		s.removeMount(f.Ref)
		return
	}
	record.mu.Lock()
	select {
	case <-record.done:
		record.mu.Unlock()
		reply.Error = "closed"
		_ = m.send(peer, reply)
		return
	default:
	}
	if f.Seq == record.lastSeq && f.ID == record.lastReply.ID && record.lastSeq != 0 {
		reply = record.lastReply
		record.mu.Unlock()
		_ = m.send(peer, reply)
		return
	}
	if f.Seq != record.lastSeq+1 || (record.attached && f.Op == opAttach) || (!record.attached && f.Op != opAttach) {
		record.mu.Unlock()
		reply.Error = "closed"
		_ = m.send(peer, reply)
		return
	}
	var err error
	startPump := false
	switch f.Op {
	case opAttach:
		record.attached = true
		reply.Rights = record.rights
		if record.rights.Observe {
			reply.Snapshot = record.view.Snapshot()
			startPump = true
		}
	case opSnapshot:
		if !record.rights.Observe {
			err = ttyapi.ErrPermissionDenied
		} else {
			reply.Snapshot = record.view.Snapshot()
			select {
			case record.ack <- struct{}{}:
			default:
			}
		}
	case opInput:
		if len(f.Event.Paste) > 64<<10 || len(f.Event.Key) > 256 {
			err = ttyapi.ErrMeshProtocol
		} else {
			err = record.view.Send(f.Event)
		}
	case opResize:
		err = record.view.Resize(f.Width, f.Height)
	case opPing:
	default:
		err = ttyapi.ErrMeshProtocol
	}
	reply.Error = wireError(err)
	record.lastSeq = f.Seq
	record.lastReply = reply
	record.timer.Reset(mountLease)
	record.mu.Unlock()
	if err := m.send(peer, reply); err != nil {
		s.removeMount(f.Ref)
		return
	}
	if startPump {
		m.wg.Add(1)
		go m.watch(record)
	}
}
func (m *meshService) watch(record *mountRecord) {
	defer m.wg.Done()
	defer m.service.removeMount(record.ref)
	for {
		select {
		case <-m.done:
			return
		case <-record.done:
			return
		case _, ok := <-record.view.Updates():
			if !ok {
				return
			}
			if err := m.send(record.recipient.Node, wireFrame{Op: opNotify, Ref: record.ref}); err != nil {
				m.service.removeMount(record.ref)
				return
			}
			// One outstanding notification per mount. Intermediate presents coalesce
			// in the broker, so a slow observer never queues a history of screen frames.
			select {
			case <-record.ack:
			case <-record.done:
				return
			case <-m.done:
				return
			}
		}
	}
}
func (m *meshService) rpc(ctx context.Context, peer string, f wireFrame, closed <-chan struct{}) (wireFrame, error) {
	if err := ctx.Err(); err != nil {
		return wireFrame{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, meshTimeout)
	defer cancel()
	f.ID = m.next.Add(1)
	p := pendingFrame{peer: peer, ref: f.Ref, reply: make(chan wireFrame, 1)}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return wireFrame{}, ttyapi.ErrServiceUnavailable
	}
	if len(m.pending) >= maxMeshPending {
		m.mu.Unlock()
		return wireFrame{}, ttyapi.ErrMeshBusy
	}
	m.pending[f.ID] = p
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.pending, f.ID); m.mu.Unlock() }()
	if err := m.send(peer, f); err != nil {
		return wireFrame{}, err
	}
	select {
	case <-closed:
		return wireFrame{}, ttyapi.ErrMountExpired
	case reply := <-p.reply:
		return reply, fromWireError(reply.Error)
	case <-ctx.Done():
		return wireFrame{}, ctx.Err()
	case <-m.done:
		return wireFrame{}, ttyapi.ErrServiceUnavailable
	}
}
func (m *meshService) attach(ctx context.Context, ref string, owner pid.PID) (ttyapi.Viewport, error) {
	peer, err := mountNode(ref)
	if err != nil {
		return nil, err
	}
	if checker, ok := m.transport.(ttyapi.MeshPeerChecker); ok {
		if err := checker.CheckPeer(peer); err != nil {
			return nil, err
		}
	}
	// A remote node may attest only processes hosted on itself. Never forward
	// an arbitrary caller PID obtained from a Lua argument or a relay payload.
	if owner.Node != m.local || owner.Host == "" || owner.UniqID == "" {
		return nil, ttyapi.ErrPermissionDenied
	}
	v := &remoteViewport{callGate: make(chan struct{}, 1), mesh: m, peer: peer, ref: ref, owner: owner, dirty: make(chan struct{}, 1), updates: make(chan ttyapi.Update, 1), done: make(chan struct{})}
	m.mu.Lock()
	if m.closed || len(m.views) >= maxMounts {
		m.mu.Unlock()
		return nil, ttyapi.ErrMeshBusy
	}
	if m.views[ref] != nil {
		m.mu.Unlock()
		return nil, ttyapi.ErrInvalidGrant
	}
	m.views[ref] = v
	// Own the goroutine before releasing the lifecycle lock; Close can safely
	// Wait even when an attachment is still awaiting its first response.
	m.wg.Add(1)
	m.mu.Unlock()
	reply, err := v.call(ctx, wireFrame{Op: opAttach})
	if err != nil {
		v.finish(true)
		m.wg.Done()
		return nil, err
	}
	v.mu.Lock()
	select {
	case <-v.done:
		v.mu.Unlock()
		m.wg.Done()
		return nil, ttyapi.ErrMountExpired
	default:
	}
	v.rights = reply.Rights
	v.snapshot = reply.Snapshot
	v.mu.Unlock()
	go func() { defer m.wg.Done(); v.run() }()
	return v, nil
}
func (m *meshService) closeOwner(owner pid.PID) {
	m.mu.Lock()
	var views []*remoteViewport
	for _, v := range m.views {
		if samePID(v.owner, owner) {
			views = append(views, v)
		}
	}
	m.mu.Unlock()
	for _, v := range views {
		v.finish(true)
	}
}
func (m *meshService) close() {
	m.once.Do(func() {
		m.mu.Lock()
		m.closed = true
		close(m.done)
		views := make([]*remoteViewport, 0, len(m.views))
		for _, v := range m.views {
			views = append(views, v)
		}
		m.mu.Unlock()
		for _, v := range views {
			v.finish(true)
		}
		m.wg.Wait()
		_ = m.transport.Receive(nil)
	})
}

type remoteViewport struct {
	updates  chan ttyapi.Update
	callGate chan struct{}
	done     chan struct{}
	dirty    chan struct{}
	mesh     *meshService
	owner    pid.PID
	ref      string
	peer     string
	snapshot ttyapi.Snapshot
	seq      uint64
	once     sync.Once
	mu       sync.Mutex
	rights   ttyapi.MountRights
}

func (v *remoteViewport) Grant() string                 { return "" }
func (v *remoteViewport) Handle() string                { return v.ref }
func (v *remoteViewport) Updates() <-chan ttyapi.Update { return v.updates }
func (v *remoteViewport) Check(ctx context.Context, right string) error {
	owner, ok := runtime.GetFramePID(ctx)
	v.mu.Lock()
	allowed := right == "" || hasRight(v.rights, right)
	v.mu.Unlock()
	if !ok || !samePID(owner, v.owner) || !allowed {
		return ttyapi.ErrPermissionDenied
	}
	if right == "" {
		return nil
	}
	select {
	case <-v.done:
		return ttyapi.ErrMountExpired
	default:
		return nil
	}
}
func (v *remoteViewport) Snapshot() ttyapi.Snapshot {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.rights.Observe {
		return ttyapi.Snapshot{}
	}
	return v.snapshot
}
func (v *remoteViewport) call(ctx context.Context, f wireFrame) (wireFrame, error) {
	select {
	case v.callGate <- struct{}{}:
		defer func() { <-v.callGate }()
	case <-ctx.Done():
		return wireFrame{}, ctx.Err()
	case <-v.done:
		return wireFrame{}, ttyapi.ErrMountExpired
	case <-v.mesh.done:
		return wireFrame{}, ttyapi.ErrServiceUnavailable
	}
	if err := ctx.Err(); err != nil {
		return wireFrame{}, err
	}
	select {
	case <-v.done:
		return wireFrame{}, ttyapi.ErrMountExpired
	default:
	}
	v.seq++
	f.Seq = v.seq
	f.Ref = v.ref
	f.Caller = v.owner
	return v.mesh.rpc(ctx, v.peer, f, v.done)
}
func (v *remoteViewport) Send(e ttyapi.Event) error { return v.send(context.Background(), e) }
func (v *remoteViewport) SendContext(ctx context.Context, e ttyapi.Event) error {
	if err := v.Check(ctx, ttyapi.RightInput); err != nil {
		return err
	}
	return v.send(ctx, e)
}
func (v *remoteViewport) send(ctx context.Context, e ttyapi.Event) error {
	v.mu.Lock()
	allowed := v.rights.Input
	v.mu.Unlock()
	if !allowed {
		return ttyapi.ErrPermissionDenied
	}
	if e.Type == "resize" {
		return v.resize(ctx, e.Width, e.Height)
	}
	_, err := v.call(ctx, wireFrame{Op: opInput, Event: e})
	if err != nil && !errors.Is(err, ttyapi.ErrInputInactive) && !errors.Is(err, ttyapi.ErrPermissionDenied) {
		v.finish(true)
	}
	return err
}
func (v *remoteViewport) Resize(w, h int) error { return v.resize(context.Background(), w, h) }
func (v *remoteViewport) ResizeContext(ctx context.Context, w, h int) error {
	if err := v.Check(ctx, ttyapi.RightResize); err != nil {
		return err
	}
	return v.resize(ctx, w, h)
}
func (v *remoteViewport) resize(ctx context.Context, w, h int) error {
	v.mu.Lock()
	allowed := v.rights.Resize
	v.mu.Unlock()
	if !allowed {
		return ttyapi.ErrPermissionDenied
	}
	if err := ttyapi.ValidateViewportSize(w, h); err != nil {
		return err
	}
	_, err := v.call(ctx, wireFrame{Op: opResize, Width: w, Height: h})
	if err != nil {
		v.finish(true)
	}
	return err
}
func (v *remoteViewport) run() {
	ticker := time.NewTicker(mountLease / 3)
	defer ticker.Stop()
	for {
		select {
		case <-v.done:
			return
		case <-v.mesh.done:
			v.finish(false)
			return
		case <-ticker.C:
			if _, err := v.call(context.Background(), wireFrame{Op: opPing}); err != nil {
				v.finish(true)
				return
			}
		case <-v.dirty:
			reply, err := v.call(context.Background(), wireFrame{Op: opSnapshot})
			if err != nil {
				v.finish(true)
				return
			}
			v.mu.Lock()
			select {
			case <-v.done:
				v.mu.Unlock()
				return
			default:
			}
			if reply.Snapshot.Revision >= v.snapshot.Revision {
				v.snapshot = reply.Snapshot
				publishLatest(v.updates, ttyapi.Update{Revision: v.snapshot.Revision})
			}
			v.mu.Unlock()
		}
	}
}
func (v *remoteViewport) finish(release bool) {
	v.once.Do(func() {
		v.mu.Lock()
		close(v.done)
		close(v.updates)
		v.snapshot = ttyapi.Snapshot{}
		v.mu.Unlock()
		v.mesh.mu.Lock()
		if v.mesh.views[v.ref] == v {
			delete(v.mesh.views, v.ref)
		}
		v.mesh.mu.Unlock()
		if release {
			_ = v.mesh.send(v.peer, wireFrame{Op: opRelease, Ref: v.ref, Caller: v.owner})
		}
	})
}
func (v *remoteViewport) Close() error { v.finish(true); return nil }
