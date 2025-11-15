package system

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newSystemError creates a new system error with metadata.
func newSystemError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newSystemIOError creates an error for I/O failures (hostname lookup, etc.).
func newSystemIOError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", operation)
	retryable := false
	return newSystemError(l, apierr.KindInternal, &retryable, err.Error(), details)
}

// newSystemValidationError creates an error for invalid input.
func newSystemValidationError(l *lua.LState, field, constraint string) lua.LValue {
	details := attrs.NewBag()
	details.Set("field", field)
	details.Set("constraint", constraint)
	retryable := false
	msg := fmt.Sprintf("%s %s", field, constraint)
	return newSystemError(l, apierr.KindInvalid, &retryable, msg, details)
}
