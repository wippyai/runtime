// SPDX-License-Identifier: MPL-2.0

// Package toml exposes TOML encoding and decoding to Lua.
package toml

import (
	"bytes"
	"time"

	tomllib "github.com/pelletier/go-toml/v2"
	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const (
	// maxDecodeBytes bounds the text handed to the parser.
	maxDecodeBytes = 256 * 1024
	// maxEncodeBytes bounds the document the encoder produces.
	maxEncodeBytes = 512 * 1024
)

// Module is the toml module definition.
var Module = &luaapi.ModuleDef{
	Name:        "toml",
	Description: "TOML encoding and decoding",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := lua.CreateTable(0, 2)
		mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
		mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
		mod.Immutable = true
		return mod, nil
	},
	Types: ModuleTypes,
}

func encodeFunc(l *lua.LState) int {
	if l.GetTop() < 1 {
		return invalidError(l, "table expected")
	}

	luaVal := l.Get(1)
	if luaVal.Type() != lua.LTTable {
		return invalidError(l, "table expected")
	}

	var buf bytes.Buffer
	if err := tomllib.NewEncoder(&buf).Encode(value.ToGoAny(luaVal)); err != nil {
		return internalError(l, err, "encode failed")
	}
	if buf.Len() > maxEncodeBytes {
		return invalidError(l, "output exceeds byte limit")
	}

	l.Push(lua.LString(buf.String()))
	l.Push(lua.LNil)
	return 2
}

func decodeFunc(l *lua.LState) int {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		return invalidError(l, "string expected")
	}

	if str == "" {
		return invalidError(l, "input cannot be empty")
	}

	if len(str) > maxDecodeBytes {
		return invalidError(l, "input exceeds byte limit")
	}

	data := map[string]any{}
	if err := tomllib.Unmarshal([]byte(str), &data); err != nil {
		return internalError(l, err, "decode failed")
	}

	textualDates(data)

	lv, err := luaconv.GoToLua(data)
	if err != nil {
		return internalError(l, err, "convert to Lua failed")
	}

	l.Push(lv)
	l.Push(lua.LNil)
	return 2
}

// textualDates replaces TOML date and time values with their textual form.
// The walk is iterative because nesting depth follows the input document.
func textualDates(root map[string]any) {
	stack := []any{root}
	for len(stack) > 0 {
		last := len(stack) - 1
		container := stack[last]
		stack = stack[:last]

		switch node := container.(type) {
		case map[string]any:
			for key, item := range node {
				if text, isDate := dateText(item); isDate {
					node[key] = text
					continue
				}
				if isContainer(item) {
					stack = append(stack, item)
				}
			}
		case []any:
			for index, item := range node {
				if text, isDate := dateText(item); isDate {
					node[index] = text
					continue
				}
				if isContainer(item) {
					stack = append(stack, item)
				}
			}
		}
	}
}

// dateText renders the four TOML date and time types as RFC 3339 text.
func dateText(item any) (string, bool) {
	switch date := item.(type) {
	case time.Time:
		return date.Format(time.RFC3339Nano), true
	case tomllib.LocalDateTime:
		return date.String(), true
	case tomllib.LocalDate:
		return date.String(), true
	case tomllib.LocalTime:
		return date.String(), true
	}
	return "", false
}

func isContainer(item any) bool {
	switch item.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}
