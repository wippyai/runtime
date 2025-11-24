package sandbox

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	ModuleName       = "workflow_sandbox"
	WorkflowTypeName = "workflow.Instance"
)

type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

func NewWorkflowSandboxModule() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return ModuleName
}

func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

func (m *Module) initModuleTable(l *lua.LState) {
	value.RegisterMethods(l, WorkflowTypeName, map[string]lua.LGFunction{
		"start":    m.workflowStart,
		"step":     m.workflowStep,
		"commands": m.workflowCommands,
		"done":     m.workflowDone,
		"result":   m.workflowResult,
		"close":    m.workflowClose,
	})

	t := l.CreateTable(0, 1)
	t.RawSetString("get", l.NewFunction(m.get))

	t.Immutable = true

	m.moduleTable = t
}

func (m *Module) get(l *lua.LState) int {
	registryID := l.CheckString(1)
	if registryID == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("registry ID is required"))
		return 2
	}

	// Get prototype factory from context
	protoFactory := process.GetPrototypes(l.Context())
	if protoFactory == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("prototype factory not found"))
		return 2
	}

	// Cast to Factory interface to access Create method
	factory, ok := protoFactory.(process.Factory)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("prototype factory does not implement Factory interface"))
		return 2
	}

	// Create workflow from prototype
	id := registry.ParseID(registryID)
	proc, err := factory.Create(id)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to create workflow: %v", err)))
		return 2
	}

	// Cast to Workflow interface
	wf, ok := proc.(process.Workflow)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("process does not implement Workflow interface"))
		return 2
	}

	instance := &WorkflowInstance{
		workflow:        wf,
		registryID:      id,
		parentCtx:       l.Context(),
		upstreamHandler: newUpstreamHandler(),
	}

	ud := l.NewUserData()
	ud.Value = instance
	ud.Metatable = value.GetTypeMetatable(l, WorkflowTypeName)
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func CheckWorkflowInstance(l *lua.LState, n int) *WorkflowInstance {
	ud := l.CheckUserData(n)
	if instance, ok := ud.Value.(*WorkflowInstance); ok {
		return instance
	}
	l.ArgError(n, "workflow.Instance expected")
	return nil
}

func (m *Module) workflowStart(l *lua.LState) int {
	instance := CheckWorkflowInstance(l, 1)
	if instance == nil {
		return 0
	}

	top := l.GetTop()
	args := make([]interface{}, 0, top-1)
	for i := 2; i <= top; i++ {
		args = append(args, l.Get(i))
	}

	err := instance.Start(args...)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(fmt.Sprintf("start failed: %v", err)))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func (m *Module) workflowStep(l *lua.LState) int {
	instance := CheckWorkflowInstance(l, 1)
	if instance == nil {
		return 0
	}

	_, err := instance.Step()
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(fmt.Sprintf("step failed: %v", err)))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func (m *Module) workflowCommands(l *lua.LState) int {
	instance := CheckWorkflowInstance(l, 1)
	if instance == nil {
		return 0
	}

	commands := instance.Commands()

	t := l.CreateTable(len(commands), 0)
	for i, cmd := range commands {
		ud := l.NewUserData()
		ud.Value = cmd
		ud.Metatable = value.GetTypeMetatable(l, "upstream.Request")
		t.RawSetInt(i+1, ud)
	}

	l.Push(t)
	return 1
}

func (m *Module) workflowDone(l *lua.LState) int {
	instance := CheckWorkflowInstance(l, 1)
	if instance == nil {
		return 0
	}

	l.Push(lua.LBool(instance.IsDone()))
	return 1
}

func (m *Module) workflowResult(l *lua.LState) int {
	instance := CheckWorkflowInstance(l, 1)
	if instance == nil {
		return 0
	}

	result := instance.GetResult()
	if result == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("workflow not done yet"))
		return 2
	}

	if result.Error != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(result.Error.Error()))
		return 2
	}

	if result.Value != nil {
		dtt := payload.GetTranscoder(l.Context())
		if dtt == nil {
			l.Push(lua.LNil)
			l.Push(lua.LString("transcoder not found in context"))
			return 2
		}

		res, err := dtt.Transcode(result.Value, payload.Lua)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to transcode result: %v", err)))
			return 2
		}

		lv, ok := res.Data().(lua.LValue)
		if !ok {
			l.Push(lua.LNil)
			l.Push(lua.LString("invalid payload format"))
			return 2
		}

		l.Push(lv)
		l.Push(lua.LNil)
		return 2
	}

	l.Push(lua.LNil)
	l.Push(lua.LNil)
	return 2
}

func (m *Module) workflowClose(l *lua.LState) int {
	instance := CheckWorkflowInstance(l, 1)
	if instance == nil {
		return 0
	}

	instance.Close()
	return 0
}
