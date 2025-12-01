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
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
	lua "github.com/yuin/gopher-lua"
)

// ChannelLayerKey is the context key for channel layer state in FrameContext.
var ChannelLayerKey = &ctxapi.Key{Name: "engine.channel_layer", Inherit: false}

// SubscribeLayerKey is the context key for subscribe layer state.
var SubscribeLayerKey = &ctxapi.Key{Name: "engine.subscribe_layer", Inherit: false}

// channelLayerContext holds per-process channel layer state.
type channelLayerContext struct {
	queue    *TaskQueue
	channels map[*Channel]int
}

// subscribeContext manages topic-to-channel mappings.
type subscribeContext struct {
	byTopic   map[string]*subscription
	byChannel map[*Channel]string
	mu        sync.RWMutex
}

func newSubscribeContext() *subscribeContext {
	return &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}
}

func (m *subscribeContext) add(topic string, ch *Channel) (*subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.byTopic[topic]; exists {
		if existing.channel != ch {
			return nil, fmt.Errorf("topic %q already subscribed", topic)
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
		return fmt.Errorf("channel not found")
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

// subscribeLayerContext holds per-process subscribe state.
type subscribeLayerContext struct {
	subs *subscribeContext
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
	fc := ctxapi.FrameFromContext(proc.ctx)
	if fc == nil {
		return nil
	}

	val, ok := fc.Get(ChannelLayerKey)
	if !ok {
		return nil
	}

	lctx := val.(*channelLayerContext)
	result := make([]ActiveChannel, 0, len(lctx.channels))
	for ch, refs := range lctx.channels {
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
	if proc.ctx == nil {
		return false
	}
	fc := ctxapi.FrameFromContext(proc.ctx)
	if fc == nil {
		return false
	}

	val, ok := fc.Get(SubscribeLayerKey)
	if !ok {
		return false
	}

	lctx := val.(*subscribeLayerContext)
	lctx.subs.mu.RLock()
	defer lctx.subs.mu.RUnlock()
	return len(lctx.subs.byTopic) > 0
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
	if converter, ok := value.(lua2api.YieldConverter); ok {
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

	script        string
	scriptName    string
	proto         *lua.FunctionProto
	mainTask      *Task
	ctx           context.Context
	moduleBinders []ModuleBinder
	stateOpts     *lua.Options

	// reusable buffer for yielded tasks
	yieldBuf []*Task

	// pendingYields tracks tasks waiting for external results
	// Uses fixed buffer for common case (up to 4 concurrent yields)
	pendingYieldsBuf [4]*Task
	pendingYields    []*Task

	// exported caches method functions extracted from module table
	exported map[string]*lua.LFunction

	// inbox holds incoming relay messages for this process
	inboxMu sync.Mutex
	inbox   []*relay.Package
}

// NewProcess creates a new Lua process with options.
func NewProcess(opts ...ProcessOption) *Process {
	p := &Process{
		threads:  make([]*Task, 0, 4),
		queue:    NewTaskQueue(),
		yieldBuf: make([]*Task, 0, 4),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Init initializes the Lua state without creating execution threads.
// Does not take context - context is set in Execute.
func (p *Process) Init() error {
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

	loadCoreLibs(p.state)

	for _, binder := range p.moduleBinders {
		binder(p.state)
	}

	return nil
}

// Execute starts execution of a method with context and input payloads.
// If not initialized, calls Init first. For pooled processes, reuses existing state.
// Only one Execute can run at a time per process - results come from Step(StepDone).
// If method is specified, the script is run once to get module table, then the method is called.
func (p *Process) Execute(ctx context.Context, method string, input payload.Payloads) error {
	if p.state == nil {
		if err := p.Init(); err != nil {
			return err
		}
	} else {
		// Clear state from previous execution
		p.threads = p.threads[:0]
		p.queue.Drain()
		p.mainTask = nil
		p.pendingYields = nil
	}

	// Set context for this execution
	p.ctx = ctx
	p.state.SetContext(ctx)

	// Create and store resource.Store in FrameContext
	store := resource.NewStore()
	if err := resource.SetStore(ctx, store); err != nil {
		if p.state != nil {
			p.state.Close()
		}
		return fmt.Errorf("failed to store resources: %w", err)
	}

	// Seal the frame to finalize context and break any parent references
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
				return fmt.Errorf("failed to load script: %w", err)
			}
		} else {
			p.state.Close()
			return fmt.Errorf("no script or proto provided")
		}
	}

	// Create main task
	p.mainTask = p.createTask(fn)

	// Convert input payloads to Lua values as arguments
	if len(input) > 0 {
		args := make([]lua.LValue, 0, len(input))
		for _, pl := range input {
			args = append(args, PayloadToLua(p.state, pl))
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
			return fmt.Errorf("failed to load script: %w", err)
		}
	} else {
		return fmt.Errorf("no script or proto provided")
	}

	// Run script synchronously to get module table
	if err := p.state.CallByParam(lua.P{
		Fn:      scriptFn,
		NRet:    1,
		Protect: true,
	}); err != nil {
		return fmt.Errorf("failed to execute script: %w", err)
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
		return fmt.Errorf("method %q not found in module", method)
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

// Send delivers an external message to the process.
func (p *Process) Send(pkg *relay.Package) error {
	p.inboxMu.Lock()
	p.inbox = append(p.inbox, pkg)
	p.inboxMu.Unlock()
	return nil
}

// DrainInbox returns and clears all incoming messages.
func (p *Process) DrainInbox() []*relay.Package {
	p.inboxMu.Lock()
	msgs := p.inbox
	p.inbox = p.inbox[:0]
	p.inboxMu.Unlock()
	return msgs
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

// processChannelYields handles channel operations internally.
func (p *Process) processChannelYields() ([]*Task, error) {
	lctx := p.ensureChannelContext()
	if lctx == nil {
		// No FrameContext - just run vmStep without channel handling
		tasks := p.queue.Drain()
		return p.vmStep(tasks...)
	}

	externalTasks := make([]*Task, 0)

	// Process all queued tasks
	boot := true
	for !lctx.queue.IsEmpty() || boot {
		boot = false

		// Drain to batch
		batch := lctx.queue.Drain()

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
				externalTasks = append(externalTasks, task)
				continue
			}

			// Update channel references
			p.updateChannelRefs(lctx, result.Block, result.Release)

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
						return nil, fmt.Errorf("task not found for channel result: %w", err)
					}

					if upd.Error != nil {
						t.ResumeWith(lua.LNil, lua.LString(upd.Error.Error()))
					} else {
						t.ResumeWith(upd.GetResult()...)
					}

					lctx.queue.Push(t)
				}
			}

			ReleaseResult(result)
		}
	}

	return externalTasks, nil
}

// ensureChannelContext gets or creates the channel layer context.
func (p *Process) ensureChannelContext() *channelLayerContext {
	if p.ctx == nil {
		return nil
	}
	fc := ctxapi.FrameFromContext(p.ctx)
	if fc == nil {
		return nil
	}

	if val, ok := fc.Get(ChannelLayerKey); ok {
		return val.(*channelLayerContext)
	}

	lctx := &channelLayerContext{
		queue:    NewTaskQueue(),
		channels: make(map[*Channel]int),
	}

	// Transfer any existing tasks from process queue to layer queue
	for _, task := range p.queue.Drain() {
		lctx.queue.Push(task)
	}

	fc.Set(ChannelLayerKey, lctx)
	return lctx
}

// updateChannelRefs handles reference counting for channels.
func (p *Process) updateChannelRefs(lctx *channelLayerContext, blocks, releases []*Channel) {
	for _, ch := range blocks {
		if _, exists := lctx.channels[ch]; !exists {
			lctx.channels[ch] = 0
		}
		lctx.channels[ch]++
	}

	for _, ch := range releases {
		if _, exists := lctx.channels[ch]; exists {
			lctx.channels[ch]--
			if lctx.channels[ch] == 0 {
				delete(lctx.channels, ch)
			}
		}
	}
}

// processSubscribeYields routes incoming messages and handles subscribe/unsubscribe.
func (p *Process) processSubscribeYields(tasks []*Task) ([]*Task, error) {
	lctx := p.ensureSubscribeContext()
	if lctx == nil {
		return tasks, nil
	}

	// Route incoming messages to subscribed channels
	for _, pkg := range p.DrainInbox() {
		for _, msg := range pkg.Messages {
			topic := string(msg.Topic)
			if sub, exists := lctx.subs.get(topic); exists {
				value := messageToLua(p.state, msg)
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
	var outTasks []*Task
	for _, task := range tasks {
		if len(task.Yielded) == 0 {
			outTasks = append(outTasks, task)
			continue
		}

		lastYield := task.Yielded[len(task.Yielded)-1]

		// Handle subscribe request
		if req, ok := lastYield.(*SubscribeRequest); ok {
			sub, err := lctx.subs.add(req.Topic, req.Channel)
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
			err := lctx.subs.remove(req.Channel)
			if err != nil {
				task.ResumeWith(lua.LFalse, lua.LString(err.Error()))
			} else {
				req.Channel.Close(nil)
				task.ResumeWith(lua.LTrue)
			}
			p.queue.Push(task)
			continue
		}

		outTasks = append(outTasks, task)
	}

	return outTasks, nil
}

// ensureSubscribeContext gets or creates the subscribe layer context.
func (p *Process) ensureSubscribeContext() *subscribeLayerContext {
	if p.ctx == nil {
		return nil
	}
	fc := ctxapi.FrameFromContext(p.ctx)
	if fc == nil {
		return nil
	}

	if val, ok := fc.Get(SubscribeLayerKey); ok {
		return val.(*subscribeLayerContext)
	}

	lctx := &subscribeLayerContext{
		subs: newSubscribeContext(),
	}
	fc.Set(SubscribeLayerKey, lctx)
	return lctx
}

// messageToLua converts a relay.Message to Lua value.
func messageToLua(l *lua.LState, msg *relay.Message) lua.LValue {
	if len(msg.Payloads) == 0 {
		return lua.LString(msg.Topic)
	}

	if len(msg.Payloads) == 1 {
		return PayloadToLua(l, msg.Payloads[0])
	}

	tbl := l.CreateTable(len(msg.Payloads), 0)
	for i, pl := range msg.Payloads {
		tbl.RawSetInt(i+1, PayloadToLua(l, pl))
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

		state, err, values := p.state.ResumeInto(task.Thread(), task.Function(), task.retBuf, task.Resumed...)
		if err != nil {
			p.removeTask(task)
			return nil, p.wrapError(task.Thread(), err)
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
func (p *Process) Close() {
	// Close resource store via context if available
	if p.ctx != nil {
		if store := resource.GetStore(p.ctx); store != nil {
			store.Close()
		}
	}

	// Clear inbox
	p.inboxMu.Lock()
	p.inbox = p.inbox[:0]
	p.inboxMu.Unlock()

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
		return lua.LNil, fmt.Errorf("process not initialized")
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
				if releasable, ok := lastYield.(lua2api.Releasable); ok {
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
func (p *Process) clearExecution() {
	// Close resource store for this execution
	if p.ctx != nil {
		if store := resource.GetStore(p.ctx); store != nil {
			store.Close()
		}
	}

	// Clear inbox
	p.inboxMu.Lock()
	p.inbox = p.inbox[:0]
	p.inboxMu.Unlock()

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

	// Clear context
	p.ctx = nil
}

// PayloadToLua converts a payload to Lua value.
func PayloadToLua(l *lua.LState, pl payload.Payload) lua.LValue {
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
// Uses restricted package loader that only supports preload table.
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
		{lua.LoadLibName, OpenRestrictedPackage},
	}

	for _, lib := range libs {
		state.Push(state.NewFunction(lib.fn))
		state.Push(lua.LString(lib.name))
		state.Call(1, 0)
	}
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
