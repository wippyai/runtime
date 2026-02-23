// SPDX-License-Identifier: MPL-2.0

package sql

import (
	lua "github.com/wippyai/go-lua"
)

func checkParams(l *lua.LState, index int) ([]any, error) {
	params := l.Get(index)

	if params == lua.LNil {
		return nil, nil
	}

	if params.Type() != lua.LTTable {
		return nil, NewInvalidParametersTypeError(params.Type().String())
	}

	tbl := params.(*lua.LTable)
	maxn := tbl.MaxN()

	if maxn == 0 {
		return nil, nil
	}

	result := make([]any, maxn)

	for i := 1; i <= maxn; i++ {
		v := tbl.RawGetInt(i)

		if v.Type() == lua.LTUserData {
			if ud, ok := v.(*lua.LUserData); ok {
				if ud.Value == "SQL_NULL" {
					result[i-1] = nil
					continue
				}
				if typed, ok := ud.Value.(*TypedValue); ok {
					result[i-1] = typed.Value
					continue
				}
			}
		}

		result[i-1] = toGoValue(v)
	}

	return result, nil
}

func toGoValue(v lua.LValue) any {
	switch v := v.(type) {
	case lua.LBool:
		return bool(v)
	case lua.LNumber:
		return float64(v)
	case lua.LInteger:
		return int64(v)
	case lua.LString:
		return string(v)
	case *lua.LNilType:
		return nil
	default:
		return nil
	}
}
