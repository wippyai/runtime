// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const (
	cdcStreamTypeName = "cdc.Stream"

	// Keep Lua-side option allocations bounded independently of the source
	// implementation. The source may apply a smaller limit, but this upper
	// bound prevents a malformed table or direct internal option from causing
	// an unbounded map/slice/channel allocation in the Lua adapter.
	defaultStreamBuffer = 64
	maxStreamItems      = 65536
)

var subscriptionCounter uint64

var Module = &luaapi.ModuleDef{
	Name:        "cdc",
	Description: "Driver-neutral CDC source streams",
	Class:       []string{luaapi.ClassStorage, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		value.RegisterTypeMethods(nil, cdcStreamTypeName, nil, streamMethods)

		mod := lua.CreateTable(0, 3)
		mod.RawSetString("list_sources", lua.LGoFunc(listSources))
		mod.RawSetString("source", lua.LGoFunc(getSource))
		mod.RawSetString("stream", lua.LGoFunc(openStream))
		mod.Immutable = true
		return mod, []luaapi.YieldType{
			{Sample: &SubscribeYield{}, CmdID: cdcapi.Subscribe},
		}
	},
	Types: ModuleTypes,
}

type Stream struct {
	Channel       *engine.Channel
	proc          *engine.Process
	cancelCleanup func()
	Source        string
	topic         string
	Options       cdcapi.StreamOptions
	mu            sync.Mutex
	subscribed    bool
	closed        bool
}

var streamMethods = map[string]lua.LGoFunc{
	"channel": streamChannel,
	"receive": streamChannel,
	"close":   streamClose,
	"release": streamClose,
}

func listSources(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	var infos []cdcapi.SourceInfo
	if registry := cdcapi.GetRegistry(ctx); registry != nil {
		infos = registry.List()
	} else {
		inspector := cdcapi.GetSourceInspector(ctx)
		if inspector == nil {
			l.Push(lua.LNil)
			l.Push(lua.NewLuaError(l, "cdc source inspector not found").
				WithKind(lua.Internal).
				WithRetryable(false))
			return 2
		}
		infos = inspector.List()
	}

	result := l.CreateTable(len(infos), 0)
	for i, info := range infos {
		result.RawSetInt(i+1, sourceInfoToTable(l, info))
	}
	l.Push(result)
	l.Push(lua.LNil)
	return 2
}

func getSource(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	name := l.CheckString(1)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "source name is required").
			WithKind(lua.Invalid).
			WithRetryable(false))
		return 2
	}

	var (
		info cdcapi.SourceInfo
		ok   bool
	)
	if cdcRegistry := cdcapi.GetRegistry(ctx); cdcRegistry != nil {
		id := registry.ParseID(name)
		source, found := cdcRegistry.Get(id)
		if found && source != nil {
			info = source.Info()
			if info.ID.NS == "" && info.ID.Name == "" {
				info.ID = id
			}
			if info.Name == "" {
				info.Name = info.ID.String()
			}
			ok = true
		}
	} else if inspector := cdcapi.GetSourceInspector(ctx); inspector != nil {
		info, ok = inspector.Get(name)
	} else {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "cdc source inspector not found").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	l.Push(sourceInfoToTable(l, info))
	l.Push(lua.LNil)
	return 2
}

func openStream(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	name := l.CheckString(1)
	if name == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "source name is required").
			WithKind(lua.Invalid).
			WithRetryable(false))
		return 2
	}
	opts, luaErr := streamOptionsFromLua(l, 2)
	if luaErr != nil {
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	ch := engine.NewChannel(streamBufferCapacity(opts.Buffer))
	engine.PushChannel(l, ch)
	l.Pop(1)

	ud := value.PushTypedUserData(l, &Stream{
		Source:  name,
		Options: opts,
		Channel: ch,
	}, cdcStreamTypeName)
	l.Pop(1)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func checkStream(l *lua.LState) *Stream {
	ud := l.CheckUserData(1)
	if stream, ok := ud.Value.(*Stream); ok {
		return stream
	}
	l.ArgError(1, "cdc.Stream expected")
	return nil
}

func streamChannel(l *lua.LState) int {
	stream := checkStream(l)
	if stream == nil {
		return 0
	}

	stream.mu.Lock()
	if stream.closed {
		stream.mu.Unlock()
		l.RaiseError("cdc stream is closed")
		return 0
	}
	if stream.subscribed && stream.Channel != nil {
		ch := stream.Channel.Value()
		stream.mu.Unlock()
		l.Push(ch)
		return 1
	}
	if stream.Channel == nil {
		stream.mu.Unlock()
		l.RaiseError("cdc stream has no channel")
		return 0
	}
	stream.mu.Unlock()

	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context found")
		return 0
	}
	pidVal, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("no process PID")
		return 0
	}

	topic := fmt.Sprintf("cdc@%d", atomic.AddUint64(&subscriptionCounter, 1))
	l.Push(AcquireSubscribeYield(stream.Source, stream.Options, stream.Channel, pidVal, topic, stream))
	return -1
}

func streamClose(l *lua.LState) int {
	stream := checkStream(l)
	if stream == nil {
		return 0
	}
	stream.close()
	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func (s *Stream) close() {
	s.closeWithUnsubscribe(true)
}

func (s *Stream) cleanup() {
	s.closeWithUnsubscribe(false)
}

func (s *Stream) closeWithUnsubscribe(unsubscribe bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	cancel := s.cancelCleanup
	s.cancelCleanup = nil
	proc := s.proc
	ch := s.Channel
	s.proc = nil
	s.subscribed = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if unsubscribe && proc != nil && ch != nil {
		proc.UnsubscribeChannel(ch)
		return
	}
	if ch != nil && !ch.IsClosed() {
		_ = ch.Close(nil)
	}
}

func sourceInfoToTable(l *lua.LState, info cdcapi.SourceInfo) *lua.LTable {
	t := l.CreateTable(0, 24)
	if !isZeroRegistryID(info.ID) {
		t.RawSetString("id", lua.LString(registryIDString(info.ID)))
	}
	if info.Kind != "" {
		t.RawSetString("kind", lua.LString(info.Kind))
	}
	state := string(info.State)
	if state == "" {
		state = string(cdcapi.SourceStateUnknown)
	}
	t.RawSetString("state", lua.LString(state))
	if info.Generation != "" {
		t.RawSetString("generation", lua.LString(info.Generation))
	}

	capabilities := l.CreateTable(0, 6)
	capabilities.RawSetString("snapshot", lua.LBool(info.Capabilities.Snapshot))
	capabilities.RawSetString("durable", lua.LBool(info.Capabilities.Durable))
	capabilities.RawSetString("replayable", lua.LBool(info.Capabilities.Replayable))
	capabilities.RawSetString("captures_external_writes", lua.LBool(info.Capabilities.CapturesExternalWrites))
	capabilities.RawSetString("before_images", lua.LBool(info.Capabilities.BeforeImages))
	capabilities.RawSetString("coalesced", lua.LBool(info.Capabilities.Coalesced))
	t.RawSetString("capabilities", capabilities)

	// Keep the legacy identity fields present with their historical defaults;
	// newer callers should use id/kind/state above.
	t.RawSetString("name", lua.LString(info.Name))
	t.RawSetString("slot", lua.LString(info.Slot))
	if info.Engine != "" {
		t.RawSetString("engine", lua.LString(info.Engine))
	}
	if info.File != "" {
		t.RawSetString("file", lua.LString(info.File))
	}
	if info.DBResource != "" {
		t.RawSetString("db_resource", lua.LString(info.DBResource))
	}
	if info.Epoch != "" {
		t.RawSetString("epoch", lua.LString(info.Epoch))
	}
	if info.Error != "" {
		t.RawSetString("error", lua.LString(info.Error))
	}
	if info.Publication != "" {
		t.RawSetString("publication", lua.LString(info.Publication))
	}
	if len(info.Tables) > 0 {
		tables := l.CreateTable(len(info.Tables), 0)
		for i, name := range info.Tables {
			tables.RawSetInt(i+1, lua.LString(name))
		}
		t.RawSetString("tables", tables)
	}
	t.RawSetString("streaming", lua.LBool(info.Streaming))
	t.RawSetString("failover", lua.LBool(info.Failover))
	t.RawSetString("temporary", lua.LBool(info.Temporary))
	t.RawSetString("snapshot", lua.LBool(info.Snapshot))
	t.RawSetString("faulted", lua.LBool(info.Faulted))
	return t
}

func streamOptionsFromLua(l *lua.LState, idx int) (cdcapi.StreamOptions, *lua.Error) {
	var opts cdcapi.StreamOptions
	if l.GetTop() < idx || l.Get(idx) == lua.LNil {
		return opts, nil
	}
	table, ok := l.Get(idx).(*lua.LTable)
	if !ok {
		return opts, lua.NewLuaError(l, "stream options must be a table").
			WithKind(lua.Invalid).
			WithRetryable(false)
	}

	if errMsg := validateOptionKeys(table); errMsg != "" {
		return opts, invalidStreamOption(l, errMsg)
	}

	var errMsg string
	if opts.Tables, errMsg = stringArrayField(table, "tables"); errMsg != "" {
		return opts, invalidStreamOption(l, errMsg)
	}
	if opts.Ops, errMsg = stringArrayField(table, "ops"); errMsg != "" {
		return opts, invalidStreamOption(l, errMsg)
	}
	if v := table.RawGetString("buffer"); v != lua.LNil {
		if v.Type() != lua.LTNumber && v.Type() != lua.LTInteger {
			return opts, invalidStreamOption(l, "buffer must be a number")
		}
		number := lua.LVAsNumber(v)
		if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) ||
			math.Trunc(float64(number)) != float64(number) ||
			number < 1 || number > lua.LNumber(maxStreamItems) {
			return opts, invalidStreamOption(l, "buffer must be a positive integer")
		}
		n := int(number)
		opts.Buffer = n
	}
	if v := table.RawGetString("snapshot"); v != lua.LNil {
		if v.Type() != lua.LTBool {
			return opts, invalidStreamOption(l, "snapshot must be a boolean")
		}
		opts.Snapshot = lua.LVAsBool(v)
	}
	if v := table.RawGetString("after"); v != lua.LNil {
		if v.Type() != lua.LTString {
			return opts, invalidStreamOption(l, "after must be a string")
		}
		after := string(v.(lua.LString))
		if strings.TrimSpace(after) == "" {
			return opts, invalidStreamOption(l, "after must not be empty")
		}
		opts.After = after
	}
	return opts, nil
}

func streamBufferCapacity(buffer int) int {
	if buffer <= 0 {
		return defaultStreamBuffer
	}
	if buffer > maxStreamItems {
		return maxStreamItems
	}
	return buffer
}

func invalidStreamOption(l *lua.LState, message string) *lua.Error {
	return lua.NewLuaError(l, message).
		WithKind(lua.Invalid).
		WithRetryable(false)
}

func validateOptionKeys(table *lua.LTable) string {
	var errMsg string
	table.ForEach(func(key, _ lua.LValue) {
		if errMsg != "" {
			return
		}
		name, ok := key.(lua.LString)
		if !ok {
			errMsg = "stream options contains unknown or non-string field"
			return
		}
		switch string(name) {
		case "tables", "ops", "buffer", "snapshot", "after":
		default:
			errMsg = "stream options contains unknown field: " + string(name)
		}
	})
	return errMsg
}

func stringArrayField(table *lua.LTable, field string) ([]string, string) {
	v := table.RawGetString(field)
	if v == lua.LNil {
		return nil, ""
	}
	t, ok := v.(*lua.LTable)
	if !ok {
		return nil, field + " must be an array of strings"
	}
	count := t.Len()
	if count > maxStreamItems {
		return nil, field + " must contain at most 65536 entries"
	}
	values := make(map[int]string, count)
	max := 0
	var errMsg string
	t.ForEach(func(key, value lua.LValue) {
		if errMsg != "" {
			return
		}
		var position int
		switch key.Type() {
		case lua.LTInteger:
			index, ok := key.(lua.LInteger)
			if !ok || index <= 0 || index > lua.LInteger(maxStreamItems) {
				errMsg = field + " must be an array of strings"
				return
			}
			position = int(index)
		case lua.LTNumber:
			number := lua.LVAsNumber(key)
			if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) ||
				math.Trunc(float64(number)) != float64(number) || number <= 0 ||
				number > lua.LNumber(maxStreamItems) {
				errMsg = field + " must be an array of strings"
				return
			}
			position = int(number)
			if position <= 0 {
				errMsg = field + " must be an array of strings"
				return
			}
		default:
			errMsg = field + " must be an array of strings"
			return
		}
		if value.Type() != lua.LTString || strings.TrimSpace(value.String()) == "" {
			errMsg = field + " must contain non-empty strings"
			return
		}
		if _, exists := values[position]; !exists && len(values) >= maxStreamItems {
			errMsg = field + " must contain at most 65536 entries"
			return
		}
		values[position] = string(value.(lua.LString))
		if position > max {
			max = position
		}
	})
	if errMsg != "" {
		return nil, errMsg
	}
	out := make([]string, max)
	for i := 1; i <= max; i++ {
		value, ok := values[i]
		if !ok {
			return nil, field + " must be a contiguous array of strings"
		}
		out[i-1] = value
	}
	return out, ""
}

func isZeroRegistryID(id registry.ID) bool {
	return id.NS == "" && id.Name == ""
}

func registryIDString(id registry.ID) string {
	return id.String()
}

func markSubscribed(stream *Stream, proc *engine.Process, topic string, cancelCleanup func()) error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		if cancelCleanup != nil {
			cancelCleanup()
		}
		return fmt.Errorf("cdc stream is closed")
	}
	stream.proc = proc
	stream.topic = topic
	stream.cancelCleanup = cancelCleanup
	stream.subscribed = true
	return nil
}
