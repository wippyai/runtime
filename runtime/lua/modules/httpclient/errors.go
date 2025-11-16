package httpclient

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// newHTTPError creates a new HTTP client error with metadata.
func newHTTPError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)

	// Wrap as userdata with error metatable
	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

// newHTTPNetworkError creates an error for network/IO failures.
func newHTTPNetworkError(l *lua.LState, err error, url, method string) lua.LValue {
	details := attrs.NewBag()
	details.Set("url", url)
	if method != "" {
		details.Set("method", method)
	}
	retryable := true
	return newHTTPError(l, apierr.KindUnavailable, &retryable, err.Error(), details)
}

// newHTTPIOError creates an error for I/O failures (reading/writing body).
func newHTTPIOError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	details.Set("operation", operation)
	retryable := false
	return newHTTPError(l, apierr.KindInternal, &retryable, err.Error(), details)
}
