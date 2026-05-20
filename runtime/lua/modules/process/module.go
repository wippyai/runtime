// SPDX-License-Identifier: MPL-2.0

package process

import (
	"fmt"
	"strings"
	"sync"
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
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/api/topology"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/security"
)

var (
	moduleTable *lua.LTable
	yieldTypes  []luaapi.YieldType
)

// fenceEntry stores a cached fence token for a PID that was resolved via global registry.
type fenceEntry struct {
	globalName string
	fenceToken uint64
}

// fenceCaches stores per-LState fence caches. Entries are cleaned up via
// resource.Store when the process ends (see getFenceCache).
var fenceCaches sync.Map // *lua.LState -> map[string]fenceEntry

// getFenceCache returns the fence cache for the given LState, creating it if needed.
// When a new cache is created, registers a cleanup callback via resource.Store
// to ensure the entry is removed when the process ends, preventing memory leaks.
func getFenceCache(l *lua.LState) map[string]fenceEntry {
	if v, ok := fenceCaches.Load(l); ok {
		return v.(map[string]fenceEntry)
	}
	m := make(map[string]fenceEntry)
	fenceCaches.Store(l, m)

	// Register cleanup via resource.Store (runs when process ends)
	ctx := l.Context()
	if ctx != nil {
		if store := resource.GetStore(ctx); store != nil {
			store.AddCleanup(func() error {
				CleanupFenceCache(l)
				return nil
			})
		}
	}

	return m
}

// CleanupFenceCache removes the fence cache for a completed LState.
// Should be called when the process terminates.
func CleanupFenceCache(l *lua.LState) {
	fenceCaches.Delete(l)
}

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

	reg := lua.CreateTable(0, 7)
	reg.RawSetString("register", lua.LGoFunc(registryRegister))
	reg.RawSetString("lookup", lua.LGoFunc(registryLookup))
	reg.RawSetString("lookup_with_fence", lua.LGoFunc(registryLookupWithFence))
	reg.RawSetString("validate_fence", lua.LGoFunc(registryValidateFence))
	reg.RawSetString("unregister", lua.LGoFunc(registryUnregister))
	reg.RawSetString("LOCAL", lua.LNumber(float64(topology.Local)))
	reg.RawSetString("GLOBAL", lua.LNumber(float64(topology.Global)))
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

// fenceInfo holds fence token data resolved during PID lookup.
type fenceInfo struct {
	globalName string
	fenceToken uint64
}

func resolvePID(l *lua.LState, pidOrName string, permission string, senderPID pidapi.PID) (pidapi.PID, *fenceInfo, error) {
	secAttrs := map[string]any{"pid": senderPID.String()}

	p, err := pidapi.ParsePID(pidOrName)
	if err == nil {
		if !security.IsAllowed(l.Context(), permission, p.String(), secAttrs) {
			return pidapi.PID{}, nil, runtimelua.NewNotAllowedError(
				strings.TrimPrefix(permission, "process."), pidOrName)
		}
		// Check fence cache for this raw PID.
		cache := getFenceCache(l)
		if entry, ok := cache[p.String()]; ok {
			return p, &fenceInfo{fenceToken: entry.fenceToken, globalName: entry.globalName}, nil
		}
		return p, nil, nil
	}

	reg, ok := getRegistry(l)
	if !ok {
		return pidapi.PID{}, nil, runtimelua.ErrCouldNotAccessRegistry
	}

	// Try global registry first for fence-aware lookup.
	var fi *fenceInfo
	globalReg := globalreg.GetRegistry(l.Context())
	if globalReg != nil {
		result, _ := globalReg.Lookup(l.Context(), pidOrName, globalreg.WithFence())
		if result.Found {
			p = result.PID
			fi = &fenceInfo{fenceToken: result.FenceToken, globalName: pidOrName}
			// Populate fence cache so future sends using the raw PID string
			// will automatically carry the fence token.
			cache := getFenceCache(l)
			cache[p.String()] = fenceEntry{globalName: pidOrName, fenceToken: result.FenceToken}

			if !security.IsAllowed(l.Context(), permission, p.String(), secAttrs) {
				return pidapi.PID{}, nil, runtimelua.NewNotAllowedError(
					strings.TrimPrefix(permission, "process."), pidOrName)
			}
			return p, fi, nil
		}
	}

	// Fall back to PID registry (local + global transparent lookup).
	p, found := reg.Lookup(pidOrName)
	if !found {
		return pidapi.PID{}, nil, runtimelua.NewCouldNotResolveError(pidOrName)
	}

	if !security.IsAllowed(l.Context(), permission, p.String(), secAttrs) {
		return pidapi.PID{}, nil, runtimelua.NewNotAllowedError(
			strings.TrimPrefix(permission, "process."), pidOrName)
	}

	return p, nil, nil
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

	target, fi, err := resolvePID(l, pidOrName, "process.send", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
	}

	yield := AcquireSendYield()
	yield.From = self
	yield.To = target
	yield.Topic = topic
	yield.Payloads = createPayloadsFromArgs(l)
	if fi != nil {
		yield.FenceToken = fi.fenceToken
		yield.GlobalName = fi.globalName
	}

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

	target, _, err := resolvePID(l, pidOrName, "process.terminate", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
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
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "cancel requires at least destination argument"))
	}

	pidOrName := l.CheckString(1)

	target, _, err := resolvePID(l, pidOrName, "process.cancel", self)
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

	target, _, err := resolvePID(l, pidOrName, "process.monitor", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
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
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "unmonitor requires a destination argument"))
	}

	pidOrName := l.CheckString(1)

	target, _, err := resolvePID(l, pidOrName, "process.unmonitor", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
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
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "link requires a destination argument"))
	}

	pidOrName := l.CheckString(1)

	target, _, err := resolvePID(l, pidOrName, "process.link", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
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
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "unlink requires a destination argument"))
	}

	pidOrName := l.CheckString(1)

	target, _, err := resolvePID(l, pidOrName, "process.unlink", self)
	if err != nil {
		return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
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

	// Determine registration mode. The second argument can be:
	//   - a number (GLOBAL=1, LOCAL=0): registration mode
	//   - a string: PID to register (legacy usage)
	//   - absent: defaults to LOCAL with self PID
	var p pidapi.PID
	mode := topology.Local
	p = self

	if l.GetTop() >= 2 {
		arg2 := l.Get(2)
		switch v := arg2.(type) {
		case lua.LNumber:
			mode = topology.RegistrationMode(int(v))
		case lua.LString:
			// Legacy: second arg is a PID string.
			var err error
			p, err = pidapi.ParsePID(string(v))
			if err != nil {
				return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Invalid))
			}
		default:
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.Invalid, "second argument must be a mode (number) or PID (string)"))
		}
	}

	if mode == topology.Global {
		// Global registration via Raft consensus.
		if !security.IsAllowed(l.Context(), "process.registry.register.global", name, secAttrs) {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to globally register name: %s", name)))
		}

		globalReg := globalreg.GetRegistry(l.Context())
		if globalReg == nil {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "global registry not available"))
		}

		_, err := globalReg.Register(l.Context(), name, p)
		if err != nil {
			return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
		}

		l.Push(lua.LTrue)
		return 1
	}

	// Local registration (default).
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

func registryLookupWithFence(l *lua.LState) int {
	name := l.CheckString(1)

	globalReg := globalreg.GetRegistry(l.Context())
	if globalReg == nil {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "global registry not available"))
	}

	result, _ := globalReg.Lookup(l.Context(), name, globalreg.WithFence())
	if !result.Found {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.NotFound, "name not registered"))
	}

	tbl := l.CreateTable(0, 2)
	tbl.RawSetString("pid", lua.LString(result.PID.String()))
	tbl.RawSetString("fence_token", lua.LNumber(float64(result.FenceToken)))

	l.Push(tbl)
	return 1
}

func registryValidateFence(l *lua.LState) int {
	name := l.CheckString(1)
	token := uint64(l.CheckNumber(2))

	globalReg := globalreg.GetRegistry(l.Context())
	if globalReg == nil {
		return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "global registry not available"))
	}

	if err := globalreg.ValidateFence(l.Context(), globalReg, name, token); err != nil {
		return pushProcessError(l, lua.LBool(false), wrapProcessError(l, err, "", lua.Conflict))
	}

	l.Push(lua.LTrue)
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

	if mode == topology.Global {
		if !security.IsAllowed(l.Context(), "process.registry.unregister.global", name, secAttrs) {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.PermissionDenied, fmt.Sprintf("not allowed to globally unregister name: %s", name)))
		}

		globalReg := globalreg.GetRegistry(l.Context())
		if globalReg == nil {
			return pushProcessError(l, lua.LNil, newProcessError(l, lua.Internal, "global registry not available"))
		}

		removed, err := globalReg.Unregister(l.Context(), name)
		if err != nil {
			return pushProcessError(l, lua.LNil, wrapProcessError(l, err, "", lua.Internal))
		}

		l.Push(lua.LBool(removed))
		return 1
	}

	// Local unregistration (default).
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
