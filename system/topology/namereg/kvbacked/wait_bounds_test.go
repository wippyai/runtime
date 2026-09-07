// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

func TestStrongDistantCallerDeadlineStillHasRuntimeWaitBound(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, 20*time.Millisecond, nil)
	r.strong.isLeader = func() bool { return false }
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.RegisterScope(ctx, "claim", mkPID("node-1", "owner"), globalapi.Strong)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			t.Fatalf("unexpected wait outcome: %v, caller=%v", err, ctx.Err())
		}
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("runtime wait inherited the caller's distant deadline")
	}
}

func TestExpiredStrongBarrierFailureDoesNotSpin(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, time.Second, nil)
	var leader atomic.Bool
	leader.Store(true)
	r.strong.isLeader = leader.Load
	var probes atomic.Int32
	r.barrier = func() error { probes.Add(1); return errors.New("barrier temporarily unavailable") }
	t.Cleanup(func() { leader.Store(false); r.strong.stopTimer("claim") })
	owner := mkPID("node-1", "owner")
	hdr := pendingHeader{PID: owner.String(), Name: "claim", RequiredNodes: []pid.NodeID{"node-1", "ghost"}, DeadlineUnixNano: time.Now().Add(-time.Second).UnixNano()}
	value, err := encode(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.engine.Set(pendingKey("claim"), value); err != nil {
		t.Fatal(err)
	}
	pe, err := r.engine.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	r.strong.leaderDrive("claim", pe.Epoch, pe.Version, hdr)
	time.Sleep(75 * time.Millisecond)
	if got := probes.Load(); got > 1 {
		t.Fatalf("expired deadline caused immediate retry loop: %d probes", got)
	}
}
