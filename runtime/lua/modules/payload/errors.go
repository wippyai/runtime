package payload

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newPayloadError creates a new payload error with metadata.
func newPayloadError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newPayloadTranscodeError creates an error for transcoding failures.
func newPayloadTranscodeError(l *lua.LState, err error, fromFormat, toFormat string) lua.LValue {
	details := attrs.NewBag()
	if fromFormat != "" {
		details.Set("from_format", fromFormat)
	}
	if toFormat != "" {
		details.Set("to_format", toFormat)
	}
	retryable := false
	return newPayloadError(l, apierr.KindInternal, &retryable, err.Error(), details)
}

// newPayloadInvalidError creates an error for invalid payload data.
func newPayloadInvalidError(l *lua.LState, msg string) lua.LValue {
	details := attrs.NewBag()
	retryable := false
	return newPayloadError(l, apierr.KindInvalid, &retryable, msg, details)
}
