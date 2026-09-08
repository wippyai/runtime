// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"go.uber.org/zap"
)

func TestCanceledProposalRetainsCapacityUntilFutureFinishes(t *testing.T) {
	waits := &proposalWaits{slots: make(chan struct{}, 1), maxBytes: 64}
	stop := make(chan struct{})
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	input := []byte("original")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := waits.run(ctx, stop, input, func(owned []byte) (*raftapi.ApplyResponse, error) {
			calls.Add(1)
			close(started)
			<-finish
			if string(owned) != "original" {
				t.Errorf("canceled caller corrupted in-flight proposal: %q", owned)
			}
			return &raftapi.ApplyResponse{Index: 1}, nil
		})
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	copy(input, "modified")
	if len(waits.slots) != 1 {
		t.Fatal("cancel prematurely freed unresolved proposal capacity")
	}
	next, stopNext := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopNext()
	_, err := waits.run(next, stop, nil, func([]byte) (*raftapi.ApplyResponse, error) { calls.Add(1); return nil, nil })
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("exceeded unresolved proposal limit: calls=%d err=%v", calls.Load(), err)
	}
	close(finish)
	// Completion frees capacity; a later request must make progress.
	recovery, stopRecovery := context.WithTimeout(context.Background(), time.Second)
	defer stopRecovery()
	out, err := waits.run(recovery, stop, nil, func([]byte) (*raftapi.ApplyResponse, error) { return &raftapi.ApplyResponse{Index: 2}, nil })
	if err != nil || out.Index != 2 {
		t.Fatalf("future completion did not restore capacity: %+v %v", out, err)
	}
}

func TestStoppedProposalTrackerDoesNotSubmit(t *testing.T) {
	waits := &proposalWaits{slots: make(chan struct{}, 1), maxBytes: 64}
	stop := make(chan struct{})
	close(stop)
	_, err := waits.run(context.Background(), stop, nil, func([]byte) (*raftapi.ApplyResponse, error) { t.Fatal("submitted after shutdown"); return nil, nil })
	if !errors.Is(err, raftapi.ErrNotRunning) {
		t.Fatalf("shutdown: %v", err)
	}
}

type pausedApplyFSM struct {
	started chan struct{}
	finish  chan struct{}
	once    sync.Once
}

func (f *pausedApplyFSM) Apply(*hraft.Log) any {
	f.once.Do(func() { close(f.started); <-f.finish })
	return "committed"
}
func (*pausedApplyFSM) Snapshot() (hraft.FSMSnapshot, error) {
	return nil, errors.New("snapshot unused in this test")
}
func (*pausedApplyFSM) Restore(reader io.ReadCloser) error { return reader.Close() }

func TestApplyContextCancelsWaitForRealRaftFuture(t *testing.T) {
	fsm := &pausedApplyFSM{started: make(chan struct{}), finish: make(chan struct{})}
	release := sync.OnceFunc(func() { close(fsm.finish) })
	cfg := hraft.DefaultConfig()
	cfg.LocalID = "one"
	cfg.LogOutput = io.Discard
	store := hraft.NewInmemStore()
	_, transport := hraft.NewInmemTransport("one")
	instance, err := hraft.NewRaft(cfg, fsm, store, store, hraft.NewInmemSnapshotStore(), transport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { release(); _ = instance.Shutdown().Error(); _ = transport.Close() })
	if err := instance.BootstrapCluster(hraft.Configuration{Servers: []hraft.Server{{ID: "one", Address: "one", Suffrage: hraft.Voter}}}).Error(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	leaderTick := time.NewTicker(10 * time.Millisecond)
	defer leaderTick.Stop()
	for instance.State() != hraft.Leader {
		select {
		case <-deadline:
			t.Fatal("single-node leader did not start")
		case <-leaderTick.C:
		}
	}
	node := NewNode("one", fsm, raftapi.Config{MaxPendingApplies: 1}, nil, zap.NewNop(), nil, nil, nil)
	node.raft, node.started = instance, true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := node.ApplyContext(ctx, []byte("proposal"), time.Second); done <- err }()
	select {
	case <-fsm.started:
	case <-time.After(time.Second):
		t.Fatal("proposal never reached FSM")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting did not honor cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller remained blocked on Raft future")
	}
	if len(node.proposals.slots) != 1 {
		t.Fatal("unresolved future lost its tracking slot")
	}
	release()
	recovery, stopRecovery := context.WithTimeout(context.Background(), time.Second)
	defer stopRecovery()
	out, err := node.ApplyContext(recovery, []byte("next"), time.Second)
	if err != nil || out.Response != "committed" {
		t.Fatalf("tracking did not recover: %+v %v", out, err)
	}
}
