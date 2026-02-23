// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func TestBindCachedLibs_TableLib(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	require.NoError(t, l.DoString(`
		local t = {3, 1, 2}
		table.sort(t)
		assert(t[1] == 1)
		assert(t[2] == 2)
		assert(t[3] == 3)
	`))
}

func TestBindCachedLibs_MathLib(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	require.NoError(t, l.DoString(`
		assert(math.abs(-5) == 5)
		assert(math.max(1, 2, 3) == 3)
	`))
}

func TestBindCachedLibs_CoroutineLib(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	require.NoError(t, l.DoString(`
		assert(type(coroutine.create) == "function")
	`))
}

func TestBindCachedLibs_StringLib(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	require.NoError(t, l.DoString(`
		assert(string.upper("hello") == "HELLO")
	`))
}

func TestBindCachedLibs_StringMetatable(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	// string methods as metatable enables "hello":upper() syntax
	require.NoError(t, l.DoString(`
		local s = ("hello"):upper()
		assert(s == "HELLO")
	`))
}

func TestBindCachedLibs_OsLib(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	require.NoError(t, l.DoString(`
		local t = os.time()
		assert(type(t) == "number")
		assert(t > 0)
	`))
}

func TestBindCachedLibs_ErrorsLib(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	require.NoError(t, l.DoString(`
		assert(type(errors) == "table")
	`))
}

func TestBindCachedLibs_MultipleStates(t *testing.T) {
	states := make([]*lua.LState, 3)
	for i := range states {
		states[i] = lua.NewState(lua.Options{SkipOpenLibs: true})
		lua.OpenBase(states[i])
		BindCachedLibs(states[i])
	}
	defer func() {
		for _, l := range states {
			l.Close()
		}
	}()

	// all share the same cached table lib
	for _, l := range states {
		require.NoError(t, l.DoString(`assert(math.pi > 3)`))
	}
}

func TestBindCachedLibs_Immutable(t *testing.T) {
	initCachedLibs()

	assert.True(t, cachedTableLib.Immutable)
	assert.True(t, cachedMathLib.Immutable)
	assert.True(t, cachedCoroutineLib.Immutable)
	assert.True(t, cachedStringLib.Immutable)
	assert.True(t, cachedErrorsLib.Immutable)
}
