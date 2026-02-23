// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
)

func TestBytesLuaTranscoders(t *testing.T) {
	mockTranscoder := NewMockTranscoder()
	RegisterBytes(mockTranscoder)

	l := lua.NewState()
	defer l.Close()

	t.Run("Bytes to Lua conversion", func(t *testing.T) {
		// Test cases
		tests := []struct {
			input    payload.Payload
			expected lua.LValue
			name     string
			wantErr  bool
		}{
			{
				name:     "simple bytes",
				input:    payload.NewPayload([]byte("hello world"), payload.Bytes),
				expected: lua.LString("hello world"),
				wantErr:  false,
			},
			{
				name:     "empty bytes",
				input:    payload.NewPayload([]byte{}, payload.Bytes),
				expected: lua.LString(""),
				wantErr:  false,
			},
			{
				name:     "bytes with special chars",
				input:    payload.NewPayload([]byte("hello\nworld"), payload.Bytes),
				expected: lua.LString("hello\nworld"),
				wantErr:  false,
			},
			{
				name:     "string interpreted as bytes",
				input:    payload.NewPayload("hello world", payload.Bytes),
				expected: lua.LString("hello world"),
				wantErr:  false,
			},
			{
				name:    "wrong format",
				input:   payload.NewPayload([]byte("test"), payload.JSON),
				wantErr: true,
			},
			{
				name:    "unsupported type",
				input:   payload.NewPayload(123, payload.Bytes),
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				transcoder := &BytesToLua{}
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

	t.Run("Lua to Bytes conversion", func(t *testing.T) {
		// Test cases
		tests := []struct {
			name     string
			input    payload.Payload
			expected []byte
			wantErr  bool
		}{
			{
				name:     "string value",
				input:    payload.NewPayload(lua.LString("hello world"), payload.Lua),
				expected: []byte("hello world"),
				wantErr:  false,
			},
			{
				name:     "number value",
				input:    payload.NewPayload(lua.LNumber(42.5), payload.Lua),
				expected: []byte("42.5"),
				wantErr:  false,
			},
			{
				name:     "boolean value - true",
				input:    payload.NewPayload(lua.LBool(true), payload.Lua),
				expected: []byte("true"),
				wantErr:  false,
			},
			{
				name:     "boolean value - false",
				input:    payload.NewPayload(lua.LBool(false), payload.Lua),
				expected: []byte("false"),
				wantErr:  false,
			},
			{
				name:     "nil value",
				input:    payload.NewPayload(lua.LNil, payload.Lua),
				expected: []byte{},
				wantErr:  false,
			},
			{
				name: "table value",
				input: func() payload.Payload {
					tbl := l.NewTable()
					l.SetTable(tbl, lua.LString("key"), lua.LString("value"))
					return payload.NewPayload(tbl, payload.Lua)
				}(),
				expected: []byte("map[key:value]"),
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
				transcoder := &ToBytes{}
				result, err := transcoder.Transcode(tt.input)

				if tt.wantErr {
					assert.Error(t, err)
					return
				}

				assert.NoError(t, err)
				assert.Equal(t, payload.Bytes, result.Format())

				bytesResult, ok := result.Data().([]byte)
				assert.True(t, ok)
				assert.True(t, bytes.Equal(tt.expected, bytesResult))
			})
		}
	})

	t.Run("Integration test with transcoder", func(t *testing.T) {
		// Bytes to Lua
		bytesPayload := payload.NewPayload([]byte("hello world"), payload.Bytes)
		luaPayload, err := mockTranscoder.Transcode(bytesPayload, payload.Lua)
		assert.NoError(t, err)
		assert.Equal(t, payload.Lua, luaPayload.Format())

		lv, ok := luaPayload.Data().(lua.LValue)
		assert.True(t, ok)
		assert.Equal(t, "hello world", lv.String())

		// Lua to Bytes
		bytesPayload, err = mockTranscoder.Transcode(luaPayload, payload.Bytes)
		assert.NoError(t, err)
		assert.Equal(t, payload.Bytes, bytesPayload.Format())

		bytesResult, ok := bytesPayload.Data().([]byte)
		assert.True(t, ok)
		assert.True(t, bytes.Equal([]byte("hello world"), bytesResult))
	})
}

func TestRegisterAllBasicFormats(t *testing.T) {
	mockTranscoder := NewMockTranscoder()
	RegisterAllBasicFormats(mockTranscoder)

	// Verify that all transcoders were registered
	formats := []payload.Format{
		payload.String,
		payload.Bytes,
		payload.JSON,
		payload.Golang,
	}

	for _, format := range formats {
		_, err := mockTranscoder.Transcode(payload.NewPayload("test", format), payload.Lua)
		// We don't really care about the success here, just that the transcoder was registered
		if err != nil {
			// Check that the error is not because the transcoder wasn't found
			assert.NotContains(t, err.Error(), "no transcoder found")
		}
	}
}
