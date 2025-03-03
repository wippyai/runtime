package funcs

import (
	"errors"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	transcode "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

type Module struct {
}

type Functions struct {
	funcs  function.Registry
	dtt    payload.Transcoder
	values *contextapi.Contexter[interface{}]
}

func NewFunctionModule() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "funcs"
}

func (m *Module) Loader(l *lua.LState) int {
	mod := l.NewTable()
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"new": m.new,
	})

	// Set up function executor metatable
	mt := l.NewTypeMetatable("function.Executor")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"with_context": m.withContext,
		"call":         m.call,
		"async":        m.async,
	}))

	// Register task type
	RegisterTask(l)

	l.Push(mod)
	return 1
}

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

func (m *Module) new(l *lua.LState) int {
	funcs, dtt, err := m.extractDependencies(l)
	if err != nil {
		l.RaiseError("failed to create executor: %v", err)
		return 0
	}

	functions := &Functions{
		funcs:  funcs,
		dtt:    dtt,
		values: contextapi.NewContexter[interface{}](),
	}

	ud := l.NewUserData()
	ud.Value = functions
	l.SetMetatable(ud, l.GetTypeMetatable("function.Executor"))
	l.Push(ud)
	return 1
}

func (m *Module) withContext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	functions, ok := ud.Value.(*Functions)
	if !ok {
		l.ArgError(1, "functions executor expected")
		return 0
	}

	ctxTable := l.CheckTable(2)

	// Create new contexter and copy existing values
	newValues := contextapi.NewContexter[interface{}]()
	if functions.values != nil {
		functions.values.Iterate(func(key string, value interface{}) {
			newValues.WithValue(key, value)
		})
	}

	// Add new values
	ctxTable.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			l.ArgError(2, "context keys must be strings")
			return
		}
		newValues.WithValue(string(key), transcode.ToGoAny(v))
	})

	// Create new Functions instance
	newFunctions := &Functions{
		funcs:  functions.funcs,
		dtt:    functions.dtt,
		values: newValues,
	}

	// Create new userdata with the new Functions instance
	newUd := l.NewUserData()
	newUd.Value = newFunctions
	l.SetMetatable(newUd, l.GetTypeMetatable("function.Executor"))
	l.Push(newUd)

	return 1
}

func validateRegistryID(id registry.ID) error {
	if id.NS == "" {
		return fmt.Errorf("namespace is required, got empty namespace in Source: %s", id.String())
	}
	if id.Name == "" {
		return fmt.Errorf("name is required, got empty name in Source: %s", id.String())
	}
	return nil
}

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
		resultChan, err := functions.funcs.Call(engine.DetachUnitOfWork(uw.Context()), t)
		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		select {
		case result := <-resultChan:
			if result.Error != nil {
				return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(result.Error.Error())}, nil)
			}

			if result.Payload != nil {
				res, err := functions.dtt.Transcode(result.Payload, payload.Lua)
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

	// Get unit of work from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work context found")
		return 0
	}

	// Create a task object with cancellation support
	t := NewTask(l, engine.DetachUnitOfWork(uw.Context()))

	// Create task userdata
	taskUd := l.NewUserData()
	taskUd.Value = t
	taskUd.Metatable = value.GetTypeMetatable(l, "function.Task")

	// Start the task in a goroutine
	uw.Run(
		func(work engine.UnitOfWork) {
			// Start the function
			resultChan, err := functions.funcs.Call(t.Context(), runtimeTask)
			if err != nil {
				t.SetError(err)
				// close chan
				_ = channel.Close(l, t.response)
				return
			}

			// Wait for result
			select {
			case result := <-resultChan:
				if result.Error != nil {
					// Error result
					t.SetError(result.Error)
					_ = channel.Close(l, t.response)
				} else if result.Payload != nil {
					// Success with payload
					res, err := functions.dtt.Transcode(result.Payload, payload.Lua)
					if err != nil {
						t.SetError(err)
						_ = channel.Close(l, t.response)
					} else {
						resultLua := res.Data().(lua.LValue)
						t.SetResult(resultLua)
						_ = channel.Send(l, t.response, resultLua)
						_ = channel.Close(l, t.response)
					}
				} else {
					// Empty success result
					t.SetResult(lua.LNil)
					_ = channel.Send(l, t.response, lua.LNil)
					_ = channel.Close(l, t.response)
				}
			case <-uw.Context().Done():
				t.SetError(ErrTaskCanceled)
				_ = channel.Close(l, t.response)
			}
		})

	// Return the task object
	l.Push(taskUd)
	return 1
}

func (f *Functions) createTask(l *lua.LState) (runtime.Task, error) {
	targetIndex := 1
	if l.Get(1).Type() == lua.LTUserData {
		targetIndex = 2 // Skip self parameter
	}

	target := l.CheckString(targetIndex)
	if target == "" {
		return runtime.Task{}, errors.New("target name is required")
	}

	// Parse and validate registry Source
	regID := registry.ParseID(target)
	if err := validateRegistryID(regID); err != nil {
		return runtime.Task{}, fmt.Errorf("invalid registry Source: %w", err)
	}

	var payloads []payload.Payload
	for i := targetIndex + 1; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	return runtime.Task{
		ID:       regID,
		Payloads: payloads,
	}, nil
}
