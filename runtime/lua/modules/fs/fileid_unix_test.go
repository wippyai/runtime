//go:build linux || darwin || freebsd

// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func statTable(t *testing.T, l *lua.LState, path string) *lua.LTable {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return pushFileInfo(l, info)
}

func TestPushFileInfo_OSIdentity(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	require.NoError(t, os.WriteFile(a, []byte("one"), 0o644))
	require.NoError(t, os.WriteFile(b, []byte("two"), 0o644))

	l := lua.NewState()
	defer l.Close()

	first := statTable(t, l, a)
	for _, key := range []string{"dev", "ino", "ctime_ns", "mtime_ns"} {
		assert.NotEqual(t, lua.LNil, first.RawGetString(key), key)
	}
	assert.IsType(t, lua.LInteger(0), first.RawGetString("ctime_ns"))
	assert.Greater(t, int64(first.RawGetString("mtime_ns").(lua.LInteger)), int64(0))

	again := statTable(t, l, a)
	assert.Equal(t, first.RawGetString("dev"), again.RawGetString("dev"))
	assert.Equal(t, first.RawGetString("ino"), again.RawGetString("ino"))
	assert.Equal(t, first.RawGetString("ctime_ns"), again.RawGetString("ctime_ns"))

	other := statTable(t, l, b)
	assert.Equal(t, first.RawGetString("dev"), other.RawGetString("dev"))
	assert.NotEqual(t, first.RawGetString("ino"), other.RawGetString("ino"))
}

func TestPushFileInfo_CtimeChangesOnWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	require.NoError(t, os.WriteFile(path, []byte("one"), 0o644))

	l := lua.NewState()
	defer l.Close()

	before := statTable(t, l, path).RawGetString("ctime_ns")
	require.NoError(t, os.WriteFile(path, []byte("two"), 0o644))
	after := statTable(t, l, path).RawGetString("ctime_ns")
	assert.NotEqual(t, before, after)
}
