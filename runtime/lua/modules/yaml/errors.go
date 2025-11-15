package yaml

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newYAMLError creates a new YAML error with metadata.
func newYAMLError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newYAMLInvalidError creates an error for invalid input.
func newYAMLInvalidError(l *lua.LState, msg string, operation string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newYAMLError(l, apierr.KindInvalid, &retryable, msg, details)
}

// newYAMLEncodeError creates an error for YAML encoding failures.
func newYAMLEncodeError(l *lua.LState, err error) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", "encode")
	retryable := false
	return newYAMLError(l, apierr.KindInternal, &retryable, fmt.Sprintf("error encoding to YAML: %v", err), details)
}

// newYAMLDecodeError creates an error for YAML decoding failures.
func newYAMLDecodeError(l *lua.LState, err error) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", "decode")
	retryable := false
	return newYAMLError(l, apierr.KindInvalid, &retryable, fmt.Sprintf("error decoding YAML: %v", err), details)
}

// newYAMLConversionError creates an error for Lua conversion failures.
func newYAMLConversionError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newYAMLError(l, apierr.KindInternal, &retryable, fmt.Sprintf("error converting to Lua: %v", err), details)
}
