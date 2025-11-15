package hash

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newHashError creates a new hash error with metadata.
func newHashError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newHashOperationError creates an error for hash operation failures.
func newHashOperationError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", operation)
	retryable := false
	return newHashError(l, apierr.KindInternal, &retryable, err.Error(), details)
}
