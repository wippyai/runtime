// SPDX-License-Identifier: MPL-2.0
package stream

import (
	"errors"
	"math"
	"unicode/utf8"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/resource"
	streamapi "github.com/wippyai/runtime/api/stream"
	streamsys "github.com/wippyai/runtime/system/stream"
)

func allocatePipe(l *lua.LState) int {
	peer := l.CheckString(1)
	limit := float64(l.CheckNumber(2))
	if len(peer) == 0 || len(peer) > 512 || !utf8.ValidString(peer) || math.IsNaN(limit) || limit < 1 || limit > 1<<40 || limit != math.Trunc(limit) {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "invalid pipe peer or byte limit").WithKind(lua.Invalid))
		return 3
	}
	l.Push(&pipeYield{peer: peer, limit: uint64(limit)})
	return -1
}

type pipeYield struct {
	peer  string
	limit uint64
}

func (*pipeYield) String() string              { return "<stream.pipe>" }
func (*pipeYield) Type() lua.LValueType        { return lua.LTUserData }
func (*pipeYield) CmdID() dispatcher.CommandID { return streamapi.Pipe }
func (y *pipeYield) ToCommand() dispatcher.Command {
	return streamapi.PipeCmd{Peer: y.peer, Limit: y.limit}
}
func (*pipeYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	fail := func(err error) []lua.LValue {
		return []lua.LValue{lua.LNil, lua.LNil, lua.WrapErrorWithLua(l, err, "allocate pipe")}
	}
	if err != nil {
		return fail(err)
	}
	r, ok := data.(streamapi.PipeResult)
	if !ok {
		return fail(errors.New("invalid pipe result"))
	}
	table := resource.GetTable(l.Context())
	if table == nil {
		return fail(streamapi.ErrNoTable)
	}
	if _, err := streamsys.Get(table, r.StreamID); err != nil {
		return fail(err)
	}
	return []lua.LValue{NewStream(l, r.StreamID), lua.LString(r.Offer), lua.LNil}
}
