// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type canceledDialNetwork struct {
	entered chan struct{}
	recordingNetwork
	calls atomic.Int32
}

func (n *canceledDialNetwork) DialContext(ctx context.Context, kind, address string) (net.Conn, error) {
	if n.calls.Add(1) > 1 {
		return n.recordingNetwork.DialContext(ctx, kind, address)
	}
	close(n.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestSharedSocketBudget_InFlightDialCancellation(t *testing.T) {
	table := preview2.NewResourceTableWithLimits(10, 1)
	defer table.Close()
	budget := table.SocketBudget()
	network := &canceledDialNetwork{entered: make(chan struct{})}
	defer network.closePeer()
	ctx := wippyhost.WithSocketBudget(netapi.WithService(ctxapi.NewRootContext(), network), budget)
	rt, module := socketTestModule(ctx, t, 1234)
	defer rt.Close(ctx)
	inst, err := module.Instantiate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)

	callCtx, cancel := context.WithCancel(ctx)
	type outcome struct {
		value any
		err   error
	}
	result := make(chan outcome, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		value, err := inst.Call(callCtx, "connect")
		result <- outcome{value, err}
	}()
	// Join before the instance's deferred Close, including assertion failures.
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("canceled dial did not join")
		}
	}()
	select {
	case <-network.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("dial never entered network service")
	}
	if got := budget.Used(); got != 1 {
		t.Fatalf("in-flight dial charge = %d, want 1", got)
	}
	cancel()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("in-flight connect: %v", got.err)
		}
		packed, ok := got.value.(uint64)
		if !ok || packed != pack(StatusTimeout, 0) {
			t.Fatalf("in-flight connect result = %#v, want packed timeout", got.value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dial did not complete after cancellation")
	}
	<-done
	if got := budget.Used(); got != 0 {
		t.Fatalf("after canceled dial charge = %d, want 0", got)
	}
	// The same instance and quota remain usable with a healthy call context.
	recoveryCtx := ctx
	status, handle := callPacked(recoveryCtx, t, inst, "connect")
	if status != StatusOK || budget.Used() != 1 {
		t.Fatalf("recovery connect status=%d charge=%d", status, budget.Used())
	}
	if status := callStatus(recoveryCtx, t, inst, "close", handle); status != StatusOK {
		t.Fatalf("recovery close status=%d", status)
	}
	if got := budget.Used(); got != 0 {
		t.Fatalf("after recovery close charge = %d, want 0", got)
	}
}
