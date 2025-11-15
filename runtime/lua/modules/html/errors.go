package html

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newHTMLError creates a new HTML error with metadata.
func newHTMLError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newHTMLRegexError creates an error for regex compilation failures.
func newHTMLRegexError(l *lua.LState, err error, pattern string) lua.LValue {
	details := attrs.NewBag()
	if pattern != "" {
		details.Set("pattern", pattern)
	}
	retryable := false
	return newHTMLError(l, apierr.KindInvalid, &retryable, err.Error(), details)
}
