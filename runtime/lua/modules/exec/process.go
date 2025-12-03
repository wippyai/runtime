package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"

	"github.com/wippyai/runtime/api/runtime/resource"
	apiexec "github.com/wippyai/runtime/api/service/exec"
	lua "github.com/yuin/gopher-lua"
)

type Process struct {
	handle        apiexec.Process
	closed        bool
	mu            sync.Mutex
	cancelCleanup func()
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

var processMethods = map[string]lua.LGFunction{
	"start":         procStart,
	"wait":          procWait,
	"signal":        procSignal,
	"write_stdin":   procWriteStdin,
	"stdout_stream": procStdout,
	"stderr_stream": procStderr,
	"close":         procClose,
}

func checkProcess(l *lua.LState, idx int) *Process {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Process); ok {
		return v
	}
	l.ArgError(idx, "process expected")
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
		l.Push(lua.LString("process is closed"))
		return 2
	}
	handle := p.handle
	p.mu.Unlock()

	err := handle.Start()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to start: %v", err)))
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
		l.Push(lua.LString("process is closed"))
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
		l.Push(lua.LString("process is closed"))
		return 2
	}
	handle := p.handle
	p.mu.Unlock()

	sig := l.CheckInt(2)
	err := handle.Signal(sig)
	if err != nil {
		errMsg := fmt.Sprintf("failed to send signal %d: %v", sig, err)
		if errors.Is(err, os.ErrProcessDone) || strings.Contains(err.Error(), "process already finished") {
			errMsg = fmt.Sprintf("failed to send signal %d (process already finished)", sig)
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(errMsg))
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
		l.Push(lua.LString("process is closed"))
		return 2
	}
	handle := p.handle
	p.mu.Unlock()

	data := l.CheckString(2)
	err := handle.WriteStdin([]byte(data))
	if err != nil {
		errMsg := fmt.Sprintf("failed to write to stdin: %v", err)
		if errors.Is(err, io.ErrClosedPipe) || strings.Contains(err.Error(), "process is not running") {
			errMsg = "failed to write to stdin (pipe closed or process not running)"
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(errMsg))
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
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("process is closed"))
		return 2
	}
	handle := p.handle
	p.mu.Unlock()

	reader := handle.Stdout()
	if reader == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("stdout not available"))
		return 2
	}

	ud := l.NewUserData()
	ud.Value = reader
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func procStderr(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("process is closed"))
		return 2
	}
	handle := p.handle
	p.mu.Unlock()

	reader := handle.Stderr()
	if reader == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("stderr not available"))
		return 2
	}

	ud := l.NewUserData()
	ud.Value = reader
	l.Push(ud)
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

func processToString(l *lua.LState) int {
	p := checkProcess(l, 1)
	if p == nil {
		return 0
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()

	if closed {
		l.Push(lua.LString("exec.Process{closed}"))
	} else {
		l.Push(lua.LString("exec.Process{}"))
	}
	return 1
}
