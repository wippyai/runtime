package activity

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newTemporalActivityError creates a structured error with kind, retryable flag, and details
func newTemporalActivityError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newActivityContextError creates an error for when activity context is not available
func newActivityContextError(l *lua.LState, operation string) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", operation)
	retryable := false
	return newTemporalActivityError(l, apierr.KindInvalid, &retryable,
		"activity context not available", details)
}
