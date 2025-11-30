package store

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	storeapi "github.com/wippyai/runtime/api/dispatcher/store"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// GetYield wraps StoreGetCmd for Lua.
type GetYield struct {
	*storeapi.StoreGetCmd
}

var getYieldPool = sync.Pool{New: func() any { return &GetYield{} }}

func AcquireGetYield() *GetYield {
	y := getYieldPool.Get().(*GetYield)
	y.StoreGetCmd = storeapi.AcquireStoreGetCmd()
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
func (y *GetYield) CmdID() dispatcher.CommandID   { return storeapi.CmdStoreGet }
func (y *GetYield) ToCommand() dispatcher.Command { return y.StoreGetCmd }
func (y *GetYield) Release()                      { ReleaseGetYield(y) }

func (y *GetYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(storeapi.StoreGetResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{engine.PayloadToLua(l, resp.Value), lua.LNil}
}

// SetYield wraps StoreSetCmd for Lua.
type SetYield struct {
	*storeapi.StoreSetCmd
}

var setYieldPool = sync.Pool{New: func() any { return &SetYield{} }}

func AcquireSetYield() *SetYield {
	y := setYieldPool.Get().(*SetYield)
	y.StoreSetCmd = storeapi.AcquireStoreSetCmd()
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
func (y *SetYield) CmdID() dispatcher.CommandID   { return storeapi.CmdStoreSet }
func (y *SetYield) ToCommand() dispatcher.Command { return y.StoreSetCmd }
func (y *SetYield) Release()                      { ReleaseSetYield(y) }

func (y *SetYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(storeapi.StoreSetResponse)
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
	*storeapi.StoreDeleteCmd
}

var deleteYieldPool = sync.Pool{New: func() any { return &DeleteYield{} }}

func AcquireDeleteYield() *DeleteYield {
	y := deleteYieldPool.Get().(*DeleteYield)
	y.StoreDeleteCmd = storeapi.AcquireStoreDeleteCmd()
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
func (y *DeleteYield) CmdID() dispatcher.CommandID   { return storeapi.CmdStoreDelete }
func (y *DeleteYield) ToCommand() dispatcher.Command { return y.StoreDeleteCmd }
func (y *DeleteYield) Release()                      { ReleaseDeleteYield(y) }

func (y *DeleteYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	resp, ok := data.(storeapi.StoreDeleteResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// HasYield wraps StoreHasCmd for Lua.
type HasYield struct {
	*storeapi.StoreHasCmd
}

var hasYieldPool = sync.Pool{New: func() any { return &HasYield{} }}

func AcquireHasYield() *HasYield {
	y := hasYieldPool.Get().(*HasYield)
	y.StoreHasCmd = storeapi.AcquireStoreHasCmd()
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
func (y *HasYield) CmdID() dispatcher.CommandID   { return storeapi.CmdStoreHas }
func (y *HasYield) ToCommand() dispatcher.Command { return y.StoreHasCmd }
func (y *HasYield) Release()                      { ReleaseHasYield(y) }

func (y *HasYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LFalse, lua.LString(err.Error())}
	}
	resp, ok := data.(storeapi.StoreHasResponse)
	if !ok {
		return []lua.LValue{lua.LFalse, lua.LString("invalid response type")}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LFalse, lua.LString(resp.Error.Error())}
	}
	return []lua.LValue{lua.LBool(resp.Exists), lua.LNil}
}
