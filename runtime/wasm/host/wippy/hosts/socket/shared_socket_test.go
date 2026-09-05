// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type dummyResource struct {
	kind  preview2.ResourceType
	drops atomic.Int32
}

func (r *dummyResource) Type() preview2.ResourceType { return r.kind }
func (r *dummyResource) Drop()                       { r.drops.Add(1) }

// Tests interleaving Preview2 unconnected TCP/UDP with core connections under a tiny shared limit (1),
// proving that dropping one frees capacity for the other profile.
func TestSharedSocketBudget_InterleavePreview2AndCore(t *testing.T) {
	listener, port := listenForSocketTest(t)
	defer listener.Close()

	table := preview2.NewResourceTableWithLimits(10, 1)
	budget := table.SocketBudget()
	if budget == nil {
		t.Fatal("expected non-nil socket budget from table")
	}

	base := socketTestContext()
	ctx := wippyhost.WithSocketBudget(base, budget)
	ctx = wippyhost.WithCallLimits(ctx, wasmapi.LimitsConfig{MaxOpenSockets: 10})

	rt, module := socketTestModule(ctx, t, port)
	defer rt.Close(ctx)

	inst, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Accept in background for core connects
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()

	// 1. Core connects first under limit 1
	status, handle1 := callPacked(ctx, t, inst, "connect")
	if status != StatusOK {
		t.Fatalf("core connect failed: %d", status)
	}
	if budget.Used() != 1 {
		t.Fatalf("expected budget used=1, got %d", budget.Used())
	}

	// Preview2 TCP socket should now be rejected under shared limit
	p2TCPSock := &dummyResource{kind: preview2.ResourceTCPSocket}
	if _, err := table.TryAdd(p2TCPSock); !errors.Is(err, preview2.ErrSocketLimit) {
		t.Fatalf("expected ErrSocketLimit for Preview2 TCP socket, got %v", err)
	}
	if p2TCPSock.drops.Load() != 0 {
		t.Fatal("TryAdd failure should not drop resource")
	}

	// Preview2 UDP socket should also be rejected under shared limit
	p2UDPSock := &dummyResource{kind: preview2.ResourceUDPSocket}
	if _, err := table.TryAdd(p2UDPSock); !errors.Is(err, preview2.ErrSocketLimit) {
		t.Fatalf("expected ErrSocketLimit for Preview2 UDP socket, got %v", err)
	}

	// Close core connection -> frees capacity for Preview2
	if res := callStatus(ctx, t, inst, "close", handle1); res != StatusOK {
		t.Fatalf("core close failed: %d", res)
	}
	if budget.Used() != 0 {
		t.Fatalf("expected budget used=0 after core close, got %d", budget.Used())
	}

	// Now Preview2 UDP socket succeeds
	udpHandle, err := table.TryAdd(p2UDPSock)
	if err != nil {
		t.Fatalf("Preview2 UDP add failed after core close: %v", err)
	}
	if budget.Used() != 1 {
		t.Fatalf("expected budget used=1 after Preview2 UDP add, got %d", budget.Used())
	}

	// Core connect should now be rejected because Preview2 owns the slot
	status, _ = callPacked(ctx, t, inst, "connect")
	if status != StatusLimit {
		t.Fatalf("core connect status = %d, want StatusLimit (%d)", status, StatusLimit)
	}

	// Drop Preview2 UDP socket -> frees capacity for core
	table.Remove(udpHandle)
	if p2UDPSock.drops.Load() != 1 {
		t.Fatalf("expected p2UDPSock dropped once, got %d", p2UDPSock.drops.Load())
	}
	if budget.Used() != 0 {
		t.Fatalf("expected budget used=0 after Preview2 remove, got %d", budget.Used())
	}

	// Now core connect succeeds again
	status, handle2 := callPacked(ctx, t, inst, "connect")
	if status != StatusOK {
		t.Fatalf("core connect failed after Preview2 socket dropped: %d", status)
	}
	if budget.Used() != 1 {
		t.Fatalf("expected budget used=1, got %d", budget.Used())
	}

	// Cleanup
	_ = callStatus(ctx, t, inst, "close", handle2)
}

// Tests that failed or cancelled dials release capacity immediately.
func TestSharedSocketBudget_FailedAndCancelledDialReleasesCapacity(t *testing.T) {
	table := preview2.NewResourceTableWithLimits(10, 1)
	budget := table.SocketBudget()

	base := socketTestContext()
	ctx := wippyhost.WithSocketBudget(base, budget)
	ctx = wippyhost.WithCallLimits(ctx, wasmapi.LimitsConfig{MaxOpenSockets: 10})

	// Use an unlistened port to trigger connection refused
	rt, module := socketTestModule(ctx, t, 1) // port 1 should fail
	defer rt.Close(ctx)

	inst, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	// Dial fails -> capacity must be released
	status, _ := callPacked(ctx, t, inst, "connect")
	if status == StatusOK {
		t.Fatal("expected connect failure to port 1")
	}
	if budget.Used() != 0 {
		t.Fatalf("expected budget used=0 after failed dial, got %d", budget.Used())
	}

	// Now set up a real listener on a second module and verify connect succeeds
	listener, realPort := listenForSocketTest(t)
	defer listener.Close()

	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()

	rt2, module2 := socketTestModule(ctx, t, realPort)
	defer rt2.Close(ctx)
	inst2, err := module2.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate module2: %v", err)
	}
	defer inst2.Close(ctx)

	// Now verify cancelled dial context releases capacity
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancel before dial
	status, _ = callPacked(cancelledCtx, t, inst2, "connect")
	if status == StatusOK {
		t.Fatal("expected connect failure on cancelled ctx")
	}
	if budget.Used() != 0 {
		t.Fatalf("expected budget used=0 after cancelled dial, got %d", budget.Used())
	}

	// Normal connect succeeds because capacity wasn't leaked
	status, handle := callPacked(ctx, t, inst2, "connect")
	if status != StatusOK {
		t.Fatalf("expected connect StatusOK, got %d", status)
	}
	if budget.Used() != 1 {
		t.Fatalf("expected budget used=1, got %d", budget.Used())
	}

	_ = callStatus(ctx, t, inst2, "close", handle)
	if budget.Used() != 0 {
		t.Fatalf("expected budget used=0 after close, got %d", budget.Used())
	}
}

// Tests that two independent actor budgets do not share quotas.
func TestSharedSocketBudget_TwoActorBudgetsIndependent(t *testing.T) {
	listener, port := listenForSocketTest(t)
	defer listener.Close()

	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()

	tableA := preview2.NewResourceTableWithLimits(10, 1)
	tableB := preview2.NewResourceTableWithLimits(10, 1)

	base := socketTestContext()
	ctxA := wippyhost.WithSocketBudget(base, tableA.SocketBudget())
	ctxA = wippyhost.WithCallLimits(ctxA, wasmapi.LimitsConfig{MaxOpenSockets: 10})

	ctxB := wippyhost.WithSocketBudget(base, tableB.SocketBudget())
	ctxB = wippyhost.WithCallLimits(ctxB, wasmapi.LimitsConfig{MaxOpenSockets: 10})

	rtA, modA := socketTestModule(ctxA, t, port)
	defer rtA.Close(ctxA)
	instA, err := modA.Instantiate(ctxA)
	if err != nil {
		t.Fatalf("instantiate A: %v", err)
	}
	defer instA.Close(ctxA)

	rtB, modB := socketTestModule(ctxB, t, port)
	defer rtB.Close(ctxB)
	instB, err := modB.Instantiate(ctxB)
	if err != nil {
		t.Fatalf("instantiate B: %v", err)
	}
	defer instB.Close(ctxB)

	// Actor A connects -> uses tableA budget
	statusA, handleA := callPacked(ctxA, t, instA, "connect")
	if statusA != StatusOK {
		t.Fatalf("actor A connect failed: %d", statusA)
	}
	if tableA.SocketBudget().Used() != 1 {
		t.Fatalf("expected tableA budget used=1, got %d", tableA.SocketBudget().Used())
	}
	if tableB.SocketBudget().Used() != 0 {
		t.Fatalf("expected tableB budget used=0, got %d", tableB.SocketBudget().Used())
	}

	// Actor A cannot connect again (limit 1)
	statusA2, _ := callPacked(ctxA, t, instA, "connect")
	if statusA2 != StatusLimit {
		t.Fatalf("actor A second connect status = %d, want StatusLimit", statusA2)
	}

	// Actor B CAN connect because budgets are independent
	statusB, handleB := callPacked(ctxB, t, instB, "connect")
	if statusB != StatusOK {
		t.Fatalf("actor B connect failed: %d", statusB)
	}
	if tableB.SocketBudget().Used() != 1 {
		t.Fatalf("expected tableB budget used=1, got %d", tableB.SocketBudget().Used())
	}

	_ = callStatus(ctxA, t, instA, "close", handleA)
	_ = callStatus(ctxB, t, instB, "close", handleB)
}

// Tests concurrent reservations racing across core socket and Preview2,
// verifying that active socket count never exceeds the cap.
func TestSharedSocketBudget_ConcurrentReservationsCannotExceedCap(t *testing.T) {
	const cap = 3
	table := preview2.NewResourceTableWithLimits(100, cap)
	budget := table.SocketBudget()

	listener, port := listenForSocketTest(t)
	defer listener.Close()

	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()

	base := socketTestContext()
	ctx := wippyhost.WithSocketBudget(base, budget)
	ctx = wippyhost.WithCallLimits(ctx, wasmapi.LimitsConfig{MaxOpenSockets: 100})

	rt, module := socketTestModule(ctx, t, port)
	defer rt.Close(ctx)

	const workers = 16
	const iterations = 10
	var wg sync.WaitGroup
	var maxObserved atomic.Int32

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			inst, err := module.Instantiate(ctx)
			if err != nil {
				t.Errorf("instantiate worker %d: %v", workerID, err)
				return
			}
			defer inst.Close(ctx)

			for j := 0; j < iterations; j++ {
				if (workerID+j)%2 == 0 {
					// Preview2 reservation
					res := &dummyResource{kind: preview2.ResourceTCPSocket}
					h, err := table.TryAdd(res)
					if err == nil {
						u := int32(budget.Used())
						for {
							m := maxObserved.Load()
							if u <= m || maxObserved.CompareAndSwap(m, u) {
								break
							}
						}
						if u > cap {
							t.Errorf("budget used %d exceeded cap %d", u, cap)
						}
						time.Sleep(time.Millisecond)
						table.Remove(h)
					}
				} else {
					// Core connect
					status, handle := callPacked(ctx, t, inst, "connect")
					if status == StatusOK {
						u := int32(budget.Used())
						for {
							m := maxObserved.Load()
							if u <= m || maxObserved.CompareAndSwap(m, u) {
								break
							}
						}
						if u > cap {
							t.Errorf("budget used %d exceeded cap %d", u, cap)
						}
						time.Sleep(time.Millisecond)
						callStatus(ctx, t, inst, "close", handle)
					}
				}
			}
		}(i)
	}

	wg.Wait()
	if maxObserved.Load() > cap {
		t.Fatalf("max observed (%d) exceeded cap (%d)", maxObserved.Load(), cap)
	}
	if budget.Used() != 0 {
		t.Fatalf("expected budget used=0 at end, got %d", budget.Used())
	}
}

// Tests cancellation during Recv releases the socket lease to the budget.
func TestSharedSocketBudget_RecvCancellationReleasesCapacity(t *testing.T) {
	table := preview2.NewResourceTableWithLimits(10, 1)
	budget := table.SocketBudget()

	listener, port := listenForSocketTest(t)
	defer listener.Close()

	base := socketTestContext()
	ctx := wippyhost.WithSocketBudget(base, budget)
	ctx = wippyhost.WithCallLimits(ctx, wasmapi.LimitsConfig{MaxOpenSockets: 10, SocketTimeoutMS: 30})

	rt, module := socketTestModule(ctx, t, port)
	defer rt.Close(ctx)

	inst, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer inst.Close(ctx)

	status, handle := callPacked(ctx, t, inst, "connect")
	if status != StatusOK {
		t.Fatalf("connect status = %d", status)
	}
	if budget.Used() != 1 {
		t.Fatalf("expected budget used=1, got %d", budget.Used())
	}

	peer, err := listener.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer peer.Close()

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	status, _ = callPacked(callCtx, t, inst, "recv", handle)
	if status != StatusTimeout {
		t.Fatalf("recv status = %d, want timeout", status)
	}

	// Cancellation should trigger conn.Close() via boundOperation AfterFunc, which releases the lease
	released := false
	for i := 0; i < 50; i++ {
		if budget.Used() == 0 {
			released = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !released {
		t.Fatalf("expected budget used=0 after recv cancellation, got %d", budget.Used())
	}

	// Capacity is freed, so another connect can acquire it
	status2, handle2 := callPacked(ctx, t, inst, "connect")
	if status2 != StatusOK {
		t.Fatalf("connect after cancellation status = %d, want OK", status2)
	}
	if budget.Used() != 1 {
		t.Fatalf("expected budget used=1, got %d", budget.Used())
	}
	_ = callStatus(ctx, t, inst, "close", handle2)
}

// Tests closing underlying net.Conn directly releases the budget lease.
func TestSharedSocketBudget_UnderlyingConnCloseReleasesBudget(t *testing.T) {
	budget := preview2.NewSocketBudget(1)
	lease, err := budget.Acquire()
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	if budget.Used() != 1 {
		t.Fatalf("expected budget used=1, got %d", budget.Used())
	}

	// Create a pipe and wrap with lease
	client, server := net.Pipe()
	defer server.Close()

	wrapped := wrapWithLease(client, lease)
	c := &connection{
		Conn: wrapped,
	}

	// Calling Close on c.Conn (underlying net.Conn wrapper) releases the lease
	if err := c.Conn.Close(); err != nil {
		t.Fatalf("close c.Conn: %v", err)
	}
	if budget.Used() != 0 {
		t.Fatalf("expected budget used=0 after c.Conn.Close(), got %d", budget.Used())
	}

	// Calling c.Close() subsequently is safe, idempotent, and does not double release
	if err := c.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("close c: %v", err)
	}
	if budget.Used() != 0 {
		t.Fatalf("expected budget used=0 after c.Close(), got %d", budget.Used())
	}
}
