package process

import (
	"fmt"
	"strings"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module provides a unified process API for all contexts
type Module struct {
	log *zap.Logger
}

// NewProcessAPIModule creates a new unified process API module
func NewProcessAPIModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "process"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	mod := l.CreateTable(0, 16) // Size adjusted for all functions

	// Register process functions directly with RawSetString for better performance
	mod.RawSetString("id", l.NewFunction(m.id))
	mod.RawSetString("pid", l.NewFunction(m.pid))
	mod.RawSetString("send", l.NewFunction(m.send))
	mod.RawSetString("spawn", l.NewFunction(m.spawn))
	mod.RawSetString("spawn_monitored", l.NewFunction(m.spawnMonitored))
	mod.RawSetString("spawn_linked", l.NewFunction(m.spawnLinked))
	mod.RawSetString("spawn_linked_monitored", l.NewFunction(m.spawnLinkedMonitored))
	mod.RawSetString("terminate", l.NewFunction(m.terminate))
	mod.RawSetString("cancel", l.NewFunction(m.cancel))
	mod.RawSetString("get_options", l.NewFunction(m.getOptions))
	mod.RawSetString("set_options", l.NewFunction(m.setOptions))
	mod.RawSetString("monitor", l.NewFunction(m.monitor))
	mod.RawSetString("unmonitor", l.NewFunction(m.unmonitor))
	mod.RawSetString("link", l.NewFunction(m.link))
	mod.RawSetString("unlink", l.NewFunction(m.unlink))

	mod.RawSetString("with_context", l.NewFunction(m.withContext))

	// Create event constants table with exact size
	events := l.CreateTable(0, 3)
	events.RawSetString("CANCEL", lua.LString(topology.KindCancel))
	events.RawSetString("EXIT", lua.LString(topology.KindExit))
	events.RawSetString("LINK_DOWN", lua.LString(topology.KindLinkDown))
	mod.RawSetString("event", events)

	// Registry table with exact size
	reg := l.CreateTable(0, 3)
	reg.RawSetString("register", l.NewFunction(m.registryRegister))
	reg.RawSetString("lookup", l.NewFunction(m.registryLookup))
	reg.RawSetString("unregister", l.NewFunction(m.registryUnregister))
	mod.RawSetString("registry", reg)

	RegisterMessageType(l)
	l.Push(mod)
	return 1
}

// getOptions returns an empty table (placeholder for process options)
// Params: none
// Returns: empty table
func (m *Module) getOptions(l *lua.LState) int {
	// Return empty table
	l.Push(l.CreateTable(0, 0))
	return 1
}

// setOptions validates options table but returns unsupported option error
// Params: options_table
// Returns: false, error_message
func (m *Module) setOptions(l *lua.LState) int {
	// Validate that argument is a table
	if l.GetTop() < 1 || l.Get(1).Type() != lua.LTTable {
		l.Push(lua.LBool(false))
		l.Push(lua.LString("options parameter must be a table"))
		return 2
	}

	// Get the first key from the table and report it as unsupported
	options := l.CheckTable(1)
	var firstKey string
	options.ForEach(func(k lua.LValue, v lua.LValue) {
		if firstKey == "" && k.Type() == lua.LTString {
			firstKey = string(k.(lua.LString))
		}
	})

	if firstKey != "" {
		l.Push(lua.LBool(false))
		l.Push(lua.LString(fmt.Sprintf("option %s is not supported", firstKey)))
	} else {
		l.Push(lua.LBool(true))
		l.Push(lua.LNil)
	}

	return 2
}

// checkPID validates context and returns Target if valid
func (m *Module) checkPID(l *lua.LState) (pubsub.PID, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return pubsub.PID{}, false
	}

	// Try to get Target from context
	pid, ok := pubsub.GetPID(ctx)
	return pid, ok
}

// getNode retrieves node from context
func (m *Module) getNode(l *lua.LState) (pubsub.Node, bool) {
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

// getProcessManager retrieves process manager from context using the standard context key
func (m *Module) getProcessManager(l *lua.LState) (process.Manager, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return nil, false
	}

	manager := process.GetProcesses(ctx)
	if manager == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no process manager found"))
		return nil, false
	}

	return manager, true
}

// getRegistry retrieves the Target registry from context
func (m *Module) getRegistry(l *lua.LState) (topology.PIDRegistry, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return nil, false
	}

	// Get reg from context
	reg := topology.GetPIDRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no reg found in context"))
		return nil, false
	}

	return reg, true
}

// getTopology retrieves the topology instance from context
func (m *Module) getTopology(l *lua.LState) (topology.Topology, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return nil, false
	}

	// Get topology from context
	topo := topology.GetTopology(ctx)
	if topo == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no topology found in context"))
		return nil, false
	}

	return topo, true
}

// resolvePID attempts to resolve a string to a Target, either by direct parsing
// or by looking up in the registry if it's not a valid Target format
func (m *Module) resolvePID(l *lua.LState, pidOrName string) (pubsub.PID, error) {
	// Try to parse as Target first
	pid, err := pubsub.ParsePID(pidOrName)
	if err == nil {
		return pid, nil
	}

	// If parsing failed, try to lookup as a name
	reg, ok := m.getRegistry(l)
	if !ok {
		return pubsub.PID{}, fmt.Errorf("could not access registry")
	}

	pid, found := reg.Lookup(pidOrName)
	if !found {
		return pubsub.PID{}, fmt.Errorf("could not resolve '%s' as Target or registered name", pidOrName)
	}

	return pid, nil
}

// pid returns the string representation of the current Target
// Returns: pid_string
func (m *Module) pid(l *lua.LState) int {
	pid, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	l.Push(lua.LString(pid.String()))
	return 1
}

// pid returns the string representation of the current Target
// Returns: pid_string
func (m *Module) id(l *lua.LState) int {
	pid, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	l.Push(lua.LString(pid.ID.String()))
	return 1
}

// send sends a message to another process (accepts Target or registered name)
// Params: destination, topic, [payload1, payload2, ...]
// Returns: success, error
func (m *Module) send(l *lua.LState) int {
	node, ok := m.getNode(l)
	if !ok {
		return 2 // Error values already pushed by getNode
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Parse required arguments
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("send requires at least destination and topic arguments"))
		return 2
	}

	pidOrName := l.CheckString(1)
	topic := l.CheckString(2)

	// Validate topic - prevent @ topics
	if strings.HasPrefix(topic, "@") {
		l.Push(lua.LNil)
		l.Push(lua.LString("cannot send to @ topics"))
		return 2
	}

	// Resolve destination (Target or name)
	pid, err := m.resolvePID(l, pidOrName)
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
	pkg := pubsub.NewMessagePackage(self, pid, messages...)

	// send message using node
	if err := node.Send(pkg); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("messages sent",
		zap.String("from", self.String()),
		zap.String("to", pid.String()),
		zap.String("topic", topic),
	)

	l.Push(lua.LTrue)
	return 1
}

// createPayloadsFromArgs converts Lua arguments to process payloads
func (m *Module) createPayloadsFromArgs(l *lua.LState, startIndex int) payload.Payloads {
	var payloads payload.Payloads

	// Convert each argument to a payload
	for i := startIndex; i <= l.GetTop(); i++ {
		payloads = append(payloads, payload.NewPayload(l.Get(i), payload.Lua))
	}

	return payloads
}

// spawn creates a new process without monitoring or linking
// Params: id, host, [arg1, arg2, ...]
// Returns: pid, error
func (m *Module) spawn(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2 // Error values already pushed by getProcessManager
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get required arguments
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("spawn requires at least id and host arguments"))
		return 2
	}

	id := l.CheckString(1) // This should be in format "namespace:name"
	hostID := l.CheckString(2)

	// Get any optional args (starting from argument 3)
	payloads := m.createPayloadsFromArgs(l, 3)

	// Create start process config
	start := &process.Start{
		HostID: hostID,
		Source: registry.ParseID(id),
		Input:  payloads,
		Lifecycle: process.Lifecycle{
			Parent:  self,
			Monitor: false,
			Link:    false,
		},
	}

	// Serve the process
	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process spawned",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
		zap.Int("num_args", len(payloads)),
	)

	// Return Target string
	l.Push(lua.LString(pid.String()))
	return 1
}

// spawnMonitored creates a new process with monitoring
// Params: id, host, [arg1, arg2, ...]
// Returns: pid, error
func (m *Module) spawnMonitored(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2 // Error values already pushed by getProcessManager
	}

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get required arguments
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("spawn_monitored requires at least id and host arguments"))
		return 2
	}

	id := l.CheckString(1) // This should be in format "namespace:name"
	hostID := l.CheckString(2)

	// Get any optional args (starting from argument 3)
	payloads := m.createPayloadsFromArgs(l, 3)

	// Create start process config
	start := &process.Start{
		HostID: hostID,
		Source: registry.ParseID(id),
		Input:  payloads,
		Lifecycle: process.Lifecycle{
			Parent:  self,
			Monitor: true,
			Link:    false,
		},
	}

	// Serve the process with monitoring
	pid, err := manager.Start(uw.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process spawned with monitoring",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
		zap.Int("num_args", len(payloads)),
	)

	// Return Target string
	l.Push(lua.LString(pid.String()))
	return 1
}

// spawnLinked creates a new process with linking
// Params: id, host, [arg1, arg2, ...]
// Returns: pid, error
func (m *Module) spawnLinked(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2 // Error values already pushed by getProcessManager
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get required arguments
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("spawn_linked requires at least id and host arguments"))
		return 2
	}

	id := l.CheckString(1) // This should be in format "namespace:name"
	hostID := l.CheckString(2)

	// Get any optional args (starting from argument 3)
	payloads := m.createPayloadsFromArgs(l, 3)

	// Create start process config
	start := &process.Start{
		HostID: hostID,
		Source: registry.ParseID(id),
		Input:  payloads,
		Lifecycle: process.Lifecycle{
			Parent:  self,
			Monitor: false,
			Link:    true,
		},
	}

	// Serve the process with linking
	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process spawned with linking",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
		zap.Int("num_args", len(payloads)),
	)

	// Return Target string
	l.Push(lua.LString(pid.String()))
	return 1
}

// spawnLinked creates a new process with linking
// Params: id, host, [arg1, arg2, ...]
// Returns: pid, error
func (m *Module) spawnLinkedMonitored(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2 // Error values already pushed by getProcessManager
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get required arguments
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("spawn_linked requires at least id and host arguments"))
		return 2
	}

	id := l.CheckString(1) // This should be in format "namespace:name"
	hostID := l.CheckString(2)

	// Get any optional args (starting from argument 3)
	payloads := m.createPayloadsFromArgs(l, 3)

	// Create start process config
	start := &process.Start{
		HostID: hostID,
		Source: registry.ParseID(id),
		Input:  payloads,
		Lifecycle: process.Lifecycle{
			Parent:  self,
			Monitor: true,
			Link:    true,
		},
	}

	// Serve the process with linking
	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process spawned with linking",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
		zap.Int("num_args", len(payloads)),
	)

	// Return Target string
	l.Push(lua.LString(pid.String()))
	return 1
}

// terminate terminates a process (accepts Target or registered name)
// Params: destination
// Returns: success, error
func (m *Module) terminate(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2 // Error values already pushed by getProcessManager
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Parse Target or name argument
	pidOrName := l.CheckString(1)

	// Resolve destination
	pid, err := m.resolvePID(l, pidOrName)
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
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
	)

	l.Push(lua.LTrue)
	return 1
}

// cancel sends a cancellation request to a process (accepts Target or registered name)
// Params: destination, deadline
// Where deadline can be a duration string (e.g. "5s") or milliseconds number
// Returns: success, error
func (m *Module) cancel(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2 // Error values already pushed by getProcessManager
	}

	// Require both destination and deadline arguments
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("cancel requires two arguments: destination and deadline"))
		return 2
	}

	// Get current process context
	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Parse Target or name argument
	pidOrName := l.CheckString(1)

	// Resolve destination
	pid, err := m.resolvePID(l, pidOrName)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
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
	if err := manager.Cancel(l.Context(), self, pid, deadline); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process cancel requested",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
		zap.Time("deadline", deadline),
	)

	l.Push(lua.LTrue)
	return 1
}

// monitor establishes monitoring of another process
// Params: destination (Target or registered name)
// Returns: success, error
func (m *Module) monitor(l *lua.LState) int {
	topo, ok := m.getTopology(l)
	if !ok {
		return 2 // Error values already pushed by getTopology
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get destination argument
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("monitor requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	// Resolve destination
	pid, err := m.resolvePID(l, pidOrName)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Set up monitoring
	if err := topo.Wait(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process monitoring established",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
	)

	l.Push(lua.LTrue)
	return 1
}

// unmonitor removes monitoring of another process
// Params: destination (Target or registered name)
// Returns: success, error
func (m *Module) unmonitor(l *lua.LState) int {
	topo, ok := m.getTopology(l)
	if !ok {
		return 2 // Error values already pushed by getTopology
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get destination argument
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("unmonitor requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	// Resolve destination
	pid, err := m.resolvePID(l, pidOrName)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Remove monitoring
	if err := topo.Release(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process monitoring removed",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
	)

	l.Push(lua.LTrue)
	return 1
}

// link establishes a bidirectional link with another process
// Params: destination (Target or registered name)
// Returns: success, error
func (m *Module) link(l *lua.LState) int {
	topo, ok := m.getTopology(l)
	if !ok {
		return 2 // Error values already pushed by getTopology
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get destination argument
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("link requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	// Resolve destination
	pid, err := m.resolvePID(l, pidOrName)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Establish link
	if err := topo.Link(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process link established",
		zap.String("from", self.String()),
		zap.String("to", pid.String()),
	)

	l.Push(lua.LTrue)
	return 1
}

// unlink removes a bidirectional link with another process
// Params: destination (Target or registered name)
// Returns: success, error
func (m *Module) unlink(l *lua.LState) int {
	topo, ok := m.getTopology(l)
	if !ok {
		return 2 // Error values already pushed by getTopology
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get destination argument
	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("unlink requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	// Resolve destination
	pid, err := m.resolvePID(l, pidOrName)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Remove link
	if err := topo.Unlink(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("process link removed",
		zap.String("from", self.String()),
		zap.String("to", pid.String()),
	)

	l.Push(lua.LTrue)
	return 1
}

// registryRegister registers a name for the current process or a specified Target
// Params: name, [pid]
// If pid is not provided, registers the current process
// Returns: success, error
func (m *Module) registryRegister(l *lua.LState) int {
	reg, ok := m.getRegistry(l)
	if !ok {
		return 2 // Error values already pushed by getRegistry
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get arguments
	name := l.CheckString(1)

	// If second argument is provided, use it as the Target to register
	// otherwise use the current process's Target
	var pid pubsub.PID
	if l.GetTop() >= 2 {
		pidStr := l.CheckString(2)
		var err error
		pid, err = pubsub.ParsePID(pidStr)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	} else {
		pid = self // Use current process Target
	}

	if pid.ID.Name == "parent" {
		l.Push(lua.LTrue)
		return 1
	}

	// Register the name
	err := reg.Register(name, pid)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("registered process name",
		zap.String("from", self.String()),
		zap.String("name", name),
		zap.String("pid", pid.String()),
	)

	l.Push(lua.LTrue)
	return 1
}

// registryLookup finds the Target registered with a given name
// Params: name
// Returns: pid, error
func (m *Module) registryLookup(l *lua.LState) int {
	reg, ok := m.getRegistry(l)
	if !ok {
		return 2 // Error values already pushed by getRegistry
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get name argument
	name := l.CheckString(1)

	// Lookup name to Target
	pid, found := reg.Lookup(name)
	if !found {
		l.Push(lua.LNil)
		l.Push(lua.LString("name not registered"))
		return 2
	}

	m.log.Debug("looked up process name",
		zap.String("from", self.String()),
		zap.String("name", name),
		zap.String("pid", pid.String()),
	)

	l.Push(lua.LString(pid.String()))
	return 1
}

// registryUnregister removes a name registration
// Params: name
// Returns: success
func (m *Module) registryUnregister(l *lua.LState) int {
	reg, ok := m.getRegistry(l)
	if !ok {
		return 2 // Error values already pushed by getRegistry
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2 // Error values already pushed by checkPID
	}

	// Get name argument
	name := l.CheckString(1)

	// Unregister name
	unregistered := reg.Unregister(name)

	m.log.Debug("unregistered process name",
		zap.String("from", self.String()),
		zap.String("name", name),
		zap.Bool("success", unregistered),
	)

	l.Push(lua.LBool(unregistered))
	return 1
}
