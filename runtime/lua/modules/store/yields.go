// SPDX-License-Identifier: MPL-2.0

package store

import (
	"errors"
	"fmt"
	"strconv"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/store"
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
		return []lua.LValue{lua.LNil, wrapStoreError(l, err, "store get")}
	}
	resp, ok := data.(store.GetResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, wrapStoreError(l, resp.Error, "store get")}
	}
	return []lua.LValue{transcodeToLua(l, resp.Value), lua.LNil}
}

func wrapStoreError(l *lua.LState, err error, context string) *lua.Error {
	luaErr := lua.WrapErrorWithLua(l, err, context)
	var apiErr apierror.Error
	if errors.As(err, &apiErr) {
		luaErr = luaErr.WithKind(lua.Kind(apiErr.Kind().String()))
		switch apiErr.Retryable() {
		case apierror.True:
			luaErr = luaErr.WithRetryable(true)
		case apierror.False:
			luaErr = luaErr.WithRetryable(false)
		}
	}
	return luaErr
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

func (y *SetYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapStoreError(l, err, "store set")}
	}
	resp, ok := data.(store.SetResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, wrapStoreError(l, resp.Error, "store set")}
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

func (y *DeleteYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapStoreError(l, err, "store delete")}
	}
	resp, ok := data.(store.DeleteResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)}
	}
	if resp.Error != nil {
		if resp.NotFound {
			return []lua.LValue{lua.LFalse, lua.LNil}
		}
		return []lua.LValue{lua.LNil, wrapStoreError(l, resp.Error, "store delete")}
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

func (y *HasYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LFalse, wrapStoreError(l, err, "store has")}
	}
	resp, ok := data.(store.HasResponse)
	if !ok {
		return []lua.LValue{lua.LFalse, lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LFalse, wrapStoreError(l, resp.Error, "store has")}
	}
	return []lua.LValue{lua.LBool(resp.Exists), lua.LNil}
}

// EntryYield wraps EntryCmd for Lua.
type EntryYield struct {
	*store.EntryCmd
}

var entryYieldPool = sync.Pool{New: func() any { return &EntryYield{} }}

func AcquireEntryYield() *EntryYield {
	y := entryYieldPool.Get().(*EntryYield)
	y.EntryCmd = store.AcquireEntryCmd()
	return y
}

func ReleaseEntryYield(y *EntryYield) {
	if y.EntryCmd != nil {
		y.EntryCmd.Release()
		y.EntryCmd = nil
	}
	entryYieldPool.Put(y)
}

func (y *EntryYield) String() string                { return "<store_entry_yield>" }
func (y *EntryYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *EntryYield) CmdID() dispatcher.CommandID   { return store.EntryCommand }
func (y *EntryYield) ToCommand() dispatcher.Command { return y.EntryCmd }
func (y *EntryYield) Release()                      { ReleaseEntryYield(y) }

func (y *EntryYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapStoreError(l, err, "store entry")}
	}
	resp, ok := data.(store.EntryResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, wrapStoreError(l, resp.Error, "store entry")}
	}
	return []lua.LValue{pushEntryTable(l, resp.Entry), lua.LNil}
}

// ListYield wraps ListCmd for Lua.
type ListYield struct {
	*store.ListCmd
}

var listYieldPool = sync.Pool{New: func() any { return &ListYield{} }}

func AcquireListYield() *ListYield {
	y := listYieldPool.Get().(*ListYield)
	y.ListCmd = store.AcquireListCmd()
	return y
}

func ReleaseListYield(y *ListYield) {
	if y.ListCmd != nil {
		y.ListCmd.Release()
		y.ListCmd = nil
	}
	listYieldPool.Put(y)
}

func (y *ListYield) String() string                { return "<store_list_yield>" }
func (y *ListYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *ListYield) CmdID() dispatcher.CommandID   { return store.ListCommand }
func (y *ListYield) ToCommand() dispatcher.Command { return y.ListCmd }
func (y *ListYield) Release()                      { ReleaseListYield(y) }

func (y *ListYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapStoreError(l, err, "store list")}
	}
	resp, ok := data.(store.ListResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, wrapStoreError(l, resp.Error, "store list")}
	}
	return []lua.LValue{pushPageTable(l, resp.Page), lua.LNil}
}

// PutYield wraps PutCmd for Lua.
type PutYield struct {
	*store.PutCmd
}

var putYieldPool = sync.Pool{New: func() any { return &PutYield{} }}

func AcquirePutYield() *PutYield {
	y := putYieldPool.Get().(*PutYield)
	y.PutCmd = store.AcquirePutCmd()
	return y
}

func ReleasePutYield(y *PutYield) {
	if y.PutCmd != nil {
		y.PutCmd.Release()
		y.PutCmd = nil
	}
	putYieldPool.Put(y)
}

func (y *PutYield) String() string                { return "<store_put_yield>" }
func (y *PutYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *PutYield) CmdID() dispatcher.CommandID   { return store.PutCommand }
func (y *PutYield) ToCommand() dispatcher.Command { return y.PutCmd }
func (y *PutYield) Release()                      { ReleasePutYield(y) }

func (y *PutYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapStoreError(l, err, "store put")}
	}
	resp, ok := data.(store.PutResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, wrapStoreError(l, resp.Error, "store put")}
	}
	return []lua.LValue{pushEntryTable(l, resp.Entry), lua.LNil}
}

func pushEntryTable(l *lua.LState, entry store.VersionedEntry) lua.LValue {
	t := l.NewTable()
	t.RawSetString("key", lua.LString(entry.Key.String()))
	t.RawSetString("value", transcodeToLua(l, entry.Value))
	t.RawSetString("version", lua.LString(strconv.FormatUint(uint64(entry.Version), 10)))
	return t
}

func pushPageTable(l *lua.LState, page store.Page) lua.LValue {
	t := l.NewTable()
	items := l.NewTable()
	for _, entry := range page.Items {
		items.Append(pushEntryTable(l, entry))
	}
	t.RawSetString("items", items)
	t.RawSetString("cursor", lua.LString(page.Cursor))
	t.RawSetString("has_more", lua.LBool(page.HasMore))
	return t
}
