// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"testing"

	xterm "github.com/gitpod-io/xterm-go"
	"github.com/stretchr/testify/require"
)

func TestRenderLineTrimsTrailingBlanksAndStyles(t *testing.T) {
	term := xterm.New(xterm.WithCols(10), xterm.WithRows(3))
	defer term.Dispose()
	term.WriteString("plain\r\n\x1b[31mred\x1b[0m  x\r\n\x1b[44m  \x1b[0m")
	buffer := term.Buffer()

	require.Equal(t, "plain", renderLine(buffer, 0))
	require.Equal(t, "\x1b[0;31mred\x1b[0m  x", renderLine(buffer, 1))
	require.Equal(t, "\x1b[0;44m  \x1b[0m", renderLine(buffer, 2))
	require.Empty(t, renderLine(buffer, 99))
	require.Empty(t, renderLine(buffer, -1))
}

func TestRenderLineEncodesColorsAndFlags(t *testing.T) {
	term := xterm.New(xterm.WithCols(20), xterm.WithRows(1))
	defer term.Dispose()
	term.WriteString("\x1b[1;4;92;48;5;200ma\x1b[0;38;2;1;2;3mb")
	require.Equal(t, "\x1b[0;1;4;92;48;5;200ma\x1b[0;38;2;1;2;3mb\x1b[0m", renderLine(term.Buffer(), 0))
}

func TestRenderLineKeepsWideRunes(t *testing.T) {
	term := xterm.New(xterm.WithCols(6), xterm.WithRows(1))
	defer term.Dispose()
	term.WriteString("a世b")
	require.Equal(t, "a世b", renderLine(term.Buffer(), 0))
}
