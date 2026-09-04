// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"errors"
	"sync"
	"syscall"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	apiexec "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	fsstream "github.com/wippyai/runtime/system/stream"
)

var errPTYOwnership = errors.New("PTY process is unavailable or already owned")

type Process struct {
	handle        apiexec.Process
	cancelCleanup func()
	stdoutID      uint64
	stderrID      uint64
	mu            sync.Mutex
	started       bool
	closed        bool
}

func NewProcess(ctx context.Context, handle apiexec.Process) *Process {
	p := &Process{
		handle: handle,
		closed: false,
	}

	store := resource.GetStore(ctx)
	if store != nil {
		p.cancelCleanup = store.AddCleanup(func() error {
			p.mu.Lock()
			defer p.mu.Unlock()
			if !p.closed && p.handle != nil {
				p.closed = true
				return p.handle.Signal(int(syscall.SIGTERM))
			}
			return nil
		})
	}

	return p
}

// takePTYProcess transfers an unstarted PTY process out of its Lua exec
// handle. The attached terminal session becomes its sole lifecycle owner.
func takePTYProcess(value lua.LValue) (apiexec.PTYProcess, error) {
	ud, ok := value.(*lua.LUserData)
	if !ok {
		return nil, errPTYOwnership
	}
	p, ok := ud.Value.(*Process)
	if !ok {
		return nil, errPTYOwnership
	}
	p.mu.Lock()
	if p.closed || p.started || p.handle == nil {
		p.mu.Unlock()
		return nil, errPTYOwnership
	}
	ptyProcess, ok := p.handle.(apiexec.PTYProcess)
	if !ok {
		p.mu.Unlock()
		return nil, apiexec.ErrPTYUnavailable
	}
	p.closed, p.handle = true, nil
	cancel := p.cancelCleanup
	p.cancelCleanup = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return ptyProcess, nil
}

// reapGrace bounds how long a released process may take to exit before it is
// killed. It only applies to a child that ignores SIGTERM; one that exits
// normally is reaped as soon as it does.
const reapGrace = 10 * time.Second

// reapReleased waits on a process whose handle the caller has given up.
//
// Wait is the only call that releases the child's entry in the OS process
// table. close() deliberately puts the handle beyond the caller's reach, so
// unless the reap happens here it never happens at all: the child is signaled,
// exits, and remains a zombie for the lifetime of the runtime -- one per
// close(), accumulating without bound.
//
// It runs detached because close() must not block a Lua coroutine on a child
// that is slow to exit, and it escalates because a goroutine parked forever on
// an unresponsive child would trade a process leak for a goroutine leak. A
// child still alive after the grace period is killed, which Wait then observes.
//
// Wait closes the stdout and stderr pipes it created. That is inherent to
// reaping and is the documented consequence of close(): the process is finished
// with, and its streams along with it.
func reapReleased(handle apiexec.Process, killed bool) {
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		_ = handle.Wait()
	}()

	if !killed {
		select {
		case <-exited:
			return
		case <-time.After(reapGrace):
			_ = handle.Signal(int(syscall.SIGKILL))
		}
	}

	select {
	case <-exited:
	case <-time.After(reapGrace):
		if canceler, ok := handle.(apiexec.WaitCanceler); ok {
			canceler.CancelWait()
		}
	}
}

var processMethods = map[string]lua.LGoFunc{
	"start":           procStart,
	"wait":            procWait,
	"signal":          procSignal,
	"write_stdin":     procWriteStdin,
	"stdout_stream":   procStdout,
	"stderr_stream":   procStderr,
	"close":           procClose,
	"resize":          procResize,
	"attach_terminal": procAttachTerminal,
}

func procResize(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}
	p.mu.Lock()
	handle := p.handle
	closed := p.closed
	p.mu.Unlock()
	if closed || handle == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process is closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	ptyProcess, ok := handle.(apiexec.PTYProcess)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process has no PTY").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	width, height := l.CheckInt(2), l.CheckInt(3)
	if err := apiexec.ValidatePTYSize(width, height); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "resize process PTY").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	if err := ptyProcess.Resize(width, height); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "resize process PTY"))
		return 2
	}
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func checkProcess(l *lua.LState, _ int) *Process {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Process); ok {
		return v
	}
	l.ArgError(1, "process expected")
	return nil
}

func procStart(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process is closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	if p.started {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process already started").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	handle := p.handle
	p.started = true
	p.mu.Unlock()

	err := handle.Start()
	if err != nil {
		p.mu.Lock()
		p.closed = true
		p.handle = nil
		cancel := p.cancelCleanup
		p.cancelCleanup = nil
		p.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "start process").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func procWait(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process is closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	if !p.started {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process not started: call start() first").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	handle := p.handle
	p.closed = true
	p.handle = nil
	cancel := p.cancelCleanup
	p.cancelCleanup = nil
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	yield := AcquireProcessWaitYield()
	yield.Process = handle

	l.Push(yield)
	return -1
}

func procSignal(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process is closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	if !p.started {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process not started: call start() first").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	handle := p.handle
	p.mu.Unlock()

	sig := l.CheckInt(2)
	err := handle.Signal(sig)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "send signal").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func procWriteStdin(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process is closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	if !p.started {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process not started: call start() first").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	handle := p.handle
	p.mu.Unlock()

	data := l.CheckString(2)
	err := handle.WriteStdin([]byte(data))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "write stdin").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func procStdout(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}
	ctx := l.Context()

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process is closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	handle := p.handle
	cachedID := p.stdoutID
	p.mu.Unlock()

	if cachedID != 0 {
		l.Push(stream.NewStream(l, cachedID))
		l.Push(lua.LNil)
		return 2
	}

	reader := handle.Stdout()
	if reader == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "stdout not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	table := resource.GetTable(ctx)
	if table == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "resource table not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	streamID := fsstream.Insert(table, reader)

	p.mu.Lock()
	p.stdoutID = streamID
	p.mu.Unlock()

	l.Push(stream.NewStream(l, streamID))
	l.Push(lua.LNil)
	return 2
}

func procStderr(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}
	ctx := l.Context()

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "process is closed").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	handle := p.handle
	cachedID := p.stderrID
	p.mu.Unlock()

	if cachedID != 0 {
		l.Push(stream.NewStream(l, cachedID))
		l.Push(lua.LNil)
		return 2
	}

	reader := handle.Stderr()
	if reader == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "stderr not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	table := resource.GetTable(ctx)
	if table == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "resource table not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	streamID := fsstream.Insert(table, reader)

	p.mu.Lock()
	p.stderrID = streamID
	p.mu.Unlock()

	l.Push(stream.NewStream(l, streamID))
	l.Push(lua.LNil)
	return 2
}

func procClose(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}
	forceStop := false
	if l.GetTop() >= 2 {
		forceStop = l.ToBool(2)
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		l.Push(lua.LTrue)
		l.Push(lua.LNil)
		return 2
	}
	p.closed = true
	handle := p.handle
	p.handle = nil
	started := p.started
	cancel := p.cancelCleanup
	p.cancelCleanup = nil
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if handle != nil {
		sig := syscall.SIGTERM
		if forceStop {
			sig = syscall.SIGKILL
		}
		_ = handle.Signal(int(sig))

		// Only a started process has an OS child to reap; waiting on one that
		// never ran would just report that. Signaling is left unconditional so
		// close() behaves exactly as it did before.
		if started {
			go reapReleased(handle, forceStop)
		}
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
