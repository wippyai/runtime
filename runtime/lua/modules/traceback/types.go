// SPDX-License-Identifier: MPL-2.0

package traceback

import (
	lua "github.com/wippyai/go-lua"
)

const (
	defaultDepth         = 20
	defaultMaxValueLen   = 200
	defaultMaxTableDepth = 3
)

type options struct {
	Depth         int
	Locals        bool
	Upvalues      bool
	MaxValueLen   int
	MaxTableDepth int
}

func defaultOptions() options {
	return options{
		Depth:         defaultDepth,
		Locals:        true,
		Upvalues:      true,
		MaxValueLen:   defaultMaxValueLen,
		MaxTableDepth: defaultMaxTableDepth,
	}
}

func parseOptions(l *lua.LState, idx int) options {
	opts := defaultOptions()

	if l.GetTop() < idx {
		return opts
	}

	v := l.Get(idx)
	if v == lua.LNil || v.Type() != lua.LTTable {
		return opts
	}

	tbl := v.(*lua.LTable)
	if d := tbl.RawGetString("depth"); isNumber(d) {
		opts.Depth = int(lua.LVAsNumber(d))
	}

	if b := tbl.RawGetString("locals"); b.Type() == lua.LTBool {
		opts.Locals = bool(b.(lua.LBool))
	}

	if b := tbl.RawGetString("upvalues"); b.Type() == lua.LTBool {
		opts.Upvalues = bool(b.(lua.LBool))
	}

	if m := tbl.RawGetString("max_value_len"); isNumber(m) {
		opts.MaxValueLen = int(lua.LVAsNumber(m))
	}

	if m := tbl.RawGetString("max_table_depth"); isNumber(m) {
		opts.MaxTableDepth = int(lua.LVAsNumber(m))
	}

	return opts
}
