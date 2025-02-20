package process

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"strings"
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
		"events": m.events, // New dedicated events function

		"send":            m.send,
		"spawn":           m.spawn,
		"spawn_monitored": m.spawnMonitored,
		"terminate":       m.terminate,
	})

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
	// Get node
	node, ok := m.getNode(l)
	if !ok {
		return 2
	}

	self, ok := m.checkProcess(l)
	if !ok {
		return 2
	}

	// Parse arguments
	pidStr := l.CheckString(1)
	topic := l.CheckString(2)
	msg := l.CheckAny(3)

	// Parse PID
	pid, err := pubsub.ParsePID(pidStr)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create batch
	batch := pubsub.NewBatch(topic, payload.NewPayload(msg, payload.Lua))

	// Send message using node
	if err := node.Send(l.Context(), pid, batch); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("message sent",
		zap.String("from", self.PID.String()),
		zap.String("pid", pid.String()),
		zap.String("topic", topic),
	)

	l.Push(lua.LTrue)
	return 1
}

func (m *ControlModule) spawn(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
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
		HostID:   pubsub.HostID(hostID),
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

	// Return PID string
	l.Push(lua.LString(pid.String()))
	return 1
}

func (m *ControlModule) terminate(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
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

// Add events function for internal messaging
func (m *ControlModule) events(l *lua.LState) int {
	procCtx, ok := m.checkProcess(l)
	if !ok {
		return 2
	}

	// Create events channel using internal @pid/events topic
	eventsName := fmt.Sprintf("events.%s", procCtx.PID)
	ch := channel.Named(eventsName, 1)

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
