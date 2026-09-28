// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	api "github.com/wippyai/runtime/api/tty"
)

func TestDecodeEventPreservesResizeDimensions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	table := l.CreateTable(0, 3)
	table.RawSetString("type", lua.LString("resize"))
	table.RawSetString("width", lua.LInteger(100))
	table.RawSetString("height", lua.LInteger(30))

	event, err := DecodeEvent(table)
	require.NoError(t, err)
	require.Equal(t, api.Event{Type: "resize", Width: 100, Height: 30}, event)
}

func TestDecodeEventRejectsMalformedFields(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		fields map[string]lua.LValue
		name   string
	}{
		{name: "missing type", fields: map[string]lua.LValue{}},
		{name: "unknown type", fields: map[string]lua.LValue{"type": lua.LString("other")}},
		{name: "fractional coordinate", fields: map[string]lua.LValue{
			"type": lua.LString("mouse"), "action": lua.LString("motion"), "button": lua.LString("left"),
			"x": lua.LNumber(1.5), "y": lua.LInteger(1),
		}},
		{name: "string dimension", fields: map[string]lua.LValue{
			"type": lua.LString("resize"), "width": lua.LString("80"), "height": lua.LInteger(24),
		}},
		{name: "non boolean modifier", fields: map[string]lua.LValue{
			"type": lua.LString("key"), "key": lua.LString("x"), "key_type": lua.LString("runes"),
			"action": lua.LString("press"), "ctrl": lua.LInteger(1),
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			table := l.CreateTable(0, len(test.fields))
			for key, value := range test.fields {
				table.RawSetString(key, value)
			}
			_, err := DecodeEvent(table)
			require.Error(t, err)
		})
	}
}
