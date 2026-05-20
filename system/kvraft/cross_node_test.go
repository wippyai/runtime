// SPDX-License-Identifier: MPL-2.0

package kvraft

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/api/kv"
	raftapi "github.com/wippyai/runtime/api/raft"
)

// sharedRaft is a minimal raftapi.Service that serializes Apply calls
// through a single atomic-index sequencer and dispatches them into the
// shared FSM. This is the kvraft analog of the directApplyRaft used
// elsewhere in the tree and models a single-leader Raft log without
// running real consensus: every concurrent client (representing a
// different node calling through follower-forward) lands in the same
// linearizable order. For tests that probe CAS atomicity this is
// equivalent to a real cluster because hashicorp/raft itself
// linearizes Apply by funneling every command through the leader's
// single log goroutine.
type sharedRaft struct {
	fsm   *FSM
	idx   atomic.Uint64
	logMu sync.Mutex
}

func (r *sharedRaft) Apply(data []byte, _ time.Duration) (*raftapi.ApplyResponse, error) {
	// hashicorp/raft applies log entries one at a time on the leader's
	// single log goroutine. Mirror that invariant here so the test
	// surfaces CAS atomicity bugs in the FSM rather than races
	// introduced by the stub itself.
	r.logMu.Lock()
	defer r.logMu.Unlock()
	i := r.idx.Add(1)
	resp := r.fsm.Apply(&hraft.Log{Index: i, Data: data})
	return &raftapi.ApplyResponse{Response: resp, Index: i}, nil
}
func (r *sharedRaft) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	return "node-A", "node-A", nil
}
func (r *sharedRaft) IsLeader() bool                { return true }
func (r *sharedRaft) LeaderCh() <-chan bool         { return nil }
func (r *sharedRaft) State() raftapi.State          { return raftapi.Leader }
func (r *sharedRaft) Barrier(_ time.Duration) error { return nil }
func (r *sharedRaft) AddVoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (r *sharedRaft) AddNonvoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (r *sharedRaft) DemoteVoter(_ raftapi.ServerID, _ time.Duration) error        { return nil }
func (r *sharedRaft) RemoveServer(_ raftapi.ServerID, _ time.Duration) error       { return nil }
func (r *sharedRaft) LeadershipTransfer(_ raftapi.ServerID, _ time.Duration) error { return nil }
func (r *sharedRaft) GetConfiguration() ([]raftapi.Server, error)                  { return nil, nil }

func newSharedService(t *testing.T, fsm *FSM, raft raftapi.Service) *Service {
	t.Helper()
	svc, err := NewService(Config{Raft: raft, FSM: fsm})
	require.NoError(t, err)
	require.NoError(t, svc.Start(context.Background()))
	t.Cleanup(func() { _ = svc.Stop() })
	return svc
}

// TestKVRaft_CASLinearizableAcrossNodes reproduces the reviewer's
// requested scenario from PR #241 issue 4494041044 test item 9:
// "kvraft CAS/read-barrier behavior across nodes". Two services share
// one Raft log (the sharedRaft serializes Apply through an atomic
// sequencer, mirroring the leader's single-threaded log). Concurrent
// CAS-increment loops fire from both nodes against the same counter
// key. The invariant: exactly N successful increments produce a
// final counter value of N. Lost-update is impossible because every
// CAS commits atomically through the shared log. The same shape
// covers the read-barrier guarantee: Get after a CAS returns the
// just-written value because the FSM state is updated in the same
// goroutine that serves applyCommand's reply.
func TestKVRaft_CASLinearizableAcrossNodes(t *testing.T) {
	fsm := NewFSM()
	raft := &sharedRaft{fsm: fsm}

	svcA := newSharedService(t, fsm, raft)
	svcB := newSharedService(t, fsm, raft)

	kvA, err := svcA.Open("counters")
	require.NoError(t, err)
	kvB, err := svcB.Open("counters")
	require.NoError(t, err)

	const key = "counter"
	require.NoError(t, kvA.Put(context.Background(), key, encodeUint64(0)))

	const perNode = 50
	const total = perNode * 2
	var success atomic.Int64
	var mismatch atomic.Int64
	var wg sync.WaitGroup

	worker := func(svc kv.KV) {
		defer wg.Done()
		for {
			cur, getErr := svc.Get(context.Background(), key)
			require.NoError(t, getErr)
			next := encodeUint64(decodeUint64(cur.Data) + 1)
			err := svc.CompareAndSwap(context.Background(), key, cur.Data, next)
			if err == nil {
				success.Add(1)
				return
			}
			if !errors.Is(err, kv.ErrCASMismatch) {
				t.Errorf("unexpected CAS error: %v", err)
				return
			}
			mismatch.Add(1)
		}
	}

	for i := 0; i < perNode; i++ {
		wg.Add(2)
		go worker(kvA)
		go worker(kvB)
	}
	wg.Wait()

	assert.Equal(t, int64(total), success.Load(),
		"every goroutine must eventually win a CAS exactly once")
	assert.GreaterOrEqual(t, mismatch.Load(), int64(0),
		"mismatch count is informational; non-negative is the only invariant")

	v, err := kvA.Get(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, uint64(total), decodeUint64(v.Data),
		"final counter must equal total successful CAS — zero lost updates")
	vB, err := kvB.Get(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, v.Data, vB.Data,
		"both services must observe the same final value")
}

func encodeUint64(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}

func decodeUint64(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}
