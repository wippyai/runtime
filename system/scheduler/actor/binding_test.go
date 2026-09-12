// SPDX-License-Identifier: MPL-2.0
package actor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

func TestBoundProcessorCannotFollowSamePIDReplacement(t *testing.T) {
	s := NewScheduler(nil, WithWorkers(1))
	target := pid.PID{Node: "local", Host: "actors", UniqID: "same"}
	proc, err := s.Submit(context.Background(), target, &IdleProcess{}, "", nil)
	require.NoError(t, err)
	bound, err := s.BindLocal(target)
	require.NoError(t, err)
	// Model reuse of the exact slot and PID, not merely another byPID entry.
	proc.queue.Reset()
	proc.gen.Store(proc.queue.Generation())
	proc.publishSignalRef()
	pkg := relay.NewPackage(pid.PID{}, target, "data")
	require.Error(t, bound.SendContext(context.Background(), pkg))
	require.Len(t, pkg.Messages, 1, "refusal retains caller ownership")
	relay.ReleasePackage(pkg)
	require.False(t, proc.queue.HasEvents())
	fresh, err := s.BindLocal(target)
	require.NoError(t, err)
	accepted := relay.NewPackage(pid.PID{}, target, "data")
	require.NoError(t, fresh.SendContext(context.Background(), accepted))
	proc.queue.Close()
	require.Empty(t, accepted.Messages)
	s.completeNoPool(proc, nil, context.Canceled)
}

func TestBoundProcessorRejectsOtherTargetAndCancellation(t *testing.T) {
	s := NewScheduler(nil, WithWorkers(1))
	target := pid.PID{UniqID: "bound"}
	proc, err := s.Submit(context.Background(), target, &IdleProcess{}, "", nil)
	require.NoError(t, err)
	defer s.completeNoPool(proc, nil, context.Canceled)
	bound, err := s.BindLocal(target)
	require.NoError(t, err)
	wrong := relay.NewPackage(pid.PID{}, pid.PID{UniqID: "other"}, "data")
	require.ErrorIs(t, bound.SendContext(context.Background(), wrong), relay.ErrBindingTarget)
	relay.ReleasePackage(wrong)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pkg := relay.NewPackage(pid.PID{}, target, "data")
	require.ErrorIs(t, bound.SendContext(ctx, pkg), context.Canceled)
	require.Len(t, pkg.Messages, 1)
	relay.ReleasePackage(pkg)
	require.False(t, proc.queue.HasEvents())
}
