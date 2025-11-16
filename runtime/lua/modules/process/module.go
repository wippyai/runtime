package process

import (
	"fmt"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/security"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const defaultCancelTimeout = "30s"

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
	m.registerContextType(l)

	mod := l.CreateTable(0, 16)

	// Register process functions
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

	// Create event constants table
	events := l.CreateTable(0, 3)
	events.RawSetString("CANCEL", lua.LString(topology.KindCancel))
	events.RawSetString("EXIT", lua.LString(topology.KindExit))
	events.RawSetString("LINK_DOWN", lua.LString(topology.KindLinkDown))
	mod.RawSetString("event", events)

	// Registry table
	reg := l.CreateTable(0, 3)
	reg.RawSetString("register", l.NewFunction(m.registryRegister))
	reg.RawSetString("lookup", l.NewFunction(m.registryLookup))
	reg.RawSetString("unregister", l.NewFunction(m.registryUnregister))
	mod.RawSetString("registry", reg)

	RegisterMessageType(l)
	l.Push(mod)
	return 1
}

// NewProcessTable creates a new mutable process table for extension by other modules
func (m *Module) NewProcessTable(l *lua.LState) *lua.LTable {
	m.registerContextType(l)

	mod := l.CreateTable(0, 16)

	// Register process functions
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

	// Create event constants table (immutable)
	events := l.CreateTable(0, 3)
	events.RawSetString("CANCEL", lua.LString(topology.KindCancel))
	events.RawSetString("EXIT", lua.LString(topology.KindExit))
	events.RawSetString("LINK_DOWN", lua.LString(topology.KindLinkDown))
	events.Immutable = true
	mod.RawSetString("event", events)

	// Registry table (immutable)
	reg := l.CreateTable(0, 3)
	reg.RawSetString("register", l.NewFunction(m.registryRegister))
	reg.RawSetString("lookup", l.NewFunction(m.registryLookup))
	reg.RawSetString("unregister", l.NewFunction(m.registryUnregister))
	reg.Immutable = true
	mod.RawSetString("registry", reg)

	RegisterMessageType(l)

	// Return mutable table (no Immutable = true)
	return mod
}

// getOptions returns an empty table (placeholder for process options)
func (m *Module) getOptions(l *lua.LState) int {
	l.Push(l.CreateTable(0, 0))
	return 1
}

// setOptions validates options table but returns unsupported option error
func (m *Module) setOptions(l *lua.LState) int {
	if l.GetTop() < 1 || l.Get(1).Type() != lua.LTTable {
		l.Push(lua.LBool(false))
		l.Push(lua.LString("options parameter must be a table"))
		return 2
	}

	options := l.CheckTable(1)
	var firstKey string
	options.ForEach(func(k lua.LValue, _ lua.LValue) {
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

// checkPID validates context and returns PID from frame context if valid
func (m *Module) checkPID(l *lua.LState) (relay.PID, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return relay.PID{}, false
	}

	pid, ok := runtime.GetFramePID(ctx)
	return pid, ok
}

// getProcessManager retrieves process manager from context
func (m *Module) getProcessManager(l *lua.LState) (process.Manager, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return nil, false
	}

	manager := process.GetManager(ctx)
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

	reg := topology.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no registry found in context"))
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
func (m *Module) resolvePID(l *lua.LState, pidOrName string, permission string) (relay.PID, error) {
	// Try to parse as Target first
	pid, err := relay.ParsePID(pidOrName)
	if err == nil {
		// Check security for resolved PID
		if !security.IsAllowed(l.Context(), permission, pid.String(), nil) {
			return relay.PID{}, fmt.Errorf("not allowed to %s: %s",
				strings.TrimPrefix(permission, "process."), pidOrName)
		}
		return pid, nil
	}

	// If parsing failed, try to lookup as a name
	reg, ok := m.getRegistry(l)
	if !ok {
		return relay.PID{}, fmt.Errorf("could not access registry")
	}

	pid, found := reg.Lookup(pidOrName)
	if !found {
		return relay.PID{}, fmt.Errorf("could not resolve '%s' as PID or registered name", pidOrName)
	}

	// Check security for resolved PID from registry
	if !security.IsAllowed(l.Context(), permission, pid.String(), nil) {
		return relay.PID{}, fmt.Errorf("not allowed to %s: %s",
			strings.TrimPrefix(permission, "process."), pidOrName)
	}

	return pid, nil
}

// pid returns the string representation of the current Target
func (m *Module) pid(l *lua.LState) int {
	pid, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	l.Push(lua.LString(pid.String()))
	return 1
}

// id returns the string representation of the current call ID
func (m *Module) id(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return 2
	}

	// Get call ID from FrameContext
	cc := ctxapi.FrameFromContext(ctx)
	if cc == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("FrameContext not found"))
		return 2
	}

	idValue, ok := cc.Get(runtime.FrameIDKey)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("call ID not found in context"))
		return 2
	}

	callID, ok := idValue.(registry.ID)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid call ID type"))
		return 2
	}

	l.Push(lua.LString(callID.String()))
	return 1
}

// send sends a message to another process (accepts pid or registered name)
func (m *Module) send(l *lua.LState) int {
	router := relay.GetRouter(l.Context())
	if router == nil {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("send requires at least destination and topic arguments"))
		return 2
	}

	pidOrName := l.CheckString(1)
	topic := l.CheckString(2)

	if strings.HasPrefix(topic, "@") {
		l.Push(lua.LNil)
		l.Push(lua.LString("cannot send to @ topics"))
		return 2
	}

	pid, err := m.resolvePID(l, pidOrName, "process.send")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "send"))
		return 2
	}

	messages := make([]*relay.Message, 0, l.GetTop()-2)
	for i := 3; i <= l.GetTop(); i++ {
		messages = append(messages, &relay.Message{
			Topic:    topic,
			Payloads: []payload.Payload{luaconv.ExportPayload(l.Get(i))},
		})
	}

	pkg := relay.NewMessagePackage(self, pid, messages...)

	if err := router.Send(pkg); err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "send"))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

// createPayloadsFromArgs converts Lua arguments to process payloads
//
//nolint:unparam // ok for now
func (m *Module) createPayloadsFromArgs(l *lua.LState, startIndex int) payload.Payloads {
	var payloads payload.Payloads

	for i := startIndex; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	return payloads
}

// spawn creates a new process without monitoring or linking
func (m *Module) spawn(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("spawn requires at least id and host arguments"))
		return 2
	}

	id := l.CheckString(1)
	hostID := l.CheckString(2)

	if !security.IsAllowed(l.Context(), "process.spawn", id, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.host", hostID, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn on host: %s", hostID)))
		return 2
	}

	payloads := m.createPayloadsFromArgs(l, 3)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)

	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Options: options,
	}

	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "spawn"))
		return 2
	}

	m.log.Debug("process spawned",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
		zap.Int("num_args", len(payloads)),
	)

	l.Push(lua.LString(pid.String()))
	return 1
}

// spawnMonitored creates a new process with monitoring
func (m *Module) spawnMonitored(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2
	}

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(newProcessInvalidError(l, "no unit of work found"))
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("spawn_monitored requires at least id and host arguments"))
		return 2
	}

	id := l.CheckString(1)
	hostID := l.CheckString(2)

	if !security.IsAllowed(l.Context(), "process.spawn", id, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.spawn.monitored", id, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn monitored process: %s", id)))
		return 2
	}

	payloads := m.createPayloadsFromArgs(l, 3)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)
	options.Set(process.LifecycleMonitorKey, true)

	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Options: options,
	}

	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "spawn_monitored"))
		return 2
	}

	m.log.Debug("process spawned with monitoring",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
		zap.Int("num_args", len(payloads)),
	)

	l.Push(lua.LString(pid.String()))
	return 1
}

// spawnLinked creates a new process with linking
func (m *Module) spawnLinked(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("spawn_linked requires at least id and host arguments"))
		return 2
	}

	id := l.CheckString(1)
	hostID := l.CheckString(2)

	if !security.IsAllowed(l.Context(), "process.spawn", id, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.spawn.linked", id, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn linked process: %s", id)))
		return 2
	}

	payloads := m.createPayloadsFromArgs(l, 3)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)
	options.Set(process.LifecycleLinkKey, true)

	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Options: options,
	}

	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "spawn_linked"))
		return 2
	}

	m.log.Debug("process spawned with linking",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
		zap.Int("num_args", len(payloads)),
	)

	l.Push(lua.LString(pid.String()))
	return 1
}

// spawnLinkedMonitored creates a new process with both linking and monitoring
func (m *Module) spawnLinkedMonitored(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("spawn_linked_monitored requires at least id and host arguments"))
		return 2
	}

	id := l.CheckString(1)
	hostID := l.CheckString(2)

	if !security.IsAllowed(l.Context(), "process.spawn", id, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.spawn.monitored", id, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn monitored process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.spawn.linked", id, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn linked process: %s", id)))
		return 2
	}

	payloads := m.createPayloadsFromArgs(l, 3)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)
	options.Set(process.LifecycleMonitorKey, true)
	options.Set(process.LifecycleLinkKey, true)

	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Options: options,
	}

	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "spawn_linked_monitored"))
		return 2
	}

	m.log.Debug("process spawned with linking and monitoring",
		zap.String("from", self.String()),
		zap.String("pid", pid.String()),
		zap.String("host", hostID),
		zap.String("process", id),
		zap.Int("num_args", len(payloads)),
	)

	l.Push(lua.LString(pid.String()))
	return 1
}

// terminate terminates a process (accepts Target or registered name)
func (m *Module) terminate(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := m.resolvePID(l, pidOrName, "process.terminate")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "terminate"))
		return 2
	}

	if err := manager.Terminate(l.Context(), pid); err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "terminate"))
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
func (m *Module) cancel(l *lua.LState) int {
	manager, ok := m.getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("cancel requires at least destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := m.resolvePID(l, pidOrName, "process.cancel")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "cancel"))
		return 2
	}

	var deadline time.Time
	if l.GetTop() >= 2 {
		switch l.Get(2).Type() {
		case lua.LTString:
			durationStr := l.CheckString(2)
			duration, err := time.ParseDuration(durationStr)
			if err != nil {
				l.Push(lua.LNil)
				l.Push(lua.LString(fmt.Sprintf("invalid duration format: %v", err)))
				return 2
			}
			deadline = time.Now().Add(duration)
		case lua.LTNumber:
			ms := l.CheckNumber(2)
			deadline = time.Now().Add(time.Duration(ms) * time.Millisecond)
		case lua.LTNil, lua.LTBool, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTTable, lua.LTChannel:
			// FIXME rework on demand
			fallthrough
		default:
			l.Push(lua.LNil)
			l.Push(lua.LString("deadline must be either a duration string or milliseconds number"))
			return 2
		}
	} else {
		duration, err := time.ParseDuration(defaultCancelTimeout)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("invalid default duration format: %v", err)))
			return 2
		}
		deadline = time.Now().Add(duration)
	}

	if err := manager.Cancel(l.Context(), self, pid, deadline); err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "cancel"))
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
func (m *Module) monitor(l *lua.LState) int {
	topo, ok := m.getTopology(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("monitor requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := m.resolvePID(l, pidOrName, "process.monitor")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "monitor"))
		return 2
	}

	if err := topo.Wait(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "monitor"))
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
func (m *Module) unmonitor(l *lua.LState) int {
	topo, ok := m.getTopology(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("unmonitor requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := m.resolvePID(l, pidOrName, "process.unmonitor")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "unmonitor"))
		return 2
	}

	if err := topo.Release(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "unmonitor"))
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
func (m *Module) link(l *lua.LState) int {
	topo, ok := m.getTopology(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("link requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := m.resolvePID(l, pidOrName, "process.link")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "link"))
		return 2
	}

	if err := topo.Link(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "link"))
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
func (m *Module) unlink(l *lua.LState) int {
	topo, ok := m.getTopology(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("unlink requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := m.resolvePID(l, pidOrName, "process.unlink")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "unlink"))
		return 2
	}

	if err := topo.Unlink(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "unlink"))
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
func (m *Module) registryRegister(l *lua.LState) int {
	reg, ok := m.getRegistry(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	name := l.CheckString(1)

	if !security.IsAllowed(l.Context(), "process.registry.register", name, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to register name: %s", name)))
		return 2
	}

	var pid relay.PID
	if l.GetTop() >= 2 {
		pidStr := l.CheckString(2)
		var err error
		pid, err = relay.ParsePID(pidStr)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(newProcessOperationError(l, err, "register"))
			return 2
		}
	} else {
		pid = self
	}

	err := reg.Register(name, pid)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newProcessOperationError(l, err, "register"))
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
func (m *Module) registryLookup(l *lua.LState) int {
	reg, ok := m.getRegistry(l)
	if !ok {
		return 2
	}

	name := l.CheckString(1)

	pid, found := reg.Lookup(name)
	if !found {
		l.Push(lua.LNil)
		l.Push(lua.LString("name not registered"))
		return 2
	}

	l.Push(lua.LString(pid.String()))
	return 1
}

// registryUnregister removes a name registration
func (m *Module) registryUnregister(l *lua.LState) int {
	reg, ok := m.getRegistry(l)
	if !ok {
		return 2
	}

	self, ok := m.checkPID(l)
	if !ok {
		return 2
	}

	name := l.CheckString(1)

	if !security.IsAllowed(l.Context(), "process.registry.unregister", name, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to unregister name: %s", name)))
		return 2
	}

	unregistered := reg.Unregister(name)

	m.log.Debug("unregistered process name",
		zap.String("from", self.String()),
		zap.String("name", name),
		zap.Bool("success", unregistered),
	)

	l.Push(lua.LBool(unregistered))
	return 1
}
