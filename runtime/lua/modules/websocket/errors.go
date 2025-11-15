package websocket

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newWSError creates a new WebSocket error with metadata.
func newWSError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newWSNetworkError creates an error for network/connection failures.
func newWSNetworkError(l *lua.LState, err error, url string) lua.LValue {
	details := attrs.NewBag()
	details.Set("url", url)
	retryable := true
	return newWSError(l, apierr.KindUnavailable, &retryable, err.Error(), details)
}

// newWSPermissionError creates an error for security violations.
func newWSPermissionError(l *lua.LState, resource string) lua.LValue {
	details := attrs.NewBag()
	details.Set("resource", resource)
	retryable := false
	msg := fmt.Sprintf("not allowed to connect to URL: %s", resource)
	return newWSError(l, apierr.KindPermissionDenied, &retryable, msg, details)
}

// newWSOperationError creates an error for send/close failures.
func newWSOperationError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", operation)
	retryable := false
	return newWSError(l, apierr.KindInternal, &retryable, err.Error(), details)
}

// newWSValidationError creates an error for invalid client state.
func newWSValidationError(l *lua.LState, msg string) lua.LValue {
	retryable := false
	return newWSError(l, apierr.KindInvalid, &retryable, msg, attrs.NewBag())
}
