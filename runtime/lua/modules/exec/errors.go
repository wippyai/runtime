// SPDX-License-Identifier: MPL-2.0

package exec

import lua "github.com/wippyai/go-lua"

func wrapExecError(l *lua.LState, err error, context string, fallback lua.Kind) *lua.Error {
	wrapped := lua.WrapErrorWithLua(l, err, context)
	if wrapped.Kind() == lua.Unknown {
		wrapped = wrapped.WithKind(fallback)
	}
	if wrapped.Retryable() == lua.TernaryUnknown {
		wrapped = wrapped.WithRetryable(false)
	}
	return wrapped
}
