package store

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/store"
	lua "github.com/yuin/gopher-lua"
)

// GetYield wraps StoreGetCmd for Lua.
type GetYield struct {
	*store.StoreGetCmd
}

var getYieldPool = sync.Pool{New: func() any { return &GetYield{} }}

func AcquireGetYield() *GetYield {
	y := getYieldPool.Get().(*GetYield)
	y.StoreGetCmd = store.AcquireStoreGetCmd()
	return y
}

func ReleaseGetYield(y *GetYield) {
	if y.StoreGetCmd != nil {
		y.StoreGetCmd.Release()
		y.StoreGetCmd = nil
	}
	getYieldPool.Put(y)
}

func (y *GetYield) String() string                { return "<store_get_yield>" }
func (y *GetYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *GetYield) CmdID() dispatcher.CommandID   { return store.CmdStoreGet }
func (y *GetYield) ToCommand() dispatcher.Command { return y.StoreGetCmd }
func (y *GetYield) Release()                      { ReleaseGetYield(y) }

func (y *GetYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(store.StoreGetResponse)
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

// SetYield wraps StoreSetCmd for Lua.
type SetYield struct {
	*store.StoreSetCmd
}

var setYieldPool = sync.Pool{New: func() any { return &SetYield{} }}

func AcquireSetYield() *SetYield {
	y := setYieldPool.Get().(*SetYield)
	y.StoreSetCmd = store.AcquireStoreSetCmd()
	return y
}

func ReleaseSetYield(y *SetYield) {
	if y.StoreSetCmd != nil {
		y.StoreSetCmd.Release()
		y.StoreSetCmd = nil
	}
	setYieldPool.Put(y)
}

func (y *SetYield) String() string                { return "<store_set_yield>" }
func (y *SetYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *SetYield) CmdID() dispatcher.CommandID   { return store.CmdStoreSet }
func (y *SetYield) ToCommand() dispatcher.Command { return y.StoreSetCmd }
func (y *SetYield) Release()                      { ReleaseSetYield(y) }

func (y *SetYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(store.StoreSetResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// DeleteYield wraps StoreDeleteCmd for Lua.
type DeleteYield struct {
	*store.StoreDeleteCmd
}

var deleteYieldPool = sync.Pool{New: func() any { return &DeleteYield{} }}

func AcquireDeleteYield() *DeleteYield {
	y := deleteYieldPool.Get().(*DeleteYield)
	y.StoreDeleteCmd = store.AcquireStoreDeleteCmd()
	return y
}

func ReleaseDeleteYield(y *DeleteYield) {
	if y.StoreDeleteCmd != nil {
		y.StoreDeleteCmd.Release()
		y.StoreDeleteCmd = nil
	}
	deleteYieldPool.Put(y)
}

func (y *DeleteYield) String() string                { return "<store_delete_yield>" }
func (y *DeleteYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *DeleteYield) CmdID() dispatcher.CommandID   { return store.CmdStoreDelete }
func (y *DeleteYield) ToCommand() dispatcher.Command { return y.StoreDeleteCmd }
func (y *DeleteYield) Release()                      { ReleaseDeleteYield(y) }

func (y *DeleteYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(store.StoreDeleteResponse)
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

// HasYield wraps StoreHasCmd for Lua.
type HasYield struct {
	*store.StoreHasCmd
}

var hasYieldPool = sync.Pool{New: func() any { return &HasYield{} }}

func AcquireHasYield() *HasYield {
	y := hasYieldPool.Get().(*HasYield)
	y.StoreHasCmd = store.AcquireStoreHasCmd()
	return y
}

func ReleaseHasYield(y *HasYield) {
	if y.StoreHasCmd != nil {
		y.StoreHasCmd.Release()
		y.StoreHasCmd = nil
	}
	hasYieldPool.Put(y)
}

func (y *HasYield) String() string                { return "<store_has_yield>" }
func (y *HasYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *HasYield) CmdID() dispatcher.CommandID   { return store.CmdStoreHas }
func (y *HasYield) ToCommand() dispatcher.Command { return y.StoreHasCmd }
func (y *HasYield) Release()                      { ReleaseHasYield(y) }

func (y *HasYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LFalse, lua.LString(err.Error())}
	}
	resp, ok := data.(store.StoreHasResponse)
	if !ok {
		return []lua.LValue{lua.LFalse, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LFalse, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{lua.LBool(resp.Exists), lua.LNil}
}
