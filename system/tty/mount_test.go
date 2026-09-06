// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	ttyapi "github.com/wippyai/runtime/api/tty"
	relaysys "github.com/wippyai/runtime/system/relay"
	securitysys "github.com/wippyai/runtime/system/security"
)

type ttyTestPolicy map[string]bool

func (ttyTestPolicy) ID() registry.ID { return registry.NewID("test", "tty") }
func (p ttyTestPolicy) Evaluate(_ security.Actor, action, _ string, _ attrs.Bag) security.Result {
	if p[action] {
		return security.Allow
	}
	return security.Deny
}
func allowTTY(ctx context.Context, t testing.TB, actions ...string) {
	t.Helper()
	p := ttyTestPolicy{}
	for _, action := range actions {
		p[action] = true
	}
	require.NoError(t, security.SetActor(ctx, security.Actor{ID: "test"}))
	require.NoError(t, security.SetScope(ctx, securitysys.NewScope([]security.Policy{p})))
}

func TestMountPermissionsRecipientAndRevocation(t *testing.T) {
	s := NewService()
	defer s.Close()
	ctx, frame, _ := processContextFor(t, s, "owner")
	defer frame.Close()
	agent, af, _ := processContextFor(t, s, "agent")
	defer af.Close()
	stranger, sf, _ := processContextFor(t, s, "stranger")
	defer sf.Close()
	allowTTY(stranger, t)
	v, err := s.Create(ctx, 80, 24)
	require.NoError(t, err)
	_, err = s.Attach(stranger, v.Handle())
	require.ErrorIs(t, err, ttyapi.ErrPermissionDenied, "a handle is not observe authority")
	target, _ := runtime.GetFramePID(agent)
	issuer := v.(ttyapi.MountableViewport)
	allowTTY(ctx, t, "tty.mount")
	_, err = issuer.Mount(ctx, target, ttyapi.MountRights{Observe: true})
	require.ErrorIs(t, err, ttyapi.ErrPermissionDenied)
	allowTTY(ctx, t, "tty.mount", ttyapi.RightObserve)
	ref, err := issuer.Mount(ctx, target, ttyapi.MountRights{Observe: true})
	require.NoError(t, err)
	_, err = s.Attach(stranger, ref)
	require.ErrorIs(t, err, ttyapi.ErrPermissionDenied)
	mounted, err := s.Attach(agent, ref)
	require.NoError(t, err)
	require.NoError(t, mounted.(ttyapi.CheckedViewport).Check(agent, ttyapi.RightObserve))
	require.ErrorIs(t, mounted.(ttyapi.CheckedViewport).Check(stranger, ttyapi.RightObserve), ttyapi.ErrPermissionDenied)
	require.ErrorIs(t, mounted.Send(ttyapi.Event{Type: "key", Key: "x"}), ttyapi.ErrPermissionDenied)
	require.ErrorIs(t, mounted.Resize(40, 12), ttyapi.ErrPermissionDenied)
	_, err = s.Attach(agent, ref)
	require.ErrorIs(t, err, ttyapi.ErrInvalidGrant)
	require.NoError(t, issuer.Revoke(ctx, ref))
	require.ErrorIs(t, mounted.(ttyapi.CheckedViewport).Check(agent, ttyapi.RightObserve), ttyapi.ErrMountExpired)
	require.Zero(t, mounted.Snapshot().Width)
	_, open := <-mounted.Updates()
	require.False(t, open)
}

type testSurfaceNetwork struct {
	nodes map[string]*testSurfaceTransport
	mu    sync.Mutex
}
type testSurfaceTransport struct {
	network   *testSurfaceNetwork
	receiver  func(string, []byte)
	local     string
	sent      atomic.Int64
	duplicate atomic.Bool
	drop      atomic.Bool
}

func (n *testSurfaceNetwork) node(id string) *testSurfaceTransport {
	if n.nodes == nil {
		n.nodes = make(map[string]*testSurfaceTransport)
	}
	c := &testSurfaceTransport{network: n, local: id}
	n.nodes[id] = c
	return c
}
func (t *testSurfaceTransport) Receive(fn func(string, []byte)) error {
	t.network.mu.Lock()
	t.receiver = fn
	t.network.mu.Unlock()
	return nil
}
func (t *testSurfaceTransport) Send(peer string, b []byte) error {
	t.sent.Add(1)
	if t.drop.Load() {
		return nil
	}
	t.network.mu.Lock()
	remote := t.network.nodes[peer]
	var fn func(string, []byte)
	if remote != nil {
		fn = remote.receiver
	}
	t.network.mu.Unlock()
	if fn == nil {
		return ttyapi.ErrServiceUnavailable
	}
	fn(t.local, b)
	if t.duplicate.Load() {
		fn(t.local, b)
	}
	return nil
}
func meshFixture(t *testing.T) (*Service, *Service, *testSurfaceTransport, *testSurfaceTransport) {
	t.Helper()
	n := &testSurfaceNetwork{}
	a, b := NewService(), NewService()
	ta, tb := n.node("a"), n.node("b")
	require.NoError(t, a.SetMesh("a", ta))
	require.NoError(t, b.SetMesh("b", tb))
	t.Cleanup(func() { require.NoError(t, a.Close()); require.NoError(t, b.Close()) })
	return a, b, ta, tb
}
func meshContext(t *testing.T, s *Service, node, id string) (context.Context, *inbox) {
	t.Helper()
	ctx := ttyapi.WithService(ctxapi.NewRootContext(), s)
	relayNode := relaysys.NewNode(node)
	box := &inbox{packages: make(chan *relay.Package, 8)}
	require.NoError(t, relayNode.RegisterHost("workers", box))
	ctx = relay.WithNode(ctx, relayNode)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	require.NoError(t, frame.Set(runtime.FramePIDKey, pid.PID{Node: node, Host: "workers", UniqID: id}))
	allowTTY(ctx, t, "tty.mount", ttyapi.RightObserve, ttyapi.RightInput, ttyapi.RightResize)
	t.Cleanup(func() { require.NoError(t, frame.Close()) })
	return ctx, box
}
func TestMeshMountDuplexSnapshotsInputAndDuplicateFrames(t *testing.T) {
	a, b, ta, tb := meshFixture(t)
	owner, box := meshContext(t, a, "a", "owner")
	agent, _ := meshContext(t, b, "b", "agent")
	view, err := a.Create(owner, 80, 24)
	require.NoError(t, err)
	binding, err := a.Binding(view.Grant())
	require.NoError(t, err)
	port, err := binding.Resolve(owner)
	require.NoError(t, err)
	require.NoError(t, port.InputController().Start())
	surface, err := port.OpenSurface(ttyapi.SurfaceOptions{})
	require.NoError(t, err)
	_, err = surface.Present(ttyapi.Frame{Rows: []string{"ready"}, Cursor: &ttyapi.Cursor{Column: 3, Visible: true}})
	require.NoError(t, err)
	target, _ := runtime.GetFramePID(agent)
	ref, err := view.(ttyapi.MountableViewport).Mount(owner, target, ttyapi.MountRights{Observe: true, Input: true})
	require.NoError(t, err)
	remote, err := b.Attach(agent, ref)
	require.NoError(t, err)
	require.Equal(t, []string{"ready"}, remote.Snapshot().Rows)
	require.Equal(t, 3, remote.Snapshot().Cursor.Column)
	tb.duplicate.Store(true)
	ta.duplicate.Store(true)
	require.NoError(t, remote.(ttyapi.RemoteViewport).SendContext(agent, ttyapi.Event{Type: "paste", Paste: "echo hello"}))
	select {
	case pkg := <-box.packages:
		require.Equal(t, "echo hello", pkg.Messages[0].Payloads[0].Data().(*ttyapi.Event).Paste)
	case <-time.After(time.Second):
		t.Fatal("no input")
	}
	select {
	case <-box.packages:
		t.Fatal("replayed input executed twice")
	case <-time.After(20 * time.Millisecond):
	}
	require.ErrorIs(t, remote.(ttyapi.RemoteViewport).ResizeContext(agent, 40, 12), ttyapi.ErrPermissionDenied)
	require.ErrorIs(t, remote.(ttyapi.RemoteViewport).SendContext(agent, ttyapi.Event{Type: "resize", Width: 40, Height: 12}), ttyapi.ErrPermissionDenied)
	for range 1000 {
		_, err = surface.Present(ttyapi.Frame{Rows: []string{"updated"}})
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { s := remote.Snapshot(); return len(s.Rows) == 1 && s.Rows[0] == "updated" }, time.Second, time.Millisecond)
	require.Less(t, ta.sent.Load()+tb.sent.Load(), int64(100), "unchanged frames do not cross the mesh")
	require.NoError(t, view.(ttyapi.MountableViewport).Revoke(owner, ref))
	require.Eventually(t, func() bool { return remote.(ttyapi.CheckedViewport).Check(agent, ttyapi.RightObserve) != nil }, time.Second, time.Millisecond)
	require.Zero(t, remote.Snapshot().Width)
}
func TestMeshRejectsWrongPeerAndClaimedProcess(t *testing.T) {
	a, b, _, tb := meshFixture(t)
	owner, _ := meshContext(t, a, "a", "owner")
	agent, _ := meshContext(t, b, "b", "agent")
	stranger, _ := meshContext(t, b, "b", "stranger")
	view, err := a.Create(owner, 80, 24)
	require.NoError(t, err)
	target, _ := runtime.GetFramePID(agent)
	ref, err := view.(ttyapi.MountableViewport).Mount(owner, target, ttyapi.MountRights{Observe: true})
	require.NoError(t, err)
	_, err = b.Attach(stranger, ref)
	require.ErrorIs(t, err, ttyapi.ErrPermissionDenied)
	// Forge the recipient in a payload arriving from an unrelated authenticated
	// peer. It must not redeem or alter the legitimate mount's sequence state.
	f := wireFrame{Version: wireVersion, Op: opAttach, Ref: ref, Caller: target, ID: 12, Seq: 1}
	var data []byte
	require.NoError(t, codec.NewEncoderBytes(&data, &a.mesh.codec).Encode(f))
	a.mesh.receive("unrelated-peer", data)
	require.Eventually(t, func() bool { return len(a.mesh.requests) == 0 }, time.Second, time.Millisecond)
	tb.duplicate.Store(true)
	remote, err := b.Attach(agent, ref)
	require.NoError(t, err)
	require.Equal(t, 80, remote.Snapshot().Width)
	require.NoError(t, remote.Close())
}
func TestMeshCancellationAndCloseReleaseResources(t *testing.T) {
	a, b, _, tb := meshFixture(t)
	owner, _ := meshContext(t, a, "a", "owner")
	agent, _ := meshContext(t, b, "b", "agent")
	view, err := a.Create(owner, 80, 24)
	require.NoError(t, err)
	target, _ := runtime.GetFramePID(agent)
	ref, err := view.(ttyapi.MountableViewport).Mount(owner, target, ttyapi.MountRights{Observe: true})
	require.NoError(t, err)
	tb.drop.Store(true)
	ctx, cancel := context.WithTimeout(agent, 20*time.Millisecond)
	defer cancel()
	_, err = b.Attach(ctx, ref)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	b.mesh.mu.Lock()
	require.Empty(t, b.mesh.pending)
	require.Empty(t, b.mesh.views)
	b.mesh.mu.Unlock()
}
func TestInputOnlyMountCannotReadSnapshot(t *testing.T) {
	a, b, _, _ := meshFixture(t)
	owner, _ := meshContext(t, a, "a", "owner")
	agent, _ := meshContext(t, b, "b", "agent")
	view, err := a.Create(owner, 80, 24)
	require.NoError(t, err)
	target, _ := runtime.GetFramePID(agent)
	ref, err := view.(ttyapi.MountableViewport).Mount(owner, target, ttyapi.MountRights{Input: true})
	require.NoError(t, err)
	remote, err := b.Attach(agent, ref)
	require.NoError(t, err)
	require.Empty(t, remote.Snapshot().Rows)
	require.Zero(t, remote.Snapshot().Width)
	require.ErrorIs(t, remote.(ttyapi.CheckedViewport).Check(agent, ttyapi.RightObserve), ttyapi.ErrPermissionDenied)
	require.NoError(t, remote.Close())
}

func TestMeshMountLeaseExpiryClosesObservation(t *testing.T) {
	a, b, _, _ := meshFixture(t)
	owner, _ := meshContext(t, a, "a", "owner")
	agent, _ := meshContext(t, b, "b", "agent")
	view, err := a.Create(owner, 80, 24)
	require.NoError(t, err)
	target, _ := runtime.GetFramePID(agent)
	ref, err := view.(ttyapi.MountableViewport).Mount(owner, target, ttyapi.MountRights{Observe: true})
	require.NoError(t, err)
	remote, err := b.Attach(agent, ref)
	require.NoError(t, err)
	a.mu.Lock()
	record := a.mounts[ref]
	a.mu.Unlock()
	record.mu.Lock()
	record.timer.Reset(time.Millisecond)
	record.mu.Unlock()
	require.Eventually(t, func() bool { return remote.(ttyapi.CheckedViewport).Check(agent, ttyapi.RightObserve) != nil }, time.Second, time.Millisecond)
	require.Zero(t, remote.Snapshot().Width)
}
