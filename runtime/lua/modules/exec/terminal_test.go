// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	ttyapi "github.com/wippyai/runtime/api/tty"
	luatty "github.com/wippyai/runtime/runtime/lua/modules/tty"
)

type terminalCompletionReceiver struct {
	failures int64
	calls    atomic.Int64
	terminal atomic.Bool
}

func (r *terminalCompletionReceiver) Send(pkg *relay.Package) error {
	call := r.calls.Add(1)
	if len(pkg.Messages) == 1 {
		payloads := pkg.Messages[0].Payloads
		if len(payloads) > 0 && payload.IsTerminal(payloads[len(payloads)-1]) {
			r.terminal.Store(true)
		}
	}
	if call <= r.failures {
		return errors.New("relay unavailable")
	}
	relay.ReleasePackage(pkg)
	return nil
}

func TestTerminalCompletionIsOneShot(t *testing.T) {
	receiver := &terminalCompletionReceiver{}
	deliverTerminalCompletion(
		context.Background(),
		receiver,
		pid.PID{Host: "test", UniqID: "1"},
		"done",
	)
	if !receiver.terminal.Load() {
		t.Fatal("completion did not carry terminal subscription marker")
	}
}

func TestDecodeTerminalEventPreservesFields(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	table := l.CreateTable(0, 9)
	table.RawSetString("type", lua.LString("mouse"))
	table.RawSetString("action", lua.LString("motion"))
	table.RawSetString("button", lua.LString("left"))
	table.RawSetString("x", lua.LInteger(11))
	table.RawSetString("y", lua.LInteger(7))
	table.RawSetString("ctrl", lua.LTrue)
	event, err := luatty.DecodeEvent(table)
	if err != nil {
		t.Fatalf("decode terminal event: %v", err)
	}
	if event.Type != "mouse" || event.Action != "motion" || event.Button != "left" ||
		event.X != 11 || event.Y != 7 || !event.Ctrl {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestEnqueueTerminalEventBackpressure(t *testing.T) {
	events := make(chan ttyapi.Event, 1)
	events <- ttyapi.Event{Type: "key", Key: "a"}
	if err := enqueueTerminalEvent(events, ttyapi.Event{Type: "mouse", Action: "motion"}); err != nil {
		t.Fatalf("pointer motion should coalesce under pressure: %v", err)
	}
	if err := enqueueTerminalEvent(events, ttyapi.Event{Type: "key", Key: "b"}); !errors.Is(err, errTerminalInputFull) {
		t.Fatalf("discrete input must report backpressure, got %v", err)
	}
	if got := <-events; got.Key != "a" {
		t.Fatalf("queued discrete input was replaced: %#v", got)
	}
}

func TestProcessExportsTerminalAttachment(t *testing.T) {
	if processMethods["attach_terminal"] == nil {
		t.Fatal("exec.Process attach_terminal method is missing")
	}
}

func TestTerminalCompletionDoesNotRetryRelayFailure(t *testing.T) {
	receiver := &terminalCompletionReceiver{failures: 1}
	deliverTerminalCompletion(
		context.Background(),
		receiver,
		pid.PID{Host: "test", UniqID: "1"},
		"done",
	)
	if calls := receiver.calls.Load(); calls != 1 {
		t.Fatalf("expected one relay attempt, got %d", calls)
	}
}

func TestTerminalCompletionSkipsEndedProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	receiver := &terminalCompletionReceiver{}
	deliverTerminalCompletion(ctx, receiver, pid.PID{Host: "test", UniqID: "1"}, "done")
	if calls := receiver.calls.Load(); calls != 0 {
		t.Fatalf("completion sent after process end: %d calls", calls)
	}
}
