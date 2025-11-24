package funcs

import (
	"context"
	"errors"
	"fmt"
	"sync"

	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	"github.com/wippyai/runtime/runtime/lua/modules/upstream"
	"github.com/wippyai/runtime/runtime/lua/security"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents the function module
type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

// Functions represents a function executor with context values
type Functions struct {
	funcs  function.Registry
	dtt    payload.Transcoder
	values contextapi.Values

	// Dedicated fields for security context to prevent overwriting/conflicting with user values
	actor    secapi.Actor
	hasActor bool
	scope    secapi.Scope
	hasScope bool

	// Options for function execution (retry, ratelimit, timeout, etc.)
	options    runtime.Bag
	hasOptions bool
}

// NewFunctionModule creates a new function module
func NewFunctionModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "funcs"
}

// Loader registers the module functions
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

// initModuleTable creates and initializes the module table once
func (m *Module) initModuleTable(l *lua.LState) {
	// Register the function executor methods once
	value.RegisterTypeMethods(l, "function.Executor", nil, map[string]lua.LGFunction{
		"with_context": m.withContext,
		"with_actor":   m.withActor,
		"with_scope":   m.withScope,
		"with_options": m.withOptions,
		"call":         m.call,
		"async":        m.async,
	})

	// Create module table
	t := l.CreateTable(0, 1)
	t.RawSetString("new", l.NewFunction(m.new))

	// Make the table immutable so it can be safely reused
	t.Immutable = true

	m.moduleTable = t
}

// extractDependencies gets the required dependencies from context
func (m *Module) extractDependencies(l *lua.LState) (function.Registry, payload.Transcoder, error) {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		return nil, nil, errors.New("no unit of work context found")
	}

	funcs := function.GetRegistry(uw.Context())
	if funcs == nil {
		return nil, nil, errors.New("function registry not found in context")
	}

	dtt := payload.GetTranscoder(uw.Context())
	if dtt == nil {
		return nil, nil, errors.New("transcoder not found in context")
	}

	return funcs, dtt, nil
}

// new creates a new function executor
func (m *Module) new(l *lua.LState) int {
	funcs, dtt, err := m.extractDependencies(l)
	if err != nil {
		l.RaiseError("failed to create executor: %v", err)
		return 0
	}

	values := contextapi.GetValues(l.Context())
	if values != nil {
		values = values.Clone().(contextapi.Values)
	} else {
		values = contextapi.NewValues()
	}

	functions := &Functions{
		funcs:      funcs,
		dtt:        dtt,
		values:     values,
		hasActor:   false,
		hasScope:   false,
		hasOptions: false,
	}

	ud := l.NewUserData()
	ud.Value = functions
	ud.Metatable = value.GetTypeMetatable(l, "function.Executor")
	l.Push(ud)
	return 1
}

// withContext creates a new executor with additional context values
func (m *Module) withContext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	functions, ok := ud.Value.(*Functions)
	if !ok {
		l.ArgError(1, "functions executor expected")
		return 0
	}

	// Add security check for custom application context
	if !security.IsAllowed(l.Context(), "funcs.context", "context", nil) {
		l.RaiseError("not allowed to call functions with custom context")
		return 0
	}

	ctxTable := l.CheckTable(2)

	// Create new Values and copy existing values
	newValues := contextapi.NewValues()
	if functions.values != nil {
		functions.values.Iterate(func(key string, value any) {
			newValues.Set(key, value)
		})
	}

	// Add new values
	ctxTable.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			l.ArgError(2, "context keys must be strings")
			return
		}
		newValues.Set(string(key), value.ToGoAny(v))
	})

	// Create new Functions instance with copied security context
	newFunctions := &Functions{
		funcs:      functions.funcs,
		dtt:        functions.dtt,
		values:     newValues,
		actor:      functions.actor,
		hasActor:   functions.hasActor,
		scope:      functions.scope,
		hasScope:   functions.hasScope,
		options:    functions.options,
		hasOptions: functions.hasOptions,
	}

	// Create new userdata with the new Functions instance
	newUd := l.NewUserData()
	newUd.Value = newFunctions
	newUd.Metatable = value.GetTypeMetatable(l, "function.Executor")
	l.Push(newUd)

	return 1
}

// withActor creates a new executor with a specific actor
func (m *Module) withActor(l *lua.LState) int {
	ud := l.CheckUserData(1)
	functions, ok := ud.Value.(*Functions)
	if !ok {
		l.ArgError(1, "functions executor expected")
		return 0
	}

	// Add security check for custom security context
	if !security.IsAllowed(l.Context(), "funcs.security", "security", nil) {
		l.RaiseError("not allowed to call functions with custom security context")
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

	// Create new Functions instance with copied values and new actor
	newFunctions := &Functions{
		funcs:      functions.funcs,
		dtt:        functions.dtt,
		values:     functions.values.Clone().(contextapi.Values),
		actor:      actor,
		hasActor:   true,
		scope:      functions.scope,
		hasScope:   functions.hasScope,
		options:    functions.options,
		hasOptions: functions.hasOptions,
	}

	// Create new userdata with the new Functions instance
	newUd := l.NewUserData()
	newUd.Value = newFunctions
	newUd.Metatable = value.GetTypeMetatable(l, "function.Executor")
	l.Push(newUd)

	return 1
}

// withScope creates a new executor with a specific scope
func (m *Module) withScope(l *lua.LState) int {
	ud := l.CheckUserData(1)
	functions, ok := ud.Value.(*Functions)
	if !ok {
		l.ArgError(1, "functions executor expected")
		return 0
	}

	// Add security check for custom security context
	if !security.IsAllowed(l.Context(), "funcs.security", "security", nil) {
		l.RaiseError("not allowed to call functions with custom security context")
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

	// Create new Functions instance with copied values and new scope
	newFunctions := &Functions{
		funcs:      functions.funcs,
		dtt:        functions.dtt,
		values:     functions.values.Clone().(contextapi.Values),
		actor:      functions.actor,
		hasActor:   functions.hasActor,
		scope:      scope,
		hasScope:   true,
		options:    functions.options,
		hasOptions: functions.hasOptions,
	}

	// Create new userdata with the new Functions instance
	newUd := l.NewUserData()
	newUd.Value = newFunctions
	newUd.Metatable = value.GetTypeMetatable(l, "function.Executor")
	l.Push(newUd)

	return 1
}

// withOptions creates a new executor with runtime options
func (m *Module) withOptions(l *lua.LState) int {
	ud := l.CheckUserData(1)
	functions, ok := ud.Value.(*Functions)
	if !ok {
		l.ArgError(1, "functions executor expected")
		return 0
	}

	// Get options table
	optionsTable := l.CheckTable(2)

	// Convert Lua table to runtime.Bag (which is attrs.Bag, which is map[string]any)
	optionsData := value.ToGoAny(optionsTable)
	var options runtime.Bag
	if dataMap, ok := optionsData.(map[string]any); ok {
		options = runtime.Bag(dataMap)
	} else {
		// If conversion didn't work, create empty bag
		options = runtime.Bag{}
	}

	// Create new Functions instance with copied values and new options
	newFunctions := &Functions{
		funcs:      functions.funcs,
		dtt:        functions.dtt,
		values:     functions.values.Clone().(contextapi.Values),
		actor:      functions.actor,
		hasActor:   functions.hasActor,
		scope:      functions.scope,
		hasScope:   functions.hasScope,
		options:    options,
		hasOptions: true,
	}

	// Create new userdata with the new Functions instance
	newUd := l.NewUserData()
	newUd.Value = newFunctions
	newUd.Metatable = value.GetTypeMetatable(l, "function.Executor")
	l.Push(newUd)

	return 1
}

// validateRegistryID validates a registry ID
func validateRegistryID(id registry.ID) error {
	if id.NS == "" {
		return fmt.Errorf("namespace is required, got empty namespace in ID: %s", id.String())
	}
	if id.Name == "" {
		return fmt.Errorf("name is required, got empty name in ID: %s", id.String())
	}
	return nil
}

// call synchronously executes a function
func (m *Module) call(l *lua.LState) int {
	ud := l.CheckUserData(1)
	functions, ok := ud.Value.(*Functions)
	if !ok {
		l.ArgError(1, "functions executor expected")
		return 0
	}

	// Get unit of work context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work context found")
		return 0
	}

	// Get target function ID for security check
	targetIndex := 1
	if l.Get(1).Type() == lua.LTUserData {
		targetIndex = 2 // Skip self parameter
	}

	target := l.CheckString(targetIndex)
	if target == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("target name is required"))
		return 2
	}

	// Parse registry ID for security check
	regID := registry.ParseID(target)
	if err := validateRegistryID(regID); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("invalid registry ID: %v", err)))
		return 2
	}

	// Add security check for function call permission
	if !security.IsAllowed(l.Context(), "funcs.call", target, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to call function: %s", target)))
		return 2
	}

	// Create task with proper context
	log := logapi.GetLogger(l.Context()).Named("funcs")
	t, err := functions.createTask(l, log)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newFuncsOperationError(l, err, "create_task"))
		return 2
	}

	// Wrap in coroutine for execution
	coroutine.Wrap(l, func() *engine.Update {
		result, err := functions.funcs.Call(uw.Context(), t)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, newFuncsOperationError(l, err, "call")}, nil)
		}

		if result.Error != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, newFuncsOperationError(l, result.Error, "call")}, nil)
		}

		if result.Value != nil {
			res, err := functions.dtt.Transcode(result.Value, payload.Lua)
			if err != nil {
				return engine.NewUpdate(nil, []lua.LValue{lua.LNil, newFuncsOperationError(l, err, "transcode")}, nil)
			}

			return engine.NewUpdate(nil, []lua.LValue{res.Data().(lua.LValue), lua.LNil}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LNil}, nil)
	})

	return -1 // Yield for coroutine
}

// async asynchronously executes a function and returns a command
func (m *Module) async(l *lua.LState) int {
	ud := l.CheckUserData(1)
	functions, ok := ud.Value.(*Functions)
	if !ok {
		l.ArgError(1, "functions executor expected")
		return 0
	}

	// Get target function ID for security check
	targetIndex := 1
	if l.Get(1).Type() == lua.LTUserData {
		targetIndex = 2 // Skip self parameter
	}

	target := l.CheckString(targetIndex)
	if target == "" {
		l.RaiseError("target name is required")
		return 0
	}

	// Parse registry ID for security check
	regID := registry.ParseID(target)
	if err := validateRegistryID(regID); err != nil {
		l.RaiseError("invalid registry ID: %v", err)
		return 0
	}

	// Add security check for function call permission
	if !security.IsAllowed(l.Context(), "funcs.call", target, nil) {
		l.RaiseError("not allowed to call function: %s", target)
		return 0
	}

	// Create task with proper validation
	log := logapi.GetLogger(l.Context()).Named("funcs")
	runtimeTask, err := functions.createTask(l, log)
	if err != nil {
		l.RaiseError("failed to create task: %v", err)
		return 0
	}

	// Get unit of work context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work context found")
		return 0
	}

	ctx, cancel := context.WithCancel(uw.Context())

	// Create a Request for the function call with the task's params
	req := upstream.NewRequest(
		l,
		runtimeTask.ID.String(),
		func(_ runtime.Command) { cancel() },
		runtimeTask.Payloads...,
	)

	uw.Run(func(_ engine.UnitOfWork) {
		// Run the function
		result, err := functions.funcs.Call(ctx, runtimeTask)
		if err != nil {
			_ = req.Complete(&runtime.Result{
				Error: err,
			})
			return
		}

		// Complete with result
		_ = req.Complete(&runtime.Result{
			Value: result.Value,
			Error: result.Error,
		})
	})

	// Return the request wrapped as upstream.Request userdata
	l.Push(upstream.WrapRequest(l, req))
	return 1
}

// createTask creates a runtime.Task from Lua parameters
func (f *Functions) createTask(l *lua.LState, _ *zap.Logger) (runtime.Task, error) {
	targetIndex := 1
	if l.Get(1).Type() == lua.LTUserData {
		targetIndex = 2 // Skip self parameter
	}

	target := l.CheckString(targetIndex)
	if target == "" {
		return runtime.Task{}, errors.New("target name is required")
	}

	// Parse and validate registry ID
	regID := registry.ParseID(target)
	if err := validateRegistryID(regID); err != nil {
		return runtime.Task{}, fmt.Errorf("invalid registry ID: %w", err)
	}

	//nolint:prealloc // ok for now
	var payloads []payload.Payload
	for i := targetIndex + 1; i <= l.GetTop(); i++ {
		val := l.Get(i)

		// Check if argument is already a payload wrapper
		if ud, ok := val.(*lua.LUserData); ok {
			if pw, ok := ud.Value.(*payloadmod.Wrapper); ok {
				payloads = append(payloads, pw.Payload)
				continue
			}
		}

		// Otherwise create a new payload
		payloads = append(payloads, luaconv.ExportPayload(val))
	}

	// Build context override pairs from Functions struct fields
	var ctxPairs []contextapi.Pair

	// Add actor if set
	if f.hasActor {
		ctxPairs = append(ctxPairs, secapi.ActorPair(f.actor))
	}

	// Add scope if set
	if f.hasScope {
		ctxPairs = append(ctxPairs, secapi.ScopePair(f.scope))
	}

	// Add custom values if set
	if f.values != nil && f.values.Len() > 0 {
		ctxPairs = append(ctxPairs, contextapi.ValuesPair(f.values))
	}

	task := runtime.Task{
		ID:       regID,
		Payloads: payloads,
		Context:  ctxPairs,
	}

	// Add options if set
	if f.hasOptions {
		task.Options = f.options
	}

	return task, nil
}
