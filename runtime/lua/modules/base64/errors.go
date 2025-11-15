package base64

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newBase64Error creates a new base64 error with metadata.
func newBase64Error(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newBase64DecodeError creates an error for base64 decoding failures.
func newBase64DecodeError(l *lua.LState, err error) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", "decode")
	retryable := false
	return newBase64Error(l, apierr.KindInvalid, &retryable, err.Error(), details)
}
