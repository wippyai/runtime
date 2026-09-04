// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"fmt"
	"sort"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	apiexec "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/security"
)

type Executor struct {
	resource resource.Resource[any]
	factory  apiexec.ProcessExecutor
	mu       sync.Mutex
	released bool
}

func NewExecutor(_ context.Context, res resource.Resource[any], factory apiexec.ProcessExecutor) *Executor {
	return &Executor{
		resource: res,
		factory:  factory,
		released: false,
	}
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
	res, execRes, err := rtresource.AcquireRegistryResource(ctx, reg, resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "acquire resource").WithKind(lua.Internal).WithRetryable(false))
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

	opts, err := parseProcessOptions(l.Get(3))
	if err != nil {
		pushInvalidOption(l, err.Error())
		return 2
	}

	if !security.IsAllowed(ctx, "exec.run", cmd, processSecurityMeta(opts)) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: execute command").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	proc, err := factory.NewProcess(cmd, opts)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(wrapExecError(l, err, "create process", lua.Internal))
		return 2
	}

	p := NewProcess(ctx, proc)

	value.PushTypedUserData(l, p, processTypeName)
	l.Push(lua.LNil)
	return 2
}

func processSecurityMeta(options apiexec.ProcessOptions) attrs.Bag {
	envNames := make([]string, 0, len(options.Env))
	for name := range options.Env {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)

	terminal := attrs.Bag{"requested": options.PTY != nil}
	if options.PTY != nil {
		width, height, _ := options.PTY.Dimensions()
		terminal["width"], terminal["height"], terminal["term"] = width, height, options.PTY.Term
	}
	return attrs.Bag{
		"work_dir":  options.WorkDir,
		"env_names": envNames,
		"pty":       terminal,
	}
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
		e.mu.Unlock()
	} else {
		e.mu.Unlock()
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
