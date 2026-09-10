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
	started := time.Now()
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
		if elapsed := time.Since(started); elapsed < time.Second {
			t.Fatalf("runtime wait ended before the Strong deadline grace: %v", elapsed)
		}
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("runtime wait inherited the caller's distant deadline")
	}
}

func TestStrongEarlierCallerDeadlineStillWins(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, 5*time.Second, nil)
	r.strong.isLeader = func() bool { return false }
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := r.RegisterScope(ctx, "claim", mkPID("node-1", "owner"), globalapi.Strong)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("earlier caller deadline outcome: %v, caller=%v", err, ctx.Err())
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("runtime deadline overrode earlier caller deadline: %v", elapsed)
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
	if got := probes.Load(); got != 1 {
		t.Fatalf("expired deadline caused immediate retry loop: %d probes", got)
	}

	deadline := time.After(1500 * time.Millisecond)
	for probes.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("failed barrier was never retried")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if got := probes.Load(); got != 2 {
		t.Fatalf("barrier retry was not bounded: %d probes", got)
	}
}
