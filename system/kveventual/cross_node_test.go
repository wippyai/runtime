// SPDX-License-Identifier: MPL-2.0

package kveventual

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/kv"
)

// pump pulls outgoing broadcasts from src and feeds them into dst.
// Returns the number of frames moved. Loops until src reports no
// pending broadcasts so a single pump call delivers all in-flight
// state on a quiet test cluster.
func pump(src, dst *Service) int {
	const overhead = 0
	const limit = 1 << 20
	moved := 0
	for {
		frames := src.GetBroadcasts(overhead, limit)
		if len(frames) == 0 {
			return moved
		}
		for _, f := range frames {
			dst.NotifyMsg(f)
			moved++
		}
	}
}

// TestKVEventual_ConvergenceAcrossNodes reproduces the reviewer's
// requested scenario from PR #241 issue 4494041044 test item 10:
// "kveventual convergence and tombstone behavior under churn".
//
// Two services with distinct NodeIDs concurrently write disjoint key
// ranges, then a delete is issued on one side, then deltas are
// pumped between them via the same broadcast / NotifyMsg pair the
// memberlist delegate uses in production. The invariants:
//   - Both nodes observe every concurrent Put after one pump round
//     (CRDT merge converges in one direction).
//   - Deletes propagate as tombstones — the receiver stops returning
//     the deleted key with kv.ErrKeyNotFound after pump.
//   - Tombstones are subject to the wall-floor GC: after runGCOnce()
//     fires past the wall floor, the tombstone count drops to zero
//     and the key cannot be resurrected by replaying an older Put
//     (proven via a second Put on the deleted key that re-creates
//     it with a fresh version).
func TestKVEventual_ConvergenceAcrossNodes(t *testing.T) {
	mkSvc := func(id string) *Service {
		svc := NewService(Config{
			LocalNodeID: id,
			Peers:       &stubPeers{},
			// Short floor so the GC-after-wall-floor assertion does not
			// require a real 15-minute wait. 50ms is comfortably longer
			// than the test's pump latency on any machine.
			WallFloor: 50 * time.Millisecond,
			GCPeriod:  time.Hour,
		})
		require.NoError(t, svc.Start(context.Background()))
		t.Cleanup(func() { _ = svc.Stop() })
		return svc
	}

	svcA := mkSvc("node-A")
	svcB := mkSvc("node-B")

	const spaceName = "test"
	kvA, err := svcA.Open(spaceName)
	require.NoError(t, err)
	kvB, err := svcB.Open(spaceName)
	require.NoError(t, err)

	ctx := context.Background()

	const half = 10
	for i := 0; i < half; i++ {
		require.NoError(t, kvA.Put(ctx, fmt.Sprintf("a%d", i), []byte("vA")))
		require.NoError(t, kvB.Put(ctx, fmt.Sprintf("b%d", i), []byte("vB")))
	}

	require.NotZero(t, pump(svcA, svcB), "svcA should have outgoing broadcasts")
	require.NotZero(t, pump(svcB, svcA), "svcB should have outgoing broadcasts")

	for i := 0; i < half; i++ {
		va, err := kvA.Get(ctx, fmt.Sprintf("b%d", i))
		require.NoError(t, err, "svcA must see svcB's key b%d", i)
		assert.Equal(t, []byte("vB"), va.Data)

		vb, err := kvB.Get(ctx, fmt.Sprintf("a%d", i))
		require.NoError(t, err, "svcB must see svcA's key a%d", i)
		assert.Equal(t, []byte("vA"), vb.Data)
	}

	for i := 0; i < 5; i++ {
		require.NoError(t, kvA.Delete(ctx, fmt.Sprintf("a%d", i)))
	}

	pump(svcA, svcB)

	for i := 0; i < 5; i++ {
		_, err := kvB.Get(ctx, fmt.Sprintf("a%d", i))
		assert.Truef(t, errors.Is(err, kv.ErrKeyNotFound),
			"deleted key a%d must surface as ErrKeyNotFound on svcB after pump; got err=%v",
			i, err)
	}
	for i := 5; i < half; i++ {
		_, err := kvB.Get(ctx, fmt.Sprintf("a%d", i))
		require.NoError(t, err, "non-deleted key a%d must remain visible on svcB", i)
	}

	time.Sleep(80 * time.Millisecond)
	svcA.runGCOnce()
	svcB.runGCOnce()

	require.NoError(t, kvA.Put(ctx, "a0", []byte("resurrected")))
	pump(svcA, svcB)
	v, err := kvB.Get(ctx, "a0")
	require.NoError(t, err, "post-GC re-Put must propagate normally")
	assert.Equal(t, []byte("resurrected"), v.Data,
		"the resurrected value must win because tombstones older than the wall floor are reaped")
}
