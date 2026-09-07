// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

func TestSchedulerSendContextUnknownTargetKeepsCallerOwnership(t *testing.T) {
	s := NewScheduler(nil, WithWorkers(1))
	pkg := relay.NewPackage(pid.PID{}, pid.PID{UniqID: "missing"}, "test")

	err := s.SendContext(context.Background(), pkg)
	require.ErrorIs(t, err, process.ErrProcessNotFound)
	// A failed admission leaves the package with the caller, so it is safe to
	// inspect/release it here rather than relying on a detached sender.
	require.Len(t, pkg.Messages, 1)
	relay.ReleasePackage(pkg)
}

func TestSchedulerSendContextCanceledBeforeAdmission(t *testing.T) {
	s := NewScheduler(nil, WithWorkers(1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pkg := relay.NewPackage(pid.PID{}, pid.PID{UniqID: "missing"}, "test")

	err := s.SendContext(ctx, pkg)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, pkg.Messages, 1)
	relay.ReleasePackage(pkg)
}

func TestSchedulerSendRejectsStaleGeneration(t *testing.T) {
	s := NewScheduler(nil, WithWorkers(1))
	target := pid.PID{UniqID: "stale-generation"}
	proc, err := s.Submit(context.Background(), target, &IdleProcess{}, "", nil)
	require.NoError(t, err)

	oldGeneration := proc.gen.Load()
	proc.queue.Reset()
	pkg := relay.NewPackage(pid.PID{}, target, "test")
	require.False(t, s.deliverToProc(proc, oldGeneration, pkg))
	// The stale sender never transfers ownership.
	require.Len(t, pkg.Messages, 1)
	relay.ReleasePackage(pkg)

	s.completeNoPool(proc, nil, context.Canceled)
	_, ok := s.byPID.Load(target.String())
	require.False(t, ok)
}

func TestSchedulerSendContextAcceptedQueueOwnsPackage(t *testing.T) {
	s := NewScheduler(nil, WithWorkers(1))
	target := pid.PID{UniqID: "accepted"}
	proc, err := s.Submit(context.Background(), target, &IdleProcess{}, "", nil)
	require.NoError(t, err)

	pkg := relay.NewPackage(pid.PID{}, target, "test")
	require.NoError(t, s.SendContext(context.Background(), pkg))
	// Queue admission transfers ownership. Closing the queue must release the
	// package exactly once and must not require the sender to release it.
	proc.queue.Close()
	require.Empty(t, pkg.Messages)

	s.completeNoPool(proc, nil, context.Canceled)
	if err := s.SendContext(context.Background(), pkg); !errors.Is(err, process.ErrProcessNotFound) {
		t.Fatalf("expected completed process to be absent, got %v", err)
	}
}
