package funcs

import (
	"context"
	"errors"
	"fmt"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"

	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	secapi "github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/runtime/lua/command"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	payloadmod "github.com/ponyruntime/pony/runtime/lua/modules/payload"
	lua "github.com/yuin/gopher-lua"
)

// Module represents the function module
type Module struct{}

// Functions represents a function executor with context values
type Functions struct {
	funcs  function.Registry
	dtt    payload.Transcoder
	values *contextapi.Contexter[interface{}]

	// Dedicated fields for security context to prevent overwriting/conflicting with user values
	actor    secapi.Actor
	hasActor bool
	scope    secapi.Scope
	hasScope bool
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
	// Register the function executor methods
	value.RegisterTypeMethods(l, "function.Executor", nil, map[string]lua.LGFunction{
		"with_context": m.withContext,
		"with_actor":   m.withActor,
		"with_scope":   m.withScope,
		"call":         m.call,
		"async":        m.async,
	})

	// Create module table
	mod := l.CreateTable(0, 1)
	mod.RawSetString("new", l.NewFunction(m.new))

	command.RegisterCommand(l)

	l.Push(mod)
	return 1
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

	functions := &Functions{
		funcs:    funcs,
		dtt:      dtt,
		values:   contextapi.NewContexter[any](),
		hasActor: false,
		hasScope: false,
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

	ctxTable := l.CheckTable(2)

	// Create new contexter and copy existing values
	newValues := contextapi.NewContexter[any]()
	if functions.values != nil {
		functions.values.Iterate(func(key string, value any) {
			newValues.SetValue(key, value)
		})
	}

	// Add new values
	ctxTable.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			l.ArgError(2, "context keys must be strings")
			return
		}

		// Don't allow overwriting security-specific keys through general context
		keyStr := string(key)
		if keyStr == "security.actor" || keyStr == "security.scope" {
			l.ArgError(2, fmt.Sprintf("reserved security key '%s' cannot be set through with_context", keyStr))
			return
		}

		newValues.SetValue(keyStr, luaconv.ToGoAny(v))
	})

	// Create new Functions instance with copied security context
	newFunctions := &Functions{
		funcs:    functions.funcs,
		dtt:      functions.dtt,
		values:   newValues,
		actor:    functions.actor,
		hasActor: functions.hasActor,
		scope:    functions.scope,
		hasScope: functions.hasScope,
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
		funcs:    functions.funcs,
		dtt:      functions.dtt,
		values:   functions.values.Clone(), // Clone the values
		actor:    actor,
		hasActor: true,
		scope:    functions.scope,
		hasScope: functions.hasScope,
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
		funcs:    functions.funcs,
		dtt:      functions.dtt,
		values:   functions.values.Clone(), // Clone the values
		actor:    functions.actor,
		hasActor: functions.hasActor,
		scope:    scope,
		hasScope: true,
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

	// Create task with proper context
	t, err := functions.createTask(l)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Wrap in coroutine for execution
	coroutine.Wrap(l, func() *engine.Update {
		// Create execution context with security context values
		execCtx := engine.DetachUnitOfWork(uw.Context())
		execCtx = functions.applySecurityContext(execCtx)

		resultChan, err := functions.funcs.Call(execCtx, t)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		select {
		case result := <-resultChan:
			if result.Error != nil {
				return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(result.Error.Error())}, nil)
			}

			if result.Value != nil {
				res, err := functions.dtt.Transcode(result.Value, payload.Lua)
				if err != nil {
					return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
				}

				return engine.NewUpdate(nil, []lua.LValue{res.Data().(lua.LValue), lua.LNil}, nil)
			}

			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LNil}, nil)

		case <-uw.Context().Done():
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString("execution canceled")}, nil)
		}
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

	// Create task with proper validation
	runtimeTask, err := functions.createTask(l)
	if err != nil {
		l.RaiseError("failed to create task: %v", err)
		return 0
	}

	// Serve the function execution in a goroutine
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work context found")
		return 0
	}

	baseCtx := engine.DetachUnitOfWork(uw.Context())

	// Apply security context
	execCtx := functions.applySecurityContext(baseCtx)
	ctx, cancel := context.WithCancel(execCtx)

	// Create a Command for the function call with the task's params
	cmd := command.NewCommand(
		l,
		runtimeTask.ID.String(),
		func(cmd runtime.Command) { cancel() },
		runtimeTask.Payloads...,
	)

	uw.Run(func(work engine.UnitOfWork) {
		// Run the function
		resultChan, err := functions.funcs.Call(ctx, runtimeTask)
		if err != nil {
			_ = cmd.Complete(&runtime.Result{
				Error: err,
			})
			return
		}

		// Wait for result
		select {
		case result := <-resultChan:
			_ = cmd.Complete(&runtime.Result{
				Value: result.Value,
				Error: result.Error,
			})
		case <-work.Context().Done():
			_ = cmd.Cancel()
		}
	})

	// Return the command object
	l.Push(command.WrapCommand(l, cmd))
	return 1
}

// applySecurityContext applies the actor and scope values to the context
func (f *Functions) applySecurityContext(ctx context.Context) context.Context {
	// Apply actor if set
	if f.hasActor {
		ctx = secapi.WithActor(ctx, f.actor)
	}

	// Apply scope if set
	if f.hasScope {
		ctx = secapi.WithScope(ctx, f.scope)
	}

	return ctx
}

// createTask creates a runtime.Task from Lua parameters
func (f *Functions) createTask(l *lua.LState) (runtime.Task, error) {
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

	return runtime.Task{
		ID:       regID,
		Payloads: payloads,
	}, nil
}
