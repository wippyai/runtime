// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/relay"
	systemrelay "github.com/wippyai/runtime/system/relay"
	"go.uber.org/zap"
)

func TestContextQueueCancellationDoesNotEnqueueWork(t *testing.T) {
	states := setupStateManager()
	states.CreateNodeState("peer")
	state := states.GetNodeState("peer")
	state.queueMu.Lock()
	blockedCtx, cancelBlocked := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- states.QueueMessageClassContext(blockedCtx, "peer", []byte("busy"), ClassRaftRPC) }()
	require.Eventually(t, func() bool { return state.queueMu.waiting.Load() != nil }, time.Second, time.Millisecond)
	cancelBlocked()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("queue acquisition did not cancel")
	}
	state.queueMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := states.QueueMessageClassContext(ctx, "peer", []byte("canceled"), ClassRaftRPC); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled admission: %v", err)
	}
	if got := drainAllData(states, "peer"); len(got) != 0 {
		t.Fatalf("rejected messages entered queue: %q", got)
	}
	if err := states.QueueMessageClassContext(context.Background(), "peer", []byte("accepted"), ClassRaftRPC); err != nil {
		t.Fatal(err)
	}
	got := drainAllData(states, "peer")
	if len(got) != 1 || string(got[0]) != "accepted" {
		t.Fatalf("accepted data: %q", got)
	}
}

func TestContextManagerDoesNotHideUnmanagedDestination(t *testing.T) {
	m := &manager{nodeStates: setupStateManager()}
	if err := m.SendToNodeContext(context.Background(), "missing", []byte("request"), ClassRaftRPC); !errors.Is(err, ErrNodeNotManaged) {
		t.Fatalf("unmanaged send reported admission: %v", err)
	}
}

func TestContextQueueManagerShutdownCancelsAdmission(t *testing.T) {
	states := setupStateManager()
	states.CreateNodeState("peer")
	state := states.GetNodeState("peer")
	state.queueMu.Lock()
	defer state.queueMu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m := &manager{ctx: ctx, nodeStates: states}
	caller, cancelCaller := context.WithCancel(t.Context())
	defer cancelCaller()
	done := make(chan error, 1)
	go func() { done <- m.SendToNodeContext(caller, "peer", []byte("rejected"), ClassRaftRPC) }()
	require.Eventually(t, func() bool { return state.queueMu.waiting.Load() != nil }, time.Second, time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("manager shutdown did not cancel queue admission")
	}
	require.Zero(t, state.queues[ClassRaftRPC].len())
}

func TestContextQueueRejectsReplacedPeerGeneration(t *testing.T) {
	states := setupStateManager()
	states.CreateNodeState("peer")
	state := states.GetNodeState("peer")
	state.queueMu.Lock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- states.QueueMessageClassContext(ctx, "peer", []byte("old"), ClassRaftRPC) }()
	require.Eventually(t, func() bool { return state.queueMu.waiting.Load() != nil }, time.Second, time.Millisecond)
	require.Same(t, state, states.detachNodeState("peer"))
	states.CreateNodeState("peer")
	state.queueMu.Unlock()
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrNodeNotManaged)
	case <-time.After(time.Second):
		t.Fatal("queue admission did not finish after peer replacement")
	}
	require.Zero(t, state.queues[ClassRaftRPC].len())
	require.Empty(t, drainAllData(states, "peer"))
}

func TestInternodeContextFailureLeavesPackageWithCaller(t *testing.T) {
	for _, failure := range []string{"canceled", "unmanaged", "encode"} {
		t.Run(failure, func(t *testing.T) {
			states := setupStateManager()
			if failure != "unmanaged" {
				states.CreateNodeState("peer")
			}
			m := &manager{nodeStates: states}
			codec := &mockCodec{}
			if failure == "encode" {
				codec.encodeError = errors.New("encode failed")
			}
			service := NewService(zap.NewNop(), m, codec, nil, nil, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "canceled" {
				cancel()
			}

			pkg := relay.NewServicePackage("local", "host", "peer", "remote", "request")
			if err := service.SendContext(ctx, pkg); err == nil {
				t.Fatal("expected failed admission")
			}
			if pkg.Target.Node != "peer" || pkg.Target.Host != "remote" || len(pkg.Messages) != 1 {
				t.Fatal("failed admission consumed caller-owned package")
			}
			relay.ReleasePackage(pkg)
		})
	}
}

func TestInternodeContextSuccessQueuesBeforeTransferringOwnership(t *testing.T) {
	states := setupStateManager()
	states.CreateNodeState("peer")
	service := NewService(zap.NewNop(), &manager{nodeStates: states}, &mockCodec{encoded: []byte("encoded-request")}, nil, nil, nil)
	router := systemrelay.NewRouter(systemrelay.NewNode("local"), service)
	pkg := relay.NewServicePackage("local", "host", "peer", "remote", "request")
	if err := router.SendContext(context.Background(), pkg); err != nil {
		relay.ReleasePackage(pkg)
		t.Fatal(err)
	}
	// The package has transferred: inspect only the independently queued bytes.
	got := drainAllData(states, "peer")
	if len(got) != 1 || string(got[0]) != "encoded-request" {
		t.Fatalf("successful admission lost data: %q", got)
	}
}

func TestContextQueueWaitsForContentionThenAdmitsOnce(t *testing.T) {
	states := setupStateManager()
	states.CreateNodeState("peer")
	state := states.GetNodeState("peer")
	state.queueMu.Lock()
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { done <- states.QueueMessageClassContext(ctx, "peer", []byte("once"), ClassRaftRPC) }()
	require.Eventually(t, func() bool { return state.queueMu.waiting.Load() != nil }, time.Second, time.Millisecond)
	state.queueMu.Unlock()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("queue unlock did not wake sender")
	}
	require.Equal(t, [][]byte{[]byte("once")}, drainAllData(states, "peer"))
}
