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

func TestBindCachedLibs_Utf8Lib(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	// "H\195\164\226\130\172" is "H", U+00E4 and U+20AC encoded as UTF-8.
	require.NoError(t, l.DoString(`
		local s = "H\195\164\226\130\172"
		assert(type(utf8) == "table")
		assert(utf8.char(72, 228, 8364) == s)
		assert(utf8.len(s) == 3)
		assert(utf8.codepoint(s, 4) == 8364)
		assert(utf8.offset(s, 3) == 4)
		assert(type(utf8.charpattern) == "string")
		local positions, codepoints = {1, 2, 4}, {72, 228, 8364}
		local n = 0
		for position, codepoint in utf8.codes(s) do
			n = n + 1
			assert(position == positions[n] and codepoint == codepoints[n])
		end
		assert(n == 3)
		assert(utf8.offset("", 0) == 1)
		assert(utf8.offset(s, -1) == 4)
		assert(utf8.codepoint(s, -3) == 8364)
		local extended = utf8.char(0xD800, 0x110000, 0x7FFFFFFF)
		local count, invalid = utf8.len(extended)
		assert(count == nil and invalid == 1)
		assert(utf8.len(extended, 1, -1, true) == 3)
		local expected = {0xD800, 0x110000, 0x7FFFFFFF}
		n = 0
		for _, codepoint in utf8.codes(extended, true) do
			n = n + 1
			assert(codepoint == expected[n])
		end
		assert(n == 3)
	`))
}

func TestBindCachedLibs_StandardLibraryCorrections(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	BindCachedLibs(l)

	require.NoError(t, l.DoString(`
		assert(math.huge == 1 / 0)
		assert(math.type(tonumber("10")) == "integer")
		assert(tonumber("1e2") == 100)
		assert(tonumber("0xffffffffffffffff") == -1)
		assert(table.concat({[0] = "zero", "one"}, ",", 0, 1) == "zero,one")
		assert(not pcall(table.concat, {"one"}, ",", 1, 2))
		local s = string.rep("a", 15)
		local pattern = "^" .. string.rep("a*", 10) .. s .. "$"
		local ok, err = pcall(string.match, s, pattern)
		assert(not ok and string.find(err, "pattern too complex", 1, true))
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
	assert.True(t, cachedUtf8Lib.Immutable)
}
