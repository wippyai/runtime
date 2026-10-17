// SPDX-License-Identifier: MPL-2.0

package fs

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestPushFileInfo_VirtualFSHasNoIdentity(t *testing.T) {
	info, err := fstest.MapFS{"a.txt": {Data: []byte("x")}}.Stat("a.txt")
	require.NoError(t, err)

	l := lua.NewState()
	defer l.Close()

	tbl := pushFileInfo(l, info)
	for _, key := range []string{"dev", "ino", "ctime_ns", "mtime_ns"} {
		assert.Equal(t, lua.LNil, tbl.RawGetString(key), key)
	}
	assert.Equal(t, lua.LNumber(1), tbl.RawGetString("size"))
}

func TestPushUint64(t *testing.T) {
	assert.Equal(t, lua.LInteger(42), pushUint64(42))
	assert.Equal(t, lua.LInteger(maxExactFloat), pushUint64(maxExactFloat))
	assert.Equal(t, lua.LString("9007199254740993"), pushUint64(maxExactFloat+1))
	assert.Equal(t, lua.LString("18446744073709551615"), pushUint64(^uint64(0)))
}
