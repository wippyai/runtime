// SPDX-License-Identifier: MPL-2.0

package global

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/pid"
)

type joinBarrierRaft struct {
	raftapi.Service
	err   error
	calls int
}

func (r *joinBarrierRaft) CommitIndex() uint64         { return 9999 }
func (r *joinBarrierRaft) Barrier(time.Duration) error { r.calls++; return r.err }

func TestJoinSnapshotUsesAppliedStateAndRequiresBarrier(t *testing.T) {
	s := newJoinTestService(t)
	p := makePID("node-2", "host", "owner")
	applyAt(t, s.fsm, &Command{Type: CmdRegister, Name: "name", PID: p}, 7)
	r := &joinBarrierRaft{Service: s.raftSvc}
	s.raftSvc = r
	snapshot, err := s.JoinNameEpoch(1)
	require.NoError(t, err)
	require.Equal(t, uint64(7), snapshot.StrongIndex)
	require.Len(t, snapshot.Entries, 1)
	require.Equal(t, 1, r.calls)
	r.err = errors.New("lost authority")
	snapshot, err = s.JoinNameEpoch(1)
	require.ErrorIs(t, err, r.err)
	require.Nil(t, snapshot, "failed authority barrier must not serve local state")
}

func TestJoinSnapshotAtomicAcrossPromotionAndRemoval(t *testing.T) {
	f := NewFSM()
	s := &Service{fsm: f}
	p := makePID("node-1", "host", "owner")
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for epoch := uint64(1); epoch < 1500; epoch += 3 {
			applyAt(t, f, &Command{Type: CmdRegisterPending, Name: "name", PID: p, NodeID: p.Node, RequiredNodes: []pid.NodeID{p.Node}, Epoch: epoch}, epoch)
			applyAt(t, f, &Command{Type: CmdRegisterAck, Name: "name", Epoch: epoch, AckerNode: p.Node}, epoch+1)
			applyAt(t, f, &Command{Type: CmdRegisterUnreserve, Name: "name", Epoch: epoch}, epoch+2)
		}
	}()
	defer writer.Wait()
	for i := 0; i < 1500; i++ {
		snapshot := buildJoinForTest(t, s)
		phase := snapshot.StrongIndex % 3
		if phase == 0 {
			require.Empty(t, snapshot.Entries, "terminal index %d", snapshot.StrongIndex)
		} else {
			require.Len(t, snapshot.Entries, 1, "index %d", snapshot.StrongIndex)
			require.Equal(t, "name", snapshot.Entries[0].Name)
			require.Equal(t, uint8(phase-1), snapshot.Entries[0].State, "index %d", snapshot.StrongIndex)
		}
	}
}

func TestJoinSnapshotRestoresAppliedIndexAfterDeletion(t *testing.T) {
	f := NewFSM()
	p := makePID("node-1", "host", "owner")
	applyAt(t, f, &Command{Type: CmdRegister, Name: "name", PID: p}, 3)
	applyAt(t, f, &Command{Type: CmdUnregister, Name: "name"}, 4)
	snap, err := f.Snapshot()
	require.NoError(t, err)
	sink := &memSink{Buffer: new(bytes.Buffer)}
	require.NoError(t, snap.Persist(sink))
	restored := NewFSM()
	require.NoError(t, restored.Restore(nopReadCloser{bytes.NewReader(sink.Bytes())}))
	captured := buildJoinForTest(t, &Service{fsm: restored})
	require.Empty(t, captured.Entries)
	require.Equal(t, uint64(4), captured.StrongIndex, "deletion revision survives empty snapshot")
}

func TestApplyCallbacksCanCaptureCompleteCommand(t *testing.T) {
	f := NewFSM()
	p := makePID("node-1", "host", "owner")
	applyAt(t, f, &Command{Type: CmdRegister, Name: "first", PID: p}, 1)
	applyAt(t, f, &Command{Type: CmdRegister, Name: "second", PID: p}, 2)
	observed := make(chan *joinResponseEnvelope, 2)
	f.SetOnBinding(func(ev BindingEvent) {
		if !ev.Deleted {
			return
		}
		// Both persistence and join readers must be callable after publication,
		// even from a synchronous notification callback.
		snapshot, err := f.Snapshot()
		if err != nil {
			t.Error(err)
			return
		}
		snapshot.Release()
		observed <- buildJoinForTest(t, &Service{fsm: f})
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		applyAt(t, f, &Command{Type: CmdRemovePID, PID: p}, 3)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Apply callback deadlocked while capturing state")
	}
	require.Len(t, observed, 2)
	for i := 0; i < 2; i++ {
		snapshot := <-observed
		require.Equal(t, uint64(3), snapshot.StrongIndex)
		require.Empty(t, snapshot.Entries, "callback must see the complete remove command")
	}
	require.Empty(t, f.applyEvents, "delivered events must not retain payloads")
}

func buildJoinForTest(t testing.TB, s *Service) *joinResponseEnvelope {
	t.Helper()
	snapshot, err := s.buildJoinSnapshot(0, DefaultJoinConfig().MaxEntries)
	require.NoError(t, err)
	return snapshot
}
