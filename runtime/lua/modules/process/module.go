// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"fmt"
	"strings"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/globalreg"
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
	"github.com/wippyai/runtime/runtime/security"
	sysprocess "github.com/wippyai/runtime/system/process"
)

var (
	moduleTable *lua.LTable
	yieldTypes  []luaapi.YieldType
)

func newProcessError(l *lua.LState, kind lua.Kind, message string) *lua.Error {
	return lua.NewLuaError(l, message).
		WithKind(kind).
		WithRetryable(false)
}

func wrapProcessError(l *lua.LState, err error, context string, fallback lua.Kind) *lua.Error {
	wrapped := lua.WrapErrorWithLua(l, err, context).WithRetryable(false)
	if wrapped.Kind() == lua.Unknown && fallback != lua.Unknown {
		wrapped = wrapped.WithKind(fallback)
	}
	return wrapped
}

func pushProcessError(l *lua.LState, value lua.LValue, err *lua.Error) int {
	l.Push(value)
	l.Push(err)
	return 2
}

func init() {
	value.RegisterTypeMethods(nil, messageTypeName,
		map[string]lua.LGoFunc{"__tostring": messageToString},
		messageMethods)

	moduleTable = lua.CreateTable(0, 22)

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
	moduleTable.RawSetString("with_options", lua.LGoFunc(spawnerNewWithOptions))

	moduleTable.RawSetString("inbox", lua.LGoFunc(inbox))
	moduleTable.RawSetString("events", lua.LGoFunc(events))
	moduleTable.RawSetString("listen", lua.LGoFunc(listen))
	moduleTable.RawSetString("unlisten", lua.LGoFunc(unlisten))
	moduleTable.RawSetString("upgrade", lua.LGoFunc(upgrade))
	moduleTable.RawSetString("exec", lua.LGoFunc(exec))

	eventsTbl := lua.CreateTable(0, 3)
	eventsTbl.RawSetString("CANCEL", lua.LString(topology.Cancel))
	eventsTbl.RawSetString("EXIT", lua.LString(topology.Exit))
	eventsTbl.RawSetString("LINK_DOWN", lua.LString(topology.LinkDown))
	eventsTbl.Immutable = true
	moduleTable.RawSetString("event", eventsTbl)

	reg := lua.CreateTable(0, 9)
	reg.RawSetString("register", lua.LGoFunc(registryRegister))
	reg.RawSetString("lookup", lua.LGoFunc(registryLookup))
	reg.RawSetString("unregister", lua.LGoFunc(registryUnregister))
	reg.RawSetString("LOCAL", lua.LNumber(float64(topology.Local)))
	reg.RawSetString("EVENTUAL", lua.LNumber(float64(topology.Eventual)))
	reg.RawSetString("CONSISTENT", lua.LNumber(float64(topology.Consistent)))
	reg.RawSetString("STRONG", lua.LNumber(float64(topology.Strong)))
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
		{Sample: &ExecYield{}, CmdID: process.Exec},
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
		pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "no context found"))
		return pidapi.PID{}, false
	}

	p, ok := runtime.GetFramePID(ctx)
	if !ok {
		pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "no process PID"))
		return pidapi.PID{}, false
	}
	return p, ok
}

func getRegistry(l *lua.LState) (topology.PIDRegistry, bool) {
	ctx := l.Context()
	if ctx == nil {
		pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "no context found"))
		return nil, false
	}

	reg := topology.GetRegistry(ctx)
	if reg == nil {
		pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "no registry found in context"))
		return nil, false
	}

	return reg, true
}

func resolvePID(l *lua.LState, pidOrName string, permission string, senderPID pidapi.PID) (sysprocess.ResolvedDestination, error) {
	secAttrs := map[string]any{"pid": senderPID.String()}

	resolved, err := sysprocess.ResolveDestination(l.Context(), pidOrName)
	if err != nil {
		return sysprocess.ResolvedDestination{}, runtimelua.NewCouldNotResolveError(pidOrName)
	}

	if !security.IsAllowed(l.Context(), permission, resolved.PID.String(), secAttrs) {
		return sysprocess.ResolvedDestination{}, runtimelua.NewNotAllowedError(
			strings.TrimPrefix(permission, "process."), pidOrName)
	}

	return resolved, nil
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
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "no context found"))
	}

	cc := ctxapi.FrameFromContext(ctx)
	if cc == nil {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "FrameContext not found"))
	}

	idValue, ok := cc.Get(runtime.FrameIDKey)
	if !ok {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "call ID not found in context"))
	}

	callID, ok := idValue.(registry.ID)
	if !ok {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "invalid call ID type"))
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
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "send requires at least destination and topic arguments"))
	}

	pidOrName := l.CheckString(1)
	topic := l.CheckString(2)

	if strings.HasPrefix(topic, "@") {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "cannot send to @ topics"))
	}

	resolved, err := resolvePID(l, pidOrName, "process.send", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
	}

	yield := AcquireSendYield()
	yield.From = self
	yield.To = resolved.PID
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
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "spawn requires at least id and host arguments"))
	}

	id := l.CheckString(1)
	hostID := l.CheckString(2)
	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.spawn", id, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to spawn process: %s", id)))
	}

	if !security.IsAllowed(l.Context(), "process.host", hostID, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to spawn on host: %s", hostID)))
	}

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.ProcessParentKey, self)

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
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "spawn_monitored requires at least id and host arguments"))
	}

	id := l.CheckString(1)
	hostID := l.CheckString(2)
	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.spawn", id, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to spawn process: %s", id)))
	}

	if !security.IsAllowed(l.Context(), "process.spawn.monitored", id, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to spawn monitored process: %s", id)))
	}

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.ProcessParentKey, self)
	options.Set(process.ProcessMonitorKey, true)

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

func spawnLinked(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 2 {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "spawn_linked requires at least id and host arguments"))
	}

	id := l.CheckString(1)
	hostID := l.CheckString(2)
	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.spawn", id, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to spawn process: %s", id)))
	}

	if !security.IsAllowed(l.Context(), "process.spawn.linked", id, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to spawn linked process: %s", id)))
	}

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.ProcessParentKey, self)
	options.Set(process.ProcessLinkKey, true)

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

func spawnLinkedMonitored(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 2 {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "spawn_linked_monitored requires at least id and host arguments"))
	}

	id := l.CheckString(1)
	hostID := l.CheckString(2)
	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.spawn", id, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to spawn process: %s", id)))
	}

	if !security.IsAllowed(l.Context(), "process.spawn.monitored", id, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to spawn monitored process: %s", id)))
	}

	if !security.IsAllowed(l.Context(), "process.spawn.linked", id, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to spawn linked process: %s", id)))
	}

	payloads := createPayloadsFromArgs(l)

	options := attrs.NewBag()
	options.Set(process.ProcessParentKey, self)
	options.Set(process.ProcessMonitorKey, true)
	options.Set(process.ProcessLinkKey, true)

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

func terminate(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	pidOrName := l.CheckString(1)

	resolved, err := resolvePID(l, pidOrName, "process.terminate", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
	}

	yield := AcquireTerminateYield()
	yield.Target = resolved.PID

	l.Push(yield)
	return -1
}

func cancel(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "cancel requires at least destination argument"))
	}

	pidOrName := l.CheckString(1)

	resolved, err := resolvePID(l, pidOrName, "process.cancel", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
	}

	var deadline time.Time
	if l.GetTop() >= 2 {
		switch l.Get(2).Type() {
		case lua.LTString:
			durationStr := l.CheckString(2)
			duration, err := time.ParseDuration(durationStr)
			if err != nil {
				return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, fmt.Sprintf("invalid duration format: %v", err)))
			}
			deadline = time.Now().Add(duration)
		case lua.LTNumber, lua.LTInteger:
			ms := l.CheckNumber(2)
			deadline = time.Now().Add(time.Duration(ms) * time.Millisecond)
		case lua.LTNil, lua.LTBool, lua.LTTable, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTChannel:
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "deadline must be either a duration string or milliseconds number"))
		}
	}

	yield := AcquireCancelYield()
	yield.From = self
	yield.Target = resolved.PID
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
		return pushProcessError(l, lua.LBool(false), newProcessError(l, lua.Invalid, "options parameter must be a table"))
	}

	proc := engine.GetProcess(l)
	if proc == nil {
		return pushProcessError(l, lua.LBool(false), newProcessError(l, lua.Internal, "no process context"))
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
		l.Push(newProcessError(l, lua.Invalid, unsupportedOption))
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
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "monitor requires a destination argument"))
	}

	pidOrName := l.CheckString(1)

	resolved, err := resolvePID(l, pidOrName, "process.monitor", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
	}

	yield := AcquireMonitorYield()
	yield.Watcher = self
	yield.Target = resolved.PID

	l.Push(yield)
	return -1
}

func unmonitor(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "unmonitor requires a destination argument"))
	}

	pidOrName := l.CheckString(1)

	resolved, err := resolvePID(l, pidOrName, "process.unmonitor", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
	}

	yield := AcquireUnmonitorYield()
	yield.Watcher = self
	yield.Target = resolved.PID

	l.Push(yield)
	return -1
}

func link(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "link requires a destination argument"))
	}

	pidOrName := l.CheckString(1)

	resolved, err := resolvePID(l, pidOrName, "process.link", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
	}

	yield := AcquireLinkYield()
	yield.From = self
	yield.To = resolved.PID

	l.Push(yield)
	return -1
}

func unlink(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 1 {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "unlink requires a destination argument"))
	}

	pidOrName := l.CheckString(1)

	resolved, err := resolvePID(l, pidOrName, "process.unlink", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
	}

	yield := AcquireUnlinkYield()
	yield.From = self
	yield.To = resolved.PID

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

	// Signature: register(name, scope?, pid?) → bool, err
	//   scope: process.registry.LOCAL | EVENTUAL | CONSISTENT | STRONG (default LOCAL)
	//   pid:   target PID string (default self)
	// Foreign PID (pid != self) requires process.registry.foreign on the
	// target PID in addition to the per-scope register capability on the name.
	mode := topology.Local
	p := self

	if l.GetTop() >= 2 && l.Get(2) != lua.LNil {
		num, ok := l.Get(2).(lua.LNumber)
		if !ok {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "scope must be a number (process.registry.LOCAL|EVENTUAL|CONSISTENT|STRONG)"))
		}
		mode = topology.RegistrationMode(int(num))
	}

	if l.GetTop() >= 3 && l.Get(3) != lua.LNil {
		raw, ok := l.Get(3).(lua.LString)
		if !ok {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "pid must be a string"))
		}
		parsed, err := pidapi.ParsePID(string(raw))
		if err != nil {
			return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "invalid pid", lua.Invalid))
		}
		p = parsed
	}

	// Mounting a name on a foreign PID is gated by an explicit second-axis
	// capability — the per-scope register permission gates the NAME, this
	// gates the TARGET PID. Default policy should deny; operators grant
	// process.registry.foreign for supervisors / hot-upgrade flows.
	if p != self {
		if !security.IsAllowed(l.Context(), "process.registry.foreign", p.String(), secAttrs) {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to register foreign pid %s under name %q", p.String(), name)))
		}
	}

	switch mode {
	case topology.Consistent, topology.Strong:
		permission := "process.registry.register.consistent"
		if mode == topology.Strong {
			permission = "process.registry.register.strong"
		}
		if !security.IsAllowed(l.Context(), permission, name, secAttrs) {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to register name (%s): %s", scopeLabel(mode), name)))
		}

		globalReg := globalreg.GetRegistry(l.Context())
		if globalReg == nil {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "global registry not available"))
		}

		_, err := globalReg.RegisterScope(l.Context(), name, p, globalreg.RegistrationMode(mode))
		if err != nil {
			return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
		}

		l.Push(lua.LTrue)
		return 1
	case topology.Eventual:
		if !security.IsAllowed(l.Context(), "process.registry.register.eventual", name, secAttrs) {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to register name (eventual): %s", name)))
		}

		eventualReg := getEventualRegistrar(l.Context())
		if eventualReg == nil {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "eventual registry not available"))
		}

		if _, err := eventualReg.Register(name, p); err != nil {
			return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.AlreadyExists))
		}

		l.Push(lua.LTrue)
		return 1
	}

	if !security.IsAllowed(l.Context(), "process.registry.register", name, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to register name: %s", name)))
	}

	_, err := reg.Register(name, p)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
	}

	l.Push(lua.LTrue)
	return 1
}

// scopeLabel returns the short string label used in error messages.
func scopeLabel(m topology.RegistrationMode) string {
	switch m {
	case topology.Local:
		return "local"
	case topology.Eventual:
		return "eventual"
	case topology.Consistent:
		return "consistent"
	case topology.Strong:
		return "strong"
	default:
		return "unknown"
	}
}

// eventualRegistrar is the minimal API the Lua surface needs from the
// EVENTUAL registry. The shape decouples the module from the concrete
// eventualreg.Service type so tests can substitute fakes.
type eventualRegistrar interface {
	Register(name string, p pidapi.PID) (pidapi.PID, error)
	Unregister(name string) bool
}

// getEventualRegistrar walks the topology context for an EventualRegistry
// and type-asserts the Register/Unregister surface the Lua glue needs.
// Returns nil when no eventual registry is bound.
func getEventualRegistrar(ctx context.Context) eventualRegistrar {
	er := topology.GetEventualRegistry(ctx)
	if er == nil {
		return nil
	}
	if reg, ok := er.(eventualRegistrar); ok {
		return reg
	}
	return nil
}

func registryLookup(l *lua.LState) int {
	reg, ok := getRegistry(l)
	if !ok {
		return 2
	}

	name := l.CheckString(1)

	p, found := reg.Lookup(name)
	if !found {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.NotFound, "name not registered"))
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

	// Check for mode argument.
	mode := topology.Local
	if l.GetTop() >= 2 {
		if modeVal, ok := l.Get(2).(lua.LNumber); ok {
			mode = topology.RegistrationMode(int(modeVal))
		}
	}

	switch mode {
	case topology.Consistent, topology.Strong:
		permission := "process.registry.unregister.consistent"
		if mode == topology.Strong {
			permission = "process.registry.unregister.strong"
		}
		if !security.IsAllowed(l.Context(), permission, name, secAttrs) {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to unregister name (%s): %s", scopeLabel(mode), name)))
		}

		globalReg := globalreg.GetRegistry(l.Context())
		if globalReg == nil {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "global registry not available"))
		}

		// Authority lives here: the primitive does not enforce holder identity,
		// so a caller with the unregister permission cannot drop another PID's
		// name. Treat owner mismatch (or unheld name) as a not-removed result
		// rather than an error, matching the existing false-on-no-removal
		// convention.
		res, err := globalReg.Lookup(l.Context(), name)
		if err != nil {
			return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
		}
		if !res.Found || res.PID != self {
			l.Push(lua.LBool(false))
			return 1
		}

		removed, err := globalReg.UnregisterScope(l.Context(), name, globalreg.RegistrationMode(mode))
		if err != nil {
			return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
		}

		l.Push(lua.LBool(removed))
		return 1
	case topology.Eventual:
		if !security.IsAllowed(l.Context(), "process.registry.unregister.eventual", name, secAttrs) {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to unregister name (eventual): %s", name)))
		}

		eventualReg := getEventualRegistrar(l.Context())
		if eventualReg == nil {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "eventual registry not available"))
		}

		l.Push(lua.LBool(eventualReg.Unregister(name)))
		return 1
	}

	if !security.IsAllowed(l.Context(), "process.registry.unregister", name, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to unregister name: %s", name)))
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
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "topic cannot be empty"))
	}

	if strings.HasPrefix(topic, "@") {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "cannot listen to @ topics"))
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

// exec spawns a process and waits for its result.
// Usage: process.exec(id, host, arg1, arg2, ...)
// Returns: value, error
func exec(l *lua.LState) int {
	self, ok := checkPID(l)
	if !ok {
		return 2
	}

	if l.GetTop() < 2 {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "exec requires id and host arguments"))
	}

	id := l.CheckString(1)
	if id == "" {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "process ID required"))
	}

	regID := registry.ParseID(id)
	if regID.NS == "" || regID.Name == "" {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "invalid process ID format (namespace:name required)"))
	}

	hostID := l.CheckString(2)
	if hostID == "" {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "host ID required"))
	}

	secAttrs := map[string]any{"pid": self.String()}

	if !security.IsAllowed(l.Context(), "process.exec", id, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to exec process: %s", id)))
	}

	if !security.IsAllowed(l.Context(), "process.host", hostID, secAttrs) {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to exec on host: %s", hostID)))
	}

	// Collect payload arguments (starting from arg 3)
	var payloads payload.Payloads
	for i := 3; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	yield := AcquireExecYield()
	yield.Source = regID
	yield.Input = payloads
	yield.HostID = hostID

	l.Push(yield)
	return -1
}
