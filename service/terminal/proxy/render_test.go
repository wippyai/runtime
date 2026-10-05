// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/tty/vt"
)

func presentOutput(t *testing.T, width, height int, output string) []string {
	t.Helper()
	_, surface := oscTitleChild(t, width, height, output)
	surface.mu.Lock()
	defer surface.mu.Unlock()
	return append([]string(nil), surface.rows...)
}

func TestRowsTrimTrailingBlanksAndStyles(t *testing.T) {
	rows := presentOutput(t, 10, 3, "plain\r\n\x1b[31mred\x1b[0m  x\r\n\x1b[44m  \x1b[0m")
	require.Equal(t, "plain", rows[0])
	require.Equal(t, "\x1b[31mred\x1b[m  x", rows[1])
	require.Equal(t, "\x1b[44m  \x1b[m", rows[2])
}

func TestRowsEncodeColorsAndFlags(t *testing.T) {
	const output = "\x1b[1;4;92;48;5;200ma\x1b[0;38;2;1;2;3mb"
	rows := presentOutput(t, 20, 1, output)
	require.True(t, strings.HasSuffix(rows[0], "\x1b[m"))

	want := vt.New(vt.Options{Cols: 20, Rows: 1})
	_, err := want.Write([]byte(output))
	require.NoError(t, err)
	got := vt.New(vt.Options{Cols: 20, Rows: 1})
	_, err = got.Write([]byte(rows[0]))
	require.NoError(t, err)
	require.Equal(t, want.Screen().Line(0), got.Screen().Line(0))
}

func TestRowsKeepWideRunes(t *testing.T) {
	require.Equal(t, "a世b", presentOutput(t, 6, 1, "a世b")[0])
}
