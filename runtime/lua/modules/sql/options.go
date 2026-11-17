// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"fmt"
	"time"

	lua "github.com/wippyai/go-lua"
)

const optionTimeout = "timeout"

// statementTimeout reads the optional statement options table at idx. Its one
// option, timeout, is a positive duration string such as "5s" that bounds the
// statement; zero means the statement runs under the call context alone.
func statementTimeout(l *lua.LState, idx int) (time.Duration, *lua.Error) {
	raw := l.Get(idx)
	if raw == lua.LNil {
		return 0, nil
	}
	options, ok := raw.(*lua.LTable)
	if !ok {
		return 0, invalidOption(l, fmt.Sprintf("statement options must be a table, got %s", raw.Type()))
	}
	unknown := lua.LNil
	options.ForEach(func(key, _ lua.LValue) {
		if unknown == lua.LNil && key != lua.LString(optionTimeout) {
			unknown = key
		}
	})
	if unknown != lua.LNil {
		return 0, invalidOption(l, fmt.Sprintf("unknown statement option %s", unknown.String()))
	}
	value := options.RawGetString(optionTimeout)
	if value == lua.LNil {
		return 0, nil
	}
	text, ok := value.(lua.LString)
	if !ok {
		return 0, invalidOption(l, fmt.Sprintf("statement timeout must be a duration string such as \"5s\", got %s", value.Type()))
	}
	timeout, err := time.ParseDuration(string(text))
	if err != nil || timeout <= 0 {
		return 0, invalidOption(l, fmt.Sprintf("statement timeout must be a positive duration such as \"5s\", got %q", string(text)))
	}
	return timeout, nil
}

func invalidOption(l *lua.LState, message string) *lua.Error {
	return lua.NewLuaError(l, message).WithKind(lua.Invalid).WithRetryable(false)
}
