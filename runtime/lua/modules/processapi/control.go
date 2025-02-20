package process

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
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
	return "process.control"
}

// Loader is the entry point for loading the module into Lua
func (m *ControlModule) Loader(l *lua.LState) int {
	// First load the info module functions
	m.info.Loader(l)

	// Get the process table that was just created by info module
	proc := l.GetGlobal("process")
	if proc == lua.LNil {
		// If for some reason process table doesn't exist, create it
		proc = l.NewTable()
		l.SetGlobal("process", proc)
	}

	procTable, ok := proc.(*lua.LTable)
	if !ok {
		l.RaiseError("process is not a table")
		return 0
	}

	// Add our control functions to the process table
	l.SetFuncs(procTable, map[string]lua.LGFunction{
		"send":            m.send,
		"spawn":           m.spawn,
		"spawn_monitored": m.spawnMonitored,
		"terminate":       m.terminate,
	})

	// No need to push anything since we're extending the existing table
	return 0
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

	procCtx := process.GetProcessContext(ctx)
	if procCtx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no process context found"))
		return nil, false
	}

	return procCtx, true
}
