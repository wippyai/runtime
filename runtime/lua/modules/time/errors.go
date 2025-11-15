package time

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newTimeError creates a new time error with metadata.
func newTimeError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newTimeOperationError creates an error for time operation failures.
func newTimeOperationError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newTimeError(l, apierr.KindInvalid, &retryable, err.Error(), details)
}
