package json

import (
	lua "github.com/yuin/gopher-lua"
)

// newJSONError creates a new JSON error with metadata.
func newJSONError(l *lua.LState, kind lua.Kind, retryable bool, msg string, details map[string]any) lua.LValue {
	e := lua.NewError(msg).
		WithKind(kind).
		WithRetryable(retryable).
		WithDetails(details)
	return e
}

// newJSONInvalidError creates an error for invalid input.
func newJSONInvalidError(l *lua.LState, msg string, operation string) lua.LValue {
	details := make(map[string]any)
	if operation != "" {
		details["operation"] = operation
	}
	return newJSONError(l, lua.KindInvalid, false, msg, details)
}

// newJSONDecodeError creates an error for JSON decoding failures.
func newJSONDecodeError(l *lua.LState, err error) lua.LValue {
	details := map[string]any{"operation": "decode"}
	return newJSONError(l, lua.KindInvalid, false, err.Error(), details)
}
