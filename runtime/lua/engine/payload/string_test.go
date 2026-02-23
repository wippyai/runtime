// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
)

func TestStringLuaTranscoders(t *testing.T) {
	mockTranscoder := NewMockTranscoder()
	RegisterString(mockTranscoder)

	l := lua.NewState()
	defer l.Close()

	t.Run("String to Lua conversion", func(t *testing.T) {
		// Test cases
		tests := []struct {
			input    payload.Payload
			expected lua.LValue
			name     string
			wantErr  bool
		}{
			{
				name:     "simple string",
				input:    payload.NewPayload("hello world", payload.String),
				expected: lua.LString("hello world"),
				wantErr:  false,
			},
			{
				name:     "empty string",
				input:    payload.NewPayload("", payload.String),
				expected: lua.LString(""),
				wantErr:  false,
			},
			{
				name:     "string with special chars",
				input:    payload.NewPayload("hello\nworld", payload.String),
				expected: lua.LString("hello\nworld"),
				wantErr:  false,
			},
			{
				name:     "byte slice",
				input:    payload.NewPayload([]byte("hello world"), payload.String),
				expected: lua.LString("hello world"),
				wantErr:  false,
			},
			{
				name:    "wrong format",
				input:   payload.NewPayload("test", payload.JSON),
				wantErr: true,
			},
			{
				name:    "unsupported type",
				input:   payload.NewPayload(123, payload.String),
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				transcoder := &StringToLua{}
				result, err := transcoder.Transcode(tt.input)

				if tt.wantErr {
					assert.Error(t, err)
					return
				}

				assert.NoError(t, err)
				assert.Equal(t, payload.Lua, result.Format())

				lv, ok := result.Data().(lua.LValue)
				assert.True(t, ok)
				assert.Equal(t, tt.expected.String(), lv.String())
			})
		}
	})

	t.Run("Lua to String conversion", func(t *testing.T) {
		// Test cases
		tests := []struct {
			name     string
			input    payload.Payload
			expected string
			wantErr  bool
		}{
			{
				name:     "string value",
				input:    payload.NewPayload(lua.LString("hello world"), payload.Lua),
				expected: "hello world",
				wantErr:  false,
			},
			{
				name:     "number value",
				input:    payload.NewPayload(lua.LNumber(42.5), payload.Lua),
				expected: "42.5",
				wantErr:  false,
			},
			{
				name:     "boolean value - true",
				input:    payload.NewPayload(lua.LBool(true), payload.Lua),
				expected: "true",
				wantErr:  false,
			},
			{
				name:     "boolean value - false",
				input:    payload.NewPayload(lua.LBool(false), payload.Lua),
				expected: "false",
				wantErr:  false,
			},
			{
				name:     "nil value",
				input:    payload.NewPayload(lua.LNil, payload.Lua),
				expected: "",
				wantErr:  false,
			},
			{
				name: "table value",
				input: func() payload.Payload {
					tbl := l.NewTable()
					l.SetTable(tbl, lua.LString("key"), lua.LString("value"))
					return payload.NewPayload(tbl, payload.Lua)
				}(),
				expected: "map[key:value]",
				wantErr:  false,
			},
			{
				name:    "wrong format",
				input:   payload.NewPayload("test", payload.String),
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				transcoder := &ToString{}
				result, err := transcoder.Transcode(tt.input)

				if tt.wantErr {
					assert.Error(t, err)
					return
				}

				assert.NoError(t, err)
				assert.Equal(t, payload.String, result.Format())

				str, ok := result.Data().(string)
				assert.True(t, ok)
				assert.Equal(t, tt.expected, str)
			})
		}
	})

	t.Run("Integration test with transcoder", func(t *testing.T) {
		// String to Lua
		stringPayload := payload.NewPayload("hello world", payload.String)
		luaPayload, err := mockTranscoder.Transcode(stringPayload, payload.Lua)
		assert.NoError(t, err)
		assert.Equal(t, payload.Lua, luaPayload.Format())

		lv, ok := luaPayload.Data().(lua.LValue)
		assert.True(t, ok)
		assert.Equal(t, "hello world", lv.String())

		// Lua to String
		stringPayload, err = mockTranscoder.Transcode(luaPayload, payload.String)
		assert.NoError(t, err)
		assert.Equal(t, payload.String, stringPayload.Format())

		str, ok := stringPayload.Data().(string)
		assert.True(t, ok)
		assert.Equal(t, "hello world", str)
	})
}
