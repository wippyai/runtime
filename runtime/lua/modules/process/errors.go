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

// newProcessNotFoundError creates an error for process not found.
func newProcessNotFoundError(l *lua.LState, pid string) lua.LValue {
	details := attrs.NewBag()
	details.Set("pid", pid)
	retryable := false
	return newProcessError(l, apierr.KindNotFound, &retryable, fmt.Sprintf("process not found: %s", pid), details)
}

// newProcessInvalidError creates an error for invalid arguments.
func newProcessInvalidError(l *lua.LState, msg string) lua.LValue {
	details := attrs.NewBag()
	retryable := false
	return newProcessError(l, apierr.KindInvalid, &retryable, msg, details)
}

// newProcessPermissionError creates an error for permission denied.
func newProcessPermissionError(l *lua.LState, operation, target string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}
	if target != "" {
		details.Set("target", target)
	}
	retryable := false
	return newProcessError(l, apierr.KindPermissionDenied, &retryable, fmt.Sprintf("not allowed to %s: %s", operation, target), details)
}
