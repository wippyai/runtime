package security

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	apierr "github.com/wippyai/runtime/api/error"
	engerr "github.com/wippyai/runtime/runtime/lua/engine/errors"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

func newSecurityError(l *lua.LState, kind apierr.Kind, retryable *bool, msg string, details attrs.Bag) lua.LValue {
	wrapped := engerr.WrapError(l, fmt.Errorf("%s", msg), "")
	wrapped.SetKind(kind)
	wrapped.SetRetryable(retryable)
	wrapped.SetDetails(details)
	ud := l.NewUserData()
	ud.Value = wrapped
	ud.Metatable = value.GetTypeMetatable(nil, "error")
	return ud
}

func newSecurityPermissionError(l *lua.LState, resource, action string) lua.LValue {
	details := attrs.NewBag()
	details.Set("resource", resource)
	details.Set("action", action)
	retryable := false
	msg := fmt.Sprintf("not allowed to %s: %s", action, resource)
	return newSecurityError(l, apierr.KindPermissionDenied, &retryable, msg, details)
}

func newSecurityOperationError(l *lua.LState, err error, operation string) lua.LValue {
	details := attrs.NewBag()
	if operation != "" {
		details.Set("operation", operation)
	}
	retryable := false
	return newSecurityError(l, apierr.KindInternal, &retryable, err.Error(), details)
}

func newSecurityValidationError(l *lua.LState, field, constraint string) lua.LValue {
	details := attrs.NewBag()
	details.Set("field", field)
	details.Set("constraint", constraint)
	retryable := false
	msg := fmt.Sprintf("%s %s", field, constraint)
	return newSecurityError(l, apierr.KindInvalid, &retryable, msg, details)
}

func newSecurityResourceError(l *lua.LState, resource, msg string) lua.LValue {
	details := attrs.NewBag()
	details.Set("resource", resource)
	retryable := false
	return newSecurityError(l, apierr.KindNotFound, &retryable, msg, details)
}
