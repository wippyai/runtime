package compress

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newCompressError creates a new compression error with metadata.
func newCompressError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newCompressInvalidError creates an error for invalid input.
func newCompressInvalidError(l *lua.LState, msg string, algorithm string) lua.LValue {
	details := attrs.NewBag()
	if algorithm != "" {
		details.Set("algorithm", algorithm)
	}
	retryable := false
	return newCompressError(l, apierr.KindInvalid, &retryable, msg, details)
}

// newCompressOperationError creates an error for compression/decompression operation failures.
func newCompressOperationError(l *lua.LState, err error, algorithm string, operation string) lua.LValue {
	details := attrs.NewBag()
	if algorithm != "" {
		details.Set("algorithm", algorithm)
	}
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newCompressError(l, apierr.KindInternal, &retryable, err.Error(), details)
}
