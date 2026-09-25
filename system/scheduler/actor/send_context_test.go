// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/topology"
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

func TestSchedulerSendNotFoundBeforeTopologyComplete(t *testing.T) {
	target := pid.PID{Node: "n1", Host: "app", UniqID: "send-before-topology-complete"}
	target = target.Precomputed()
	watcher := pid.PID{Node: "n1", Host: "app", UniqID: "send-before-topology-watcher"}
	watcher = watcher.Precomputed()
	router := newRecordingRelayReceiver()
	topo := topology.NewTopology(router, "n1")
	require.NoError(t, topo.Register(target))
	require.NoError(t, topo.Register(watcher))
	require.NoError(t, topo.Monitor(watcher, target))

	completionEntered := make(chan struct{})
	releaseCompletion := make(chan struct{})
	lifecycle := &testLifecycle{onComplete: func(_ context.Context, p pid.PID, result *runtime.Result) {
		close(completionEntered)
		<-releaseCompletion
		topo.Complete(p, result)
	}}
	s := NewScheduler(nil, WithWorkers(1), WithLifecycle(lifecycle))
	proc, err := s.Submit(context.Background(), target, &IdleProcess{}, "", nil)
	require.NoError(t, err)

	completed := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCompletion) }) }
	go func() {
		s.completeNoPool(proc, nil, context.Canceled)
		close(completed)
	}()
	defer func() {
		release()
		select {
		case <-completed:
		case <-time.After(time.Second):
			t.Error("completion did not finish during test cleanup")
		}
	}()
	select {
	case <-completionEntered:
	case <-time.After(time.Second):
		t.Fatal("completion lifecycle did not reach its gate")
	}

	pkg := relay.NewPackage(pid.PID{}, target, "test")
	err = s.SendContext(context.Background(), pkg)
	require.ErrorIs(t, err, process.ErrProcessNotFound)
	require.Len(t, pkg.Messages, 1)
	relay.ReleasePackage(pkg)

	require.NoError(t, topo.Demonitor(watcher, target))
	release()
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("completion did not finish after releasing topology hook")
	}
	packages := router.take()
	for _, captured := range packages {
		relay.ReleasePackage(captured)
	}
	require.Empty(t, packages)
}

type recordingRelayReceiver struct {
	packages chan *relay.Package
}

func newRecordingRelayReceiver() *recordingRelayReceiver {
	return &recordingRelayReceiver{packages: make(chan *relay.Package, 16)}
}

func (r *recordingRelayReceiver) Send(pkg *relay.Package) error {
	r.packages <- pkg
	return nil
}

func (r *recordingRelayReceiver) take() []*relay.Package {
	var packages []*relay.Package
	for {
		select {
		case pkg := <-r.packages:
			packages = append(packages, pkg)
		default:
			return packages
		}
	}
}
