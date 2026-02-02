package store

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/store"
	lua "github.com/wippyai/go-lua"
)

// GetYield wraps GetCmd for Lua.
type GetYield struct {
	*store.GetCmd
}

var getYieldPool = sync.Pool{New: func() any { return &GetYield{} }}

func AcquireGetYield() *GetYield {
	y := getYieldPool.Get().(*GetYield)
	y.GetCmd = store.AcquireGetCmd()
	return y
}

func ReleaseGetYield(y *GetYield) {
	if y.GetCmd != nil {
		y.GetCmd.Release()
		y.GetCmd = nil
	}
	getYieldPool.Put(y)
}

func (y *GetYield) String() string                { return "<store_get_yield>" }
func (y *GetYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *GetYield) CmdID() dispatcher.CommandID   { return store.Get }
func (y *GetYield) ToCommand() dispatcher.Command { return y.GetCmd }
func (y *GetYield) Release()                      { ReleaseGetYield(y) }

func (y *GetYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(store.GetResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{transcodeToLua(l, resp.Value), lua.LNil}
}

// transcodeToLua converts a payload to Lua value using context transcoder.
func transcodeToLua(l *lua.LState, pl payload.Payload) lua.LValue {
	if pl == nil {
		return lua.LNil
	}

	// Already a Lua value
	if pl.Format() == payload.Lua {
		if lv, ok := pl.Data().(lua.LValue); ok {
			return lv
		}
	}

	// Try transcoding via context transcoder
	ctx := l.Context()
	dtt := payload.GetTranscoder(ctx)
	if dtt != nil {
		transcoded, err := dtt.Transcode(pl, payload.Lua)
		if err == nil {
			if lv, ok := transcoded.Data().(lua.LValue); ok {
				return lv
			}
		}
	}

	// Fallback: return as string representation
	return lua.LString(fmt.Sprintf("%v", pl.Data()))
}

// SetYield wraps SetCmd for Lua.
type SetYield struct {
	*store.SetCmd
}

var setYieldPool = sync.Pool{New: func() any { return &SetYield{} }}

func AcquireSetYield() *SetYield {
	y := setYieldPool.Get().(*SetYield)
	y.SetCmd = store.AcquireSetCmd()
	return y
}

func ReleaseSetYield(y *SetYield) {
	if y.SetCmd != nil {
		y.SetCmd.Release()
		y.SetCmd = nil
	}
	setYieldPool.Put(y)
}

func (y *SetYield) String() string                { return "<store_set_yield>" }
func (y *SetYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *SetYield) CmdID() dispatcher.CommandID   { return store.Set }
func (y *SetYield) ToCommand() dispatcher.Command { return y.SetCmd }
func (y *SetYield) Release()                      { ReleaseSetYield(y) }

func (y *SetYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(store.SetResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// DeleteYield wraps DeleteCmd for Lua.
type DeleteYield struct {
	*store.DeleteCmd
}

var deleteYieldPool = sync.Pool{New: func() any { return &DeleteYield{} }}

func AcquireDeleteYield() *DeleteYield {
	y := deleteYieldPool.Get().(*DeleteYield)
	y.DeleteCmd = store.AcquireDeleteCmd()
	return y
}

func ReleaseDeleteYield(y *DeleteYield) {
	if y.DeleteCmd != nil {
		y.DeleteCmd.Release()
		y.DeleteCmd = nil
	}
	deleteYieldPool.Put(y)
}

func (y *DeleteYield) String() string                { return "<store_delete_yield>" }
func (y *DeleteYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *DeleteYield) CmdID() dispatcher.CommandID   { return store.Delete }
func (y *DeleteYield) ToCommand() dispatcher.Command { return y.DeleteCmd }
func (y *DeleteYield) Release()                      { ReleaseDeleteYield(y) }

func (y *DeleteYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(store.DeleteResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		if resp.NotFound {
			return []lua.LValue{lua.LFalse, lua.LNil}
		}
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// HasYield wraps HasCmd for Lua.
type HasYield struct {
	*store.HasCmd
}

var hasYieldPool = sync.Pool{New: func() any { return &HasYield{} }}

func AcquireHasYield() *HasYield {
	y := hasYieldPool.Get().(*HasYield)
	y.HasCmd = store.AcquireHasCmd()
	return y
}

func ReleaseHasYield(y *HasYield) {
	if y.HasCmd != nil {
		y.HasCmd.Release()
		y.HasCmd = nil
	}
	hasYieldPool.Put(y)
}

func (y *HasYield) String() string                { return "<store_has_yield>" }
func (y *HasYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *HasYield) CmdID() dispatcher.CommandID   { return store.Has }
func (y *HasYield) ToCommand() dispatcher.Command { return y.HasCmd }
func (y *HasYield) Release()                      { ReleaseHasYield(y) }

func (y *HasYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LFalse, lua.LString(err.Error())}
	}
	resp, ok := data.(store.HasResponse)
	if !ok {
		return []lua.LValue{lua.LFalse, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LFalse, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{lua.LBool(resp.Exists), lua.LNil}
}
