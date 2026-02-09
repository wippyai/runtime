package clocks

import (
	"context"
	"errors"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	wippypoll "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestMonotonicClockHost_SubscribeInstantAndNilResources(t *testing.T) {
	withNil := &MonotonicClockHost{}
	if got := withNil.SubscribeInstant(context.Background(), 1); got != 0 {
		t.Fatalf("SubscribeInstant(nil resources) = %d, want 0", got)
	}

	res := preview2.NewResourceTable()
	host := NewMonotonicClockHost(res)
	handle := host.SubscribeInstant(context.Background(), 0)
	if handle == 0 {
		t.Fatal("SubscribeInstant() returned zero handle")
	}
}

func TestDispatcherTimerPollableHelpers(t *testing.T) {
	var nilPollable *DispatcherTimerPollable
	nilPollable.Block(context.Background())

	deadline := time.Now().Add(15 * time.Millisecond)
	p := NewDispatcherTimerPollable(deadline)
	if p.Type() != preview2.ResourcePollable {
		t.Fatalf("Type() = %v, want %v", p.Type(), preview2.ResourcePollable)
	}
	p.Drop()
	if p.Ready() {
		t.Fatal("Ready() should be false before deadline")
	}
	if got := p.Remaining(); got <= 0 {
		t.Fatalf("Remaining() = %v, want > 0", got)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("Block() expected panic when async context is missing")
		}
	}()
	p.Block(context.Background())
}

func TestSleepPendingOp(t *testing.T) {
	op := &SleepPendingOp{duration: 5 * time.Millisecond}
	if got := op.CmdID(); got != wasmengine.CommandID(clockapi.Sleep) {
		t.Fatalf("CmdID() = %d, want %d", got, wasmengine.CommandID(clockapi.Sleep))
	}

	cmd, ok := op.ToCommand().(clockapi.SleepCmd)
	if !ok {
		t.Fatalf("ToCommand() type = %T, want clockapi.SleepCmd", op.ToCommand())
	}
	if cmd.Duration != 5*time.Millisecond {
		t.Fatalf("ToCommand().Duration = %v, want %v", cmd.Duration, 5*time.Millisecond)
	}

	if _, err := op.Execute(context.Background()); !errors.Is(err, ErrPollAsyncDispatchRequired) {
		t.Fatalf("Execute() error = %v, want %v", err, ErrPollAsyncDispatchRequired)
	}
}

func TestHostPollAndNilGuards(t *testing.T) {
	var nilHost *wippypoll.Host
	if got := nilHost.Namespace(); got != wippypoll.PollNamespace {
		t.Fatalf("Namespace() = %q, want %q", got, wippypoll.PollNamespace)
	}
	if out := nilHost.Poll(context.Background(), []uint32{1, 2}); out != nil {
		t.Fatalf("Poll(nil host) = %#v, want nil", out)
	}

	res := preview2.NewResourceTable()
	mono := NewMonotonicClockHost(res)
	pollHost := wippypoll.NewHost(res)

	ready := mono.SubscribeDuration(context.Background(), 0)
	waiting := mono.SubscribeDuration(context.Background(), uint64(20*time.Millisecond))

	indexes := pollHost.Poll(context.Background(), []uint32{ready, waiting})
	if len(indexes) == 0 {
		t.Fatalf("Poll() = %#v, expected at least one ready index", indexes)
	}
}
