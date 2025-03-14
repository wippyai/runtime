package process

import (
	"github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua2 "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// ProcessWithContext represents a process spawner with context values
type ProcessWithContext struct {
	module *Module
	values *context.Contexter[interface{}]
}

// withContext creates a new process spawner with context values
func (m *Module) withContext(l *lua.LState) int {
	// Create new contexter
	values := context.NewContexter[interface{}]()

	// Check if this is a chained call on an existing ProcessWithContext
	if l.GetTop() >= 1 && l.Get(1).Type() == lua.LTUserData {
		ud := l.CheckUserData(1)
		if spawner, ok := ud.Value.(*ProcessWithContext); ok {
			// Copy existing values to the new contexter
			if spawner.values != nil {
				spawner.values.Iterate(func(key string, value interface{}) {
					values.WithValue(key, value)
				})
			}

			// Get context table from second argument
			ctxTable := l.CheckTable(2)

			// Add new values from table
			ctxTable.ForEach(func(k, v lua.LValue) {
				key, ok := k.(lua.LString)
				if !ok {
					l.ArgError(2, "context keys must be strings")
					return
				}
				values.WithValue(string(key), lua2.ToGoAny(v))
			})
		} else {
			l.ArgError(1, "process spawner or module expected")
			return 0
		}
	} else {
		// First call - get context table from first argument
		ctxTable := l.CheckTable(1)

		// Add values from table
		ctxTable.ForEach(func(k, v lua.LValue) {
			key, ok := k.(lua.LString)
			if !ok {
				l.ArgError(1, "context keys must be strings")
				return
			}
			values.WithValue(string(key), v)
		})
	}

	// Create new spawner with context
	spawner := &ProcessWithContext{
		module: m,
		values: values,
	}

	// Create userdata with metatable for method chaining
	ud := l.NewUserData()
	ud.Value = spawner
	ud.Metatable = value.GetTypeMetatable(l, "process.WithContext")

	l.Push(ud)
	return 1
}

// contextSpawn spawns a process with context values
func (m *Module) contextSpawn(l *lua.LState) int {
	// Get spawner from userdata
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*ProcessWithContext)
	if !ok {
		l.ArgError(1, "process spawner expected")
		return 0
	}

	// Get arguments
	if l.GetTop() < 3 {
		l.RaiseError("spawn requires at least id and host arguments")
		return 0
	}

	id := l.CheckString(2)
	hostID := l.CheckString(3)

	// Get unit of work
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work context found")
		return 0
	}

	// Get process manager
	manager := process.GetProcesses(ctx)
	if manager == nil {
		l.RaiseError("no process manager found")
		return 0
	}

	// Get PID
	self, ok := pubsub.GetPID(ctx)
	if !ok {
		l.RaiseError("no PID found in context")
		return 0
	}

	// Create payloads from remaining args
	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	// Create start configuration
	start := &process.Start{
		HostID: hostID,
		Source: registry.ParseID(id),
		Input:  payloads,
		Lifecycle: process.Lifecycle{
			Parent:  self,
			Monitor: false,
			Link:    false,
		},
	}

	// Start the process with our context
	pid, err := manager.Start(ctx, start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Log the operation
	spawner.module.log.Debug("process spawned with context",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
		zap.Int("num_args", len(payloads)),
	)

	// Return PID string
	l.Push(lua.LString(pid.String()))
	return 1
}
