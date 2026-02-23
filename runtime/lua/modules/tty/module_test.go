// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/service/terminal"
	ttyapi "github.com/wippyai/runtime/api/tty"
	svcterm "github.com/wippyai/runtime/service/terminal"
)

func bindTTY(l *lua.LState) {
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
}

func TestModuleInfo(t *testing.T) {
	info := Module.Info()
	assert.Equal(t, "tty", info.Name)
	assert.NotEmpty(t, info.Description)
}

func TestModuleBuild(t *testing.T) {
	tbl, yields := Module.Build()
	require.NotNil(t, tbl)
	assert.Equal(t, 5, len(yields))
}

func TestYieldTypes(t *testing.T) {
	_, yields := Module.Build()

	expectedCmds := map[int]bool{
		int(ttyapi.StartInput):   false,
		int(ttyapi.StopInput):    false,
		int(ttyapi.ScreenSize):   false,
		int(ttyapi.EnableMouse):  false,
		int(ttyapi.DisableMouse): false,
	}

	for _, y := range yields {
		cmdID := int(y.CmdID)
		if _, ok := expectedCmds[cmdID]; ok {
			expectedCmds[cmdID] = true
		}
	}

	for cmdID, found := range expectedCmds {
		if !found {
			t.Errorf("missing yield type for command ID %d", cmdID)
		}
	}
}

func TestModuleFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	mod := l.GetGlobal("tty")
	require.Equal(t, lua.LTTable, mod.Type())

	modTbl := mod.(*lua.LTable)
	funcs := []string{"start", "stop", "screen_size", "events", "style", "bind"}
	for _, name := range funcs {
		assert.Equal(t, lua.LTFunction, modTbl.RawGetString(name).Type(), "missing function: %s", name)
	}
}

func TestModuleConstants(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	mod := l.GetGlobal("tty").(*lua.LTable)

	// Borders
	borders := mod.RawGetString("borders")
	require.Equal(t, lua.LTTable, borders.Type())
	borderTbl := borders.(*lua.LTable)
	assert.Equal(t, "normal", borderTbl.RawGetString("NORMAL").String())
	assert.Equal(t, "rounded", borderTbl.RawGetString("ROUNDED").String())
	assert.Equal(t, "thick", borderTbl.RawGetString("THICK").String())
	assert.Equal(t, "double", borderTbl.RawGetString("DOUBLE").String())

	// Align
	align := mod.RawGetString("align")
	require.Equal(t, lua.LTTable, align.Type())
	alignTbl := align.(*lua.LTable)
	assert.Equal(t, lua.LNumber(0), alignTbl.RawGetString("LEFT"))
	assert.Equal(t, lua.LNumber(0.5), alignTbl.RawGetString("CENTER"))
	assert.Equal(t, lua.LNumber(1), alignTbl.RawGetString("RIGHT"))

	// Text utilities and position
	text := mod.RawGetString("text")
	require.Equal(t, lua.LTTable, text.Type())
	textTbl := text.(*lua.LTable)
	assert.Equal(t, lua.LTFunction, textTbl.RawGetString("width").Type())
	assert.Equal(t, lua.LTFunction, textTbl.RawGetString("join_horizontal").Type())

	pos := textTbl.RawGetString("position")
	require.Equal(t, lua.LTTable, pos.Type())
}

// Stub InputController for testing
type stubInputController struct {
	startErr   error
	stopErr    error
	sizeErr    error
	sizeCols   int
	sizeRows   int
	startCalls int
	stopCalls  int
}

func (s *stubInputController) Start() error {
	s.startCalls++
	return s.startErr
}

func (s *stubInputController) Stop() error {
	s.stopCalls++
	return s.stopErr
}

func (s *stubInputController) ScreenSize() (int, int, error) {
	return s.sizeCols, s.sizeRows, s.sizeErr
}

func (s *stubInputController) EnableMouse()  {}
func (s *stubInputController) DisableMouse() {}

func TestTTYStart_NoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	ret := ttyStart(l)
	assert.Equal(t, 2, ret)
	assert.Equal(t, lua.LNil, l.Get(1))
}

func TestTTYStart_NoInputController(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	tc := terminal.NewTerminalContext(nil, nil, nil)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	ret := ttyStart(l)
	assert.Equal(t, 2, ret)
	assert.Equal(t, lua.LNil, l.Get(1))
}

func TestTTYStart_WithInputController(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	tc := terminal.NewTerminalContext(nil, nil, nil)
	tc.Input = &stubInputController{}
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	ret := ttyStart(l)
	assert.Equal(t, -1, ret)
	_, ok := l.Get(1).(*StartInputYield)
	assert.True(t, ok)
}

func TestTTYStop_WithInputController(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	tc := terminal.NewTerminalContext(nil, nil, nil)
	tc.Input = &stubInputController{}
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	ret := ttyStop(l)
	assert.Equal(t, -1, ret)
	_, ok := l.Get(1).(*StopInputYield)
	assert.True(t, ok)
}

func TestTTYScreenSize_WithInputController(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	tc := terminal.NewTerminalContext(nil, nil, nil)
	tc.Input = &stubInputController{}
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	ret := ttyScreenSize(l)
	assert.Equal(t, -1, ret)
	_, ok := l.Get(1).(*ScreenSizeYield)
	assert.True(t, ok)
}

func TestStartInputYield(t *testing.T) {
	y := AcquireStartInputYield()
	assert.Equal(t, ttyapi.StartInput, y.CmdID())
	assert.Equal(t, ttyapi.StartInput, y.ToCommand().CmdID())
	assert.Contains(t, y.String(), "start_input")
	assert.Equal(t, lua.LTUserData, y.Type())
	ReleaseStartInputYield(y)
}

func TestStopInputYield(t *testing.T) {
	y := AcquireStopInputYield()
	assert.Equal(t, ttyapi.StopInput, y.CmdID())
	assert.Equal(t, ttyapi.StopInput, y.ToCommand().CmdID())
	ReleaseStopInputYield(y)
}

func TestScreenSizeYield(t *testing.T) {
	y := AcquireScreenSizeYield()
	assert.Equal(t, ttyapi.ScreenSize, y.CmdID())
	assert.Equal(t, ttyapi.ScreenSize, y.ToCommand().CmdID())
	ReleaseScreenSizeYield(y)
}

func TestStartInputYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireStartInputYield()
	values := y.HandleResult(l, true, nil)
	assert.Equal(t, lua.LTrue, values[0])
	assert.Equal(t, lua.LNil, values[1])
	ReleaseStartInputYield(y)
}

func TestStartInputYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireStartInputYield()
	values := y.HandleResult(l, nil, errors.New("failed"))
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
	ReleaseStartInputYield(y)
}

func TestScreenSizeYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireScreenSizeYield()
	values := y.HandleResult(l, []int{80, 24}, nil)
	assert.Equal(t, lua.LNumber(80), values[0])
	assert.Equal(t, lua.LNumber(24), values[1])
	assert.Equal(t, lua.LNil, values[2])
	ReleaseScreenSizeYield(y)
}

func TestScreenSizeYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireScreenSizeYield()
	values := y.HandleResult(l, nil, errors.New("no terminal"))
	assert.Equal(t, lua.LNil, values[0])
	assert.Equal(t, lua.LNil, values[1])
	assert.NotEqual(t, lua.LNil, values[2])
	ReleaseScreenSizeYield(y)
}

func TestEventHandler_KeyEvent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ev := &svcterm.TTYEvent{
		Type:    "key",
		Key:     "a",
		KeyType: "runes",
		Ctrl:    true,
	}
	payloads := []payload.Payload{payload.New(ev)}

	result := eventHandler(nil, l, pid.PID{}, "", payloads)
	require.NotNil(t, result)

	tbl, ok := result.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "key", tbl.RawGetString("type").String())
	assert.Equal(t, "a", tbl.RawGetString("key").String())
	assert.Equal(t, "runes", tbl.RawGetString("key_type").String())
	assert.Equal(t, lua.LTrue, tbl.RawGetString("ctrl"))
	assert.Equal(t, lua.LFalse, tbl.RawGetString("alt"))
}

func TestEventHandler_MouseEvent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ev := &svcterm.TTYEvent{
		Type:   "mouse",
		Action: "press",
		Button: "left",
		X:      10,
		Y:      20,
	}
	payloads := []payload.Payload{payload.New(ev)}

	result := eventHandler(nil, l, pid.PID{}, "", payloads)
	tbl, ok := result.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "mouse", tbl.RawGetString("type").String())
	assert.Equal(t, "press", tbl.RawGetString("action").String())
	assert.Equal(t, "left", tbl.RawGetString("button").String())
	assert.Equal(t, lua.LNumber(10), tbl.RawGetString("x"))
	assert.Equal(t, lua.LNumber(20), tbl.RawGetString("y"))
}

func TestEventHandler_ResizeEvent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ev := &svcterm.TTYEvent{
		Type:   "resize",
		Width:  120,
		Height: 40,
	}
	payloads := []payload.Payload{payload.New(ev)}

	result := eventHandler(nil, l, pid.PID{}, "", payloads)
	tbl, ok := result.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "resize", tbl.RawGetString("type").String())
	assert.Equal(t, lua.LNumber(120), tbl.RawGetString("width"))
	assert.Equal(t, lua.LNumber(40), tbl.RawGetString("height"))
}

func TestEventHandler_FocusEvent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ev := &svcterm.TTYEvent{Type: "focus", Focused: true}
	payloads := []payload.Payload{payload.New(ev)}

	result := eventHandler(nil, l, pid.PID{}, "", payloads)
	tbl, ok := result.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "focus", tbl.RawGetString("type").String())
	assert.Equal(t, lua.LTrue, tbl.RawGetString("focused"))
}

func TestEventHandler_PasteEvent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ev := &svcterm.TTYEvent{Type: "paste", Paste: "hello world"}
	payloads := []payload.Payload{payload.New(ev)}

	result := eventHandler(nil, l, pid.PID{}, "", payloads)
	tbl, ok := result.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "paste", tbl.RawGetString("type").String())
	assert.Equal(t, "hello world", tbl.RawGetString("text").String())
}

func TestEventHandler_EmptyPayloads(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := eventHandler(nil, l, pid.PID{}, "", nil)
	assert.Equal(t, lua.LNil, result)
}

func TestEventHandler_WrongPayloadType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	payloads := []payload.Payload{payload.New("not a TTYEvent")}
	result := eventHandler(nil, l, pid.PID{}, "", payloads)
	assert.Equal(t, lua.LNil, result)
}

func TestStyle_Create(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local s = tty.style()
		if s == nil then error("style should not be nil") end
	`)
	assert.NoError(t, err)
}

func TestStyle_Render(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local s = tty.style()
		local result = s:render("hello")
		if result == nil or result == "" then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_Chainable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local s = tty.style()
			:bold()
			:italic()
			:underline()
			:foreground("#FF0000")
			:background("#000000")
			:padding(1, 2)
			:margin(1)
			:width(40)
			:height(5)
			:max_width(80)
			:max_height(10)
			:align(tty.align.CENTER)
		local result = s:render("test")
		if result == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_Border(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local s = tty.style()
			:border("rounded")
			:border_foreground("#888888")
		local result = s:render("boxed")
		if result == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_Copy(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local s1 = tty.style():width(10)
		local s2 = s1:copy():width(20)
		local r1 = s1:render("a")
		local r2 = s2:render("a")
		if r1 == r2 then error("copy should create independent style") end
	`)
	assert.NoError(t, err)
}

func TestStyle_ValueSemantics(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty

		-- Chainable methods return new styles without mutating the original
		local base = tty.style():foreground("#FF0000")
		local derived = base:width(20)
		local r1 = base:render("test")
		local r2 = derived:render("test")
		if r1 == r2 then error("method should return new style, not mutate original") end

		-- Shared style reused with different parameters produces independent results
		local shared = tty.style():foreground("#FF0000")
		local a = shared:width(10):render("x")
		local b = shared:width(20):render("x")
		if a == b then error("reuse with different width should produce different results") end

		-- Original shared style remains unaffected after derived usage
		local before = shared:render("y")
		local _ = shared:bold():italic():width(50):render("z")
		local after = shared:render("y")
		if before ~= after then error("original style was mutated by chained calls") end
	`)
	assert.NoError(t, err)
}

func TestText_Width(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local w = tty.text.width("hello")
		if w ~= 5 then error("expected width 5, got " .. w) end
	`)
	assert.NoError(t, err)
}

func TestText_Height(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local h = tty.text.height("line1\nline2\nline3")
		if h ~= 3 then error("expected height 3, got " .. h) end
	`)
	assert.NoError(t, err)
}

func TestText_Size(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local w, h = tty.text.size("hello\nworld!")
		if w ~= 6 then error("expected width 6, got " .. w) end
		if h ~= 2 then error("expected height 2, got " .. h) end
	`)
	assert.NoError(t, err)
}

func TestText_JoinHorizontal(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local result = tty.text.join_horizontal(tty.text.position.CENTER, "left", "right")
		if result == nil or result == "" then error("join should produce output") end
	`)
	assert.NoError(t, err)
}

func TestText_JoinVertical(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local result = tty.text.join_vertical(tty.text.position.LEFT, "top", "bottom")
		if result == nil or result == "" then error("join should produce output") end
	`)
	assert.NoError(t, err)
}

func TestText_MaxWidth(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local max = tty.text.max_width({"short", "much longer string"})
		if max ~= 18 then error("expected max width 18, got " .. max) end
	`)
	assert.NoError(t, err)
}

func TestText_MaxHeight(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local max = tty.text.max_height({"one", "one\ntwo\nthree"})
		if max ~= 3 then error("expected max height 3, got " .. max) end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_Create(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local kb = tty.bind({keys = {"q", "ctrl+c"}, help = {key = "q", desc = "quit"}})
		if kb == nil then error("keybinding should not be nil") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_Matches(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local kb = tty.bind({keys = {"q", "ctrl+c"}, help = {key = "q", desc = "quit"}})

		-- Match by key
		local event = {type = "key", key = "q", key_type = "runes", ctrl = false, alt = false, shift = false}
		if not kb:matches(event) then error("should match 'q'") end

		-- Match by keystroke
		local ctrl_c = {type = "key", key = "c", key_type = "runes", ctrl = true, alt = false, shift = false}
		if not kb:matches(ctrl_c) then error("should match 'ctrl+c'") end

		-- No match
		local other = {type = "key", key = "x", key_type = "runes", ctrl = false, alt = false, shift = false}
		if kb:matches(other) then error("should not match 'x'") end

		-- Not a key event
		local mouse = {type = "mouse", action = "press"}
		if kb:matches(mouse) then error("should not match mouse events") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_EnableDisable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local kb = tty.bind({keys = {"q"}, help = {key = "q", desc = "quit"}})

		if not kb:is_enabled() then error("should be enabled by default") end

		kb:set_enabled(false)
		if kb:is_enabled() then error("should be disabled") end

		local event = {type = "key", key = "q", key_type = "runes", ctrl = false, alt = false, shift = false}
		if kb:matches(event) then error("disabled binding should not match") end

		kb:set_enabled(true)
		if not kb:matches(event) then error("re-enabled binding should match") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_Help(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local kb = tty.bind({keys = {"q"}, help = {key = "q", desc = "quit"}})
		local h = kb:help()
		if h.key ~= "q" then error("expected help key 'q'") end
		if h.desc ~= "quit" then error("expected help desc 'quit'") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_SpecialKeyMatches(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local kb = tty.bind({keys = {"esc", "enter"}, help = {key = "esc", desc = "cancel"}})

		local esc = {type = "key", key = "esc", key_type = "esc", ctrl = false, alt = false, shift = false}
		if not kb:matches(esc) then error("should match 'esc'") end

		local enter = {type = "key", key = "enter", key_type = "enter", ctrl = false, alt = false, shift = false}
		if not kb:matches(enter) then error("should match 'enter'") end
	`)
	assert.NoError(t, err)
}

func TestModuleTypes(t *testing.T) {
	m := ModuleTypes()
	require.NotNil(t, m)
	assert.NotNil(t, m.Export)
}

// --- Yield HandleResult edge cases ---

func TestStopInputYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireStopInputYield()
	values := y.HandleResult(l, true, nil)
	assert.Equal(t, lua.LTrue, values[0])
	assert.Equal(t, lua.LNil, values[1])
	ReleaseStopInputYield(y)
}

func TestStopInputYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireStopInputYield()
	values := y.HandleResult(l, nil, errors.New("stop failed"))
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
	ReleaseStopInputYield(y)
}

func TestStopInputYield_StringAndType(t *testing.T) {
	y := AcquireStopInputYield()
	defer ReleaseStopInputYield(y)

	assert.Contains(t, y.String(), "stop_input")
	assert.Equal(t, lua.LTUserData, y.Type())
}

func TestScreenSizeYield_StringAndType(t *testing.T) {
	y := AcquireScreenSizeYield()
	defer ReleaseScreenSizeYield(y)

	assert.Contains(t, y.String(), "screen_size")
	assert.Equal(t, lua.LTUserData, y.Type())
}

func TestScreenSizeYield_HandleResult_InvalidLength(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireScreenSizeYield()
	values := y.HandleResult(l, []int{80}, nil)
	assert.Equal(t, lua.LNil, values[0])
	assert.Equal(t, lua.LNil, values[1])
	assert.NotEqual(t, lua.LNil, values[2])
	ReleaseScreenSizeYield(y)
}

func TestScreenSizeYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireScreenSizeYield()
	values := y.HandleResult(l, "not a slice", nil)
	assert.Equal(t, lua.LNil, values[0])
	assert.Equal(t, lua.LNil, values[1])
	assert.NotEqual(t, lua.LNil, values[2])
	ReleaseScreenSizeYield(y)
}

func TestHandleBoolResult_NilData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	values := handleBoolResult(l, nil, nil, "test")
	assert.Equal(t, lua.LTrue, values[0])
	assert.Equal(t, lua.LNil, values[1])
}

func TestHandleBoolResult_FalseValue(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	values := handleBoolResult(l, false, nil, "test")
	assert.Equal(t, lua.LFalse, values[0])
	assert.Equal(t, lua.LNil, values[1])
}

func TestHandleBoolResult_InvalidType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	values := handleBoolResult(l, "not a bool", nil, "test")
	assert.Equal(t, lua.LNil, values[0])
	assert.NotEqual(t, lua.LNil, values[1])
}

// --- ttyStop / ttyScreenSize no-context paths ---

func TestTTYStop_NoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	ret := ttyStop(l)
	assert.Equal(t, 2, ret)
	assert.Equal(t, lua.LNil, l.Get(1))
}

func TestTTYStop_NoInputController(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	tc := terminal.NewTerminalContext(nil, nil, nil)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	ret := ttyStop(l)
	assert.Equal(t, 2, ret)
	assert.Equal(t, lua.LNil, l.Get(1))
}

func TestTTYScreenSize_NoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	ret := ttyScreenSize(l)
	assert.Equal(t, 3, ret)
	assert.Equal(t, lua.LNil, l.Get(1))
	assert.Equal(t, lua.LNil, l.Get(2))
}

func TestTTYScreenSize_NoInputController(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.SetTop(0)

	tc := terminal.NewTerminalContext(nil, nil, nil)
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	_ = terminal.WithTerminalContext(ctx, tc)
	l.SetContext(ctx)

	ret := ttyScreenSize(l)
	assert.Equal(t, 3, ret)
	assert.Equal(t, lua.LNil, l.Get(1))
	assert.Equal(t, lua.LNil, l.Get(2))
}

// --- Event handler edge cases ---

func TestEventHandler_StartEvent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ev := &svcterm.TTYEvent{Type: "start", Width: 80, Height: 24}
	payloads := []payload.Payload{payload.New(ev)}

	result := eventHandler(nil, l, pid.PID{}, "", payloads)
	tbl, ok := result.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "start", tbl.RawGetString("type").String())
	assert.Equal(t, lua.LNumber(80), tbl.RawGetString("width"))
	assert.Equal(t, lua.LNumber(24), tbl.RawGetString("height"))
}

func TestEventHandler_FocusBlurEvent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	blur := &svcterm.TTYEvent{Type: "focus", Focused: false}
	payloads := []payload.Payload{payload.New(blur)}

	result := eventHandler(nil, l, pid.PID{}, "", payloads)
	tbl, ok := result.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, lua.LFalse, tbl.RawGetString("focused"))
}

func TestEventHandler_MouseWithModifiers(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ev := &svcterm.TTYEvent{
		Type:   "mouse",
		Action: "motion",
		Button: "middle",
		X:      5,
		Y:      10,
		Alt:    true,
		Ctrl:   false,
		Shift:  true,
	}
	payloads := []payload.Payload{payload.New(ev)}

	result := eventHandler(nil, l, pid.PID{}, "", payloads)
	tbl, ok := result.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "motion", tbl.RawGetString("action").String())
	assert.Equal(t, "middle", tbl.RawGetString("button").String())
	assert.Equal(t, lua.LTrue, tbl.RawGetString("alt"))
	assert.Equal(t, lua.LFalse, tbl.RawGetString("ctrl"))
	assert.Equal(t, lua.LTrue, tbl.RawGetString("shift"))
}

func TestEventHandler_KeyWithAllModifiers(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ev := &svcterm.TTYEvent{
		Type:    "key",
		Key:     "a",
		KeyType: "runes",
		Alt:     true,
		Ctrl:    true,
		Shift:   true,
	}
	payloads := []payload.Payload{payload.New(ev)}

	result := eventHandler(nil, l, pid.PID{}, "", payloads)
	tbl, ok := result.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, lua.LTrue, tbl.RawGetString("alt"))
	assert.Equal(t, lua.LTrue, tbl.RawGetString("ctrl"))
	assert.Equal(t, lua.LTrue, tbl.RawGetString("shift"))
}

// --- buildKeystroke tests ---

func TestBuildKeystroke(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		keyType  string
		expected string
		ctrl     bool
		alt      bool
		shift    bool
	}{
		{name: "simple char", key: "a", keyType: "runes", expected: "a"},
		{name: "ctrl+c", key: "c", keyType: "runes", expected: "ctrl+c", ctrl: true},
		{name: "alt+x", key: "x", keyType: "runes", expected: "alt+x", alt: true},
		{name: "shift+a", key: "A", keyType: "runes", expected: "shift+A", shift: true},
		{name: "ctrl+alt+del", key: "delete", keyType: "delete", expected: "ctrl+alt+delete", ctrl: true, alt: true},
		{name: "special key enter", key: "enter", keyType: "enter", expected: "enter"},
		{name: "special key esc", key: "esc", keyType: "esc", expected: "esc"},
		{name: "ctrl+shift+f1", key: "f1", keyType: "f1", expected: "ctrl+shift+f1", ctrl: true, shift: true},
		{name: "empty key"},
		{name: "unknown keytype", key: "?", keyType: "unknown", expected: "?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildKeystroke(tt.key, tt.keyType, tt.ctrl, tt.alt, tt.shift)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- Style methods ---

func TestStyle_Strikethrough(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():strikethrough()
		if s == nil then error("strikethrough should return style") end
		local r = s:render("test")
		if r == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_Faint(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():faint()
		if s == nil then error("faint should return style") end
	`)
	assert.NoError(t, err)
}

func TestStyle_Blink(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():blink()
		if s == nil then error("blink should return style") end
	`)
	assert.NoError(t, err)
}

func TestStyle_Reverse(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():reverse()
		if s == nil then error("reverse should return style") end
	`)
	assert.NoError(t, err)
}

func TestStyle_Inline(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():inline()
		local r = s:render("inline text")
		if r == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_AlignVertical(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():height(5):align_vertical(tty.align.CENTER)
		local r = s:render("centered")
		if r == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_BorderBackground(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style()
			:border("rounded")
			:border_background("#000000")
		local r = s:render("test")
		if r == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_Margin(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():margin(1, 2, 3, 4)
		local r = s:render("text")
		if r == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_RenderMultipleArgs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style()
		local r = s:render("hello", " ", "world")
		if r == nil or r == "" then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_DisableBoolParam(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():bold(true):bold(false)
		if s == nil then error("should return style") end
	`)
	assert.NoError(t, err)
}

func TestStyle_AllBorderTypes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local borders = {"normal", "rounded", "thick", "double", "hidden"}
		for _, b in ipairs(borders) do
			local s = tty.style():border(b)
			local r = s:render("x")
			if r == nil then error("render failed for border: " .. b) end
		end
	`)
	assert.NoError(t, err)
}

func TestStyle_BorderWithSides(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():border("rounded", true, false, true, false)
		local r = s:render("partial border")
		if r == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_MaxWidthMaxHeight(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():max_width(10):max_height(3)
		local r = s:render("this is a longer string that should be capped")
		if r == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_ToString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style()
		local str = tostring(s)
		if str ~= "tty.Style{}" then error("expected 'tty.Style{}', got " .. str) end
	`)
	assert.NoError(t, err)
}

func TestStyle_DefaultBorder(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style():border("unknown_type")
		local r = s:render("fallback")
		if r == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

func TestStyle_MultipleColorArgs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local s = tty.style()
			:border("rounded")
			:border_foreground("#FF0000", "#00FF00", "#0000FF", "#FFFFFF")
			:border_background("#000000", "#111111", "#222222", "#333333")
		local r = s:render("multi-color border")
		if r == nil then error("render should produce output") end
	`)
	assert.NoError(t, err)
}

// --- Text edge cases ---

func TestText_WidthEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local w = tty.text.width("")
		if w ~= 0 then error("expected width 0, got " .. w) end
	`)
	assert.NoError(t, err)
}

func TestText_HeightSingleLine(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local h = tty.text.height("single")
		if h ~= 1 then error("expected height 1, got " .. h) end
	`)
	assert.NoError(t, err)
}

func TestText_SizeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local w, h = tty.text.size("")
		if w ~= 0 then error("expected width 0, got " .. w) end
		if h ~= 1 then error("expected height 1, got " .. h) end
	`)
	assert.NoError(t, err)
}

func TestText_MaxWidthEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local max = tty.text.max_width({})
		if max ~= 0 then error("expected 0, got " .. max) end
	`)
	assert.NoError(t, err)
}

func TestText_MaxHeightEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local max = tty.text.max_height({})
		if max ~= 0 then error("expected 0, got " .. max) end
	`)
	assert.NoError(t, err)
}

func TestText_JoinHorizontalContent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local result = tty.text.join_horizontal(tty.text.position.TOP, "AB", "CD")
		if result ~= "ABCD" then error("expected 'ABCD', got '" .. result .. "'") end
	`)
	assert.NoError(t, err)
}

func TestText_JoinVerticalContent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local result = tty.text.join_vertical(tty.text.position.LEFT, "top", "bottom")
		local h = tty.text.height(result)
		if h ~= 2 then error("expected height 2, got " .. h) end
	`)
	assert.NoError(t, err)
}

// --- KeyBinding edge cases ---

func TestKeyBinding_MatchesAltKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local kb = tty.bind({keys = {"alt+x"}, help = {key = "alt+x", desc = "special"}})
		local event = {type = "key", key = "x", key_type = "runes", ctrl = false, alt = true, shift = false}
		if not kb:matches(event) then error("should match 'alt+x'") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_MatchesShiftKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local kb = tty.bind({keys = {"shift+tab"}, help = {key = "shift+tab", desc = "prev"}})
		local event = {type = "key", key = "tab", key_type = "tab", ctrl = false, alt = false, shift = true}
		if not kb:matches(event) then error("should match 'shift+tab'") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_MatchesCtrlAltCombo(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local kb = tty.bind({keys = {"ctrl+alt+delete"}, help = {key = "ctrl+alt+del", desc = "reset"}})
		local event = {type = "key", key = "delete", key_type = "delete", ctrl = true, alt = true, shift = false}
		if not kb:matches(event) then error("should match 'ctrl+alt+delete'") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_NoMatchWrongModifier(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local kb = tty.bind({keys = {"ctrl+c"}, help = {key = "ctrl+c", desc = "quit"}})
		-- Plain 'c' without ctrl should not match "ctrl+c" binding via keystroke
		local event = {type = "key", key = "c", key_type = "runes", ctrl = false, alt = false, shift = false}
		if kb:matches(event) then error("should not match plain 'c' for ctrl+c binding") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_ToString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local kb = tty.bind({keys = {"q", "ctrl+c"}, help = {key = "q", desc = "quit"}})
		local str = tostring(kb)
		if str ~= "tty.KeyBinding{q, ctrl+c}" then error("expected 'tty.KeyBinding{q, ctrl+c}', got '" .. str .. "'") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_EmptyKeys(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local kb = tty.bind({keys = {}, help = {key = "", desc = ""}})
		local event = {type = "key", key = "q", key_type = "runes", ctrl = false, alt = false, shift = false}
		if kb:matches(event) then error("empty binding should not match anything") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_SetEnabledChainable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local kb = tty.bind({keys = {"q"}, help = {key = "q", desc = "quit"}})
		local returned = kb:set_enabled(false)
		if returned ~= kb then error("set_enabled should return self") end
	`)
	assert.NoError(t, err)
}

func TestKeyBinding_NoHelpTable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local kb = tty.bind({keys = {"q"}})
		local h = kb:help()
		if h.key ~= "" then error("expected empty help key") end
		if h.desc ~= "" then error("expected empty help desc") end
	`)
	assert.NoError(t, err)
}

// --- Constants completeness ---

func TestModuleConstants_Hidden(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	mod := l.GetGlobal("tty").(*lua.LTable)
	borderTbl := mod.RawGetString("borders").(*lua.LTable)
	assert.Equal(t, "hidden", borderTbl.RawGetString("HIDDEN").String())
}

func TestModuleConstants_TextPosition(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	mod := l.GetGlobal("tty").(*lua.LTable)
	textTbl := mod.RawGetString("text").(*lua.LTable)
	posTbl := textTbl.RawGetString("position").(*lua.LTable)

	assert.Equal(t, lua.LNumber(0), posTbl.RawGetString("TOP"))
	assert.Equal(t, lua.LNumber(0), posTbl.RawGetString("LEFT"))
	assert.Equal(t, lua.LNumber(0.5), posTbl.RawGetString("CENTER"))
	assert.Equal(t, lua.LNumber(1), posTbl.RawGetString("BOTTOM"))
	assert.Equal(t, lua.LNumber(1), posTbl.RawGetString("RIGHT"))
}

func TestModuleConstants_TextFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	mod := l.GetGlobal("tty").(*lua.LTable)
	textTbl := mod.RawGetString("text").(*lua.LTable)

	funcs := []string{
		"width", "height", "size",
		"join_horizontal", "join_vertical",
		"max_width", "max_height",
		"place", "place_horizontal", "place_vertical",
	}
	for _, name := range funcs {
		assert.Equal(t, lua.LTFunction, textTbl.RawGetString(name).Type(), "missing text function: %s", name)
	}
}

func TestText_Place(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local result = tty.text.place(20, 5, tty.text.position.CENTER, tty.text.position.CENTER, "hi")
		if result == nil or result == "" then error("place should produce output") end
		local w = tty.text.width(result)
		if w ~= 20 then error("expected width 20, got " .. w) end
		local h = tty.text.height(result)
		if h ~= 5 then error("expected height 5, got " .. h) end
	`)
	assert.NoError(t, err)
}

func TestText_PlaceHorizontal(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local result = tty.text.place_horizontal(30, tty.text.position.CENTER, "centered")
		if result == nil or result == "" then error("place_horizontal should produce output") end
		local w = tty.text.width(result)
		if w ~= 30 then error("expected width 30, got " .. w) end
	`)
	assert.NoError(t, err)
}

func TestText_PlaceVertical(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local result = tty.text.place_vertical(5, tty.text.position.CENTER, "mid")
		if result == nil or result == "" then error("place_vertical should produce output") end
		local h = tty.text.height(result)
		if h ~= 5 then error("expected height 5, got " .. h) end
	`)
	assert.NoError(t, err)
}

func TestText_PlaceTopLeft(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local result = tty.text.place(10, 3, tty.text.position.LEFT, tty.text.position.TOP, "AB")
		local h = tty.text.height(result)
		if h ~= 3 then error("expected height 3, got " .. h) end
	`)
	assert.NoError(t, err)
}

func TestText_PlaceBottomRight(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)

	err := l.DoString(`
		local tty = tty
		local result = tty.text.place(10, 3, tty.text.position.RIGHT, tty.text.position.BOTTOM, "XY")
		local w = tty.text.width(result)
		if w ~= 10 then error("expected width 10, got " .. w) end
	`)
	assert.NoError(t, err)
}
