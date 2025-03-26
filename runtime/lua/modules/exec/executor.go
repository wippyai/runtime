package exec

import (
	"context"
	"fmt"
	"sync" // Import sync

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	apiexec "github.com/ponyruntime/pony/api/service/exec"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Executor wraps an acquired ProcessExecutor factory resource for Lua
type Executor struct {
	log       *zap.Logger
	mu        sync.Mutex
	resource  resource.Resource[any]
	factory   apiexec.ProcessExecutor
	onRelease context.CancelFunc // UoW cleanup cancel func for the factory handle
}

// NewExecutor creates a new Executor wrapper with UoW integration for the factory handle
func NewExecutor(uw engine.UnitOfWork, res resource.Resource[any], factory apiexec.ProcessExecutor, log *zap.Logger) *Executor {
	wrapper := &Executor{
		resource: res,
		factory:  factory,
		log:      log,
	}

	wrapper.onRelease = uw.AddCleanup(func() error {
		wrapper.log.Debug("UoW cleanup: Releasing executor factory resource handle")
		wrapper.mu.Lock()
		resHandle := wrapper.resource
		wrapper.resource = nil
		wrapper.factory = nil
		wrapper.mu.Unlock()

		if resHandle != nil {
			resHandle.Release()
			wrapper.log.Debug("UoW cleanup: Executor factory resource handle released")
		}
		return nil
	})
	wrapper.log.Debug("Created Executor factory wrapper and registered UoW cleanup")
	return wrapper
}

// CheckExecutor checks if the Lua argument is a valid, non-released Executor userdata
func CheckExecutor(l *lua.LState, n int) *Executor {
	ud := l.CheckUserData(n)
	execWrapper, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(n, "expected executor object")
		return nil
	}

	execWrapper.mu.Lock()
	valid := execWrapper.resource != nil && execWrapper.factory != nil
	execWrapper.mu.Unlock()

	if !valid {
		l.RaiseError("executor has been released")
		return nil
	}
	return execWrapper
}

// WrapExecutor wraps an Executor struct as Lua userdata
func WrapExecutor(l *lua.LState, execWrapper *Executor) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = execWrapper
	l.SetMetatable(ud, value.GetTypeMetatable(l, executorMetatable))
	return ud
}

// --- Lua Functions ---

// execGet (Lua: exec.get(id)) acquires a process executor factory resource
func execGet(l *lua.LState, log *zap.Logger) int {
	idStr := l.CheckString(1)
	if idStr == "" {
		l.RaiseError("resource ID is required")
		return 0
	}
	log = log.With(zap.String("id", idStr))
	log.Debug("Acquiring process executor factory resource")

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work found in context")
		return 0
	}

	reg := resource.GetResources(uw.Context())
	if reg == nil {
		l.RaiseError("resource registry not found in context")
		return 0
	}

	resID := registry.ParseID(idStr)
	res, err := reg.Acquire(uw.Context(), resID, resource.ModeNormal)
	if err != nil {
		log.Error("Failed to acquire resource", zap.Error(err))
		l.RaiseError("failed to acquire resource '%s': %v", idStr, err)
		return 0
	}
	log.Debug("Successfully acquired resource handle")

	execInstance, err := res.Get()
	if err != nil {
		log.Error("Failed to get underlying factory from resource", zap.Error(err))
		res.Release()
		l.RaiseError("failed to get executor factory from resource '%s': %v", idStr, err)
		return 0
	}

	factory, ok := execInstance.(apiexec.ProcessExecutor)
	if !ok {
		log.Error("Resource is not a process executor factory", zap.Any("type", fmt.Sprintf("%T", execInstance)))
		res.Release()
		l.RaiseError("resource '%s' is not a process executor factory: %T", idStr, execInstance)
		return 0
	}

	execWrapper := NewExecutor(uw, res, factory, log)
	ud := WrapExecutor(l, execWrapper)
	l.Push(ud)
	return 1
}

// executorNewProcess (Lua: executor:new_process(cmd, opts_table))
func executorNewProcess(l *lua.LState) int {
	execWrapper := CheckExecutor(l, 1)
	if execWrapper == nil {
		return 0
	}
	cmd := l.CheckString(2)
	optsTable := l.OptTable(3, l.CreateTable(0, 0))

	execWrapper.log.Debug("Lua calling executor:new_process()", zap.String("cmd", cmd))

	procOpts := apiexec.ProcessOptions{}
	wd := optsTable.RawGetString("work_dir")
	if wdStr, ok := wd.(lua.LString); ok {
		procOpts.WorkDir = string(wdStr)
	}
	envTable := optsTable.RawGetString("env")
	if envT, ok := envTable.(*lua.LTable); ok {
		procOpts.Env = make(map[string]string)
		envT.ForEach(func(k lua.LValue, v lua.LValue) {
			procOpts.Env[k.String()] = v.String()
		})
	}

	execWrapper.mu.Lock()
	if execWrapper.factory == nil {
		execWrapper.mu.Unlock()
		l.RaiseError("executor has been released")
		return 0
	}
	factory := execWrapper.factory
	execWrapper.mu.Unlock()

	// *** This is where the actual apiexec.Process is created ***
	processHandle, err := factory.NewProcess(cmd, procOpts)
	if err != nil {
		execWrapper.log.Error("Failed to create new process via factory", zap.Error(err))
		l.RaiseError("failed to create process: %v", err)
		return 0
	}
	execWrapper.log.Info("Created new process handle", zap.String("cmd", cmd))

	// Wrap the returned apiexec.Process handle in its own userdata
	ud := WrapProcess(l, processHandle, execWrapper.log) // Pass logger down
	l.Push(ud)
	return 1
}

// executorRelease (Lua: executor:release()) - Releases the factory resource handle
func executorRelease(l *lua.LState) int {
	ud := l.CheckUserData(1)
	execWrapper, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "expected executor object")
		return 0
	}
	execWrapper.log.Debug("Lua calling executor:release()")

	execWrapper.mu.Lock()
	if execWrapper.resource == nil {
		execWrapper.mu.Unlock()
		execWrapper.log.Debug("Executor:release() called on already released handle")
		l.Push(lua.LTrue)
		return 1
	}
	onRelease := execWrapper.onRelease
	execWrapper.resource = nil
	execWrapper.factory = nil
	execWrapper.onRelease = nil
	execWrapper.mu.Unlock()

	// var releaseErr error // Removed unused variable
	if onRelease != nil {
		execWrapper.log.Debug("Executor:release() executing explicit release via onRelease")
		// Call context.CancelFunc without arguments
		onRelease()
	}

	execWrapper.log.Info("Executor factory resource handle released explicitly")
	l.Push(lua.LTrue)
	return 1
}
