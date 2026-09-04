// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"math"

	lua "github.com/wippyai/go-lua"
)

const maxTerminalDimension = 65535

func integerValue(value lua.LValue) (int, bool) {
	var number float64
	switch value := value.(type) {
	case lua.LInteger:
		number = float64(value)
	case lua.LNumber:
		number = float64(value)
	default:
		return 0, false
	}
	if math.Trunc(number) != number || number > float64(math.MaxInt) || number < float64(math.MinInt) {
		return 0, false
	}
	return int(number), true
}

func invalidArgument(l *lua.LState, message string) int {
	l.Push(lua.LNil)
	l.Push(lua.NewLuaError(l, message).WithKind(lua.Invalid).WithRetryable(false))
	return 2
}
