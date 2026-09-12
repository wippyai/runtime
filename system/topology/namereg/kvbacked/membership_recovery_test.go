// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	systemkv "github.com/wippyai/runtime/system/kv"
)

type failedMembershipVote struct {
	*systemkv.Service
	failing atomic.Bool
	refused atomic.Int32
}

func (e *failedMembershipVote) SetIfAbsent(key string, value []byte) (kvapi.Version, bool, error) {
	if strings.HasPrefix(key, ackPrefix) && e.failing.Load() {
		e.refused.Add(1)
		return 0, false, errors.New("injected vote authority loss")
	}
	return e.Service.SetIfAbsent(key, value)
}

func TestMembershipRecoveryPreservesExclusionAfterFailedVote(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Minute, nil)
	engine := &failedMembershipVote{Service: r.engine.(*systemkv.Service)}
	r.engine = engine
	r.strong.retryInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, r.StartReconciler(ctx))
	t.Cleanup(func() {
		cancel()
		<-r.reconciler.Load().done
	})
	engine.failing.Store(true)
	owner := mkPID("node-1", "owner")
	header, _ := seedStrongPending(t, r, pendingHeader{Name: "recover", PID: owner.String(), RequiredNodes: []pid.NodeID{"node-1"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()})
	require.Eventually(t, func() bool { return engine.refused.Load() > 0 && !r.NameReady() }, time.Second, time.Millisecond)
	held, ok := r.IsStrongReserved("recover")
	require.True(t, ok)
	require.True(t, held.Equal(owner), "failed vote must retain cross-scope exclusion")
	require.NoError(t, r.reconcileContext().Err(), "recovery owner must survive transient vote failure")
	engine.failing.Store(false)
	// No external KV event follows: resubscribe + seed must drive the pending.
	require.Eventually(t, func() bool {
		entry, err := engine.Get(activeKey("recover"))
		if err != nil || !r.NameReady() {
			return false
		}
		active, err := decodeActive(entry.Value)
		return err == nil && active.PID == owner.String() && active.AttemptID == header.AttemptID
	}, 2*time.Second, time.Millisecond)
}
