// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	luatty "github.com/wippyai/runtime/runtime/lua/modules/tty"
	"github.com/wippyai/runtime/service/exec/native"
	"github.com/wippyai/runtime/service/terminal/proxy"
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

func TestTerminalSessionExportsPID(t *testing.T) {
	if terminalSessionMethods["pid"] == nil {
		t.Fatal("exec.TerminalSession pid method is missing")
	}
}

type terminalSessionIdentity struct {
	err error
	pid int
}

func (i terminalSessionIdentity) Pid() (int, error) { return i.pid, i.err }

func callTerminalSessionPID(t *testing.T, identity interface{ Pid() (int, error) }) (lua.LValue, lua.LValue) {
	t.Helper()
	l := lua.NewState()
	defer l.Close()
	value.PushTypedUserData(l, newTerminalSession(nil, nil, identity), terminalSessionTypeName)
	if returns := terminalSessionPID(l); returns != 2 {
		t.Fatalf("terminalSessionPID returned %d values, want 2", returns)
	}
	return l.Get(-2), l.Get(-1)
}

func TestTerminalSessionPID(t *testing.T) {
	pid, err := callTerminalSessionPID(t, terminalSessionIdentity{pid: 42})
	if got, ok := pid.(lua.LInteger); !ok || int(got) != 42 {
		t.Fatalf("pid = %v, want 42", pid)
	}
	if err != lua.LNil {
		t.Fatalf("error = %v, want nil", err)
	}
}

func TestTerminalSessionPIDUnavailable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	value.PushTypedUserData(l, newTerminalSession(nil, nil, nil), terminalSessionTypeName)

	if returns := terminalSessionPID(l); returns != 2 {
		t.Fatalf("terminalSessionPID returned %d values, want 2", returns)
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

func TestTerminalSessionPIDPreservesIdentityErrors(t *testing.T) {
	failure := errors.New("pid lookup failed")
	pid, err := callTerminalSessionPID(t, terminalSessionIdentity{err: failure})
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

func TestTerminalSessionPIDBeforeStart(t *testing.T) {
	pid, err := callTerminalSessionPID(t, terminalSessionIdentity{err: native.ErrProcessNotStarted})
	if pid != lua.LNil {
		t.Fatalf("pid = %v, want nil", pid)
	}
	luaErr, ok := err.(*lua.Error)
	if !ok {
		t.Fatalf("error = %v, want Lua error", err)
	}
	if luaErr.Kind() != lua.Internal || !strings.Contains(luaErr.Error(), "process not started") {
		t.Fatalf("error = %v (kind %v), want wrapped not-started error", luaErr, luaErr.Kind())
	}
}

type delayedTerminalProcess struct {
	startEntered chan struct{}
	startRelease chan struct{}
}

func (p *delayedTerminalProcess) Start() error {
	close(p.startEntered)
	<-p.startRelease
	return nil
}
func (*delayedTerminalProcess) Signal(int) error        { return nil }
func (*delayedTerminalProcess) WriteStdin([]byte) error { return nil }
func (*delayedTerminalProcess) Stdout() io.ReadCloser   { return io.NopCloser(strings.NewReader("")) }
func (*delayedTerminalProcess) Stderr() io.ReadCloser   { return nil }
func (*delayedTerminalProcess) Wait() error             { return nil }
func (*delayedTerminalProcess) Resize(int, int) error   { return nil }
func (*delayedTerminalProcess) Pid() (int, error)       { return 42, nil }

type terminalSessionSurface struct{}

func (*terminalSessionSurface) Present(ttyapi.Frame) (ttyapi.PresentStats, error) {
	return ttyapi.PresentStats{}, nil
}
func (*terminalSessionSurface) Invalidate()  {}
func (*terminalSessionSurface) Close() error { return nil }

func TestTerminalSessionPIDDoesNotBlockDuringAsyncStart(t *testing.T) {
	process := &delayedTerminalProcess{
		startEntered: make(chan struct{}),
		startRelease: make(chan struct{}),
	}
	bridge, err := proxy.New(process, &terminalSessionSurface{}, 1, 1)
	if err != nil {
		t.Fatalf("create terminal proxy: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- bridge.Run(context.Background(), make(chan ttyapi.Event)) }()
	<-process.startEntered

	l := lua.NewState()
	defer l.Close()
	value.PushTypedUserData(l, newTerminalSession(bridge, nil, process), terminalSessionTypeName)
	result := make(chan struct{})
	go func() {
		if returns := terminalSessionPID(l); returns != 2 {
			t.Errorf("terminalSessionPID returned %d values, want 2", returns)
		}
		close(result)
	}()
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("pid blocked while asynchronous process startup was in progress")
	}
	if l.Get(-2) != lua.LNil {
		t.Fatalf("pid = %v, want nil before startup completes", l.Get(-2))
	}
	if got, ok := l.Get(-1).(*lua.Error); !ok || got.Kind() != lua.Unavailable || got.Retryable() != lua.TernaryTrue {
		t.Fatalf("error = %v, want retryable unavailable not-started error", l.Get(-1))
	}

	close(process.startRelease)
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("proxy did not finish after startup release")
	}
}

func TestTerminalSessionPIDReportsCompletedStartupFailure(t *testing.T) {
	process := &delayedTerminalProcess{
		startEntered: make(chan struct{}),
		startRelease: make(chan struct{}),
	}
	bridge, err := proxy.New(process, &terminalSessionSurface{}, 1, 1)
	if err != nil {
		t.Fatalf("create terminal proxy: %v", err)
	}
	failure := errors.New("terminal startup failed")
	session := newTerminalSession(bridge, nil, process)
	session.err = failure
	session.done.Store(true)

	l := lua.NewState()
	defer l.Close()
	value.PushTypedUserData(l, session, terminalSessionTypeName)
	if returns := terminalSessionPID(l); returns != 2 {
		t.Fatalf("terminalSessionPID returned %d values, want 2", returns)
	}
	if l.Get(-2) != lua.LNil {
		t.Fatalf("pid = %v, want nil after startup failure", l.Get(-2))
	}
	luaErr, ok := l.Get(-1).(*lua.Error)
	if !ok || !errors.Is(luaErr, failure) || luaErr.Retryable() != lua.TernaryFalse {
		t.Fatalf("error = %v, want non-retryable startup failure", l.Get(-1))
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
