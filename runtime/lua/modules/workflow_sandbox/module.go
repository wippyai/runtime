package sandbox

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/workflow/std"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	luaconv "github.com/wippyai/runtime/system/payload/lua"

	lua "github.com/yuin/gopher-lua"
)

const (
	ModuleName       = "workflow_sandbox"
	WorkflowTypeName = "workflow.Instance"
	CommandTypeName  = "runtime.Command"
	TaskTypeName     = "sandbox.Task"
)

type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

func NewWorkflowSandboxModule() *Module {
	return &Module{}
}

// Info returns module metadata
func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        ModuleName,
		Description: "Workflow sandbox testing",
		Class:       []string{luaapi.ClassWorkflow, luaapi.ClassDeterministic},
	}
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
		"start":     m.workflowStart,
		"step":      m.workflowStep,
		"commands":  m.workflowCommands,
		"done":      m.workflowDone,
		"result":    m.workflowResult,
		"close":     m.workflowClose,
		"send":      m.workflowSend,
		"push_task": m.workflowPushTask,
	})

	// Register runtime.Command metatable for interface-based access
	value.RegisterMethods(l, CommandTypeName, map[string]lua.LGFunction{
		"id":       commandID,
		"type":     commandType,
		"params":   commandParams,
		"header":   commandHeader,
		"result":   commandResult,
		"complete": commandComplete,
		"cancel":   commandCancel,
	})

	// Register sandbox.Task metatable for testing inbound tasks
	value.RegisterMethods(l, TaskTypeName, map[string]lua.LGFunction{
		"type":      m.taskType,
		"input":     m.taskInput,
		"completed": m.taskCompleted,
		"result":    m.taskResult,
	})

	t := l.CreateTable(0, 2)
	t.RawSetString("get", l.NewFunction(m.get))
	t.RawSetString("new_task", l.NewFunction(m.newTask))

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
		ud.Metatable = value.GetTypeMetatable(l, CommandTypeName)
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

// CheckCommand extracts runtime.Command from userdata
func CheckCommand(l *lua.LState, n int) runtime.Command {
	ud := l.CheckUserData(n)
	if cmd, ok := ud.Value.(runtime.Command); ok {
		return cmd
	}
	l.ArgError(n, "runtime.Command expected")
	return nil
}

func commandID(l *lua.LState) int {
	cmd := CheckCommand(l, 1)
	if cmd == nil {
		return 0
	}
	l.Push(lua.LString(cmd.ID()))
	return 1
}

func commandType(l *lua.LState) int {
	cmd := CheckCommand(l, 1)
	if cmd == nil {
		return 0
	}
	l.Push(lua.LString(cmd.Type()))
	return 1
}

func commandParams(l *lua.LState) int {
	cmd := CheckCommand(l, 1)
	if cmd == nil {
		return 0
	}

	params := cmd.Params()
	t := l.CreateTable(len(params), 0)
	for i, p := range params {
		t.RawSetInt(i+1, payloadmod.WrapPayload(l, p))
	}
	l.Push(t)
	return 1
}

// commandHeader extracts and transcodes the header payload (Params[0]) based on command type.
// Returns a Lua table with the typed header fields.
func commandHeader(l *lua.LState) int {
	cmd := CheckCommand(l, 1)
	if cmd == nil {
		return 0
	}

	params := cmd.Params()
	if len(params) == 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString("no header payload"))
		return 2
	}

	headerPayload := params[0]
	cmdType := string(cmd.Type())

	// Validate header type matches command type
	var header any
	var expectedType string

	switch cmdType {
	case std.TypeFuncsCall:
		h, ok := headerPayload.Data().(*std.FuncsCallHeader)
		if !ok {
			l.Push(lua.LNil)
			l.Push(lua.LString("invalid funcs.call header type"))
			return 2
		}
		expectedType = "FuncsCallHeader"
		header = h

	case std.TypeContractCall:
		h, ok := headerPayload.Data().(*std.ContractCallHeader)
		if !ok {
			l.Push(lua.LNil)
			l.Push(lua.LString("invalid contract.call header type"))
			return 2
		}
		expectedType = "ContractCallHeader"
		header = h

	case std.TypeTimerSleep:
		h, ok := headerPayload.Data().(*std.TimerHeader)
		if !ok {
			l.Push(lua.LNil)
			l.Push(lua.LString("invalid timer.sleep header type"))
			return 2
		}
		expectedType = "TimerHeader"
		header = h

	case std.TypeProcessSend:
		h, ok := headerPayload.Data().(*std.ProcessSendHeader)
		if !ok {
			l.Push(lua.LNil)
			l.Push(lua.LString("invalid process.send header type"))
			return 2
		}
		expectedType = "ProcessSendHeader"
		header = h

	case std.TypeChildWorkflow:
		h, ok := headerPayload.Data().(*std.ChildWorkflowHeader)
		if !ok {
			l.Push(lua.LNil)
			l.Push(lua.LString("invalid workflow.child header type"))
			return 2
		}
		expectedType = "ChildWorkflowHeader"
		header = h

	default:
		l.Push(lua.LNil)
		l.Push(lua.LString("unknown command type: " + cmdType))
		return 2
	}

	headerTable, err := headerToTable(header)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to convert %s to table: %v", expectedType, err)))
		return 2
	}

	l.Push(headerTable)
	l.Push(lua.LNil)
	return 2
}

// headerToTable converts a typed header struct to a Lua table using luaconv.GoToLua.
// It also converts registry.ID fields to their string representation.
func headerToTable(header any) (*lua.LTable, error) {
	lv, err := luaconv.GoToLua(header)
	if err != nil {
		return nil, err
	}
	tbl, ok := lv.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("expected table, got %s", lv.Type())
	}
	return tbl, nil
}

func commandResult(l *lua.LState) int {
	cmd := CheckCommand(l, 1)
	if cmd == nil {
		return 0
	}

	result := cmd.Result()
	if result == nil {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	if result.Error != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(result.Error.Error()))
		return 2
	}

	if result.Value != nil {
		l.Push(payloadmod.WrapPayload(l, result.Value))
		l.Push(lua.LNil)
		return 2
	}

	l.Push(lua.LNil)
	l.Push(lua.LNil)
	return 2
}

func commandComplete(l *lua.LState) int {
	cmd := CheckCommand(l, 1)
	if cmd == nil {
		return 0
	}

	resultValue := l.Get(2)
	result := &runtime.Result{
		Value: payload.NewPayload(resultValue, payload.Lua),
	}

	if err := cmd.Complete(result); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func commandCancel(l *lua.LState) int {
	cmd := CheckCommand(l, 1)
	if cmd == nil {
		return 0
	}

	if err := cmd.Cancel(); err != nil {
		l.Push(lua.LString(err.Error()))
		return 1
	}

	l.Push(lua.LNil)
	return 1
}

// MockTask implements std.Task for testing inbound tasks in workflows.
// Test code creates a MockTask, sends it to the workflow, and inspects the result.
type MockTask struct {
	taskType  string
	input     payload.Payloads
	completed bool
	result    payload.Payload
	err       error
	mu        sync.Mutex
}

// Type returns the task type
func (t *MockTask) Type() string {
	return t.taskType
}

// Input returns the task input payloads
func (t *MockTask) Input() payload.Payloads {
	return t.input
}

// Complete marks the task as completed with a result
func (t *MockTask) Complete(value payload.Payload) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.completed {
		return fmt.Errorf("task already completed")
	}

	t.result = value
	t.completed = true
	return nil
}

// Fail marks the task as failed with an error
func (t *MockTask) Fail(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.completed {
		return fmt.Errorf("task already completed")
	}

	t.err = err
	t.completed = true
	return nil
}

// IsCompleted returns whether the task has been completed
func (t *MockTask) IsCompleted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.completed
}

// GetResult returns the task result (for test inspection)
func (t *MockTask) GetResult() (payload.Payload, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.result, t.err
}

// newTask creates a new MockTask for testing
// Usage: local task = sandbox.new_task("query", arg1, arg2, ...)
func (m *Module) newTask(l *lua.LState) int {
	taskType := l.CheckString(1)

	// Collect input payloads from remaining arguments
	top := l.GetTop()
	inputs := make(payload.Payloads, 0, top-1)
	for i := 2; i <= top; i++ {
		inputs = append(inputs, payload.NewPayload(l.Get(i), payload.Lua))
	}

	task := &MockTask{
		taskType: taskType,
		input:    inputs,
	}

	ud := l.NewUserData()
	ud.Value = task
	ud.Metatable = value.GetTypeMetatable(l, TaskTypeName)
	l.Push(ud)
	return 1
}

// CheckMockTask extracts MockTask from userdata
func CheckMockTask(l *lua.LState, n int) *MockTask {
	ud := l.CheckUserData(n)
	if task, ok := ud.Value.(*MockTask); ok {
		return task
	}
	l.ArgError(n, "sandbox.Task expected")
	return nil
}

// taskType returns the task type string
func (m *Module) taskType(l *lua.LState) int {
	task := CheckMockTask(l, 1)
	if task == nil {
		return 0
	}
	l.Push(lua.LString(task.Type()))
	return 1
}

// taskInput returns the first input payload (for simplicity)
func (m *Module) taskInput(l *lua.LState) int {
	task := CheckMockTask(l, 1)
	if task == nil {
		return 0
	}

	inputs := task.Input()
	if len(inputs) == 0 {
		l.Push(lua.LNil)
		return 1
	}

	// Return first input as Lua value
	input := inputs[0]
	if input.Format() == payload.Lua {
		if lv, ok := input.Data().(lua.LValue); ok {
			l.Push(lv)
			return 1
		}
	}

	// Try to transcode
	dtt := payload.GetTranscoder(l.Context())
	if dtt != nil {
		res, err := dtt.Transcode(input, payload.Lua)
		if err == nil {
			if lv, ok := res.Data().(lua.LValue); ok {
				l.Push(lv)
				return 1
			}
		}
	}

	l.Push(lua.LNil)
	return 1
}

// taskCompleted returns whether the task has been completed
func (m *Module) taskCompleted(l *lua.LState) int {
	task := CheckMockTask(l, 1)
	if task == nil {
		return 0
	}
	l.Push(lua.LBool(task.IsCompleted()))
	return 1
}

// taskResult returns the task result (value, error)
func (m *Module) taskResult(l *lua.LState) int {
	task := CheckMockTask(l, 1)
	if task == nil {
		return 0
	}

	if !task.IsCompleted() {
		l.Push(lua.LNil)
		l.Push(lua.LString("task not completed"))
		return 2
	}

	result, err := task.GetResult()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if result == nil {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	// Return result as Lua value
	if result.Format() == payload.Lua {
		if lv, ok := result.Data().(lua.LValue); ok {
			l.Push(lv)
			l.Push(lua.LNil)
			return 2
		}
	}

	// Handle Go primitive types directly
	if result.Format() == payload.Golang {
		switch v := result.Data().(type) {
		case string:
			l.Push(lua.LString(v))
			l.Push(lua.LNil)
			return 2
		case int:
			l.Push(lua.LNumber(v))
			l.Push(lua.LNil)
			return 2
		case int64:
			l.Push(lua.LNumber(v))
			l.Push(lua.LNil)
			return 2
		case float64:
			l.Push(lua.LNumber(v))
			l.Push(lua.LNil)
			return 2
		case bool:
			l.Push(lua.LBool(v))
			l.Push(lua.LNil)
			return 2
		}
	}

	// Try to transcode
	dtt := payload.GetTranscoder(l.Context())
	if dtt != nil {
		res, err := dtt.Transcode(result, payload.Lua)
		if err == nil {
			if lv, ok := res.Data().(lua.LValue); ok {
				l.Push(lv)
				l.Push(lua.LNil)
				return 2
			}
		}
	}

	l.Push(lua.LNil)
	l.Push(lua.LString("failed to convert result"))
	return 2
}

// workflowSend delivers a message package to the workflow
// Usage: wf:send(topic, payload1, payload2, ...)
// After send(), call step() to process the queued messages (Temporal model)
func (m *Module) workflowSend(l *lua.LState) int {
	instance := CheckWorkflowInstance(l, 1)
	if instance == nil {
		return 0
	}

	topic := l.CheckString(2)
	if topic == "" {
		l.Push(lua.LFalse)
		l.Push(lua.LString("topic is required"))
		return 2
	}

	// Collect payloads from remaining arguments
	top := l.GetTop()
	payloads := make(payload.Payloads, 0, top-2)
	for i := 3; i <= top; i++ {
		val := l.Get(i)
		// If it's a userdata with a Go value, extract the Go value directly
		if ud, ok := val.(*lua.LUserData); ok && ud.Value != nil {
			payloads = append(payloads, payload.NewPayload(ud.Value, payload.Golang))
		} else {
			payloads = append(payloads, payload.NewPayload(val, payload.Lua))
		}
	}

	// Build relay package using NewPackage
	pkg := relay.NewPackage(relay.PID{}, relay.PID{}, topic, payloads...)

	if err := instance.Send(pkg); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(fmt.Sprintf("send failed: %v", err)))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

// workflowPushTask delivers a task to the workflow via TaskReceiver
// Usage: wf:push_task(task) where task is a sandbox.Task userdata
// After push_task(), call step() to process the task
func (m *Module) workflowPushTask(l *lua.LState) int {
	instance := CheckWorkflowInstance(l, 1)
	if instance == nil {
		return 0
	}

	task := CheckMockTask(l, 2)
	if task == nil {
		return 0
	}

	if err := instance.PushTask(task); err != nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString(fmt.Sprintf("push_task failed: %v", err)))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
