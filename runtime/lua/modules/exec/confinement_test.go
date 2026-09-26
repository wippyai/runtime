// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestParseConfinementNarrowing(t *testing.T) {
	l := setupState()
	defer l.Close()

	fs := l.NewTable()
	write := l.NewTable()
	write.RawSetInt(1, lua.LString("/srv/ws/demo/pkg"))
	fs.RawSetString("write", write)
	fs.RawSetString("read", l.NewTable())

	limits := l.NewTable()
	limits.RawSetString("wall_s", lua.LInteger(60))
	confine := l.NewTable()
	confine.RawSetString("fs", fs)
	confine.RawSetString("limits", limits)
	confine.RawSetString("network", lua.LString("none"))

	options := l.NewTable()
	options.RawSetString("confine", confine)
	parsed, err := parseProcessOptions(options)
	require.NoError(t, err)
	require.NotNil(t, parsed.Confine)
	require.NotNil(t, parsed.Confine.FS.Read)
	require.Empty(t, *parsed.Confine.FS.Read)
	require.Nil(t, parsed.Confine.FS.Exec)
	require.Equal(t, []string{"/srv/ws/demo/pkg"}, *parsed.Confine.FS.Write)
	require.EqualValues(t, 60, *parsed.Confine.Limits.WallSec)
	require.Equal(t, "none", *parsed.Confine.Network)
}

func TestParseConfinementRejectsEntryOnlyAndUnknownFields(t *testing.T) {
	l := setupState()
	defer l.Close()

	for _, key := range []string{"work_dir_roots", "home", "network_proxy", "future_grant"} {
		confine := l.NewTable()
		confine.RawSetString(key, lua.LString("anything"))
		options := l.NewTable()
		options.RawSetString("confine", confine)
		_, err := parseProcessOptions(options)
		require.ErrorContains(t, err, key)
	}
}

func TestParseConfinementRejectsMalformedGrantsAndLimits(t *testing.T) {
	l := setupState()
	defer l.Close()

	for _, value := range []lua.LValue{lua.LInteger(0), lua.LNumber(1.5), lua.LString("60")} {
		limits := l.NewTable()
		limits.RawSetString("wall_s", value)
		confine := l.NewTable()
		confine.RawSetString("limits", limits)
		options := l.NewTable()
		options.RawSetString("confine", confine)
		_, err := parseProcessOptions(options)
		require.ErrorContains(t, err, "confine.limits.wall_s")
	}

	list := l.NewTable()
	list.RawSetInt(2, lua.LString("/srv/ws/demo"))
	fs := l.NewTable()
	fs.RawSetString("read", list)
	confine := l.NewTable()
	confine.RawSetString("fs", fs)
	options := l.NewTable()
	options.RawSetString("confine", confine)
	_, err := parseProcessOptions(options)
	require.ErrorContains(t, err, "confine.fs.read")
}
