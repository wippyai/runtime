package fs

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newFSError creates a new filesystem error with metadata.
func newFSError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newFSNotFoundError creates an error for file/directory not found.
func newFSNotFoundError(l *lua.LState, path string) lua.LValue {
	details := attrs.NewBag()
	details.Set("path", path)
	retryable := false
	return newFSError(l, apierr.KindNotFound, &retryable, fmt.Sprintf("no such file or directory: %s", path), details)
}

// newFSPermissionError creates an error for permission denied.
func newFSPermissionError(l *lua.LState, path, operation string) lua.LValue {
	details := attrs.NewBag()
	details.Set("path", path)
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newFSError(l, apierr.KindPermissionDenied, &retryable, fmt.Sprintf("permission denied: %s", path), details)
}

// newFSIOError creates an error for I/O failures.
func newFSIOError(l *lua.LState, err error, path, operation string) lua.LValue {
	details := attrs.NewBag()
	if path != "" {
		details.Set("path", path)
	}
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newFSError(l, apierr.KindInternal, &retryable, err.Error(), details)
}
