// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

type mockNode struct {
	hosts map[pid.HostID]relay.Receiver
	mu    sync.RWMutex
}

func newMockNode() *mockNode {
	return &mockNode{
		hosts: make(map[pid.HostID]relay.Receiver),
	}
}

func (n *mockNode) ID() pid.NodeID { return "test-node" }

func (n *mockNode) GetHost(id pid.HostID) (relay.Receiver, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	h, ok := n.hosts[id]
	return h, ok
}

func (n *mockNode) RegisterHost(id pid.HostID, host relay.Receiver) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.hosts[id] = host
	return nil
}

func (n *mockNode) UnregisterHost(id pid.HostID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.hosts, id)
}

func (n *mockNode) Send(_ *relay.Package) error { return nil }

func (n *mockNode) Attach(_ pid.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return func() {}, nil
}

func (n *mockNode) Detach(_ pid.PID) {}

type mockHost struct {
	runErr             error
	terminateErr       error
	sendErr            error
	returnPID          pid.PID
	mu                 sync.Mutex
	runCalled          bool
	terminateCalled    bool
	sendCalled         bool
	acceptsAttachments bool
}

func (h *mockHost) AcceptsFrameAttachments() bool { return h.acceptsAttachments }

func (h *mockHost) Run(_ context.Context, start *process.Start) (pid.PID, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.runCalled = true
	if h.runErr != nil {
		return pid.PID{}, h.runErr
	}
	if h.returnPID.UniqID != "" {
		return h.returnPID, nil
	}
	return pid.PID{
		Host:   start.HostID,
		UniqID: "proc-1",
	}, nil
}

func (h *mockHost) Terminate(_ context.Context, _ pid.PID) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.terminateCalled = true
	return h.terminateErr
}

func (h *mockHost) Send(_ *relay.Package) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendCalled = true
	return h.sendErr
}

type nonProcessHost struct{}

func (h *nonProcessHost) Send(_ *relay.Package) error { return nil }

func TestManager_Start(t *testing.T) {
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	procID, err := mgr.Start(context.Background(), &process.Start{
		HostID: "test-host",
		Source: registry.NewID("test", "source"),
	})

	require.NoError(t, err)
	assert.Equal(t, "test-host", procID.Host)
	assert.Equal(t, "proc-1", procID.UniqID)
	assert.True(t, host.runCalled)
}

func TestManagerStartRollsBackResolverAttachmentsWhenHostRejectsStart(t *testing.T) {
	node := newMockNode()
	host := &mockHost{runErr: errors.New("host rejected start"), acceptsAttachments: true}
	_ = node.RegisterHost("test-host", host)
	closed := 0
	attachment := &testManagerAttachment{closed: &closed}
	resolver := ctxapi.NewFrameResolvers()
	require.NoError(t, resolver.Register("attachment", 1, func(context.Context, attrs.Attributes) ([]ctxapi.Pair, error) {
		return []ctxapi.Pair{{Key: &ctxapi.Key{Name: "attachment"}, Value: attachment}}, nil
	}))
	ctx := ctxapi.WithFrameResolvers(ctxapi.NewRootContext(), resolver)

	_, err := NewManager(node, zap.NewNop()).Start(ctx, &process.Start{
		HostID: "test-host", Source: registry.NewID("test", "source"),
	})
	require.Error(t, err)
	require.Equal(t, 1, closed)
}

func TestManagerStartPreservesCallerContextWhenResolutionFails(t *testing.T) {
	node := newMockNode()
	host := &mockHost{acceptsAttachments: true}
	_ = node.RegisterHost("test-host", host)
	sentinel := errors.New("resolution failed")
	resolver := ctxapi.NewFrameResolvers()
	require.NoError(t, resolver.Register("failure", 1, func(context.Context, attrs.Attributes) ([]ctxapi.Pair, error) {
		return nil, sentinel
	}))
	ctx := ctxapi.WithFrameResolvers(ctxapi.NewRootContext(), resolver)
	original := ctxapi.Pair{Key: &ctxapi.Key{Name: "caller"}, Value: "preserved"}
	start := &process.Start{
		HostID: "test-host", Source: registry.NewID("test", "source"),
		Context: []ctxapi.Pair{original},
	}

	_, err := NewManager(node, zap.NewNop()).Start(ctx, start)
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, []ctxapi.Pair{original}, start.Context)
	require.False(t, host.runCalled)
}

func TestManagerStartRejectsAttachmentsForNonFrameHost(t *testing.T) {
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("remote-host", host)
	closed := 0
	resolver := ctxapi.NewFrameResolvers()
	require.NoError(t, resolver.Register("attachment", 1, func(context.Context, attrs.Attributes) ([]ctxapi.Pair, error) {
		return []ctxapi.Pair{{Key: "attachment", Value: &testManagerAttachment{closed: &closed}}}, nil
	}))
	ctx := ctxapi.WithFrameResolvers(ctxapi.NewRootContext(), resolver)

	_, err := NewManager(node, zap.NewNop()).Start(ctx, &process.Start{
		HostID: "remote-host", Source: registry.NewID("test", "source"),
	})
	require.ErrorIs(t, err, ErrFrameAttachmentsUnsupported)
	require.Equal(t, 1, closed)
	require.False(t, host.runCalled)
}

type testManagerAttachment struct {
	closed *int
	once   sync.Once
}

func (a *testManagerAttachment) release() error {
	a.once.Do(func() { *a.closed++ })
	return nil
}
func (a *testManagerAttachment) Close() error    { return a.release() }
func (a *testManagerAttachment) Rollback() error { return a.release() }

func TestManager_Start_HostNotFound(t *testing.T) {
	node := newMockNode()
	mgr := NewManager(node, zap.NewNop())

	p, err := mgr.Start(context.Background(), &process.Start{
		HostID: "nonexistent",
		Source: registry.NewID("test", "source"),
	})

	require.Error(t, err)
	assert.Equal(t, pid.PID{}, p)
	assert.Contains(t, err.Error(), "host not found")
}

func TestManager_Start_InvalidHost(t *testing.T) {
	node := newMockNode()
	_ = node.RegisterHost("invalid-host", &nonProcessHost{})

	mgr := NewManager(node, zap.NewNop())

	p, err := mgr.Start(context.Background(), &process.Start{
		HostID: "invalid-host",
		Source: registry.NewID("test", "source"),
	})

	require.Error(t, err)
	assert.Equal(t, pid.PID{}, p)
	assert.Contains(t, err.Error(), "does not implement process.Host")
}

func TestManager_Start_HostError(t *testing.T) {
	node := newMockNode()
	host := &mockHost{runErr: errors.New("host run failed")}
	_ = node.RegisterHost("error-host", host)

	mgr := NewManager(node, zap.NewNop())

	p, err := mgr.Start(context.Background(), &process.Start{
		HostID: "error-host",
		Source: registry.NewID("test", "source"),
	})

	require.Error(t, err)
	assert.Equal(t, pid.PID{}, p)
	assert.Contains(t, err.Error(), "host run failed")
}

func TestManager_Cancel(t *testing.T) {
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	from := pid.PID{Host: "caller", UniqID: "caller-1"}
	p := pid.PID{Host: "test-host", UniqID: "proc-1"}
	reason := "test cancel"

	err := mgr.Cancel(context.Background(), from, p, reason)

	require.NoError(t, err)
	assert.True(t, host.sendCalled)
}

func TestManager_Cancel_HostNotFound(t *testing.T) {
	node := newMockNode()
	mgr := NewManager(node, zap.NewNop())

	from := pid.PID{Host: "caller", UniqID: "caller-1"}
	p := pid.PID{Host: "nonexistent", UniqID: "proc-1"}
	reason := "test cancel"

	err := mgr.Cancel(context.Background(), from, p, reason)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "host not found")
}

func TestManager_Cancel_SendError(t *testing.T) {
	node := newMockNode()
	host := &mockHost{sendErr: errors.New("send failed")}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	from := pid.PID{Host: "caller", UniqID: "caller-1"}
	p := pid.PID{Host: "test-host", UniqID: "proc-1"}
	reason := "test cancel"

	err := mgr.Cancel(context.Background(), from, p, reason)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "send failed")
}

func TestManager_Terminate(t *testing.T) {
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	p := pid.PID{Host: "test-host", UniqID: "proc-1"}
	err := mgr.Terminate(context.Background(), p)

	require.NoError(t, err)
	assert.True(t, host.terminateCalled)
}

func TestManager_Terminate_HostNotFound(t *testing.T) {
	node := newMockNode()
	mgr := NewManager(node, zap.NewNop())

	p := pid.PID{Host: "nonexistent", UniqID: "proc-1"}
	err := mgr.Terminate(context.Background(), p)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "host not found")
}

func TestManager_Terminate_InvalidHost(t *testing.T) {
	node := newMockNode()
	_ = node.RegisterHost("invalid-host", &nonProcessHost{})

	mgr := NewManager(node, zap.NewNop())

	p := pid.PID{Host: "invalid-host", UniqID: "proc-1"}
	err := mgr.Terminate(context.Background(), p)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not implement process.Host")
}

func TestManager_Terminate_HostError(t *testing.T) {
	node := newMockNode()
	host := &mockHost{terminateErr: errors.New("terminate failed")}
	_ = node.RegisterHost("error-host", host)

	mgr := NewManager(node, zap.NewNop())

	p := pid.PID{Host: "error-host", UniqID: "proc-1"}
	err := mgr.Terminate(context.Background(), p)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "terminate failed")
}

func TestManager_ConcurrentOperations(t *testing.T) {
	node := newMockNode()
	host := &mockHost{}
	_ = node.RegisterHost("test-host", host)

	mgr := NewManager(node, zap.NewNop())

	var wg sync.WaitGroup
	const numOps = 20

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = mgr.Start(context.Background(), &process.Start{
				HostID: "test-host",
				Source: registry.NewID("test", "source"),
			})
		}()
	}

	// Should complete without deadlock or panic
	assert.NotPanics(t, func() {
		wg.Wait()
	})
}
