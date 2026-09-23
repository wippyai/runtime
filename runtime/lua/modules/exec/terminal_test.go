// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	execapi "github.com/wippyai/runtime/api/service/exec"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
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
		&terminalResult{},
	)
	if !receiver.terminal.Load() {
		t.Fatal("completion did not carry terminal subscription marker")
	}
}

func TestTerminalResultKeepsExitAndTerminalErrorSeparate(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	failure := errors.New("terminal presentation failed")
	result := terminalCompletionHandler(context.Background(), l, pid.PID{}, "done", []payload.Payload{
		payload.New(&terminalResult{
			Exit:          &execapi.ExitStatus{Code: 7},
			TerminalError: failure,
		}),
	})
	out, ok := result.(*lua.LTable)
	if !ok {
		t.Fatalf("result = %T, want table", result)
	}
	exit, ok := out.RawGetString("exit").(*lua.LTable)
	if !ok || exit.RawGetString("code") != lua.LInteger(7) {
		t.Fatalf("exit = %v, want code 7", out.RawGetString("exit"))
	}
	terminalErr, ok := out.RawGetString("terminal_error").(*lua.Error)
	if !ok || !errors.Is(terminalErr, failure) {
		t.Fatalf("terminal_error = %v, want presentation failure", out.RawGetString("terminal_error"))
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

func TestOnlyExecutorExportsTerminalConstructor(t *testing.T) {
	if executorMethods["terminal"] == nil {
		t.Fatal("exec.Executor terminal method is missing")
	}
	if processMethods["attach_terminal"] != nil {
		t.Fatal("exec.Process still exports attach_terminal")
	}
}

func TestTerminalProcessExportsPID(t *testing.T) {
	if terminalProcessMethods["pid"] == nil {
		t.Fatal("exec.TerminalProcess pid method is missing")
	}
}

type terminalProcessIdentity struct {
	err error
	pid int
}

func (i terminalProcessIdentity) Pid() (int, error) { return i.pid, i.err }

func callTerminalProcessPID(t *testing.T, identity interface{ Pid() (int, error) }) (lua.LValue, lua.LValue) {
	t.Helper()
	l := lua.NewState()
	defer l.Close()
	value.PushTypedUserData(l, newTerminalProcess(nil, nil, identity), terminalProcessTypeName)
	if returns := terminalProcessPID(l); returns != 2 {
		t.Fatalf("terminalProcessPID returned %d values, want 2", returns)
	}
	return l.Get(-2), l.Get(-1)
}

func TestTerminalProcessPID(t *testing.T) {
	pid, err := callTerminalProcessPID(t, terminalProcessIdentity{pid: 42})
	if got, ok := pid.(lua.LInteger); !ok || int(got) != 42 {
		t.Fatalf("pid = %v, want 42", pid)
	}
	if err != lua.LNil {
		t.Fatalf("error = %v, want nil", err)
	}
}

func TestTerminalProcessPIDUnavailable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	value.PushTypedUserData(l, newTerminalProcess(nil, nil, nil), terminalProcessTypeName)

	if returns := terminalProcessPID(l); returns != 2 {
		t.Fatalf("terminalProcessPID returned %d values, want 2", returns)
	}
	if l.Get(-2) != lua.LNil {
		t.Fatalf("pid = %v, want nil", l.Get(-2))
	}
	err, ok := l.Get(-1).(*lua.Error)
	if !ok {
		t.Fatalf("error = %v, want Lua error", l.Get(-1))
	}
	if err.Kind() != lua.Unavailable || err.Error() != "process has no host process id" {
		t.Fatalf("error = %v (kind %v), want unavailable host process id", err, err.Kind())
	}
}

func TestTerminalProcessPIDPreservesIdentityErrors(t *testing.T) {
	failure := errors.New("pid lookup failed")
	pid, err := callTerminalProcessPID(t, terminalProcessIdentity{err: failure})
	if pid != lua.LNil {
		t.Fatalf("pid = %v, want nil", pid)
	}
	luaErr, ok := err.(*lua.Error)
	if !ok {
		t.Fatalf("error = %v, want Lua error", err)
	}
	if !errors.Is(luaErr, failure) || !strings.Contains(luaErr.Error(), "read process id") {
		t.Fatalf("error = %v, want wrapped PID failure", luaErr)
	}
}

func TestTerminalCompletionDoesNotRetryRelayFailure(t *testing.T) {
	receiver := &terminalCompletionReceiver{failures: 1}
	deliverTerminalCompletion(
		context.Background(),
		receiver,
		pid.PID{Host: "test", UniqID: "1"},
		"done",
		&terminalResult{},
	)
	if calls := receiver.calls.Load(); calls != 1 {
		t.Fatalf("expected one relay attempt, got %d", calls)
	}
}

func TestTerminalCompletionSkipsEndedProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	receiver := &terminalCompletionReceiver{}
	deliverTerminalCompletion(ctx, receiver, pid.PID{Host: "test", UniqID: "1"}, "done", &terminalResult{})
	if calls := receiver.calls.Load(); calls != 0 {
		t.Fatalf("completion sent after process end: %d calls", calls)
	}
}
