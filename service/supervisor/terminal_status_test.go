// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	topologyapi "github.com/wippyai/runtime/api/topology"
	syssupervisor "github.com/wippyai/runtime/system/supervisor"
)

func TestServiceStopDoesNotConsumeTerminalStatus(t *testing.T) {
	svc := newTestService()
	svc.statusCh = make(chan any, 1)
	svc.doneCh = make(chan struct{})
	want := errors.New("terminal failure")
	svc.statusCh <- want
	close(svc.statusCh)
	close(svc.doneCh)
	require.NoError(t, svc.Stop(context.Background()))
	got, ok := <-svc.statusCh
	require.True(t, ok, "Stop stole the supervisor's terminal result")
	require.Same(t, want, got)
}

type signaledCancelNode struct {
	canceled chan struct{}
	mockNode
}

func (n *signaledCancelNode) Send(p *relay.Package) error {
	select {
	case n.canceled <- struct{}{}:
	default:
	}
	return n.mockNode.Send(p)
}

func TestServiceStopPreservesLiveTerminalStatus(t *testing.T) {
	svc := newTestService()
	node := &signaledCancelNode{canceled: make(chan struct{}, 1)}
	ctx := setupTestContext(node, &mockTopology{}, &mockProcessManager{startedPID: pid.PID{UniqID: "child"}})
	status, err := svc.Start(ctx)
	require.NoError(t, err)
	stopCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- svc.Stop(stopCtx) }()
	select {
	case <-node.canceled:
	case <-time.After(time.Second):
		t.Fatal("Stop did not send cancellation")
	}
	want := errors.New("child failed during shutdown")
	node.attachCh <- relay.NewPackage(svc.childPID, svc.supervisorPID, topologyapi.TopicEvents,
		payload.New(&topologyapi.ExitEvent{Kind: topologyapi.Exit, Result: &runtime.Result{Error: want}}))
	require.NoError(t, <-stopped)
	got, open := <-status
	require.True(t, open, "Stop consumed the only terminal status")
	require.ErrorIs(t, got.(error), want)
}

type delayedStartManager struct {
	canceled chan struct{}
	release  chan struct{}
	mockProcessManager
}

func (m *delayedStartManager) Start(ctx context.Context, _ *processapi.Start) (pid.PID, error) {
	<-ctx.Done()
	close(m.canceled)
	<-m.release
	return m.startedPID, nil
}

func TestControllerLateProcessStartRetainsExitMonitor(t *testing.T) {
	svc := newTestService()
	node := &signaledCancelNode{canceled: make(chan struct{}, 4)}
	manager := &delayedStartManager{
		mockProcessManager: mockProcessManager{startedPID: pid.PID{UniqID: "late-child"}},
		canceled:           make(chan struct{}), release: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(setupTestContext(node, &mockTopology{}, manager))
	defer cancel()
	controller := syssupervisor.NewController(ctx, svc, supervisor.LifecycleConfig{
		StartTimeout: 10 * time.Millisecond, StopTimeout: time.Second,
		RetryPolicy: supervisor.RetryPolicy{MaxAttempts: 1},
	}, nil)
	require.Error(t, controller.Start())
	<-manager.canceled
	done := svc.doneCh
	close(manager.release)
	select {
	case <-node.canceled:
	case <-time.After(time.Second):
		t.Fatal("late-started process did not receive cancellation")
	}
	select {
	case <-done:
		t.Fatal("canceled context was mistaken for a real child exit")
	default:
	}
	node.attachCh <- relay.NewPackage(pid.PID{}, pid.PID{}, topologyapi.TopicEvents,
		payload.New(&topologyapi.ExitEvent{Kind: topologyapi.Exit, Result: &runtime.Result{}}))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("late child exit was no longer monitored")
	}
	require.NoError(t, controller.Stop())
}
