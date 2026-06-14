// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"fmt"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime/resource"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
)

type SubscribeYield struct {
	Channel *engine.Channel
	Stream  *Stream
	PID     pid.PID
	Source  string
	Topic   string
	Options cdcapi.StreamOptions
}

var cdcSubscribeYieldPool = sync.Pool{
	New: func() any { return &SubscribeYield{} },
}

func AcquireSubscribeYield(source string, opts cdcapi.StreamOptions, ch *engine.Channel, p pid.PID, topic string, stream *Stream) *SubscribeYield {
	y := cdcSubscribeYieldPool.Get().(*SubscribeYield)
	y.Source = source
	y.Options = opts
	y.Channel = ch
	y.PID = p
	y.Topic = topic
	y.Stream = stream
	return y
}

func ReleaseSubscribeYield(y *SubscribeYield) {
	y.Channel = nil
	y.Stream = nil
	y.Options = cdcapi.StreamOptions{}
	y.PID = pid.PID{}
	y.Source = ""
	y.Topic = ""
	cdcSubscribeYieldPool.Put(y)
}

func (y *SubscribeYield) String() string       { return "<cdc_subscribe_yield>" }
func (y *SubscribeYield) Type() lua.LValueType { return lua.LTUserData }

func (y *SubscribeYield) CmdID() dispatcher.CommandID {
	return cdcapi.Subscribe
}

func (y *SubscribeYield) ToCommand() dispatcher.Command {
	return cdcapi.SubscribeCmd{
		Source:  y.Source,
		Options: y.Options,
		PID:     y.PID,
		Topic:   y.Topic,
	}
}

func (y *SubscribeYield) Release() { ReleaseSubscribeYield(y) }

func (y *SubscribeYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "cdc stream")}
	}

	sub, ok := data.(cdcapi.Subscription)
	if !ok || sub.Stop == nil {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid cdc subscription").WithKind(lua.Internal).WithRetryable(false)}
	}

	proc := engine.GetProcess(l)
	if proc == nil {
		sub.Stop()
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "no process context").WithKind(lua.Internal).WithRetryable(false)}
	}

	if err := proc.SubscribeExisting(y.Topic, y.Channel); err != nil {
		sub.Stop()
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "cdc stream")}
	}
	proc.SetTopicHandler(y.Topic, cdcChangeHandler)
	if !proc.SetSubscriptionCleanup(y.Channel, sub.Stop) {
		sub.Stop()
		proc.UnsubscribeChannel(y.Channel)
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "failed to register cdc subscription cleanup").WithKind(lua.Internal).WithRetryable(true)}
	}

	var cancelCleanup func()
	if ctx := l.Context(); ctx != nil {
		if store := resource.GetStore(ctx); store != nil && y.Stream != nil {
			stream := y.Stream
			cancelCleanup = store.AddCleanup(func() error {
				stream.cleanup()
				return nil
			})
		}
	}

	if y.Stream != nil {
		if err := markSubscribed(y.Stream, proc, y.Topic, cancelCleanup); err != nil {
			sub.Stop()
			proc.UnsubscribeChannel(y.Channel)
			return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "cdc stream")}
		}
	}

	return []lua.LValue{y.Channel.Value()}
}

func cdcChangeHandler(_ context.Context, l *lua.LState, _ pid.PID, _ string, payloads []payload.Payload) lua.LValue {
	if len(payloads) == 0 {
		return lua.LNil
	}
	change, ok := payloads[0].Data().(cdcapi.Change)
	if !ok {
		if ptr, ptrOK := payloads[0].Data().(*cdcapi.Change); ptrOK && ptr != nil {
			change = *ptr
			ok = true
		}
	}
	if !ok {
		return lua.NewLuaError(l, fmt.Sprintf("cdc change payload has invalid type %T", payloads[0].Data())).
			WithKind(lua.Internal).
			WithRetryable(false)
	}

	lv, err := changeToLua(l, change)
	if err != nil {
		return lua.NewLuaError(l, fmt.Sprintf("cdc change conversion failed: %v", err)).
			WithKind(lua.Internal).
			WithRetryable(false)
	}
	return lv
}

func changeToLua(l *lua.LState, change cdcapi.Change) (lua.LValue, error) {
	tbl := l.CreateTable(0, 10)
	tbl.RawSetString("source", lua.LString(change.Source))
	tbl.RawSetString("op", lua.LString(change.Op))
	tbl.RawSetString("schema", lua.LString(change.Schema))
	tbl.RawSetString("table", lua.LString(change.Table))
	tbl.RawSetString("relation", lua.LString(change.Relation))
	tbl.RawSetString("lsn", lua.LString(change.LSN))
	if change.CommitLSN != "" {
		tbl.RawSetString("commit_lsn", lua.LString(change.CommitLSN))
	}
	if change.XID != 0 {
		tbl.RawSetString("xid", lua.LInteger(change.XID))
	}
	if change.Before != nil {
		before, err := luapayload.GoToLua(change.Before)
		if err != nil {
			return nil, err
		}
		tbl.RawSetString("before", before)
	}
	if change.After != nil {
		after, err := luapayload.GoToLua(change.After)
		if err != nil {
			return nil, err
		}
		tbl.RawSetString("after", after)
	}
	return tbl, nil
}
