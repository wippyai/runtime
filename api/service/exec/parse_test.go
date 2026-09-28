// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{name: "words", command: "cmd  --flag value", want: []string{"cmd", "--flag", "value"}},
		{name: "unicode whitespace", command: "cmd\tfirst\nsecond", want: []string{"cmd", "first", "second"}},
		{name: "quoted and adjacent", command: `cmd "hello "'world'`, want: []string{"cmd", "hello world"}},
		{name: "empty argument", command: `cmd '' tail`, want: []string{"cmd", "", "tail"}},
		{name: "escaped separator", command: `cmd hello\ world`, want: []string{"cmd", "hello world"}},
		{name: "literal unknown escape", command: `cmd \q "a\q"`, want: []string{"cmd", `\q`, `a\q`}},
		{name: "trailing backslash", command: `cmd value\`, want: []string{"cmd", `value\`}},
		{name: "windows path", command: `cmd "C:\Program Files\App"`, want: []string{"cmd", `C:\Program Files\App`}},
		{name: "no expansion", command: `cmd "$HOME" '$(pwd)'`, want: []string{"cmd", "$HOME", "$(pwd)"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseCommand(test.command)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestParseCommandRejectsMissingExecutableAndMalformedQuotes(t *testing.T) {
	for _, command := range []string{"", "   ", `''`, `""`} {
		_, err := ParseCommand(command)
		require.ErrorIs(t, err, ErrCommandRequired)
	}
	for _, command := range []string{`cmd "missing`, "cmd 'missing"} {
		_, err := ParseCommand(command)
		require.ErrorIs(t, err, ErrInvalidCommand)
	}
}
