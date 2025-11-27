package engine2

import (
	"context"
	"fmt"
	"strings"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/low-engine-v2/scheduler"
	lua "github.com/yuin/gopher-lua"
)

// ProcessOption configures a Process.
type ProcessOption func(*Process)

// WithLayer adds a layer to the process.
func WithLayer(layer Layer) ProcessOption {
	return func(p *Process) {
		p.layers = append(p.layers, layer)
	}
}

// WithScript sets the Lua script to execute (will be compiled on Start).
func WithScript(script, name string) ProcessOption {
	return func(p *Process) {
		p.script = script
		p.scriptName = name
	}
}

// WithProto sets a precompiled FunctionProto to execute (faster startup).
func WithProto(proto *lua.FunctionProto) ProcessOption {
	return func(p *Process) {
		p.proto = proto
	}
}

// ModuleBinder is a function that binds modules to a Lua state.
type ModuleBinder func(*lua.LState)

// WithModuleBinder adds module binders to be called after state creation.
func WithModuleBinder(binder ModuleBinder) ProcessOption {
	return func(p *Process) {
		p.moduleBinders = append(p.moduleBinders, binder)
	}
}

// WithStateOptions sets custom Lua state options for memory/performance tuning.
func WithStateOptions(opts lua.Options) ProcessOption {
	return func(p *Process) {
		p.stateOpts = &opts
	}
}

// Process implements scheduler.Process for Lua execution.
// Combines VM + CVM + Runner into a single unit.
type Process struct {
	state   *lua.LState
	threads []*Task
	queue   *TaskQueue
	layers  []Layer

	script        string
	scriptName    string
	proto         *lua.FunctionProto
	mainTask      *Task
	ctx           context.Context
	moduleBinders []ModuleBinder
	stateOpts     *lua.Options

	// reusable buffer for yielded tasks
	yieldBuf []*Task

	// pendingYield tracks task waiting for external result (e.g., time.sleep)
	pendingYield *Task
}

// NewProcess creates a new Lua process with options.
func NewProcess(opts ...ProcessOption) *Process {
	p := &Process{
		threads:  make([]*Task, 0, 4),
		queue:    NewTaskQueue(),
		layers:   make([]Layer, 0, 2),
		yieldBuf: make([]*Task, 0, 4),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Start initializes the process with context and input payloads.
func (p *Process) Start(ctx context.Context, input payload.Payloads) error {
	p.ctx = ctx

	// Create Lua state with optimized settings
	opts := lua.Options{
		RegistrySize:        128,
		RegistryMaxSize:     256 * 256,
		RegistryGrowStep:    16,
		SkipOpenLibs:        true,
		CallStackSize:       128,
		MinimizeStackMemory: true,
	}
	if p.stateOpts != nil {
		opts = *p.stateOpts
	}
	p.state = lua.NewState(opts)

	// Load core libs manually (no OS/IO for security)
	loadCoreLibs(p.state)
	p.state.SetContext(ctx)

	// Apply module binders
	for _, binder := range p.moduleBinders {
		binder(p.state)
	}

	// Create and store Resources in FrameContext
	res := NewResources()
	fc := ctxapi.FrameFromContext(ctx)
	if fc != nil {
		if err := fc.Set(ResourcesKey, res); err != nil {
			p.state.Close()
			return fmt.Errorf("failed to store resources: %w", err)
		}
	}

	// Load function - use precompiled proto if available, otherwise compile
	var fn *lua.LFunction
	if p.proto != nil {
		fn = p.state.LoadProto(p.proto)
	} else if p.script != "" {
		var err error
		fn, err = p.state.Load(strings.NewReader(p.script), p.scriptName)
		if err != nil {
			p.state.Close()
			return fmt.Errorf("failed to load script: %w", err)
		}
	} else {
		p.state.Close()
		return fmt.Errorf("no script or proto provided")
	}

	// Create main task
	p.mainTask = p.createTask(fn)

	// Convert input payloads to Lua values as arguments
	if len(input) > 0 {
		args := make([]lua.LValue, 0, len(input))
		for _, pl := range input {
			args = append(args, payloadToLua(p.state, pl))
		}
		p.mainTask.Resumed = args
	}

	return nil
}

// Step advances the process by one iteration.
func (p *Process) Step(results *scheduler.YieldResults) (scheduler.StepResult, error) {
	// Resume from handler results if any
	if results != nil {
		// Route results to the task that yielded the external command
		targetTask := p.pendingYield
		if targetTask == nil {
			targetTask = p.mainTask
		}

		if targetTask != nil {
			if results.Data != nil {
				if luaVals, ok := results.Data.([]lua.LValue); ok {
					targetTask.Resumed = luaVals
				}
			}
			// Re-queue task if it's in yield state
			if targetTask.State == lua.ResumeYield {
				p.queue.Push(targetTask)
			}
		}
		p.pendingYield = nil
	}

	// Process all tasks through layers (inner to outer)
	var externalTasks []*Task
	var err error

	if len(p.layers) > 0 {
		externalTasks, err = p.layers[0].Step(p)
		if err != nil {
			return scheduler.StepResult{Status: scheduler.StepDone}, err
		}
		for i := 1; i < len(p.layers); i++ {
			externalTasks, err = p.layers[i].Step(p, externalTasks...)
			if err != nil {
				return scheduler.StepResult{Status: scheduler.StepDone}, err
			}
		}
	} else {
		tasks := p.queue.Drain()
		externalTasks, err = p.vmStep(tasks...)
		if err != nil {
			return scheduler.StepResult{Status: scheduler.StepDone}, err
		}
	}

	// Check completion
	if len(externalTasks) == 0 && p.queue.IsEmpty() && len(p.threads) == 0 {
		return scheduler.StepResult{Status: scheduler.StepDone}, nil
	}

	// Convert external yields to commands
	var result scheduler.StepResult
	result.Status = scheduler.StepContinue

	for _, task := range externalTasks {
		if len(task.Yielded) == 0 {
			p.queue.Push(task)
			continue
		}

		// Check for scheduler commands in yielded values
		cmd := p.yieldToCommand(task)
		if cmd != nil {
			// Track which task yielded this command
			p.pendingYield = task

			if result.YieldCount < scheduler.MaxYields {
				result.YieldsBuf[result.YieldCount] = cmd
				result.YieldCount++
			} else {
				if result.Yields == nil {
					result.Yields = make([]scheduler.Command, 0, 8)
					for i := 0; i < scheduler.MaxYields; i++ {
						result.Yields = append(result.Yields, result.YieldsBuf[i])
					}
				}
				result.Yields = append(result.Yields, cmd)
			}
		} else {
			p.queue.Push(task)
		}
	}

	// Determine status
	if result.YieldCount == 0 && !p.queue.IsEmpty() {
		result.Status = scheduler.StepContinue
	} else if result.YieldCount == 0 && len(p.threads) > 0 {
		// Check if we're waiting for external messages (subscriptions)
		if HasSubscriptions(p) {
			result.Status = scheduler.StepIdle
		} else {
			// Deadlock: threads exist but nothing can progress
			return scheduler.StepResult{Status: scheduler.StepDone}, &DeadlockError{
				ThreadCount: len(p.threads),
				Message:     "all coroutines blocked with no pending operations",
			}
		}
	}

	return result, nil
}

// Send delivers an external message to the process.
func (p *Process) Send(pkg *relay.Package) error {
	res := GetResources(p.ctx)
	if res == nil {
		return fmt.Errorf("resources not found in context")
	}
	res.QueueMessage(pkg)
	return nil
}

// State returns the underlying Lua state.
func (p *Process) State() *lua.LState {
	return p.state
}

// GetTask retrieves a Task by its thread state.
func (p *Process) GetTask(thread *lua.LState) (*Task, error) {
	for _, task := range p.threads {
		if task.Thread() == thread {
			return task, nil
		}
	}
	return nil, fmt.Errorf("task not found")
}

// GetTasks returns all tasks.
func (p *Process) GetTasks() []*Task {
	return p.threads
}

// Queue returns the task queue.
func (p *Process) Queue() *TaskQueue {
	return p.queue
}

// vmStep executes VM step on tasks, returns yielded tasks.
func (p *Process) vmStep(tasks ...*Task) ([]*Task, error) {
	for _, t := range tasks {
		p.queue.Push(t)
	}

	// Reuse yield buffer
	p.yieldBuf = p.yieldBuf[:0]

	for !p.queue.IsEmpty() {
		task := p.queue.Pop()
		if task == nil {
			continue
		}

		if task.State != lua.ResumeYield {
			continue
		}

		state, err, values := p.state.ResumeInto(task.Thread(), task.Function(), task.retBuf, task.Resumed...)
		if err != nil {
			p.removeTask(task)
			return nil, err
		}

		task.State = state
		task.Yielded = values
		task.retBuf = values[:0:cap(values)]
		task.Resumed = nil

		if task.IsBlocked() {
			continue
		}

		switch state {
		case lua.ResumeYield:
			// Check for SpawnRequest
			if p.handleSpawnRequest(task, values) {
				continue
			}
			p.yieldBuf = append(p.yieldBuf, task)
		case lua.ResumeOK, lua.ResumeError:
			p.removeTask(task)
		}
	}

	return p.yieldBuf, nil
}

// createTask creates a new coroutine task.
func (p *Process) createTask(fn *lua.LFunction) *Task {
	thread, _ := p.state.NewThread()
	thread.SetContext(p.ctx)

	task := NewTask(thread, fn)
	p.threads = append(p.threads, task)
	p.queue.Push(task)

	return task
}

// removeTask removes a task from tracking.
func (p *Process) removeTask(task *Task) {
	for i, t := range p.threads {
		if t == task {
			task.Close()
			p.threads = append(p.threads[:i], p.threads[i+1:]...)
			return
		}
	}
}

// handleSpawnRequest checks if yielded values contain a SpawnRequest and handles it.
func (p *Process) handleSpawnRequest(task *Task, values []lua.LValue) bool {
	if len(values) == 0 {
		return false
	}

	// SpawnRequest is yielded directly
	req, ok := values[len(values)-1].(*lua.SpawnRequest)
	if !ok {
		return false
	}

	// Create new task and queue it
	newTask := p.createTask(req.Fn)
	lua.ReleaseSpawnRequest(req)

	// Resume original task with the new thread
	task.ResumeWith(newTask.Thread())
	task.Yielded = nil
	p.queue.Push(task)

	return true
}

// yieldToCommand converts yielded Lua values to scheduler commands.
func (p *Process) yieldToCommand(task *Task) scheduler.Command {
	if len(task.Yielded) == 0 {
		return nil
	}

	// Check last yielded value for convertible types
	lastValue := task.Yielded[len(task.Yielded)-1]
	cmd := ConvertYieldToCommand(lastValue)

	// Release pooled yield objects after conversion
	if sleepYield, ok := lastValue.(*SleepYield); ok && cmd != nil {
		ReleaseSleepYield(sleepYield)
	}

	return cmd
}

// Close releases all process resources.
func (p *Process) Close() {
	// Close resources via context if available
	if res := GetResources(p.ctx); res != nil {
		res.Close()
	}

	// Close all threads
	for _, task := range p.threads {
		task.Close()
	}
	p.threads = nil

	// Drain queue
	p.queue.Drain()

	// Close main state
	if p.state != nil {
		p.state.Close()
		p.state = nil
	}
}

// payloadToLua converts a payload to Lua value.
func payloadToLua(l *lua.LState, pl payload.Payload) lua.LValue {
	if pl == nil {
		return lua.LNil
	}

	data := pl.Data()
	switch v := data.(type) {
	case string:
		return lua.LString(v)
	case float64:
		return lua.LNumber(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case bool:
		return lua.LBool(v)
	case nil:
		return lua.LNil
	}

	// Fallback: convert via fmt
	return lua.LString(fmt.Sprintf("%v", data))
}

// loadCoreLibs loads core Lua libraries (no OS/IO for security).
func loadCoreLibs(state *lua.LState) {
	libs := []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
		{lua.CoroutineLibName, lua.OpenCoroutine},
	}

	for _, lib := range libs {
		state.Push(state.NewFunction(lib.fn))
		state.Push(lua.LString(lib.name))
		state.Call(1, 0)
	}
}
