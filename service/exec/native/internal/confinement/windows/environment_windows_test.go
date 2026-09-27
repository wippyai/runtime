// SPDX-License-Identifier: MPL-2.0

//go:build windows

package windows

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

func TestBuildEnvironmentBlockAddsTrustedBootstrap(t *testing.T) {
	block, err := buildEnvironmentBlock([]string{"WIPPY_PINNED=yes"}, []requiredEnvironmentVariable{
		{name: "SYSTEMROOT", value: `C:\Windows`},
		{name: "LOCALAPPDATA", value: `C:\Users\runner\AppData\Local`},
	})
	require.NoError(t, err)
	require.Equal(t, "LOCALAPPDATA=C:\\Users\\runner\\AppData\\Local\x00"+
		"SYSTEMROOT=C:\\Windows\x00WIPPY_PINNED=yes\x00\x00", string(utf16.Decode(block)))
}

func TestBuildEnvironmentBlockRejectsInvalidInput(t *testing.T) {
	required := []requiredEnvironmentVariable{{name: "SYSTEMROOT", value: `C:\Windows`}}
	for _, test := range []struct {
		name   string
		values []string
		need   []requiredEnvironmentVariable
	}{
		{name: "missing bootstrap", need: []requiredEnvironmentVariable{{name: "LOCALAPPDATA"}}},
		{name: "conflicting bootstrap", values: []string{`systemroot=D:\Windows`}, need: required},
		{name: "matching bootstrap is platform owned", values: []string{`SystemRoot=C:\Windows`}, need: required},
		{name: "AppContainer temp is platform owned", values: []string{`temp=C:\Temp`}, need: required},
		{name: "case insensitive duplicate", values: []string{"TOKEN=one", "token=two"}, need: required},
		{name: "missing name", values: []string{"=value"}, need: required},
		{name: "embedded NUL", values: []string{"TOKEN=before\x00after"}, need: required},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := buildEnvironmentBlock(test.values, test.need)
			require.Error(t, err)
			require.False(t, strings.Contains(err.Error(), "before\x00after"))
		})
	}
}
