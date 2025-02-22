package process

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"strings"
	"time"
)

// ControlModule represents the process control extension module
type ControlModule struct {
	log  *zap.Logger
	info *Module // Base process info module
}

// NewProcessControlModule creates a new process control module
func NewProcessControlModule(log *zap.Logger) *ControlModule {
	return &ControlModule{
		log:  log,
		info: NewProcessContextModule(log), // Create the base info module
	}
}

// Name returns the module name
func (m *ControlModule) Name() string {
	return "process"
}

// Loader is the entry point for loading the module into Lua
func (m *ControlModule) Loader(l *lua.LState) int {
	// Create module table
	mod := l.NewTable()

	// Register functions
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"info":       m.info.info,
		"pid":        m.info.pid,
		"input_args": m.info.initArgs,

		"listen": m.listen,
		"events": m.events,

		"send":            m.send,
		"spawn":           m.spawn,
		"spawn_monitored": m.spawnMonitored,
		"terminate":       m.terminate,
		"cancel":          m.cancel,
	})

	mod.RawSetString("EVENT_CANCEL", lua.LString(topology.KindCancel))
	mod.RawSetString("EVENT_RESULT", lua.LString(topology.KindResult))

	l.Push(mod)
	return 1
}

func (m *ControlModule) getNode(l *lua.LState) (pubsub.Node, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return nil, false
	}

	node := pubsub.GetNode(ctx)
	if node == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no node found in context"))
		return nil, false
	}

	return node, true
}

func (m *ControlModule) getProcessManager(l *lua.LState) (process.Manager, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return nil, false
	}

	manager := process.GetProcessManager(ctx)
	if manager == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no process manager found"))
		return nil, false
	}

	return manager, true
}

func (m *ControlModule) send(l *lua.LState) int {
	node, ok := m.getNode(l)
	if !ok {
		return 2
	}

	self, ok := m.checkProcess(l)
	if !ok {
		return 2
	}

	// Parse required arguments
	pidStr := l.CheckString(1)
	topic := l.CheckString(2)

	// Validate topic - prevent @ topics
	if strings.HasPrefix(topic, "@") {
		l.Push(lua.LNil)
		l.Push(lua.LString("cannot send to @ topics"))
		return 2
	}

	// Parse PID
	pid, err := pubsub.ParsePID(pidStr)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create message batch from variadic arguments
	var messages []*pubsub.Message
	for i := 3; i <= l.GetTop(); i++ {
		messages = append(messages, &pubsub.Message{
			Topic:    topic,
			Payloads: []payload.Payload{payload.NewPayload(l.Get(i), payload.Lua)},
		})
	}

	// Create package with all messages
	pkg := &pubsub.Package{
		PID:      pid,
		Messages: messages,
	}

	// Send message using node
	if err := node.Send(pkg); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("messages sent",
		zap.String("from", self.PID.String()),
		zap.String("pid", pid.String()),
		zap.String("topic", topic),
		zap.Int("count", len(messages)),
	)

	l.Push(lua.LTrue)
	return 1
}

func (m *ControlModule) spawn(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := m.checkProcess(l)
	if !ok {
		return 2
	}

	// Get required arguments
	id := l.CheckString(1) // This should be in format "namespace:name"
	hostID := l.CheckString(2)

	// Optional args table
	var payloads payload.Payloads
	if l.GetTop() > 2 {
		args := l.CheckTable(3)
		// Convert Lua table to payloads
		payloads = append(payloads, payload.NewPayload(args, payload.Lua))
	}

	// Parse registry ID
	regID := registry.ParseID(id)

	// Create start process config
	start := &process.StartProcess{
		HostID:   pubsub.HostID(hostID),
		ID:       regID,
		Payloads: payloads,
	}

	// Start the process
	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process spawned",
		zap.String("from", self.PID.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
	)

	// Return PID string
	l.Push(lua.LString(pid.String()))
	return 1
}

func (m *ControlModule) spawnMonitored(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2
	}

	// Get current process context
	procCtx, ok := m.checkProcess(l)
	if !ok {
		return 2
	}

	// Get required arguments
	id := l.CheckString(1) // This should be in format "namespace:name"
	hostID := l.CheckString(2)

	// Optional args table
	var payloads payload.Payloads
	if l.GetTop() > 2 {
		args := l.CheckTable(3)
		// Convert Lua table to payloads
		payloads = append(payloads, payload.NewPayload(args, payload.Lua))
	}

	// Parse registry ID
	regID := registry.ParseID(id)

	// Create start process config
	start := &process.StartProcess{
		HostID:   hostID,
		ID:       regID,
		Payloads: payloads,
	}

	// Start the process with monitoring
	pid, err := manager.StartMonitored(l.Context(), procCtx.PID, start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("monitored process spawned",
		zap.String("from", procCtx.PID.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
	)

	// Return PID string
	l.Push(lua.LString(pid.String()))
	return 1
}

func (m *ControlModule) terminate(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := m.checkProcess(l)
	if !ok {
		return 2
	}

	// Parse PID argument
	pidStr := l.CheckString(1)
	pid, err := pubsub.ParsePID(pidStr)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Terminate process
	if err := manager.Terminate(l.Context(), pid); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process terminated",
		zap.String("from", self.PID.String()),
		zap.String("pid", pid.String()),
	)

	l.Push(lua.LTrue)
	return 1
}

func (m *ControlModule) cancel(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2
	}

	// Require both PID and deadline arguments
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("cancel requires two arguments: pid and deadline"))
		return 2
	}

	// Parse PID argument
	pidStr := l.CheckString(1)
	pid, err := pubsub.ParsePID(pidStr)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Get current process context
	procCtx, ok := m.checkProcess(l)
	if !ok {
		return 2
	}

	// Parse required deadline argument
	var deadline time.Time
	switch l.Get(2).Type() {
	case lua.LTString:
		// Parse as duration string (e.g. "1s", "500ms")
		durationStr := l.CheckString(2)
		duration, err := time.ParseDuration(durationStr)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("invalid duration format: %v", err)))
			return 2
		}
		deadline = time.Now().Add(duration)

	case lua.LTNumber:
		// Parse as milliseconds
		ms := l.CheckNumber(2)
		deadline = time.Now().Add(time.Duration(ms) * time.Millisecond)

	default:
		l.Push(lua.LNil)
		l.Push(lua.LString("deadline must be either a duration string or milliseconds number"))
		return 2
	}

	// Cancel process with deadline
	if err := manager.Cancel(l.Context(), procCtx.PID, pid, deadline); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process cancel requested",
		zap.String("from", procCtx.PID.String()),
		zap.String("pid", pid.String()),
		zap.Time("deadline", deadline),
	)

	l.Push(lua.LTrue)
	return 1
}

// checkProcess validates context and returns process context if valid
func (m *ControlModule) checkProcess(l *lua.LState) (*process.Context, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return nil, false
	}

	procCtx := process.GetContext(ctx)
	if procCtx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no process context found"))
		return nil, false
	}

	return procCtx, true
}

// AddCleanup events function for internal messaging
func (m *ControlModule) events(l *lua.LState) int {
	ch := channel.Named(process.TopicEvents, 1)

	return subscribe.Subscribe(l, ch, process.TopicEvents)
}

// Modified listen function with @ validation
func (m *ControlModule) listen(l *lua.LState) int {
	topic := l.CheckString(1)
	if topic == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("topic cannot be empty"))
		return 2
	}

	// Prevent usage of @ topics in ports
	if strings.HasPrefix(topic, "@") {
		l.Push(lua.LNil)
		l.Push(lua.LString("cannot use @ topics with ports"))
		return 2
	}

	portName := fmt.Sprintf("listen.%s", topic)
	ch := channel.Named(portName, 1)

	return subscribe.Subscribe(l, ch, topic)
}
