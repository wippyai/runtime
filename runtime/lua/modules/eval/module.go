package eval

import (
	"context"
	"fmt"
	"sync"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

const (
	PermissionEvalRun     = "eval.run"
	PermissionEvalCompile = "eval.compile"
	ProgramTypeName       = "eval.Program"
)

type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

// Program represents a compiled Lua program that can be executed multiple times.
// The runner and config fields are immutable after creation.
// The timeout field can be modified via set_timeout() and is protected by mu.
type Program struct {
	runner  *engine.Runner // immutable
	config  *evalConfig    // immutable
	timeout time.Duration  // mutable, protected by mu
	mu      sync.RWMutex
}

// NewEvalModule creates a new eval module instance.
func NewEvalModule() *Module {
	return &Module{}
}

// Name returns the module name.
func (m *Module) Name() string {
	return "eval"
}

// Loader implements luaapi.Module interface.
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

func (m *Module) initModuleTable(l *lua.LState) {
	value.RegisterMethods(l, ProgramTypeName, map[string]lua.LGFunction{
		"run":         m.programRun,
		"set_timeout": m.programSetTimeout,
	})

	t := l.CreateTable(0, 2)
	t.RawSetString("compile", l.NewFunction(m.compile))
	t.RawSetString("run", l.NewFunction(m.run))

	t.Immutable = true

	m.moduleTable = t
}

func (m *Module) compile(l *lua.LState) int {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work context found"))
		return 2
	}

	if !security.IsAllowed(uw.Context(), PermissionEvalCompile, "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied: eval.compile"))
		return 2
	}

	source := l.CheckString(1)
	if source == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("source is required"))
		return 2
	}

	method := l.CheckString(2)
	if method == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("method is required"))
		return 2
	}

	config := &evalConfig{
		Source:  source,
		Method:  method,
		Modules: []string{},
		Imports: map[string]string{},
	}

	if l.GetTop() >= 3 {
		configTable := l.CheckTable(3)

		if modules := configTable.RawGetString("modules"); modules != lua.LNil {
			if modulesTable, ok := modules.(*lua.LTable); ok {
				modulesTable.ForEach(func(_, v lua.LValue) {
					if modStr, ok := v.(lua.LString); ok {
						config.Modules = append(config.Modules, string(modStr))
					}
				})
			}
		}

		if imports := configTable.RawGetString("imports"); imports != lua.LNil {
			if importsTable, ok := imports.(*lua.LTable); ok {
				importsTable.ForEach(func(k, v lua.LValue) {
					if kStr, kOk := k.(lua.LString); kOk {
						if vStr, vOk := v.(lua.LString); vOk {
							config.Imports[string(kStr)] = string(vStr)
						}
					}
				})
			}
		}
	}

	runner, err := buildIsolatedRunner(uw.Context(), config)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to build runner: %v", err)))
		return 2
	}

	program := &Program{
		runner: runner,
		config: config,
	}

	ud := l.NewUserData()
	ud.Value = program
	ud.Metatable = value.GetTypeMetatable(l, ProgramTypeName)
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// CheckProgram checks that the value at index n is an eval.Program and returns it.
func CheckProgram(l *lua.LState, n int) *Program {
	ud := l.CheckUserData(n)
	if program, ok := ud.Value.(*Program); ok {
		return program
	}
	l.ArgError(n, "eval.Program expected")
	return nil
}

func (m *Module) programSetTimeout(l *lua.LState) int {
	program := CheckProgram(l, 1)
	if program == nil {
		return 0
	}

	timeoutStr := l.CheckString(2)
	duration, err := time.ParseDuration(timeoutStr)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(fmt.Sprintf("invalid timeout duration: %v", err)))
		return 2
	}

	program.mu.Lock()
	program.timeout = duration
	program.mu.Unlock()

	return 0
}

func (m *Module) programRun(l *lua.LState) int {
	program := CheckProgram(l, 1)
	if program == nil {
		return 0
	}

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work context found"))
		return 2
	}

	method := l.CheckString(2)

	top := l.GetTop()
	args := make([]lua.LValue, 0, top-2)
	for i := 3; i <= top; i++ {
		args = append(args, l.Get(i))
	}

	program.mu.RLock()
	timeout := program.timeout
	runner := program.runner
	program.mu.RUnlock()

	ctx := uw.Context()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	coroutine.Wrap(l, func() *engine.Update {
		defer cancel()

		frameCtx, _ := ctxapi.OpenFrameContext(ctx)

		result, err := runner.Execute(frameCtx, method, args...)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("execution failed: %v", err))}, nil)
		}

		if result == nil || result == lua.LNil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LNil}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{result, lua.LNil}, nil)
	})

	return -1
}

func (m *Module) run(l *lua.LState) int {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work context found"))
		return 2
	}

	if !security.IsAllowed(uw.Context(), PermissionEvalRun, "", nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("permission denied: eval.run"))
		return 2
	}

	configTable := l.CheckTable(1)

	source := configTable.RawGetString("source")
	if source == lua.LNil {
		l.Push(lua.LNil)
		l.Push(lua.LString("source is required"))
		return 2
	}
	sourceStr, ok := source.(lua.LString)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("source must be a string"))
		return 2
	}

	method := "main"
	if methodVal := configTable.RawGetString("method"); methodVal != lua.LNil {
		if methodStr, ok := methodVal.(lua.LString); ok {
			method = string(methodStr)
		}
	}

	config := &evalConfig{
		Source:  string(sourceStr),
		Method:  method,
		Modules: []string{},
		Imports: map[string]string{},
	}

	if modules := configTable.RawGetString("modules"); modules != lua.LNil {
		if modulesTable, ok := modules.(*lua.LTable); ok {
			modulesTable.ForEach(func(_, v lua.LValue) {
				if modStr, ok := v.(lua.LString); ok {
					config.Modules = append(config.Modules, string(modStr))
				}
			})
		}
	}

	if imports := configTable.RawGetString("imports"); imports != lua.LNil {
		if importsTable, ok := imports.(*lua.LTable); ok {
			importsTable.ForEach(func(k, v lua.LValue) {
				if kStr, kOk := k.(lua.LString); kOk {
					if vStr, vOk := v.(lua.LString); vOk {
						config.Imports[string(kStr)] = string(vStr)
					}
				}
			})
		}
	}

	var args []lua.LValue
	if argsVal := configTable.RawGetString("args"); argsVal != lua.LNil {
		if argsTable, ok := argsVal.(*lua.LTable); ok {
			argsTable.ForEach(func(_, v lua.LValue) {
				args = append(args, v)
			})
		}
	}

	ctx := uw.Context()
	ctx, cancel := context.WithCancel(ctx)

	coroutine.Wrap(l, func() *engine.Update {
		defer cancel()

		runner, err := buildIsolatedRunner(ctx, config)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("failed to build runner: %v", err))}, nil)
		}
		defer runner.Close()

		frameCtx, _ := ctxapi.OpenFrameContext(ctx)

		result, err := runner.Execute(frameCtx, method, args...)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("execution failed: %v", err))}, nil)
		}

		if result == nil || result == lua.LNil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LNil}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{result, lua.LNil}, nil)
	})

	return -1
}

// evalConfig holds configuration for building an isolated Lua runner.
type evalConfig struct {
	Source  string            // Lua source code to compile
	Method  string            // Method name to export
	Modules []string          // Module names to load
	Imports map[string]string // Import aliases mapping to registry IDs
}
