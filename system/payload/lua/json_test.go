package lua

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/payload"
	lua "github.com/yuin/gopher-lua"
)

func TestJsonLuaTranscoders(t *testing.T) {
	mockTranscoder := NewMockTranscoder()
	RegisterJSON(mockTranscoder)

	l := lua.NewState()
	defer l.Close()

	t.Run("JSON to Lua conversion", func(t *testing.T) {
		// Test cases
		tests := []struct {
			name     string
			input    payload.Payload
			validate func(*testing.T, payload.Payload)
			wantErr  bool
		}{
			{
				name:  "simple object",
				input: payload.NewPayload(`{"name":"test","age":30}`, payload.JSON),
				validate: func(t *testing.T, p payload.Payload) {
					lv, ok := p.Data().(lua.LValue)
					assert.True(t, ok)
					assert.Equal(t, lua.LTTable, lv.Type())

					tbl := lv.(*lua.LTable)
					assert.Equal(t, lua.LString("test"), tbl.RawGetString("name"))
					assert.Equal(t, lua.LNumber(30), tbl.RawGetString("age"))
				},
				wantErr: false,
			},
			{
				name:  "array",
				input: payload.NewPayload(`[1,2,"three"]`, payload.JSON),
				validate: func(t *testing.T, p payload.Payload) {
					lv, ok := p.Data().(lua.LValue)
					assert.True(t, ok)
					assert.Equal(t, lua.LTTable, lv.Type())

					tbl := lv.(*lua.LTable)
					assert.Equal(t, lua.LNumber(1), tbl.RawGetInt(1))
					assert.Equal(t, lua.LNumber(2), tbl.RawGetInt(2))
					assert.Equal(t, lua.LString("three"), tbl.RawGetInt(3))
				},
				wantErr: false,
			},
			{
				name:    "invalid JSON",
				input:   payload.NewPayload(`{"invalid":}`, payload.JSON),
				wantErr: true,
			},
			{
				name:    "wrong format",
				input:   payload.NewPayload("test", payload.String),
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				transcoder := &JSONToLua{}
				result, err := transcoder.Transcode(tt.input)

				if tt.wantErr {
					assert.Error(t, err)
					return
				}

				assert.NoError(t, err)
				assert.Equal(t, payload.Lua, result.Format())
				if tt.validate != nil {
					tt.validate(t, result)
				}
			})
		}
	})

	t.Run("Lua to JSON conversion", func(t *testing.T) {
		// Test cases
		tests := []struct {
			name     string
			input    payload.Payload
			expected string
			wantErr  bool
		}{
			{
				name: "simple table",
				input: func() payload.Payload {
					tbl := l.NewTable()
					l.SetTable(tbl, lua.LString("name"), lua.LString("test"))
					l.SetTable(tbl, lua.LString("age"), lua.LNumber(30))
					return payload.NewPayload(tbl, payload.Lua)
				}(),
				expected: `{"name":"test","age":30}`,
				wantErr:  false,
			},
			{
				name: "array table",
				input: func() payload.Payload {
					tbl := l.NewTable()
					l.SetTable(tbl, lua.LNumber(1), lua.LNumber(1))
					l.SetTable(tbl, lua.LNumber(2), lua.LNumber(2))
					l.SetTable(tbl, lua.LNumber(3), lua.LString("three"))
					return payload.NewPayload(tbl, payload.Lua)
				}(),
				expected: `[1,2,"three"]`,
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
				transcoder := &ToJSON{}
				result, err := transcoder.Transcode(tt.input)

				if tt.wantErr {
					assert.Error(t, err)
					return
				}

				assert.NoError(t, err)
				assert.Equal(t, payload.JSON, result.Format())

				// Compare JSON strings (ignore whitespace differences)
				resultStr := string(result.Data().([]byte))
				assert.JSONEq(t, tt.expected, resultStr)
			})
		}
	})
}
