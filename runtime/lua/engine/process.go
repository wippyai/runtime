package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
)

// subscribeContext manages topic-to-channel mappings.
type subscribeContext struct {
	byTopic   map[string]*subscription
	byChannel map[*Channel]string
	mu        sync.RWMutex
}

func (m *subscribeContext) add(topic string, ch *Channel) (*subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.byTopic[topic]; exists {
		if existing.channel != ch {
			return nil, NewTopicAlreadySubscribedError(topic)
		}
		return existing, nil
	}

	sub := &subscription{topic: topic, channel: ch}
	m.byTopic[topic] = sub
	m.byChannel[ch] = topic
	return sub, nil
}

func (m *subscribeContext) remove(ch *Channel) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	topic, exists := m.byChannel[ch]
	if !exists {
		return ErrChannelNotFound
	}
	delete(m.byTopic, topic)
	delete(m.byChannel, ch)
	return nil
}

func (m *subscribeContext) get(topic string) (*subscription, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sub, exists := m.byTopic[topic]
	return sub, exists
}

// subscription links a topic to a channel.
type subscription struct {
	topic   string
	channel *Channel
}

// SubscribeRequest is yielded to request a topic subscription.
type SubscribeRequest struct {
	Topic   string
	Channel *Channel
}

func (r *SubscribeRequest) String() string       { return "<subscribe_request>" }
func (r *SubscribeRequest) Type() lua.LValueType { return lua.LTUserData }

// UnsubscribeRequest is yielded to unsubscribe a channel.
type UnsubscribeRequest struct {
	Channel *Channel
}

func (r *UnsubscribeRequest) String() string       { return "<unsubscribe_request>" }
func (r *UnsubscribeRequest) Type() lua.LValueType { return lua.LTUserData }

// ActiveChannel represents a channel blocking execution.
type ActiveChannel struct {
	Name  string
	Slots int
	Refs  int
}

// GetActiveChannels returns channels currently blocking execution.
func GetActiveChannels(proc *Process) []ActiveChannel {
	pc := GetProcessContext(proc.ctx)
	if pc == nil {
		return nil
	}

	channels := pc.Channels()
	result := make([]ActiveChannel, 0, len(channels))
	for ch, refs := range channels {
		result = append(result, ActiveChannel{
			Name:  ch.Name(),
			Slots: ch.Slots(),
			Refs:  refs,
		})
	}

	return result
}

// HasSubscriptions returns true if the process has any active subscriptions.
func HasSubscriptions(proc *Process) bool {
	pc := GetProcessContext(proc.ctx)
	if pc == nil {
		return false
	}
	return pc.HasSubscriptions()
}

// HandledYield is implemented by yields that know how to convert
// handler results back to Lua values. This allows each module to
// define its own result conversion without central dispatch.
type HandledYield interface {
	lua.LValue
	HandleResult(l *lua.LState, data any, err error) []lua.LValue
}

// ConvertYieldToCommand attempts to convert a Lua yield value to a scheduler command.
func ConvertYieldToCommand(value lua.LValue) dispatcher.Command {
	if converter, ok := value.(luaapi.YieldConverter); ok {
		return converter.ToCommand()
	}
	return nil
}

// ProcessOption configures a Process.
type ProcessOption func(*Process)

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

// WithModuleBinder adds module binders via inline factory.
// For high-performance use cases, prefer creating a Factory directly.
func WithModuleBinder(binder ModuleBinder) ProcessOption {
	return func(p *Process) {
		if p.factory == nil {
			p.factory = &Factory{}
		}
		p.factory.moduleBinders = append(p.factory.moduleBinders, binder)
	}
}

// WithStateOptions sets custom Lua state options via inline factory.
// For high-performance use cases, prefer creating a Factory directly.
func WithStateOptions(opts lua.Options) ProcessOption {
	return func(p *Process) {
		if p.factory == nil {
			p.factory = &Factory{}
		}
		p.factory.stateOpts = &opts
	}
}

// Process implements scheduler.Process for Lua execution.
// Combines VM + CVM + Runner into a single unit.
// Request-specific state is stored in ProcessContext (via FrameContext), not here.
// Module binders and state options are stored in Factory for sharing across processes.
type Process struct {
	state   *lua.LState
	threads []*Task
	queue   *TaskQueue

	script     string
	scriptName string
	proto      *lua.FunctionProto
	mainTask   *Task
	ctx        context.Context

	// factory holds shared config (binders, state options)
	factory *Factory

	// reusable buffer for yielded tasks
	yieldBuf []*Task

	// pendingYields tracks tasks waiting for external results
	// Uses fixed buffer for common case (up to 4 concurrent yields)
	pendingYieldsBuf [4]*Task
	pendingYields    []*Task

	// externalTasks reusable slice for non-channel tasks in processChannelYields
	externalTasks []*Task

	// outTasks reusable slice for output tasks in processSubscribeYields
	outTasks []*Task

	// exported caches method functions extracted from module table
	exported map[string]*lua.LFunction
}

// NewProcess creates a new Lua process with options.
// Uses Factory internally to ensure state is properly initialized.
func NewProcess(opts ...ProcessOption) *Process {
	// Create a temporary process to extract options
	tmp := &Process{factory: &Factory{}}
	for _, opt := range opts {
		opt(tmp)
	}

	// Merge options into factory
	tmp.factory.proto = tmp.proto
	tmp.factory.script = tmp.script
	tmp.factory.scriptName = tmp.scriptName

	// Initialize state via factory
	tmp.threads = make([]*Task, 0, 4)
	tmp.queue = NewTaskQueue()
	tmp.yieldBuf = make([]*Task, 0, 4)
	tmp.externalTasks = make([]*Task, 0, 8)
	tmp.outTasks = make([]*Task, 0, 8)
	tmp.state = tmp.factory.CreateState()

	return tmp
}

// Execute starts execution of a method with context and input payloads.
// State must be initialized by Factory - processes are created via Factory.Create().
// Only one Execute can run at a time per process - results come from Step(StepDone).
// If method is specified, the script is run once to get module table, then the method is called.
func (p *Process) Execute(ctx context.Context, method string, input payload.Payloads) error {
	if p.state == nil {
		return ErrStateNotInitialized
	}

	// Clear state from previous execution (for pooled processes)
	p.threads = p.threads[:0]
	p.queue.Drain()
	p.mainTask = nil
	p.pendingYields = nil

	// Set context for this execution
	p.ctx = ctx
	p.state.SetContext(ctx)

	// Create and store resource.Store in FrameContext
	store := resource.NewStore()
	if err := resource.SetStore(ctx, store); err != nil {
		if p.state != nil {
			p.state.Close()
		}
		return NewStoreResourcesError(err)
	}

	// Create and store ProcessContext for request-specific state
	pc := acquireProcessContext()
	if err := setProcessContext(ctx, pc); err != nil {
		_ = pc.Close()
		return NewStoreProcessContextError(err)
	}

	// Seal the frame - no more modifications allowed after this
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		fc.Seal()
	}

	// Determine which function to execute
	var fn *lua.LFunction

	// If method specified, try to use cached function or extract from module
	if method != "" {
		if p.exported != nil {
			if cached, ok := p.exported[method]; ok {
				fn = cached
			}
		}

		// No cached function - need to run script to get module and extract method
		if fn == nil {
			if err := p.extractMethod(method); err != nil {
				return err
			}
			fn = p.exported[method]
		}
	} else {
		// No method - run the script directly (legacy behavior)
		if p.proto != nil {
			fn = p.state.LoadProto(p.proto)
		} else if p.script != "" {
			var err error
			fn, err = p.state.Load(strings.NewReader(p.script), p.scriptName)
			if err != nil {
				p.state.Close()
				return NewLoadScriptError(err)
			}
		} else {
			p.state.Close()
			return ErrNoScriptOrProto
		}
	}

	// Create main task
	p.mainTask = p.createTask(fn)

	// Convert input payloads to Lua values as arguments
	if len(input) > 0 {
		args := make([]lua.LValue, 0, len(input))
		for _, pl := range input {
			args = append(args, transcodeToLua(ctx, pl))
		}
		p.mainTask.Resumed = args
	}

	return nil
}

// extractMethod runs the script to get module table and extracts the method function.
func (p *Process) extractMethod(method string) error {
	// Load script function
	var scriptFn *lua.LFunction
	if p.proto != nil {
		scriptFn = p.state.LoadProto(p.proto)
	} else if p.script != "" {
		var err error
		scriptFn, err = p.state.Load(strings.NewReader(p.script), p.scriptName)
		if err != nil {
			return NewLoadScriptError(err)
		}
	} else {
		return ErrNoScriptOrProto
	}

	// Run script synchronously to get module table
	if err := p.state.CallByParam(lua.P{
		Fn:      scriptFn,
		NRet:    1,
		Protect: true,
	}); err != nil {
		return NewExecuteScriptError(err)
	}

	// Get return value
	ret := p.state.Get(-1)
	p.state.Pop(1)

	// Initialize exported map
	if p.exported == nil {
		p.exported = make(map[string]*lua.LFunction)
	}

	// Extract function from return value
	var fn *lua.LFunction
	switch v := ret.(type) {
	case *lua.LFunction:
		// Script returned function directly
		fn = v
	case *lua.LTable:
		// Script returned module table - extract method by name
		val := v.RawGetString(method)
		if val.Type() == lua.LTFunction {
			fn = val.(*lua.LFunction)
		}
	}

	if fn == nil {
		return NewMethodNotFoundError(method)
	}

	p.exported[method] = fn
	return nil
}

// Step advances the process by one iteration.
func (p *Process) Step(results *scheduler.YieldResults) (scheduler.StepResult, error) {
	// Resume from handler results if any
	if results != nil && len(p.pendingYields) > 0 {
		p.distributeResults(results)
		p.pendingYields = nil
	}

	// Process channel yields (inner layer)
	externalTasks, err := p.processChannelYields()
	if err != nil {
		p.clearExecution()
		return scheduler.StepResult{Status: scheduler.StepDone}, err
	}

	// Process subscribe yields (outer layer)
	externalTasks, err = p.processSubscribeYields(externalTasks)
	if err != nil {
		p.clearExecution()
		return scheduler.StepResult{Status: scheduler.StepDone}, err
	}

	// Check completion
	if len(externalTasks) == 0 && p.queue.IsEmpty() && len(p.threads) == 0 {
		p.clearExecution()
		return scheduler.StepResult{Status: scheduler.StepDone}, nil
	}

	// Convert external yields to commands
	var result scheduler.StepResult
	result.Status = scheduler.StepContinue

	// Reset pending yields, use fixed buffer for zero-alloc common case
	p.pendingYields = p.pendingYieldsBuf[:0]

	for _, task := range externalTasks {
		if len(task.Yielded) == 0 {
			p.queue.Push(task)
			continue
		}

		// Check for scheduler commands in yielded values
		cmd := p.yieldToCommand(task)
		if cmd != nil {
			// Track all tasks that yielded commands (in order)
			if len(p.pendingYields) < len(p.pendingYieldsBuf) {
				p.pendingYields = append(p.pendingYields, task)
			} else {
				// Overflow to heap allocation for rare case of many concurrent yields
				if cap(p.pendingYields) == len(p.pendingYieldsBuf) {
					overflow := make([]*Task, len(p.pendingYields), len(p.pendingYields)*2)
					copy(overflow, p.pendingYields)
					p.pendingYields = overflow
				}
				p.pendingYields = append(p.pendingYields, task)
			}
			result.AddYield(cmd)
		} else {
			p.queue.Push(task)
		}
	}

	// Determine status
	if result.YieldCount() == 0 && !p.queue.IsEmpty() {
		result.Status = scheduler.StepContinue
	} else if result.YieldCount() == 0 && len(p.threads) > 0 {
		// Check if we're waiting for external messages (subscriptions)
		if HasSubscriptions(p) {
			result.Status = scheduler.StepIdle
		} else {
			// Deadlock: threads exist but nothing can progress
			p.clearExecution()
			return scheduler.StepResult{Status: scheduler.StepDone}, &DeadlockError{
				ThreadCount: len(p.threads),
				Message:     "all coroutines blocked with no pending operations",
			}
		}
	}

	return result, nil
}

// Send delivers an external message to the process via ProcessContext.
func (p *Process) Send(pkg *relay.Package) error {
	pc := GetProcessContext(p.ctx)
	if pc == nil {
		return ErrProcessContextNotAvailable
	}
	pc.QueueMessage(pkg)
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
	return nil, ErrTaskNotFound
}

// GetTasks returns all tasks.
func (p *Process) GetTasks() []*Task {
	return p.threads
}

// Queue returns the task queue.
func (p *Process) Queue() *TaskQueue {
	return p.queue
}

// processChannelYields handles channel operations internally.
func (p *Process) processChannelYields() ([]*Task, error) {
	pc := GetProcessContext(p.ctx)
	if pc == nil {
		// No ProcessContext - just run vmStep without channel handling
		tasks := p.queue.Drain()
		return p.vmStep(tasks...)
	}

	channelQueue := pc.ChannelQueue()
	channels := pc.Channels()
	p.externalTasks = p.externalTasks[:0]

	// Transfer tasks from process queue to channel queue on first call
	for _, task := range p.queue.Drain() {
		channelQueue.Push(task)
	}

	// Process all queued tasks
	boot := true
	for !channelQueue.IsEmpty() || boot {
		boot = false

		// Drain to batch
		batch := channelQueue.Drain()

		// Run through VM step
		vmTasks, err := p.vmStep(batch...)
		if err != nil {
			return nil, err
		}

		// Process each yielded task
		for _, task := range vmTasks {
			if len(task.Yielded) == 0 {
				continue
			}

			// Check if yield is a channel operation
			value := task.Yielded[len(task.Yielded)-1]
			result, ok := value.(*ChannelResult)
			if !ok {
				p.externalTasks = append(p.externalTasks, task)
				continue
			}

			// Update channel references
			p.updateChannelRefs(channels, result.Block, result.Release)

			// Process updates from channel operation
			updates := result.GetUpdates()
			if result.Yields && len(updates) > 0 {
				for _, upd := range updates {
					if upd.State == nil {
						continue
					}

					t, err := p.GetTask(upd.State)
					if err != nil {
						ReleaseResult(result)
						return nil, NewTaskNotFoundForChannelError(err)
					}

					if upd.Error != nil {
						t.ResumeWith(lua.LNil, lua.LString(upd.Error.Error()))
					} else {
						t.ResumeWith(upd.GetResult()...)
					}

					channelQueue.Push(t)
				}
			}

			ReleaseResult(result)
		}
	}

	return p.externalTasks, nil
}

// updateChannelRefs handles reference counting for channels.
func (p *Process) updateChannelRefs(channels map[*Channel]int, blocks, releases []*Channel) {
	for _, ch := range blocks {
		if _, exists := channels[ch]; !exists {
			channels[ch] = 0
		}
		channels[ch]++
	}

	for _, ch := range releases {
		if _, exists := channels[ch]; exists {
			channels[ch]--
			if channels[ch] == 0 {
				delete(channels, ch)
			}
		}
	}
}

// processSubscribeYields routes incoming messages and handles subscribe/unsubscribe.
func (p *Process) processSubscribeYields(tasks []*Task) ([]*Task, error) {
	pc := GetProcessContext(p.ctx)
	if pc == nil {
		return tasks, nil
	}

	subs := pc.Subscriptions()

	// Route incoming messages to subscribed channels
	for _, pkg := range pc.DrainInbox() {
		for _, msg := range pkg.Messages {
			topic := string(msg.Topic)
			if sub, exists := subs.get(topic); exists {
				value := messageToLua(p.ctx, p.state, msg)
				result := sub.channel.Send(nil, value, nil)

				// Wake any blocked receivers
				updates := result.GetUpdates()
				if result.Yields && len(updates) > 0 {
					for _, upd := range updates {
						if upd.State == nil {
							continue
						}
						t, err := p.GetTask(upd.State)
						if err == nil {
							t.ResumeWith(upd.GetResult()...)
							p.queue.Push(t)
						}
					}
				}

				ReleaseResult(result)
			}
		}
	}

	// Handle subscribe/unsubscribe yields from incoming tasks
	p.outTasks = p.outTasks[:0]
	for _, task := range tasks {
		if len(task.Yielded) == 0 {
			p.outTasks = append(p.outTasks, task)
			continue
		}

		lastYield := task.Yielded[len(task.Yielded)-1]

		// Handle subscribe request
		if req, ok := lastYield.(*SubscribeRequest); ok {
			sub, err := subs.add(req.Topic, req.Channel)
			if err != nil {
				task.ResumeWith(lua.LNil, lua.LString(err.Error()))
			} else {
				task.ResumeWith(sub.channel.Value())
			}
			p.queue.Push(task)
			continue
		}

		// Handle unsubscribe request
		if req, ok := lastYield.(*UnsubscribeRequest); ok {
			err := subs.remove(req.Channel)
			if err != nil {
				task.ResumeWith(lua.LFalse, lua.LString(err.Error()))
			} else {
				req.Channel.Close(nil)
				task.ResumeWith(lua.LTrue)
			}
			p.queue.Push(task)
			continue
		}

		p.outTasks = append(p.outTasks, task)
	}

	return p.outTasks, nil
}

// messageToLua converts a relay.Message to Lua value.
func messageToLua(ctx context.Context, l *lua.LState, msg *relay.Message) lua.LValue {
	if len(msg.Payloads) == 0 {
		return lua.LString(msg.Topic)
	}

	if len(msg.Payloads) == 1 {
		return transcodeToLua(ctx, msg.Payloads[0])
	}

	tbl := l.CreateTable(len(msg.Payloads), 0)
	for i, pl := range msg.Payloads {
		tbl.RawSetInt(i+1, transcodeToLua(ctx, pl))
	}
	return tbl
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

		thread := task.Thread()
		state, err, values := p.state.ResumeInto(thread, task.Function(), task.retBuf, task.Resumed...)
		if err != nil {
			// Wrap error BEFORE removing task - removeTask closes the thread
			// which returns it to pool, causing race if another goroutine reuses it
			wrapped := p.wrapError(thread, err)
			p.removeTask(task)
			return nil, wrapped
		}

		task.State = state
		task.Yielded = values
		task.retBuf = values[:0:cap(values)]
		task.Resumed = nil

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
	thread := p.state.NewThreadWithContext(p.ctx)
	task := NewTask(thread, fn)
	p.threads = append(p.threads, task)
	p.queue.Push(task)

	return task
}

// removeTask removes a task from tracking.
func (p *Process) removeTask(task *Task) {
	n := len(p.threads)
	if n == 0 {
		return
	}
	for i := 0; i < n; i++ {
		if p.threads[i] == task {
			task.Close()
			// Remove by swap-with-last (O(1), order doesn't matter)
			last := n - 1
			if i != last && last >= 0 {
				p.threads[i] = p.threads[last]
			}
			p.threads = p.threads[:last]
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
func (p *Process) yieldToCommand(task *Task) dispatcher.Command {
	if len(task.Yielded) == 0 {
		return nil
	}

	// Check last yielded value for convertible types
	lastValue := task.Yielded[len(task.Yielded)-1]
	cmd := ConvertYieldToCommand(lastValue)

	// NOTE: Do NOT release the yield object here - the cmd shares the same
	// underlying data (e.g., CallYield.CallCmd). The yield object should be
	// released after the handler has processed the command (in handleYieldResults).

	return cmd
}

// Close releases all process resources.
// Called by scheduler when process completes or pool shuts down.
// Note: Per-execution resources (Store, ProcessContext) are automatically
// released when FrameContext is released - they implement ctxapi.Closer.
func (p *Process) Close() {
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

	// Clear context reference
	p.ctx = nil
}

// SyncExecute runs the script directly without coroutines or scheduler.
// This is a fast path for simple synchronous calls that don't need yields.
// The Lua state must be initialized via Start first.
func (p *Process) SyncExecute(ctx context.Context, args ...lua.LValue) (lua.LValue, error) {
	if p.state == nil {
		return lua.LNil, ErrProcessNotInitialized
	}

	p.state.SetContext(ctx)

	// Load function from proto
	fn := p.state.LoadProto(p.proto)

	// Call function directly
	if err := p.state.CallByParam(lua.P{
		Fn:      fn,
		NRet:    1,
		Protect: true,
	}, args...); err != nil {
		return lua.LNil, err
	}

	// Get result
	result := p.state.Get(-1)
	p.state.Pop(1)

	return result, nil
}

// distributeResults routes handler results to the correct pending tasks.
// For single yield: results.Data is the direct result
// For multiple yields: results.Data is []any with results in order
func (p *Process) distributeResults(results *scheduler.YieldResults) {
	n := len(p.pendingYields)
	if n == 0 {
		return
	}

	// Single yield case - direct routing (most common, optimized path)
	if n == 1 {
		task := p.pendingYields[0]
		p.resumeTaskWithResult(task, results.Data, results.Error)
		return
	}

	// Multiple yields case - results come as []any in order
	resultSlice, ok := results.Data.([]any)
	if !ok {
		// Fallback: if not a slice, route to first task only
		p.resumeTaskWithResult(p.pendingYields[0], results.Data, results.Error)
		for i := 1; i < n; i++ {
			task := p.pendingYields[i]
			if task.State == lua.ResumeYield {
				p.queue.Push(task)
			}
		}
		return
	}

	// Distribute each result to corresponding task
	for i := 0; i < n && i < len(resultSlice); i++ {
		task := p.pendingYields[i]
		p.resumeTaskWithResult(task, resultSlice[i], nil)
	}
}

// resumeTaskWithResult converts handler result to Lua values and queues task.
func (p *Process) resumeTaskWithResult(task *Task, data any, err error) {
	if data != nil || err != nil {
		if len(task.Yielded) > 0 {
			lastYield := task.Yielded[len(task.Yielded)-1]
			if hy, ok := lastYield.(HandledYield); ok {
				task.Resumed = hy.HandleResult(p.state, data, err)
				// Release the yield object now that the handler is done
				if releasable, ok := lastYield.(luaapi.Releasable); ok {
					releasable.Release()
				}
			} else if luaVals, ok := data.([]lua.LValue); ok {
				task.Resumed = luaVals
			}
		} else if luaVals, ok := data.([]lua.LValue); ok {
			task.Resumed = luaVals
		}
	}
	if task.State == lua.ResumeYield {
		p.queue.Push(task)
	}
}

// clearExecution clears coroutine tracking after execution completes.
// Called automatically by Step when returning StepDone.
// The Lua state is preserved for reuse.
// Note: ProcessContext and resource.Store are NOT released here -
// they are owned by the scheduler/host and released via OnComplete callback.
func (p *Process) clearExecution() {
	// Close all spawned threads but keep them referenced for GC
	for _, task := range p.threads {
		task.Close()
	}
	p.threads = p.threads[:0]

	// Drain queue
	p.queue.Drain()

	// Clear main task reference
	p.mainTask = nil
	p.pendingYields = nil

	// Clear yield buffer
	p.yieldBuf = p.yieldBuf[:0]

	// Clear context from LState to release all references
	if p.state != nil {
		p.state.RemoveContext()
	}

	// Clear context reference (but don't release ProcessContext - scheduler owns it)
	p.ctx = nil
}

// transcodeToLua converts a payload to Lua value using context transcoder.
func transcodeToLua(ctx context.Context, pl payload.Payload) lua.LValue {
	if pl == nil {
		return lua.LNil
	}

	// Already a Lua value
	if pl.Format() == payload.Lua {
		if lv, ok := pl.Data().(lua.LValue); ok {
			return lv
		}
	}

	// Try transcoding via context transcoder
	dtt := payload.GetTranscoder(ctx)
	if dtt != nil {
		transcoded, err := dtt.Transcode(pl, payload.Lua)
		if err == nil {
			if lv, ok := transcoded.Data().(lua.LValue); ok {
				return lv
			}
		}
	}

	// Fallback: return as string representation
	return lua.LString(fmt.Sprintf("%v", pl.Data()))
}

// wrapError wraps an error with Lua stack trace and metadata.
// If the error is already a lua.Error, extracts and returns it.
// Otherwise creates a new lua.Error with the current Lua stack.
func (p *Process) wrapError(thread *lua.LState, err error) error {
	if err == nil {
		return nil
	}

	// Check if already wrapped
	if e := lua.GetError(err); e != nil {
		return e
	}

	// Wrap with stack trace from the provided thread (or main state)
	l := thread
	if l == nil {
		l = p.state
	}

	return lua.WrapErrorWithLua(l, err, "")
}
