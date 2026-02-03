package exec

import (
	"context"
	"sync"
	"syscall"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	apiexec "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"
	fsstream "github.com/wippyai/runtime/system/stream"
)

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

var processMethods = map[string]lua.LGoFunc{
	"start":         procStart,
	"wait":          procWait,
	"signal":        procSignal,
	"write_stdin":   procWriteStdin,
	"stdout_stream": procStdout,
	"stderr_stream": procStderr,
	"close":         procClose,
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
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
