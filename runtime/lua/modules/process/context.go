package process

import (
	contextbase "context"
	"github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// WithContext represents a process spawner with context values
type WithContext struct {
	module *Module
	values *context.Contexter[any]
}

// withContext creates a new process spawner with context values
func (m *Module) withContext(l *lua.LState) int {
	// Check if this is a chained call on an existing WithContext
	if l.GetTop() >= 1 && l.Get(1).Type() == lua.LTUserData {
		ud := l.CheckUserData(1)
		spawner, ok := ud.Value.(*WithContext)
		if !ok {
			l.ArgError(1, "process spawner expected")
			return 0
		}

		// Create new contexter and copy existing values
		newValues := context.NewContexter[any]()
		if spawner.values != nil {
			spawner.values.Iterate(func(key string, value any) {
				newValues.SetValue(key, value)
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
			newValues.SetValue(string(key), luaconv.ToGoAny(v))
		})

		// Create new spawner with merged context
		newSpawner := &WithContext{
			module: m,
			values: newValues,
		}

		// Create userdata with the new spawner
		newUd := l.NewUserData()
		newUd.Value = newSpawner
		newUd.Metatable = value.GetTypeMetatable(l, "process.WithContext")
		l.Push(newUd)
		return 1
	}

	// First call - create a new contexter
	values := context.NewContexter[any]()

	// Get context table from first argument
	ctxTable := l.CheckTable(1)

	// Add values from table
	ctxTable.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			l.ArgError(1, "context keys must be strings")
			return
		}
		values.SetValue(string(key), luaconv.ToGoAny(v))
	})

	// Create new spawner with context
	spawner := &WithContext{
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

// applyContextToProcess applies context values from a Contexter to a process context
func applyContextToProcess(ctx contextbase.Context, contextValues *context.Contexter[any]) contextbase.Context {
	if contextValues == nil || contextValues.Len() == 0 {
		return ctx
	}

	// todo: implement
	return ctx
}

// contextSpawn spawns a process with context values
func (m *Module) contextSpawn(l *lua.LState) int {
	// Get spawner from userdata
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*WithContext)
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

	// Get context and PID
	ctx := l.Context()
	self, ok := pubsub.GetPID(ctx)
	if !ok {
		l.RaiseError("no PID found in context")
		return 0
	}

	// Apply context values using the Contexter pattern
	ctx = applyContextToProcess(ctx, spawner.values)

	// Get process manager
	manager := process.GetProcesses(ctx)
	if manager == nil {
		l.RaiseError("no process manager found")
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

// contextSpawnMonitored spawns a monitored process with context
func (m *Module) contextSpawnMonitored(l *lua.LState) int {
	// Get spawner from userdata
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*WithContext)
	if !ok {
		l.ArgError(1, "process spawner expected")
		return 0
	}

	// Get arguments
	if l.GetTop() < 3 {
		l.RaiseError("spawn_monitored requires at least id and host arguments")
		return 0
	}

	id := l.CheckString(2)
	hostID := l.CheckString(3)

	// Get context and PID
	ctx := l.Context()
	self, ok := pubsub.GetPID(ctx)
	if !ok {
		l.RaiseError("no PID found in context")
		return 0
	}

	// Apply context values using the Contexter pattern
	ctx = applyContextToProcess(ctx, spawner.values)

	// Get process manager
	manager := process.GetProcesses(ctx)
	if manager == nil {
		l.RaiseError("no process manager found")
		return 0
	}

	// Create payloads from remaining args
	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	// Create start configuration with monitoring
	start := &process.Start{
		HostID: hostID,
		Source: registry.ParseID(id),
		Input:  payloads,
		Lifecycle: process.Lifecycle{
			Parent:  self,
			Monitor: true,
			Link:    false,
		},
	}

	// Start the process with monitoring
	pid, err := manager.Start(ctx, start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Log the operation
	spawner.module.log.Debug("process spawned with context and monitoring",
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

// contextSpawnLinked spawns a linked process with context
func (m *Module) contextSpawnLinked(l *lua.LState) int {
	// Get spawner from userdata
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*WithContext)
	if !ok {
		l.ArgError(1, "process spawner expected")
		return 0
	}

	// Get arguments
	if l.GetTop() < 3 {
		l.RaiseError("spawn_linked requires at least id and host arguments")
		return 0
	}

	id := l.CheckString(2)
	hostID := l.CheckString(3)

	// Get context and PID
	ctx := l.Context()
	self, ok := pubsub.GetPID(ctx)
	if !ok {
		l.RaiseError("no PID found in context")
		return 0
	}

	// Apply context values using the Contexter pattern
	ctx = applyContextToProcess(ctx, spawner.values)

	// Get process manager
	manager := process.GetProcesses(ctx)
	if manager == nil {
		l.RaiseError("no process manager found")
		return 0
	}

	// Create payloads from remaining args
	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	// Create start configuration with linking
	start := &process.Start{
		HostID: hostID,
		Source: registry.ParseID(id),
		Input:  payloads,
		Lifecycle: process.Lifecycle{
			Parent:  self,
			Monitor: false,
			Link:    true,
		},
	}

	// Start the process with linking
	pid, err := manager.Start(ctx, start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Log the operation
	spawner.module.log.Debug("process spawned with context and linking",
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

// contextSpawnLinkedMonitored spawns a linked and monitored process with context
func (m *Module) contextSpawnLinkedMonitored(l *lua.LState) int {
	// Get spawner from userdata
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*WithContext)
	if !ok {
		l.ArgError(1, "process spawner expected")
		return 0
	}

	// Get arguments
	if l.GetTop() < 3 {
		l.RaiseError("spawn_linked_monitored requires at least id and host arguments")
		return 0
	}

	id := l.CheckString(2)
	hostID := l.CheckString(3)

	// Get context and PID
	ctx := l.Context()
	self, ok := pubsub.GetPID(ctx)
	if !ok {
		l.RaiseError("no PID found in context")
		return 0
	}

	// Apply context values using the Contexter pattern
	ctx = applyContextToProcess(ctx, spawner.values)

	// Get process manager
	manager := process.GetProcesses(ctx)
	if manager == nil {
		l.RaiseError("no process manager found")
		return 0
	}

	// Create payloads from remaining args
	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	// Create start configuration with both linking and monitoring
	start := &process.Start{
		HostID: hostID,
		Source: registry.ParseID(id),
		Input:  payloads,
		Lifecycle: process.Lifecycle{
			Parent:  self,
			Monitor: true,
			Link:    true,
		},
	}

	// Start the process with linking and monitoring
	pid, err := manager.Start(ctx, start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Log the operation
	spawner.module.log.Debug("process spawned with context, linking and monitoring",
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

// registerContextType registers the WithContext type and its methods
func (m *Module) registerContextType(l *lua.LState) {
	// Register WithContext type
	value.RegisterTypeMethods(l, "process.WithContext", nil, map[string]lua.LGFunction{
		"with_context":           m.withContext,
		"spawn":                  m.contextSpawn,
		"spawn_monitored":        m.contextSpawnMonitored,
		"spawn_linked":           m.contextSpawnLinked,
		"spawn_linked_monitored": m.contextSpawnLinkedMonitored,
	})
}
