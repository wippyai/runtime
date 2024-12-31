package exec

import (
	"context"
	"errors"
	"github.com/ponyruntime/go-lua"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	transcode "github.com/ponyruntime/pony/pkg/payload/lua"
)

// todo: we might need some whitelist of what can actually be called from Lua

type Module struct {
	appContext context.Context
}

type Executor struct {
	dtt           payload.Transcoder
	exec          runtime.Executor
	appContext    context.Context
	threadContext context.Context
	contextValues map[string]interface{}
}

func NewExecutorModule(ctx context.Context) *Module {
	return &Module{appContext: ctx}
}

func (m *Module) Name() string {
	return "executor"
}

func (m *Module) Loader(l *lua.LState) int {
	mod := l.NewTable()
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"new":  m.new,
		"call": m.globalCall,
		"run":  m.globalRun,
	})

	mt := l.NewTypeMetatable("executor.Executor")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"with_context": m.withContext,
		"call":         m.call,
		"run":          m.run,
	}))

	l.Push(mod)
	return 1
}

func (m *Module) extractDependencies(l *lua.LState) (runtime.Executor, payload.Transcoder, error) {
	ctx := l.Context()
	if ctx == nil {
		return nil, nil, errors.New("no context found")
	}

	exec, ok := ctx.Value(contextapi.ExecutorCtx).(runtime.Executor)
	if !ok {
		return nil, nil, errors.New("executor not found in context")
	}

	dtt, ok := ctx.Value(contextapi.TranscoderCtx).(payload.Transcoder)
	if !ok {
		return nil, nil, errors.New("transcoder not found in context")
	}

	return exec, dtt, nil
}

func (m *Module) new(l *lua.LState) int {
	exec, dtt, err := m.extractDependencies(l)
	if err != nil {
		l.RaiseError(err.Error())
		return 0
	}

	executor := &Executor{
		dtt:           dtt,
		exec:          exec,
		appContext:    m.appContext,
		threadContext: l.Context(),
		contextValues: make(map[string]interface{}),
	}

	ud := l.NewUserData()
	ud.Value = executor
	l.SetMetatable(ud, l.GetTypeMetatable("executor.Executor"))
	l.Push(ud)
	return 1
}

func (m *Module) globalCall(l *lua.LState) int {
	executor, err := m.makeExecutor(l)
	if err != nil {
		l.RaiseError(err.Error())
		return 0
	}

	return executor.handleCall(l)
}

func (m *Module) globalRun(l *lua.LState) int {
	executor, err := m.makeExecutor(l)
	if err != nil {
		l.RaiseError(err.Error())
		return 0
	}

	return executor.handleRun(l)
}

func (m *Module) makeExecutor(l *lua.LState) (*Executor, error) {
	exec, dtt, err := m.extractDependencies(l)
	if err != nil {
		return nil, err
	}

	executor := &Executor{
		dtt:           dtt,
		exec:          exec,
		appContext:    m.appContext,
		threadContext: l.Context(),
		contextValues: make(map[string]interface{}),
	}

	return executor, nil
}

func (m *Module) withContext(l *lua.LState) int {
	ud := l.CheckUserData(1)
	executor, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "executor expected")
		return 0
	}

	ctxTable := l.CheckTable(2)
	executor.contextValues = make(map[string]interface{})

	ctxTable.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			l.ArgError(2, "context keys must be strings")
			return
		}
		executor.contextValues[string(key)] = transcode.ToGoAny(v)
	})

	l.Push(ud)
	return 1
}

func (m *Module) call(l *lua.LState) int {
	ud := l.CheckUserData(1)
	executor, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "executor expected")
		return 0
	}
	return executor.handleCall(l)
}

func (m *Module) run(l *lua.LState) int {
	ud := l.CheckUserData(1)
	executor, ok := ud.Value.(*Executor)
	if !ok {
		l.ArgError(1, "executor expected")
		return 0
	}
	return executor.handleRun(l)
}

func (e *Executor) handleCall(l *lua.LState) int {
	return e.executeSync(l, e.createTask(l, e.threadContext))
}

func (e *Executor) handleRun(l *lua.LState) int {
	return e.executeAsync(l, e.createTask(l, e.appContext))
}

func (e *Executor) createTask(l *lua.LState, ctx context.Context) runtime.Task {
	targetIndex := 1
	if l.Get(1).Type() == lua.LTUserData {
		targetIndex = 2
	}

	target := l.CheckString(targetIndex)
	if target == "" {
		l.RaiseError("target name is required")
	}

	if len(e.contextValues) > 0 {
		for k, v := range e.contextValues {
			ctx = context.WithValue(ctx, k, v)
		}
	}

	var payloads []payload.Payload
	for i := targetIndex + 1; i <= l.GetTop(); i++ {
		arg := l.Get(i)
		if arg != lua.LNil {
			payloads = append(payloads, payload.NewPayload(arg, payload.Lua))
		}
	}

	return runtime.Task{
		Context:  ctx,
		Target:   registry.ID(target),
		Payloads: payloads,
	}
}

func (e *Executor) executeSync(l *lua.LState, task runtime.Task) int {
	resultChan, err := e.exec.Execute(task)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	var result *runtime.Result
	select {
	case r := <-resultChan:
		result = r
	case <-task.Context.Done():
		l.Push(lua.LNil)
		l.Push(lua.LString("execution cancelled"))
		return 2
	}

	if result.Error != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(result.Error.Error()))
		return 2
	}

	if result.Payload != nil {
		res, err := e.dtt.Transcode(result.Payload, payload.Lua)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
		l.Push(res.Data().(lua.LValue))
	} else {
		l.Push(lua.LNil)
	}
	l.Push(lua.LNil)
	return 2
}

func (e *Executor) executeAsync(l *lua.LState, task runtime.Task) int {
	_, err := e.exec.Execute(task)
	if err != nil {
		l.Push(lua.LString(err.Error()))
		return 1
	}
	l.Push(lua.LNil)
	return 1
}
