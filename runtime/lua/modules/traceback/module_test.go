// SPDX-License-Identifier: MPL-2.0

package traceback

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

func newTestState(t *testing.T) *lua.LState {
	t.Helper()

	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	t.Cleanup(l.Close)
	lua.OpenBase(l)
	lua.OpenString(l)
	lua.OpenTable(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	return l
}

func TestModule_Load(t *testing.T) {
	l := newTestState(t)

	mod := l.GetGlobal("traceback")
	require.Equal(t, lua.LTTable, mod.Type(), "traceback module not registered")

	modTbl := mod.(*lua.LTable)
	for _, fn := range []string{"format", "frames"} {
		assert.Equal(t, lua.LTFunction, modTbl.RawGetString(fn).Type(), "%s function not registered", fn)
	}
}

func TestModule_Immutable(t *testing.T) {
	tbl, _ := Module.Build()
	assert.True(t, tbl.Immutable, "module table must be immutable")
}

func TestFrames_CapturesCallerLocals(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function inner()
			local x = 42
			local greeting = "hello"
			result = traceback.frames(1)
		end
		inner()
	`)

	require.NoError(t, err)
	result := l.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type(), "frames() must return a table")

	frames := result.(*lua.LTable)
	require.Equal(t, lua.LTTable, frames.RawGetInt(1).Type(), "first frame missing")

	first := frames.RawGetInt(1).(*lua.LTable)
	assert.Equal(t, lua.LTString, first.RawGetString("source").Type())
	assert.Equal(t, lua.LTNumber, first.RawGetString("currentline").Type())
	assert.Equal(t, "inner", first.RawGetString("name").String())

	locals := first.RawGetString("locals")
	require.Equal(t, lua.LTTable, locals.Type(), "locals must be a table")

	localsMap := localsToMap(t, locals.(*lua.LTable))
	assert.Equal(t, lua.LNumber(42), lua.LVAsNumber(localsMap["x"]), "local x must be captured")
	assert.Equal(t, lua.LString("hello"), localsMap["greeting"], "local greeting must be captured")
}

func TestFrames_WalksMultipleFrames(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function leaf()
			result = traceback.frames(1)
		end
		local function middle()
			local mid = "mid-value"
			leaf()
		end
		local function top()
			local topvar = 7
			middle()
		end
		top()
	`)

	require.NoError(t, err)
	frames := l.GetGlobal("result").(*lua.LTable)

	count := 0
	frames.ForEach(func(_, _ lua.LValue) { count++ })
	require.GreaterOrEqual(t, count, 3, "must capture at least 3 frames (leaf, middle, top)")
}

func TestFrames_DefaultLevelSkipsGoFrame(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function caller()
			local marker = "present"
			result = traceback.frames()
		end
		caller()
	`)

	require.NoError(t, err)
	frames := l.GetGlobal("result").(*lua.LTable)
	first := frames.RawGetInt(1).(*lua.LTable)

	locals := localsToMap(t, first.RawGetString("locals").(*lua.LTable))
	assert.Equal(t, lua.LString("present"), locals["marker"], "default level must start at the Lua caller")
}

func TestFrames_BadLevelReturnsEmpty(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		result = traceback.frames(9999)
		assert(type(result) == "table")
		local count = 0
		for _ in pairs(result) do count = count + 1 end
		assert(count == 0, "expected empty table for out-of-range level")
	`)

	require.NoError(t, err)
}

func TestFormat_ProducesStringWithLocals(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function inner()
			local x = 42
			local greeting = "hello"
			result = traceback.format(1)
		end
		inner()
	`)

	require.NoError(t, err)
	result := l.GetGlobal("result")
	require.Equal(t, lua.LTString, result.Type(), "format() must return a string")

	s := result.String()
	assert.Contains(t, s, "inner", "formatted trace should name the function")
	assert.Contains(t, s, "x", "formatted trace should list local x")
	assert.Contains(t, s, "greeting", "formatted trace should list local greeting")
}

func TestFormat_BadLevelReturnsEmptyString(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		result = traceback.format(9999)
		assert(type(result) == "string")
		assert(#result == 0, "expected empty string for out-of-range level")
	`)

	require.NoError(t, err)
}

func TestFormat_CyclicTableDoesNotInfiniteLoop(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function inner()
			local cyclic = {}
			cyclic.self = cyclic
			result = traceback.format(1)
		end
		inner()
	`)

	require.NoError(t, err, "format() must terminate on cyclic tables")
	s := l.GetGlobal("result").String()
	assert.Contains(t, s, "cyclic", "cyclic local should still be named")
}

func TestFormat_OptsDepthLimitsFrames(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function leaf()
			result = traceback.format(1, { depth = 1 })
		end
		local function middle() leaf() end
		local function top() middle() end
		top()
	`)

	require.NoError(t, err)
	s := l.GetGlobal("result").String()

	frameCount := strings.Count(s, "[0]")
	frameCount += strings.Count(s, "[1]")
	frameCount += strings.Count(s, "[2]")
	frameCount += strings.Count(s, "[3]")
	assert.LessOrEqual(t, frameCount, 1, "depth=1 should yield at most 1 frame")
}

func TestFormat_OptsDisableLocals(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function inner()
			local secret = "s3cret"
			result = traceback.format(1, { locals = false })
		end
		inner()
	`)

	require.NoError(t, err)
	s := l.GetGlobal("result").String()
	assert.NotContains(t, s, "s3cret", "locals=false must omit local values")
}

func TestFrames_XPCallHandlerCapturesThrowSite(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function boom()
			local trouble = "found-me"
			error("kaboom")
		end
		xpcall(boom, function(err)
			result = traceback.frames(1)
		end)
	`)

	require.NoError(t, err, "xpcall must catch the error (fork fix required)")
	frames := l.GetGlobal("result").(*lua.LTable)

	allLocals := make(map[string]lua.LValue)
	frames.ForEach(func(_, frame lua.LValue) {
		ft, ok := frame.(*lua.LTable)
		if !ok {
			return
		}

		loc := ft.RawGetString("locals")
		if loc.Type() != lua.LTTable {
			return
		}

		for k, v := range localsToMap(t, loc.(*lua.LTable)) {
			allLocals[k] = v
		}
	})

	_, hasTrouble := allLocals["trouble"]
	assert.True(t, hasTrouble, "xpcall handler must capture locals at the throw site (trouble); got locals: %v", allLocals)
}

func TestFrames_UpvaluesCapturedByDefault(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local upval = tostring("upvalue-content")
		local function inner()
			local _ = upval
			result = traceback.frames(1)
		end
		inner()
	`)

	require.NoError(t, err)
	frames := l.GetGlobal("result").(*lua.LTable)
	first := frames.RawGetInt(1).(*lua.LTable)

	upvalues := first.RawGetString("upvalues")
	require.Equal(t, lua.LTTable, upvalues.Type())

	uvMap := localsToMap(t, upvalues.(*lua.LTable))
	assert.Equal(t, lua.LString("upvalue-content"), uvMap["upval"], "upvalue must be captured")
}

func TestFrames_OptsDepthProducesExactCount(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function leaf() result = traceback.frames(1, { depth = 2 }) end
		local function mid() leaf() end
		local function top() mid() end
		top()
	`)

	require.NoError(t, err)
	frames := l.GetGlobal("result").(*lua.LTable)

	count := 0
	frames.ForEach(func(_, _ lua.LValue) { count++ })
	assert.Equal(t, 2, count, "depth=2 must produce exactly 2 frames")
}

func TestFormat_OptsMaxValueLenTruncates(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function inner()
			local big = string.rep("x", 500)
			result = traceback.format(1, { max_value_len = 10 })
		end
		inner()
	`)

	require.NoError(t, err)
	s := l.GetGlobal("result").String()
	assert.Contains(t, s, "xxxxxxxxxx...", "long string must be truncated to max_value_len with ellipsis")
	assert.NotContains(t, s, strings.Repeat("x", 50), "must not contain the full untruncated value")
}

func TestFormat_OptsMaxTableDepthCaps(t *testing.T) {
	l := newTestState(t)

	err := l.DoString(`
		local function inner()
			local nested = { a = { b = { c = { d = "deep" } } } }
			result = traceback.format(1, { max_table_depth = 2 })
		end
		inner()
	`)

	require.NoError(t, err)
	s := l.GetGlobal("result").String()
	assert.Contains(t, s, "{...}", "table beyond max_table_depth must render as {...}")
	assert.NotContains(t, s, "deep", "table beyond the cap must not render its deep contents")
}

func localsToMap(t *testing.T, tbl *lua.LTable) map[string]lua.LValue {
	t.Helper()

	out := make(map[string]lua.LValue)
	tbl.ForEach(func(_, v lua.LValue) {
		entry, ok := v.(*lua.LTable)
		if !ok {
			return
		}

		name := entry.RawGetString("name").String()
		val := entry.RawGetString("value")
		out[name] = val
	})

	return out
}
