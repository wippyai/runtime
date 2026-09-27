// SPDX-License-Identifier: MPL-2.0

package darwin

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileProfileKeepsRightsSeparate(t *testing.T) {
	profile, err := CompileProfile(Profile{Grants: []Grant{
		{Path: "/read", Read: true},
		{Path: "/write", Write: true},
		{Path: "/exec", Exec: true},
	}, NetworkUnrestricted: true, AllowFork: true})
	require.NoError(t, err)
	require.Contains(t, profile, `(allow file-read* (subpath "/read"))`)
	require.NotContains(t, profile, `(allow file-write-data file-write-create file-write-unlink (subpath "/read"))`)
	require.Contains(t, profile, `(allow file-write-data file-write-create file-write-unlink (subpath "/write"))`)
	require.Contains(t, profile, `(allow file-map-executable process-exec (subpath "/exec"))`)
	require.Contains(t, profile, `(allow network*)`)
	require.Contains(t, profile, `(allow process-fork)`)
}

func TestCompileProfileEscapesPaths(t *testing.T) {
	profile, err := CompileProfile(Profile{Grants: []Grant{{Path: `/tmp/a"b`, Read: true}}})
	require.NoError(t, err)
	require.True(t, strings.Contains(profile, `/tmp/a\"b`))
	_, err = CompileProfile(Profile{Grants: []Grant{{Path: "/tmp/a\nb", Read: true}}})
	require.Error(t, err)
}
