package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/api/topology"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	lua "github.com/yuin/gopher-lua"
)

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

	// result holds the mainTask's return value when it completes
	result payload.Payload

	// execErr holds an error returned in second return value (Go's value, error pattern)
	execErr error

	// factory holds shared config (binders, state options)
	factory *Factory

	// reusable buffer for yielded tasks
	yieldBuf []*Task

	// pendingYields tracks yields waiting for external operations
	pendingYields map[uint64]*Task

	// yieldSeq is a monotonic counter for generating unique yield tags
	yieldSeq uint64

	// externalTasks reusable slice for non-channel tasks in processChannelYields
	externalTasks []*Task

	// outTasks reusable slice for output tasks in processSubscribeYields
	outTasks []*Task

	// exported caches method functions extracted from module table
	exported map[string]*lua.LFunction

	// Channel layer state (moved from ProcessContext)
	channelQueue *TaskQueue
	channels     map[*Channel]int

	// Subscribe layer state (moved from ProcessContext)
	subs *subscribeContext

	// Topic handlers for custom message processing
	handlers map[string]TopicHandler
}

// GetProcess retrieves the Process from LState via Owner.
// Returns nil if Owner is not set or not a Process.
func GetProcess(l *lua.LState) *Process {
	if l.G == nil || l.G.Owner == nil {
		return nil
	}
	p, _ := l.G.Owner.(*Process)
	return p
}

// Subscribe registers a channel to receive messages for a topic.
func (p *Process) Subscribe(topic string, ch *Channel) error {
	if p.subs == nil {
		return luaapi.ErrProcessContextNotAvailable
	}
	_, err := p.subs.add(topic, ch)
	return err
}

// SetTopicHandler registers a handler for a topic.
func (p *Process) SetTopicHandler(topic string, handler TopicHandler) {
	if p.handlers == nil {
		p.handlers = make(map[string]TopicHandler, 4)
	}
	p.handlers[topic] = handler
}

// GetTopicHandler retrieves a handler for a topic.
func (p *Process) GetTopicHandler(topic string) (TopicHandler, bool) {
	if p.handlers == nil {
		return nil, false
	}
	h, ok := p.handlers[topic]
	return h, ok
}

// RemoveTopicHandler removes a handler for a topic.
func (p *Process) RemoveTopicHandler(topic string) {
	delete(p.handlers, topic)
}

// ChannelQueue returns the channel layer task queue, creating it if needed.
func (p *Process) ChannelQueue() *TaskQueue {
	if p.channelQueue == nil {
		p.channelQueue = NewTaskQueue()
	}
	return p.channelQueue
}

// HasSubscriptions returns true if there are active subscriptions.
func (p *Process) HasSubscriptions() bool {
	if p.subs == nil {
		return false
	}
	p.subs.mu.RLock()
	defer p.subs.mu.RUnlock()
	return len(p.subs.byTopic) > 0
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
func (p *Process) Init(ctx context.Context, method string, input payload.Payloads) error {
	if p.state == nil {
		return luaapi.ErrStateNotInitialized
	}

	// Clear state from previous execution (for pooled processes)
	p.threads = p.threads[:0]
	p.queue.Drain()
	p.mainTask = nil
	p.pendingYields = nil

	// Set context for this execution
	p.ctx = ctx
	p.state.SetContext(ctx)

	// Set Owner for fast access from modules
	p.state.G.Owner = p

	// Create and store resource.Store in FrameContext
	store := resource.NewStore()
	if err := resource.SetStore(ctx, store); err != nil {
		if p.state != nil {
			p.state.Close()
		}
		return luaapi.NewStoreResourcesError(err)
	}

	// Initialize channel and subscription state
	if p.channels == nil {
		p.channels = make(map[*Channel]int, 4)
	} else {
		clear(p.channels)
	}
	if p.subs == nil {
		p.subs = &subscribeContext{
			byTopic:   make(map[string]*subscription, 4),
			byChannel: make(map[*Channel]string, 4),
		}
	} else {
		clear(p.subs.byTopic)
		clear(p.subs.byChannel)
	}
	clear(p.handlers)
	if p.channelQueue != nil {
		p.channelQueue.Drain()
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
				return luaapi.NewLoadScriptError(err)
			}
		} else {
			p.state.Close()
			return luaapi.ErrNoScriptOrProto
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
			return luaapi.NewLoadScriptError(err)
		}
	} else {
		return luaapi.ErrNoScriptOrProto
	}

	// Run script synchronously to get module table
	if err := p.state.CallByParam(lua.P{
		Fn:      scriptFn,
		NRet:    1,
		Protect: true,
	}); err != nil {
		return luaapi.NewExecuteScriptError(err)
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
		return luaapi.NewMethodNotFoundError(method)
	}

	p.exported[method] = fn
	return nil
}

// Step advances the process by one iteration.
// events contains yield completions and messages from the scheduler.
// out is the scheduler-owned buffer where the process writes yields and status.
func (p *Process) Step(events []process.Event, out *process.StepOutput) error {
	// Collect messages from events
	var messages []*relay.Package
	for _, ev := range events {
		switch ev.Type {
		case process.EventYieldComplete:
			if len(p.pendingYields) > 0 {
				p.distributeEvent(ev)
			}
		case process.EventMessage:
			if pkg, ok := ev.Data.(*relay.Package); ok {
				messages = append(messages, pkg)
			}
		}
	}

	// Process channel yields (inner layer)
	externalTasks, err := p.processChannelYields()
	if err != nil {
		p.clearExecution()
		out.Done(nil)
		return err
	}

	// Process subscribe yields (outer layer)
	externalTasks, err = p.processSubscribeYields(externalTasks, messages)
	if err != nil {
		p.clearExecution()
		out.Done(nil)
		return err
	}

	// Check completion
	if len(externalTasks) == 0 && p.queue.IsEmpty() && len(p.threads) == 0 {
		result := p.result
		execErr := p.execErr
		p.clearExecution()
		out.Done(result)
		return execErr
	}

	// Initialize pendingYields map if needed
	if p.pendingYields == nil {
		p.pendingYields = make(map[uint64]*Task, 4)
	}

	// Convert external yields to commands
	yieldCount := 0
	for _, task := range externalTasks {
		if len(task.Yielded) == 0 {
			p.queue.Push(task)
			continue
		}

		// Check for scheduler commands in yielded values
		cmd := p.yieldToCommand(task)
		if cmd != nil {
			p.yieldSeq++
			p.pendingYields[p.yieldSeq] = task
			out.Yield(cmd, p.yieldSeq)
			yieldCount++
		} else {
			p.queue.Push(task)
		}
	}

	// Determine status
	if yieldCount == 0 && !p.queue.IsEmpty() {
		out.Continue()
	} else if yieldCount == 0 && len(p.threads) > 0 {
		// Check if we're waiting for external operations
		if len(p.pendingYields) > 0 {
			out.Continue()
		} else if p.HasSubscriptions() || len(p.channels) > 0 {
			out.Idle()
		} else {
			p.clearExecution()
			out.Done(nil)
			return &luaapi.DeadlockError{
				ThreadCount: len(p.threads),
				Message:     "all coroutines blocked with no pending operations",
			}
		}
	}

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
	return nil, luaapi.ErrTaskNotFound
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
	// Fast path: no channels, run vmStep directly
	if p.channels == nil {
		return p.vmStep(p.queue.Drain()...)
	}

	channels := p.channels
	channelQueue := p.ChannelQueue()
	p.externalTasks = p.externalTasks[:0]

	// Transfer tasks from process queue to channel queue
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
			if !ok || result == nil {
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
						return nil, luaapi.NewTaskNotFoundForChannelError(err)
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
// messages are received via EventMessage events from the scheduler.
func (p *Process) processSubscribeYields(tasks []*Task, messages []*relay.Package) ([]*Task, error) {
	if p.subs == nil {
		return tasks, nil
	}

	subs := p.subs

	// Route incoming messages to subscribed channels
	for _, pkg := range messages {
		for _, msg := range pkg.Messages {
			topic := msg.Topic
			handlerTopic := topic
			sub, exists := subs.get(topic)
			if !exists {
				// Fallback to inbox for non-@ topics
				if !strings.HasPrefix(topic, "@") {
					sub, exists = subs.get(string(topology.TopicInbox))
					if exists {
						handlerTopic = string(topology.TopicInbox)
					}
				}
				if !exists {
					continue
				}
			}

			// Check for terminal payload - unsubscribe and close channel
			if len(msg.Payloads) == 1 && payload.IsTerminal(msg.Payloads[0]) {
				p.RemoveTopicHandler(topic)
				_ = subs.remove(sub.channel)
				sub.channel.Close(nil)
				continue
			}

			// Check for terminal at end of multi-payload message (result + terminal pattern)
			hasTerminal := len(msg.Payloads) > 1 && payload.IsTerminal(msg.Payloads[len(msg.Payloads)-1])
			payloads := msg.Payloads
			if hasTerminal {
				payloads = msg.Payloads[:len(msg.Payloads)-1]
			}

			// Check for topic handler
			var value lua.LValue
			if handler, ok := p.GetTopicHandler(handlerTopic); ok {
				value = handler(p.ctx, p.state, pkg.Source, topic, payloads)
				if value == nil {
					// Handler processed but doesn't want to send to channel
					if hasTerminal {
						p.RemoveTopicHandler(topic)
						_ = subs.remove(sub.channel)
						sub.channel.Close(nil)
					}
					continue
				}
			} else {
				value = PayloadsToLua(p.ctx, p.state, payloads)
			}
			result := sub.channel.Send(nil, value, nil)
			if result == nil {
				continue
			}

			// Wake any blocked receivers
			if result.Yields {
				for _, upd := range result.GetUpdates() {
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

			// Close channel after sending if terminal was present
			if hasTerminal {
				p.RemoveTopicHandler(topic)
				_ = subs.remove(sub.channel)
				sub.channel.Close(nil)
			}
		}
		relay.ReleasePackage(pkg)
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
				if req.Handler != nil {
					p.SetTopicHandler(req.Topic, req.Handler)
				}
				// Wrap channel as userdata if not already wrapped
				chValue := sub.channel.Value()
				if chValue == nil {
					PushChannel(task.thread, sub.channel)
					chValue = task.thread.Get(-1)
					task.thread.Pop(1)
				}
				task.ResumeWith(chValue)
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

// PayloadsToLua converts a slice of payloads to Lua value.
func PayloadsToLua(ctx context.Context, l *lua.LState, payloads []payload.Payload) lua.LValue {
	if len(payloads) == 0 {
		return lua.LNil
	}

	if len(payloads) == 1 {
		return transcodeToLua(ctx, payloads[0])
	}

	tbl := l.CreateTable(len(payloads), 0)
	for i, pl := range payloads {
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
		case lua.ResumeOK:
			// Capture mainTask's return value before removing
			if task == p.mainTask && len(values) > 0 {
				// Check for error in second return value (Go's value, error pattern)
				if len(values) >= 2 {
					if err := extractReturnError(values[1]); err != nil {
						p.result = nil
						p.execErr = err
						p.removeTask(task)
						continue
					}
				}
				p.result = luaconv.ExportPayload(values[0])
			}
			p.removeTask(task)
		case lua.ResumeError:
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
	var cmd dispatcher.Command
	if converter, ok := lastValue.(luaapi.YieldConverter); ok {
		cmd = converter.ToCommand()
	}

	// NOTE: Do NOT release the yield object here - the cmd shares the same
	// underlying data (e.g., CallYield.CallCmd). The yield object should be
	// released after the handler has processed the command (in handleYieldResults).
	// TODO: Rethink

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
		return lua.LNil, luaapi.ErrProcessNotInitialized
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

// distributeEvent routes a yield completion event to the correct pending task.
// Uses tag-based correlation for O(1) direct lookup.
func (p *Process) distributeEvent(ev process.Event) {
	if len(p.pendingYields) == 0 {
		return
	}

	if ev.Tag == 0 {
		return
	}

	task, exists := p.pendingYields[ev.Tag]
	if !exists || task == nil {
		return
	}

	p.resumeTaskWithResult(task, ev.Data, ev.Error)

	// Remove completed task from pending map
	delete(p.pendingYields, ev.Tag)
}

// resumeTaskWithResult converts handler result to Lua values and queues task.
func (p *Process) resumeTaskWithResult(task *Task, data any, err error) {
	if len(task.Yielded) > 0 {
		lastYield := task.Yielded[len(task.Yielded)-1]
		if hy, ok := lastYield.(luaapi.HandledYield); ok {
			task.Resumed = hy.HandleResult(p.state, data, err)
			if releasable, ok := lastYield.(luaapi.Releasable); ok {
				releasable.Release()
			}
		} else if luaVals, ok := data.([]lua.LValue); ok {
			task.Resumed = luaVals
		}
	} else if luaVals, ok := data.([]lua.LValue); ok {
		task.Resumed = luaVals
	}
	if task.State == lua.ResumeYield {
		p.queue.Push(task)
	}
}

// clearExecution clears coroutine tracking after execution completes.
// Called automatically by Step when returning StepDone.
// The Lua state is preserved for reuse.
// Note: Does NOT clear p.ctx - that is done by Reset() which is called
// by the scheduler after removing the process from the active map.
func (p *Process) clearExecution() {
	// Close all spawned threads but keep them referenced for GC
	for _, task := range p.threads {
		task.Close()
	}
	p.threads = p.threads[:0]

	// Drain queue
	p.queue.Drain()

	// Clear main task reference and result
	p.mainTask = nil
	p.pendingYields = nil
	p.result = nil
	p.execErr = nil

	// Clear channel/subscription state
	clear(p.channels)
	if p.subs != nil {
		clear(p.subs.byTopic)
		clear(p.subs.byChannel)
	}
	clear(p.handlers)
	if p.channelQueue != nil {
		p.channelQueue.Drain()
	}

	// Clear yield buffer
	p.yieldBuf = p.yieldBuf[:0]

	// Clear context from LState to release all references
	if p.state != nil {
		p.state.RemoveContext()
	}

	// Note: p.ctx is cleared by Reset(), not here
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

// extractReturnError checks if a Lua value represents an error in the
// second return position (following Go's value, error pattern).
// Returns the error if val is a string or a LuaError userdata, nil otherwise.
func extractReturnError(val lua.LValue) error {
	if val == nil || val == lua.LNil {
		return nil
	}

	// String error
	if s, ok := val.(lua.LString); ok {
		return luaapi.NewScriptReturnError(string(s))
	}

	// LuaError userdata
	if e, ok := lua.AsError(val); ok {
		return e
	}

	return nil
}

// wrapError wraps an error with Lua stack trace and metadata.
// If the error is already a lua.Error, extracts and returns it.
// Otherwise creates a new lua.Error with the current Lua stack.
func (p *Process) wrapError(thread *lua.LState, err error) error {
	if err == nil {
		return nil
	}

	// Check if already wrapped
	var e *lua.Error
	if errors.As(err, &e) {
		return e
	}

	// Wrap with stack trace from the provided thread (or main state)
	l := thread
	if l == nil {
		l = p.state
	}

	return lua.WrapErrorWithLua(l, err, "")
}
