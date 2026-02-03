package exec

import (
	"context"
	"fmt"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	apiexec "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
)

type Executor struct {
	resource      resource.Resource[any]
	factory       apiexec.ProcessExecutor
	cancelCleanup func()
	mu            sync.Mutex
	released      bool
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

var executorMethods = map[string]lua.LGoFunc{
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
		l.Push(lua.NewLuaError(l, "no context").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	id := l.CheckString(1)
	if id == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "resource id is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	if !security.IsAllowed(ctx, "exec.get", id, nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: access executor").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "resource registry not found").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	resID := registry.ParseID(id)
	res, err := reg.Acquire(ctx, resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "acquire resource").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	execRes, err := res.Get()
	if err != nil {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "get resource").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	factory, ok := execRes.(apiexec.ProcessExecutor)
	if !ok {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, fmt.Sprintf("resource is not an executor: %T", execRes)).WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	e := NewExecutor(ctx, res, factory)

	value.PushTypedUserData(l, e, executorTypeName)
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
		l.Push(lua.NewLuaError(l, "executor is released").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}
	factory := e.factory
	e.mu.Unlock()

	cmd := l.CheckString(2)
	if cmd == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "command is required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	if !security.IsAllowed(ctx, "exec.run", cmd, nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: execute command").WithKind(lua.Invalid).WithRetryable(false))
		return 2
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
		l.Push(lua.WrapErrorWithLua(l, err, "create process").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	p := NewProcess(ctx, proc)

	value.PushTypedUserData(l, p, processTypeName)
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
