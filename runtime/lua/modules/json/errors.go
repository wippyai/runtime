package json

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newJSONError creates a new JSON error with metadata.
func newJSONError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newJSONInvalidError creates an error for invalid input.
func newJSONInvalidError(l *lua.LState, msg string, operation string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newJSONError(l, apierr.KindInvalid, &retryable, msg, details)
}

// newJSONEncodeError creates an error for JSON encoding failures.
func newJSONEncodeError(l *lua.LState, err error) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", "encode")
	retryable := false
	return newJSONError(l, apierr.KindInternal, &retryable, err.Error(), details)
}

// newJSONDecodeError creates an error for JSON decoding failures.
func newJSONDecodeError(l *lua.LState, err error) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", "decode")
	retryable := false
	return newJSONError(l, apierr.KindInvalid, &retryable, err.Error(), details)
}
