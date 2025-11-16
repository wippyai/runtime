package process

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newProcessError creates a new process error with metadata.
func newProcessError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newProcessOperationError creates an error for process operations.
func newProcessOperationError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newProcessError(l, apierr.KindInternal, &retryable, err.Error(), details)
}

// newProcessInvalidError creates an error for invalid arguments.
func newProcessInvalidError(l *lua.LState, msg string) lua.LValue {
	details := attrs.NewBag()
	retryable := false
	return newProcessError(l, apierr.KindInvalid, &retryable, msg, details)
}
