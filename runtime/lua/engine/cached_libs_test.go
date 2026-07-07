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

func TestBindCachedLibs_DebugLib_Present(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	require.NoError(t, l.DoString(`
		assert(type(debug) == "table")
	`))
}

func TestBindCachedLibs_DebugLib_HasSafeFuncs(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	for _, fn := range []string{"traceback", "getinfo", "getlocal", "getupvalue"} {
		err := l.DoString(`assert(type(debug.` + fn + `) == "function", "debug.` + fn + ` must be a function")`)
		require.NoError(t, err, "debug.%s must be exposed", fn)
	}
}

func TestBindCachedLibs_DebugLib_MissingUnsafeFuncs(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	for _, fn := range []string{"setlocal", "setmetatable", "setupvalue", "getmetatable"} {
		err := l.DoString(`assert(debug.` + fn + ` == nil, "debug.` + fn + ` must be absent")`)
		require.NoError(t, err, "debug.%s must NOT be exposed", fn)
	}
}

func TestBindCachedLibs_DebugLib_Immutable(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	setErr := l.DoString(`
		local ok, err = pcall(function()
			debug.setlocal = function() end
		end)
		assert(not ok, "mutating the immutable debug table must fail")
	`)
	require.NoError(t, setErr, "mutating debug table must error, not crash")

	getErr := l.DoString(`assert(debug.setlocal == nil, "setlocal must remain nil")`)
	require.NoError(t, getErr)
}

func TestBindCachedLibs_DebugLib_GetInfoWorks(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	err := l.DoString(`
		local function sample() return debug.getinfo(1) end
		local info = sample()
		assert(type(info) == "table")
		assert(type(info.currentline) == "number")
		assert(type(info.source) == "string")
		assert(info.name == "sample")
	`)
	require.NoError(t, err)
}

func TestBindCachedLibs_DebugLib_GetLocalWorks(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	err := l.DoString(`
		local function sample()
			local captured = "value-here"
			local name, value = debug.getlocal(1, 1)
			assert(name == "captured", "first local name should be 'captured'")
			assert(value == "value-here", "first local value should be 'value-here'")
		end
		sample()
	`)
	require.NoError(t, err)
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
	assert.True(t, cachedDebugLib.Immutable)
}
