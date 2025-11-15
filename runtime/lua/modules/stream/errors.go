package stream

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newStreamError creates a new stream error with metadata.
func newStreamError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newStreamInvalidError creates an error for invalid stream state or arguments.
func newStreamInvalidError(l *lua.LState, msg string) lua.LValue {
	details := attrs.NewBag()
	retryable := false
	return newStreamError(l, apierr.KindInvalid, &retryable, msg, details)
}

// newStreamIOError creates an error for I/O failures.
func newStreamIOError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newStreamError(l, apierr.KindInternal, &retryable, err.Error(), details)
}

// newStreamScannerError creates an error for scanner failures.
func newStreamScannerError(l *lua.LState, err error) lua.LValue {
	details := attrs.NewBag()
	retryable := false
	return newStreamError(l, apierr.KindInternal, &retryable, err.Error(), details)
}
