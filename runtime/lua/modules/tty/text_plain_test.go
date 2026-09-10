// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestTextPlain(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"empty", "", ""},
		{"unicode", "\x1b[31m界é🙂\x1b[0m", "界é🙂"},
		{"lines", "first\n\tsecond\r\nthird", "first\n\tsecond\nthird"},
		{"hyperlink", "\x1b]8;;https://example.com\x1b\\label\x1b]8;;\x1b\\", "label"},
		{"clipboard", "before\x1b]52;c;c2VjcmV0\aafter", "beforeafter"},
		{"dcs", "before\x1bPpayload\x1b\\after", "beforeafter"},
		{"controls", "a\x00\a\b\x7fb", "ab"},
		{"unterminated_osc", "before\x1b]52;c;hidden", "before"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			bindTTY(l)
			l.SetGlobal("input", lua.LString(tc.input))
			l.SetGlobal("expected", lua.LString(tc.want))
			require.NoError(t, l.DoString(`assert(tty.text.plain(input) == expected)`))
		})
	}
}

func TestTextPlainRejectsNonString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)
	require.NoError(t, l.DoString(`assert(type(tty.text.plain) == "function"); assert(not pcall(tty.text.plain, {}))`))
}

func TestTextPlainAfterCellCut(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)
	l.SetGlobal("styled", lua.LString("\x1b[31mA界éZ\x1b[0m"))
	require.NoError(t, l.DoString(`assert(tty.text.plain(tty.text.cut(styled, 1, 4)) == "界é")`))
}
