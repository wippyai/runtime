package process

import (
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// WithContext represents a process spawner with context values
type WithContext struct {
	module *Module
	values context.Values

	// Dedicated fields for security context to prevent overwriting/conflicting with user values
	actor    secapi.Actor
	hasActor bool
	scope    secapi.Scope
	hasScope bool
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

		// Add security check for custom application context
		if !security.IsAllowed(l.Context(), "process.context", "context", nil) {
			l.RaiseError("not allowed to spawn processes with custom context")
			return 0
		}

		// Check context table from second argument
		ctxTable := l.CheckTable(2)

		// Check for security-related keys before proceeding
		hasSecurity := false
		ctxTable.ForEach(func(k, _ lua.LValue) {
			if hasSecurity {
				return
			}

			key, ok := k.(lua.LString)
			if !ok {
				return
			}

			keyStr := string(key)
			if strings.HasPrefix(keyStr, "security.") ||
				keyStr == "security.actor" ||
				keyStr == "security.scope" {
				hasSecurity = true
			}
		})

		// If attempting to set security context, verify permission
		if hasSecurity && !security.IsAllowed(l.Context(), "process.security", "security", nil) {
			l.RaiseError("not allowed to spawn processes with custom security context")
			return 0
		}

		// Create new Values and copy existing values
		newValues := context.NewValues()
		if spawner.values != nil {
			spawner.values.Iterate(func(key string, value any) {
				newValues.Set(key, value)
			})
		}

		// Add new values from table
		ctxTable.ForEach(func(k, v lua.LValue) {
			key, ok := k.(lua.LString)
			if !ok {
				l.ArgError(2, "context keys must be strings")
				return
			}

			newValues.Set(string(key), luaconv.ToGoAny(v))
		})

		// Create new spawner with merged context, preserving security context
		newSpawner := &WithContext{
			module:   m,
			values:   newValues,
			actor:    spawner.actor,
			hasActor: spawner.hasActor,
			scope:    spawner.scope,
			hasScope: spawner.hasScope,
		}

		// Create userdata with the new spawner
		newUd := l.NewUserData()
		newUd.Value = newSpawner
		newUd.Metatable = value.GetTypeMetatable(l, "process.WithContext")
		l.Push(newUd)
		return 1
	}

	// Add security check for custom application context
	if !security.IsAllowed(l.Context(), "process.context", "context", nil) {
		l.RaiseError("not allowed to spawn processes with custom context")
		return 0
	}

	// First call - create new Values
	values := context.GetValues(l.Context())
	if values != nil {
		values = values.Clone().(context.Values)
	} else {
		values = context.NewValues()
	}

	// Get context table from first argument
	ctxTable := l.CheckTable(1)

	// All context values are now allowed as we have dedicated methods for security context

	// Add values from table
	ctxTable.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			l.ArgError(1, "context keys must be strings")
			return
		}

		values.Set(string(key), luaconv.ToGoAny(v))
	})

	// Check for existing security context to inherit
	var actor secapi.Actor
	var scope secapi.Scope
	hasActor := false
	hasScope := false

	if a, ok := secapi.GetActor(l.Context()); ok {
		actor = a
		hasActor = true
	}
	if s, ok := secapi.GetScope(l.Context()); ok {
		scope = s
		hasScope = true
	}

	// Create new spawner with context, inheriting security context from current frame
	spawner := &WithContext{
		module:   m,
		values:   values,
		actor:    actor,
		hasActor: hasActor,
		scope:    scope,
		hasScope: hasScope,
	}

	// Create userdata with metatable for method chaining
	ud := l.NewUserData()
	ud.Value = spawner
	ud.Metatable = value.GetTypeMetatable(l, "process.WithContext")

	l.Push(ud)
	return 1
}

// withActor creates a new process spawner with a specific actor
func (m *Module) withActor(l *lua.LState) int {
	// Check if this is a chained call on an existing WithContext
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*WithContext)
	if !ok {
		l.ArgError(1, "process spawner expected")
		return 0
	}

	// Add security check for custom security context
	if !security.IsAllowed(l.Context(), "process.security", "security", nil) {
		l.RaiseError("not allowed to spawn processes with custom security context")
		return 0
	}

	// Check if we're setting actor
	if l.Get(2).Type() == lua.LTNil {
		l.ArgError(2, "actor cannot be nil - security context cannot be removed")
		return 0
	}

	// Get actor
	actorUD := l.CheckUserData(2)
	actor, ok := actorUD.Value.(secapi.Actor)
	if !ok {
		l.ArgError(2, "Actor expected")
		return 0
	}

	// Create new spawner with copied values and new actor
	newSpawner := &WithContext{
		module:   spawner.module,
		values:   spawner.values.Clone().(context.Values), // Clone the values
		actor:    actor,
		hasActor: true,
		scope:    spawner.scope,
		hasScope: spawner.hasScope,
	}

	// Create userdata with the new spawner
	newUd := l.NewUserData()
	newUd.Value = newSpawner
	newUd.Metatable = value.GetTypeMetatable(l, "process.WithContext")
	l.Push(newUd)

	return 1
}

// withScope creates a new process spawner with a specific scope
func (m *Module) withScope(l *lua.LState) int {
	// Check if this is a chained call on an existing WithContext
	ud := l.CheckUserData(1)
	spawner, ok := ud.Value.(*WithContext)
	if !ok {
		l.ArgError(1, "process spawner expected")
		return 0
	}

	// Add security check for custom security context
	if !security.IsAllowed(l.Context(), "process.security", "security", nil) {
		l.RaiseError("not allowed to spawn processes with custom security context")
		return 0
	}

	// Check if we're setting scope
	if l.Get(2).Type() == lua.LTNil {
		l.ArgError(2, "scope cannot be nil - security context cannot be removed")
		return 0
	}

	// Get scope
	scopeUD := l.CheckUserData(2)
	scope, ok := scopeUD.Value.(secapi.Scope)
	if !ok {
		l.ArgError(2, "Scope expected")
		return 0
	}

	// Create new spawner with copied values and new scope
	newSpawner := &WithContext{
		module:   spawner.module,
		values:   spawner.values.Clone().(context.Values), // Clone the values
		actor:    spawner.actor,
		hasActor: spawner.hasActor,
		scope:    scope,
		hasScope: true,
	}

	// Create userdata with the new spawner
	newUd := l.NewUserData()
	newUd.Value = newSpawner
	newUd.Metatable = value.GetTypeMetatable(l, "process.WithContext")
	l.Push(newUd)

	return 1
}

// buildContextPairs builds context.Pair slice from WithContext spawner
func buildContextPairs(spawner *WithContext) []context.Pair {
	if spawner == nil {
		return nil
	}

	var pairs []context.Pair

	// Add actor if set
	if spawner.hasActor {
		pairs = append(pairs, secapi.ActorPair(spawner.actor))
	}

	// Add scope if set
	if spawner.hasScope {
		pairs = append(pairs, secapi.ScopePair(spawner.scope))
	}

	// Add custom values if set
	if spawner.values != nil && spawner.values.Len() > 0 {
		pairs = append(pairs, context.ValuesPair(spawner.values))
	}

	return pairs
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

	// Add security check for spawning processes
	if !security.IsAllowed(l.Context(), "process.spawn", id, nil) {
		l.RaiseError("not allowed to spawn process: %s", id)
		return 0
	}

	// Get PID from frame context
	ctx := l.Context()
	self, ok := runtimeapi.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	// Get process manager
	manager := process.GetManager(ctx)
	if manager == nil {
		l.RaiseError("no process manager found")
		return 0
	}

	// Create payloads from remaining args
	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	// Create Options with lifecycle parameters
	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)

	// Create start configuration with context overrides
	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: buildContextPairs(spawner),
		Options: options,
	}

	// Start the process with our context
	pid, err := manager.Start(ctx, start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "spawn"))
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

	// Add security check for spawning processes
	if !security.IsAllowed(l.Context(), "process.spawn", id, nil) {
		l.RaiseError("not allowed to spawn process: %s", id)
		return 0
	}

	// Add security check for monitoring
	if !security.IsAllowed(l.Context(), "process.spawn.monitored", id, nil) {
		l.RaiseError("not allowed to spawn monitored process: %s", id)
		return 0
	}

	// Get PID from frame context
	ctx := l.Context()
	self, ok := runtimeapi.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	// Get process manager
	manager := process.GetManager(ctx)
	if manager == nil {
		l.RaiseError("no process manager found")
		return 0
	}

	// Create payloads from remaining args
	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	// Create Options with lifecycle parameters
	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)
	options.Set(process.LifecycleMonitorKey, true)

	// Create start configuration with monitoring and context overrides
	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: buildContextPairs(spawner),
		Options: options,
	}

	// Start the process with monitoring
	pid, err := manager.Start(ctx, start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "spawn_monitored"))
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

	// Add security check for spawning processes
	if !security.IsAllowed(l.Context(), "process.spawn", id, nil) {
		l.RaiseError("not allowed to spawn process: %s", id)
		return 0
	}

	// Add security check for linking
	if !security.IsAllowed(l.Context(), "process.spawn.linked", id, nil) {
		l.RaiseError("not allowed to spawn linked process: %s", id)
		return 0
	}

	// Get PID from frame context
	ctx := l.Context()
	self, ok := runtimeapi.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	// Get process manager
	manager := process.GetManager(ctx)
	if manager == nil {
		l.RaiseError("no process manager found")
		return 0
	}

	// Create payloads from remaining args
	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	// Create Options with lifecycle parameters
	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)
	options.Set(process.LifecycleLinkKey, true)

	// Create start configuration with linking and context overrides
	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: buildContextPairs(spawner),
		Options: options,
	}

	// Start the process with linking
	pid, err := manager.Start(ctx, start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "spawn_linked"))
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

	// Add security check for spawning processes
	if !security.IsAllowed(l.Context(), "process.spawn", id, nil) {
		l.RaiseError("not allowed to spawn process: %s", id)
		return 0
	}

	// Add security check for monitoring
	if !security.IsAllowed(l.Context(), "process.spawn.monitored", id, nil) {
		l.RaiseError("not allowed to spawn monitored process: %s", id)
		return 0
	}

	// Add security check for linking
	if !security.IsAllowed(l.Context(), "process.spawn.linked", id, nil) {
		l.RaiseError("not allowed to spawn linked process: %s", id)
		return 0
	}

	// Get PID from frame context
	ctx := l.Context()
	self, ok := runtimeapi.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no PID found in frame context")
		return 0
	}

	// Get process manager
	manager := process.GetManager(ctx)
	if manager == nil {
		l.RaiseError("no process manager found")
		return 0
	}

	// Create payloads from remaining args
	var payloads payload.Payloads
	for i := 4; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	// Create Options with lifecycle parameters
	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)
	options.Set(process.LifecycleMonitorKey, true)
	options.Set(process.LifecycleLinkKey, true)

	// Create start configuration with linking, monitoring, and context overrides
	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: buildContextPairs(spawner),
		Options: options,
	}

	// Start the process with linking and monitoring
	pid, err := manager.Start(ctx, start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "spawn_linked_monitored"))
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
		"with_actor":             m.withActor,
		"with_scope":             m.withScope,
		"spawn":                  m.contextSpawn,
		"spawn_monitored":        m.contextSpawnMonitored,
		"spawn_linked":           m.contextSpawnLinked,
		"spawn_linked_monitored": m.contextSpawnLinkedMonitored,
	})
}
