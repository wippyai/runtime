package exec

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	apiexec "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

type Executor struct {
	resource      resource.Resource[any]
	factory       apiexec.ProcessExecutor
	released      bool
	mu            sync.Mutex
	cancelCleanup func()
}

func NewExecutor(ctx context.Context, res resource.Resource[any], factory apiexec.ProcessExecutor) *Executor {
	e := &Executor{
		resource: res,
		factory:  factory,
		released: false,
	}

	store := rtresource.GetStore(ctx)
	if store != nil {
		e.cancelCleanup = store.AddCleanup(func() error {
			e.mu.Lock()
			defer e.mu.Unlock()
			if !e.released && e.resource != nil {
				e.resource.Release()
				e.released = true
			}
			return nil
		})
	}

	return e
}

var executorMethods = map[string]lua.LGFunction{
	"exec":    executorExec,
	"release": executorRelease,
}

func checkExecutor(l *lua.LState, idx int) *Executor {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Executor); ok {
		return v
	}
	l.ArgError(idx, "executor expected")
	return nil
}

func execGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	id := l.CheckString(1)
	if id == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource id is required"))
		return 2
	}

	if !security.IsAllowed(ctx, "exec.get", id, nil) {
		l.RaiseError("not allowed to access executor: %s", id)
		return 0
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource registry not found"))
		return 2
	}

	resID := registry.ParseID(id)
	res, err := reg.Acquire(ctx, resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to acquire resource: %v", err)))
		return 2
	}

	execRes, err := res.Get()
	if err != nil {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to get resource: %v", err)))
		return 2
	}

	factory, ok := execRes.(apiexec.ProcessExecutor)
	if !ok {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("resource is not an executor: %T", execRes)))
		return 2
	}

	e := NewExecutor(ctx, res, factory)

	value.PushUserData(l, e, executorMetatable)
	l.Push(lua.LNil)
	return 2
}

func executorExec(l *lua.LState) int {
	e := checkExecutor(l, 1)
	if e == nil {
		return 0
	}
	ctx := l.Context()

	e.mu.Lock()
	if e.released {
		e.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("executor is released"))
		return 2
	}
	factory := e.factory
	e.mu.Unlock()

	cmd := l.CheckString(2)
	if cmd == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("command is required"))
		return 2
	}

	if !security.IsAllowed(ctx, "exec.run", cmd, nil) {
		l.RaiseError("not allowed to execute command: %s", cmd)
		return 0
	}

	opts := apiexec.ProcessOptions{}
	if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTTable {
		optsTable := l.CheckTable(3)

		if wd := optsTable.RawGetString("work_dir"); wd != lua.LNil {
			if wdStr, ok := wd.(lua.LString); ok {
				opts.WorkDir = string(wdStr)
			}
		}

		if envTable := optsTable.RawGetString("env"); envTable != lua.LNil {
			if envT, ok := envTable.(*lua.LTable); ok {
				opts.Env = make(map[string]string)
				envT.ForEach(func(k, v lua.LValue) {
					opts.Env[k.String()] = v.String()
				})
			}
		}
	}

	proc, err := factory.NewProcess(cmd, opts)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create process: %v", err)))
		return 2
	}

	p := NewProcess(ctx, proc)

	value.PushUserData(l, p, processMetatable)
	l.Push(lua.LNil)
	return 2
}

func executorRelease(l *lua.LState) int {
	e := checkExecutor(l, 1)
	if e == nil {
		return 0
	}
	e.mu.Lock()
	if !e.released && e.resource != nil {
		e.resource.Release()
		e.resource = nil
		e.released = true
		cancel := e.cancelCleanup
		e.cancelCleanup = nil
		e.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	} else {
		e.mu.Unlock()
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func executorToString(l *lua.LState) int {
	e := checkExecutor(l, 1)
	if e == nil {
		return 0
	}
	e.mu.Lock()
	released := e.released
	e.mu.Unlock()

	if released {
		l.Push(lua.LString("exec.Executor{released}"))
	} else {
		l.Push(lua.LString("exec.Executor{}"))
	}
	return 1
}
