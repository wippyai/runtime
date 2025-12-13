package process

import (
	"fmt"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

func init() {
	value.RegisterTypeMethods(nil, messageTypeName,
		map[string]lua.LGoFunc{"__tostring": messageToString},
		messageMethods)
}

// Module is the process module definition.
var Module = &luaapi.ModuleDef{
	Name:        "process",
	Description: "Process management and messaging",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 21)

	mod.RawSetString("id", lua.LGoFunc(processID))
	mod.RawSetString("pid", lua.LGoFunc(processPID))
	mod.RawSetString("send", lua.LGoFunc(send))
	mod.RawSetString("spawn", lua.LGoFunc(spawn))
	mod.RawSetString("spawn_monitored", lua.LGoFunc(spawnMonitored))
	mod.RawSetString("spawn_linked", lua.LGoFunc(spawnLinked))
	mod.RawSetString("spawn_linked_monitored", lua.LGoFunc(spawnLinkedMonitored))
	mod.RawSetString("terminate", lua.LGoFunc(terminate))
	mod.RawSetString("cancel", lua.LGoFunc(cancel))
	mod.RawSetString("get_options", lua.LGoFunc(getOptions))
	mod.RawSetString("set_options", lua.LGoFunc(setOptions))
	mod.RawSetString("monitor", lua.LGoFunc(monitor))
	mod.RawSetString("unmonitor", lua.LGoFunc(unmonitor))
	mod.RawSetString("link", lua.LGoFunc(link))
	mod.RawSetString("unlink", lua.LGoFunc(unlink))
	mod.RawSetString("with_context", lua.LGoFunc(spawnerNew))

	mod.RawSetString("inbox", lua.LGoFunc(inbox))
	mod.RawSetString("events", lua.LGoFunc(events))
	mod.RawSetString("listen", lua.LGoFunc(listen))
	mod.RawSetString("unlisten", lua.LGoFunc(unlisten))

	eventsTbl := lua.CreateTable(0, 3)
	eventsTbl.RawSetString("CANCEL", lua.LString(topology.KindCancel))
	eventsTbl.RawSetString("EXIT", lua.LString(topology.KindExit))
	eventsTbl.RawSetString("LINK_DOWN", lua.LString(topology.KindLinkDown))
	eventsTbl.Immutable = true
	mod.RawSetString("event", eventsTbl)

	reg := lua.CreateTable(0, 3)
	reg.RawSetString("register", lua.LGoFunc(registryRegister))
	reg.RawSetString("lookup", lua.LGoFunc(registryLookup))
	reg.RawSetString("unregister", lua.LGoFunc(registryUnregister))
	reg.Immutable = true
	mod.RawSetString("registry", reg)

	mod.Immutable = true
	return mod, nil
}

// BindGlobal loads the process module and sets it as a global variable.
// TODO: refactor callers to use Module.Load directly - this is redundant
func BindGlobal(l *lua.LState) {
	Module.Load(l)
}

func checkPID(l *lua.LState) (pid.PID, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return pid.PID{}, false
	}

	p, ok := runtime.GetFramePID(ctx)
	return p, ok
}

func getProcessManager(l *lua.LState) (process.Manager, bool) {
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

func getRegistry(l *lua.LState) (topology.PIDRegistry, bool) {
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

func getTopology(l *lua.LState) (topology.Topology, bool) {
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

func resolvePID(l *lua.LState, pidOrName string, permission string) (pid.PID, error) {
	p, err := pid.ParsePID(pidOrName)
	if err == nil {
		if !security.IsAllowed(l.Context(), permission, p.String(), nil) {
			return pid.PID{}, luaapi.NewNotAllowedError(
				strings.TrimPrefix(permission, "process."), pidOrName)
		}
		return p, nil
	}

	reg, ok := getRegistry(l)
	if !ok {
		return pid.PID{}, luaapi.ErrCouldNotAccessRegistry
	}

	p, found := reg.Lookup(pidOrName)
	if !found {
		return pid.PID{}, luaapi.NewCouldNotResolveError(pidOrName)
	}

	if !security.IsAllowed(l.Context(), permission, p.String(), nil) {
		return pid.PID{}, luaapi.NewNotAllowedError(
			strings.TrimPrefix(permission, "process."), pidOrName)
	}

	return p, nil
}

func createPayloadsFromArgs(l *lua.LState) payload.Payloads {
	var payloads payload.Payloads // todo: properly presize!
	for i := 3; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}
	return payloads
}

func buildSecurityContext(l *lua.LState) []ctxapi.Pair {
	ctx := l.Context()
	if ctx == nil {
		return nil
	}

	var pairs []ctxapi.Pair
	if actor, ok := secapi.GetActor(ctx); ok {
		pairs = append(pairs, secapi.ActorPair(actor))
	}
	if scope, ok := secapi.GetScope(ctx); ok {
		pairs = append(pairs, secapi.ScopePair(scope))
	}
	if values := ctxapi.GetValues(ctx); values != nil && values.Len() > 0 {
		pairs = append(pairs, ctxapi.ValuesPair(values))
	}
	return pairs
}

func processPID(l *lua.LState) int {
	pid, ok := checkPID(l)
	if !ok {
		return 2
	}
	l.Push(lua.LString(pid.String()))
	return 1
}

func processID(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return 2
	}

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

func send(l *lua.LState) int {
	router := relay.GetRouter(l.Context())
	if router == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no router found"))
		return 2
	}

	self, ok := checkPID(l)
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

	pid, err := resolvePID(l, pidOrName, "process.send")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
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
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func spawn(l *lua.LState) int {
	manager, ok := getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := checkPID(l)
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

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)

	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: buildSecurityContext(l),
		Options: options,
	}

	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(pid.String()))
	return 1
}

func spawnMonitored(l *lua.LState) int {
	manager, ok := getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := checkPID(l)
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

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)
	options.Set(process.LifecycleMonitorKey, true)

	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: buildSecurityContext(l),
		Options: options,
	}

	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(pid.String()))
	return 1
}

func spawnLinked(l *lua.LState) int {
	manager, ok := getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := checkPID(l)
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

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)
	options.Set(process.LifecycleLinkKey, true)

	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: buildSecurityContext(l),
		Options: options,
	}

	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(pid.String()))
	return 1
}

func spawnLinkedMonitored(l *lua.LState) int {
	manager, ok := getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := checkPID(l)
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

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)
	options.Set(process.LifecycleMonitorKey, true)
	options.Set(process.LifecycleLinkKey, true)

	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: buildSecurityContext(l),
		Options: options,
	}

	pid, err := manager.Start(l.Context(), start)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(pid.String()))
	return 1
}

func terminate(l *lua.LState) int {
	manager, ok := getProcessManager(l)
	if !ok {
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := resolvePID(l, pidOrName, "process.terminate")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := manager.Terminate(l.Context(), pid); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func cancel(l *lua.LState) int {
	manager, ok := getProcessManager(l)
	if !ok {
		return 2
	}

	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("cancel requires at least destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := resolvePID(l, pidOrName, "process.cancel")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	var deadline time.Time
	if l.GetTop() >= 2 {
		switch l.Get(2).Type() { //nolint:exhaustive // only string/number types valid for duration
		case lua.LTString:
			durationStr := l.CheckString(2)
			duration, err := time.ParseDuration(durationStr)
			if err != nil {
				l.Push(lua.LNil)
				l.Push(lua.LString(fmt.Sprintf("invalid duration format: %v", err)))
				return 2
			}
			deadline = time.Now().Add(duration)
		case lua.LTNumber, lua.LTInteger:
			ms := l.CheckNumber(2)
			deadline = time.Now().Add(time.Duration(ms) * time.Millisecond)
		default:
			l.Push(lua.LNil)
			l.Push(lua.LString("deadline must be either a duration string or milliseconds number"))
			return 2
		}
	}

	if err := manager.Cancel(l.Context(), self, pid, deadline); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func getOptions(l *lua.LState) int {
	proc := engine.GetProcess(l)

	options := l.CreateTable(0, 1)
	if proc != nil {
		options.RawSetString("trap_links", lua.LBool(proc.IsTrapLinks()))
	} else {
		options.RawSetString("trap_links", lua.LBool(false))
	}

	l.Push(options)
	return 1
}

func setOptions(l *lua.LState) int {
	if l.GetTop() < 1 || l.Get(1).Type() != lua.LTTable {
		l.Push(lua.LBool(false))
		l.Push(lua.LString("options parameter must be a table"))
		return 2
	}

	proc := engine.GetProcess(l)
	if proc == nil {
		l.Push(lua.LBool(false))
		l.Push(lua.LString("no process context"))
		return 2
	}

	options := l.CheckTable(1)
	var unsupportedOption string

	options.ForEach(func(k lua.LValue, v lua.LValue) {
		if k.Type() != lua.LTString {
			return
		}
		key := string(k.(lua.LString))

		switch key {
		case "trap_links":
			if v.Type() != lua.LTBool {
				unsupportedOption = "trap_links must be a boolean"
				return
			}
			proc.SetTrapLinks(bool(v.(lua.LBool)))
		default:
			if unsupportedOption == "" {
				unsupportedOption = fmt.Sprintf("option %s is not supported", key)
			}
		}
	})

	if unsupportedOption != "" {
		l.Push(lua.LBool(false))
		l.Push(lua.LString(unsupportedOption))
	} else {
		l.Push(lua.LBool(true))
		l.Push(lua.LNil)
	}

	return 2
}

func monitor(l *lua.LState) int {
	topo, ok := getTopology(l)
	if !ok {
		return 2
	}

	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("monitor requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := resolvePID(l, pidOrName, "process.monitor")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := topo.Monitor(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func unmonitor(l *lua.LState) int {
	topo, ok := getTopology(l)
	if !ok {
		return 2
	}

	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("unmonitor requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := resolvePID(l, pidOrName, "process.unmonitor")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := topo.Demonitor(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func link(l *lua.LState) int {
	topo, ok := getTopology(l)
	if !ok {
		return 2
	}

	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("link requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := resolvePID(l, pidOrName, "process.link")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := topo.Link(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func unlink(l *lua.LState) int {
	topo, ok := getTopology(l)
	if !ok {
		return 2
	}

	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("unlink requires a destination argument"))
		return 2
	}

	pidOrName := l.CheckString(1)

	pid, err := resolvePID(l, pidOrName, "process.unlink")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := topo.Unlink(self, pid); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func registryRegister(l *lua.LState) int {
	reg, ok := getRegistry(l)
	if !ok {
		return 2
	}

	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	name := l.CheckString(1)

	if !security.IsAllowed(l.Context(), "process.registry.register", name, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to register name: %s", name)))
		return 2
	}

	var p pid.PID
	if l.GetTop() >= 2 {
		pidStr := l.CheckString(2)
		var err error
		p, err = pid.ParsePID(pidStr)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	} else {
		p = self
	}

	err := reg.Register(name, p)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func registryLookup(l *lua.LState) int {
	reg, ok := getRegistry(l)
	if !ok {
		return 2
	}

	name := l.CheckString(1)

	p, found := reg.Lookup(name)
	if !found {
		l.Push(lua.LNil)
		l.Push(lua.LString("name not registered"))
		return 2
	}

	l.Push(lua.LString(p.String()))
	return 1
}

func registryUnregister(l *lua.LState) int {
	reg, ok := getRegistry(l)
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
	l.Push(lua.LBool(unregistered))
	return 1
}

func inbox(l *lua.LState) int {
	req := &engine.SubscribeRequest{
		Topic:   topology.TopicInbox,
		BufSize: 0,
		Handler: MessageHandler,
	}
	l.Push(req)
	return -1 // yield
}

func events(l *lua.LState) int {
	req := &engine.SubscribeRequest{
		Topic:   topology.TopicEvents,
		BufSize: 0,
		Handler: nil, // events use default PayloadsToLua conversion
	}
	l.Push(req)
	return -1 // yield
}

func listen(l *lua.LState) int {
	topic := l.CheckString(1)
	if topic == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("topic cannot be empty"))
		return 2
	}

	if strings.HasPrefix(topic, "@") {
		l.Push(lua.LNil)
		l.Push(lua.LString("cannot listen to @ topics"))
		return 2
	}

	// Check for options table (second argument)
	// Options: { message = true } to receive Message objects instead of raw payloads
	var handler engine.TopicHandler // default: nil = raw payloads (Lua tables/strings)
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		options := l.CheckTable(2)
		if msgMode := options.RawGetString("message"); msgMode == lua.LTrue {
			handler = MessageHandler // Message objects with :from(), :payload(), :topic()
		}
	}

	req := &engine.SubscribeRequest{
		Topic:   topic,
		BufSize: 1,
		Handler: handler,
	}
	l.Push(req)
	return -1
}

func unlisten(l *lua.LState) int {
	ud := l.CheckUserData(1)
	ch, ok := ud.Value.(*engine.Channel)
	if !ok {
		l.ArgError(1, "channel expected")
		return 0
	}

	req := &engine.UnsubscribeRequest{
		Channel: ch,
	}
	l.Push(req)
	return -1
}
