package builder

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newBuilderError creates a new SQL builder error with metadata.
func newBuilderError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newBuilderInvalidError creates an error for invalid builder operations.
func newBuilderInvalidError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newBuilderError(l, apierr.KindInvalid, &retryable, err.Error(), details)
}

// newBuilderConversionError creates an error for conversion failures.
func newBuilderConversionError(l *lua.LState, msg string) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", "conversion")
	retryable := false
	return newBuilderError(l, apierr.KindInternal, &retryable, msg, details)
}

// newBuilderOperationError creates an error for general operation failures.
func newBuilderOperationError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newBuilderError(l, apierr.KindInternal, &retryable, err.Error(), details)
}
