// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	workflowapi "github.com/wippyai/runtime/api/runtime/workflow"
)

// ExecYield is yielded when executing a child workflow.
type ExecYield struct {
	workflowapi.ExecCmd
}

// String implements lua.LValue.
func (y *ExecYield) String() string { return "<exec_yield>" }

// Type implements lua.LValue.
func (y *ExecYield) Type() lua.LValueType { return lua.LTUserData }

// ToCommand returns the embedded command for the dispatcher.
func (y *ExecYield) ToCommand() dispatcher.Command { return &y.ExecCmd }

// CmdID returns the command ID.
func (y *ExecYield) CmdID() dispatcher.CommandID { return workflowapi.Exec }

// HandleResult handles the result of a child workflow exec.
func (y *ExecYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		// WrapErrorWithLua extracts kind/retryable from error chain
		luaErr := lua.WrapErrorWithLua(l, err, "workflow exec failed")
		// Only set defaults if not already extracted from chain
		if luaErr.Kind() == lua.Unknown {
			luaErr = luaErr.WithKind(lua.Internal)
		}
		if luaErr.Retryable() == lua.TernaryUnknown {
			luaErr = luaErr.WithRetryable(true)
		}
		return []lua.LValue{lua.LNil, luaErr}
	}

	result, ok := data.(workflowapi.ExecResult)
	if !ok {
		luaErr := lua.NewLuaError(l, "unexpected result type").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	if result.Error != nil {
		luaErr := lua.WrapErrorWithLua(l, result.Error, "workflow exec failed")
		if luaErr.Kind() == lua.Unknown {
			luaErr = luaErr.WithKind(lua.Internal)
		}
		if luaErr.Retryable() == lua.TernaryUnknown {
			luaErr = luaErr.WithRetryable(true)
		}
		return []lua.LValue{lua.LNil, luaErr}
	}

	if result.Value != nil {
		if result.Value.Format() == payload.Lua {
			if lv, ok := result.Value.Data().(lua.LValue); ok {
				return []lua.LValue{lv, lua.LNil}
			}
		}

		ctx := l.Context()
		if ctx == nil {
			luaErr := lua.NewLuaError(l, "no context available").
				WithKind(lua.Internal).
				WithRetryable(false)
			return []lua.LValue{lua.LNil, luaErr}
		}

		dtt := payload.GetTranscoder(ctx)
		if dtt == nil {
			luaErr := lua.NewLuaError(l, "transcoder not found").
				WithKind(lua.Internal).
				WithRetryable(false)
			return []lua.LValue{lua.LNil, luaErr}
		}

		luaPayload, err := dtt.Transcode(result.Value, payload.Lua)
		if err != nil {
			luaErr := lua.WrapErrorWithLua(l, err, "failed to transcode result").
				WithKind(lua.Internal).
				WithRetryable(false)
			return []lua.LValue{lua.LNil, luaErr}
		}
		if lv, ok := luaPayload.Data().(lua.LValue); ok {
			return []lua.LValue{lv, lua.LNil}
		}

		luaErr := lua.NewLuaError(l, "transcoded data is not a valid Lua value").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LNil, lua.LNil}
}

// VersionYield is yielded when requesting a version number.
type VersionYield struct {
	workflowapi.VersionCmd
}

// String implements lua.LValue.
func (y *VersionYield) String() string { return "<version_yield>" }

// Type implements lua.LValue.
func (y *VersionYield) Type() lua.LValueType { return lua.LTUserData }

// ToCommand returns the embedded command for the dispatcher.
func (y *VersionYield) ToCommand() dispatcher.Command { return &y.VersionCmd }

// CmdID returns the command ID.
func (y *VersionYield) CmdID() dispatcher.CommandID { return workflowapi.Version }

// HandleResult handles the result of a version request.
func (y *VersionYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		// Fallback to max version on error
		return []lua.LValue{lua.LNumber(y.MaxSupported), lua.LNil}
	}

	result, ok := data.(workflowapi.VersionResult)
	if !ok {
		// Fallback to max version if result is unexpected
		return []lua.LValue{lua.LNumber(y.MaxSupported), lua.LNil}
	}

	return []lua.LValue{lua.LNumber(result.Version), lua.LNil}
}

// UpsertAttrsYield is yielded when upserting search attributes or memo.
type UpsertAttrsYield struct {
	workflowapi.UpsertAttrsCmd
}

// String implements lua.LValue.
func (y *UpsertAttrsYield) String() string { return "<upsert_attrs_yield>" }

// Type implements lua.LValue.
func (y *UpsertAttrsYield) Type() lua.LValueType { return lua.LTUserData }

// ToCommand returns the embedded command for the dispatcher.
func (y *UpsertAttrsYield) ToCommand() dispatcher.Command { return &y.UpsertAttrsCmd }

// CmdID returns the command ID.
func (y *UpsertAttrsYield) CmdID() dispatcher.CommandID { return workflowapi.UpsertAttrs }

// HandleResult handles the result of an upsert attrs operation.
func (y *UpsertAttrsYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "upsert attrs failed")
		if luaErr.Kind() == lua.Unknown {
			luaErr = luaErr.WithKind(lua.Internal)
		}
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}
