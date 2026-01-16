package process

import (
	"fmt"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/topology"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable *lua.LTable
	yieldTypes  []luaapi.YieldType
)

func init() {
	value.RegisterTypeMethods(nil, messageTypeName,
		map[string]lua.LGoFunc{"__tostring": messageToString},
		messageMethods)

	moduleTable = lua.CreateTable(0, 21)

	moduleTable.RawSetString("id", lua.LGoFunc(processID))
	moduleTable.RawSetString("pid", lua.LGoFunc(processPID))
	moduleTable.RawSetString("send", lua.LGoFunc(send))
	moduleTable.RawSetString("spawn", lua.LGoFunc(spawn))
	moduleTable.RawSetString("spawn_monitored", lua.LGoFunc(spawnMonitored))
	moduleTable.RawSetString("spawn_linked", lua.LGoFunc(spawnLinked))
	moduleTable.RawSetString("spawn_linked_monitored", lua.LGoFunc(spawnLinkedMonitored))
	moduleTable.RawSetString("terminate", lua.LGoFunc(terminate))
	moduleTable.RawSetString("cancel", lua.LGoFunc(cancel))
	moduleTable.RawSetString("get_options", lua.LGoFunc(getOptions))
	moduleTable.RawSetString("set_options", lua.LGoFunc(setOptions))
	moduleTable.RawSetString("monitor", lua.LGoFunc(monitor))
	moduleTable.RawSetString("unmonitor", lua.LGoFunc(unmonitor))
	moduleTable.RawSetString("link", lua.LGoFunc(link))
	moduleTable.RawSetString("unlink", lua.LGoFunc(unlink))
	moduleTable.RawSetString("with_context", lua.LGoFunc(spawnerNew))

	moduleTable.RawSetString("inbox", lua.LGoFunc(inbox))
	moduleTable.RawSetString("events", lua.LGoFunc(events))
	moduleTable.RawSetString("listen", lua.LGoFunc(listen))
	moduleTable.RawSetString("unlisten", lua.LGoFunc(unlisten))
	moduleTable.RawSetString("upgrade", lua.LGoFunc(upgrade))
	moduleTable.RawSetString("run", lua.LGoFunc(run))

	eventsTbl := lua.CreateTable(0, 3)
	eventsTbl.RawSetString("CANCEL", lua.LString(topology.Cancel))
	eventsTbl.RawSetString("EXIT", lua.LString(topology.Exit))
	eventsTbl.RawSetString("LINK_DOWN", lua.LString(topology.LinkDown))
	eventsTbl.Immutable = true
	moduleTable.RawSetString("event", eventsTbl)

	reg := lua.CreateTable(0, 3)
	reg.RawSetString("register", lua.LGoFunc(registryRegister))
	reg.RawSetString("lookup", lua.LGoFunc(registryLookup))
	reg.RawSetString("unregister", lua.LGoFunc(registryUnregister))
	reg.Immutable = true
	moduleTable.RawSetString("registry", reg)

	moduleTable.Immutable = true

	yieldTypes = []luaapi.YieldType{
		{Sample: &SendYield{}, CmdID: process.Send},
		{Sample: &SpawnYield{}, CmdID: process.Spawn},
		{Sample: &TerminateYield{}, CmdID: process.Terminate},
		{Sample: &CancelYield{}, CmdID: process.Cancel},
		{Sample: &MonitorYield{}, CmdID: process.Monitor},
		{Sample: &UnmonitorYield{}, CmdID: process.Unmonitor},
		{Sample: &LinkYield{}, CmdID: process.Link},
		{Sample: &UnlinkYield{}, CmdID: process.Unlink},
		{Sample: &RunYield{}, CmdID: process.Run},
	}
}

// Module is the process module definition.
var Module = &luaapi.ModuleDef{
	Name:        "process",
	Description: "Process management and messaging",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic, luaapi.ClassWorkflow},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		return moduleTable, yieldTypes
	},
	Types: ModuleTypes,
}

func checkPID(l *lua.LState) (pidapi.PID, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return pidapi.PID{}, false
	}

	p, ok := runtime.GetFramePID(ctx)
	return p, ok
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

func resolvePID(l *lua.LState, pidOrName string, permission string, senderPID pidapi.PID) (pidapi.PID, error) {
	secAttrs := map[string]any{"pid": senderPID.String()}

	p, err := pidapi.ParsePID(pidOrName)
	if err == nil {
		if !security.IsAllowed(l.Context(), permission, p.String(), secAttrs) {
			return pidapi.PID{}, runtimelua.NewNotAllowedError(
				strings.TrimPrefix(permission, "process."), pidOrName)
		}
		return p, nil
	}

	reg, ok := getRegistry(l)
	if !ok {
		return pidapi.PID{}, runtimelua.ErrCouldNotAccessRegistry
	}

	p, found := reg.Lookup(pidOrName)
	if !found {
		return pidapi.PID{}, runtimelua.NewCouldNotResolveError(pidOrName)
	}

	if !security.IsAllowed(l.Context(), permission, p.String(), secAttrs) {
		return pidapi.PID{}, runtimelua.NewNotAllowedError(
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

	target, err := resolvePID(l, pidOrName, "process.send", self)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	yield := AcquireSendYield()
	yield.From = self
	yield.To = target
	yield.Topic = topic
	yield.Payloads = createPayloadsFromArgs(l)

	l.Push(yield)
	return -1
}

func spawn(l *lua.LState) int {
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
	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.spawn", id, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.host", hostID, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn on host: %s", hostID)))
		return 2
	}

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)

	yield := AcquireSpawnYield()
	yield.Start = &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: ctxapi.PropagatedPairs(l.Context()),
		Options: options,
	}

	l.Push(yield)
	return -1
}

func spawnMonitored(l *lua.LState) int {
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
	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.spawn", id, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.spawn.monitored", id, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn monitored process: %s", id)))
		return 2
	}

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)

	yield := AcquireSpawnYield()
	yield.Start = &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: ctxapi.PropagatedPairs(l.Context()),
		Options: options,
	}
	yield.Monitor = true

	l.Push(yield)
	return -1
}

func spawnLinked(l *lua.LState) int {
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
	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.spawn", id, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.spawn.linked", id, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn linked process: %s", id)))
		return 2
	}

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)

	yield := AcquireSpawnYield()
	yield.Start = &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: ctxapi.PropagatedPairs(l.Context()),
		Options: options,
	}
	yield.Link = true

	l.Push(yield)
	return -1
}

func spawnLinkedMonitored(l *lua.LState) int {
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
	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.spawn", id, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.spawn.monitored", id, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn monitored process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.spawn.linked", id, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to spawn linked process: %s", id)))
		return 2
	}

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.LifecycleParentKey, self)

	yield := AcquireSpawnYield()
	yield.Start = &process.Start{
		HostID:  hostID,
		Source:  registry.ParseID(id),
		Input:   payloads,
		Context: ctxapi.PropagatedPairs(l.Context()),
		Options: options,
	}
	yield.Monitor = true
	yield.Link = true

	l.Push(yield)
	return -1
}

func terminate(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	pidOrName := l.CheckString(1)

	target, err := resolvePID(l, pidOrName, "process.terminate", self)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	yield := AcquireTerminateYield()
	yield.Target = target

	l.Push(yield)
	return -1
}

func cancel(l *lua.LState) int {
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

	target, err := resolvePID(l, pidOrName, "process.cancel", self)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
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
		case lua.LTNumber, lua.LTInteger:
			ms := l.CheckNumber(2)
			deadline = time.Now().Add(time.Duration(ms) * time.Millisecond)
		case lua.LTNil, lua.LTBool, lua.LTTable, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTChannel:
			l.Push(lua.LNil)
			l.Push(lua.LString("deadline must be either a duration string or milliseconds number"))
			return 2
		}
	}

	yield := AcquireCancelYield()
	yield.From = self
	yield.Target = target
	yield.Deadline = deadline

	l.Push(yield)
	return -1
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

	target, err := resolvePID(l, pidOrName, "process.monitor", self)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	yield := AcquireMonitorYield()
	yield.Watcher = self
	yield.Target = target

	l.Push(yield)
	return -1
}

func unmonitor(l *lua.LState) int {
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

	target, err := resolvePID(l, pidOrName, "process.unmonitor", self)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	yield := AcquireUnmonitorYield()
	yield.Watcher = self
	yield.Target = target

	l.Push(yield)
	return -1
}

func link(l *lua.LState) int {
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

	target, err := resolvePID(l, pidOrName, "process.link", self)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	yield := AcquireLinkYield()
	yield.From = self
	yield.To = target

	l.Push(yield)
	return -1
}

func unlink(l *lua.LState) int {
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

	target, err := resolvePID(l, pidOrName, "process.unlink", self)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	yield := AcquireUnlinkYield()
	yield.From = self
	yield.To = target

	l.Push(yield)
	return -1
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
	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.registry.register", name, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to register name: %s", name)))
		return 2
	}

	var p pidapi.PID
	if l.GetTop() >= 2 {
		pidStr := l.CheckString(2)
		var err error
		p, err = pidapi.ParsePID(pidStr)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	} else {
		p = self
	}

	_, err := reg.Register(name, p)
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

	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	name := l.CheckString(1)
	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.registry.unregister", name, secAttrs) {
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

func upgrade(l *lua.LState) int {
	req := &engine.UpgradeRequest{}

	// arg 1: optional registry ID string (nil or empty = same definition)
	if l.GetTop() >= 1 && l.Get(1).Type() == lua.LTString {
		req.Source = registry.ParseID(l.CheckString(1))
	}

	// args 2+: payloads for new process
	if l.GetTop() >= 2 {
		for i := 2; i <= l.GetTop(); i++ {
			req.Input = append(req.Input, luaconv.ExportPayload(l.Get(i)))
		}
	}

	l.Push(req)
	return -1 // yield
}

// run spawns a process and waits for its result.
// Usage: process.run(id, host, arg1, arg2, ...)
// Returns: value, error
func run(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("run requires id and host arguments"))
		return 2
	}

	id := l.CheckString(1)
	if id == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("process ID required"))
		return 2
	}

	regID := registry.ParseID(id)
	if regID.NS == "" || regID.Name == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid process ID format (namespace:name required)"))
		return 2
	}

	hostID := l.CheckString(2)
	if hostID == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("host ID required"))
		return 2
	}

	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.run", id, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to run process: %s", id)))
		return 2
	}

	if !security.IsAllowed(l.Context(), "process.host", hostID, secAttrs) {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("not allowed to run on host: %s", hostID)))
		return 2
	}

	// Collect payload arguments (starting from arg 3)
	var payloads payload.Payloads
	for i := 3; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	yield := AcquireRunYield()
	yield.Source = regID
	yield.Input = payloads
	yield.HostID = hostID

	l.Push(yield)
	return -1
}
