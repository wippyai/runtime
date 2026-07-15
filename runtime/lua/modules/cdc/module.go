// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"fmt"
	"sync"
	"sync/atomic"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const cdcStreamTypeName = "cdc.Stream"

var subscriptionCounter uint64

var Module = &luaapi.ModuleDef{
	Name:        "cdc",
	Description: "Postgres CDC source streams",
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

	inspector := cdcapi.GetSourceInspector(ctx)
	if inspector == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "cdc source inspector not found").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	infos := inspector.List()
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

	inspector := cdcapi.GetSourceInspector(ctx)
	if inspector == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "cdc source inspector not found").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	info, ok := inspector.Get(name)
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

	ch := engine.NewChannel(64)
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
	t := l.CreateTable(0, 12)
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

	opts.Tables = stringArrayField(table, "tables")
	opts.Ops = stringArrayField(table, "ops")
	if v := table.RawGetString("buffer"); v != lua.LNil {
		if v.Type() != lua.LTNumber && v.Type() != lua.LTInteger {
			return opts, lua.NewLuaError(l, "buffer must be a number").
				WithKind(lua.Invalid).
				WithRetryable(false)
		}
		n := int(lua.LVAsNumber(v))
		if n <= 0 || n > 65536 {
			return opts, lua.NewLuaError(l, "buffer must be between 1 and 65536").
				WithKind(lua.Invalid).
				WithRetryable(false)
		}
		opts.Buffer = n
	}
	if v := table.RawGetString("snapshot"); v != lua.LNil {
		opts.Snapshot = lua.LVAsBool(v)
	}
	return opts, nil
}

func stringArrayField(table *lua.LTable, field string) []string {
	v := table.RawGetString(field)
	if v == lua.LNil {
		return nil
	}
	t, ok := v.(*lua.LTable)
	if !ok {
		return nil
	}
	out := make([]string, 0, t.Len())
	t.ForEach(func(_, value lua.LValue) {
		if value.Type() == lua.LTString {
			out = append(out, value.String())
		}
	})
	return out
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
