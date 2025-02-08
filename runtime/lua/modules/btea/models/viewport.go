package models

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	lua "github.com/yuin/gopher-lua"
)

// Viewport wraps viewport.Model for Lua
type Viewport struct {
	model viewport.Model
}

func (v *Viewport) Init() tea.Cmd {
	return v.model.Init()
}

func (v *Viewport) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := v.model.Update(msg)
	v.model = model

	return v, cmd
}

func (v *Viewport) View() string {
	return v.View()
}

// RegisterViewport registers the viewport component
func RegisterViewport(l *lua.LState, mod *lua.LTable) {
	// Create and register the viewport metatable
	mt := l.NewTypeMetatable("btea.Viewport")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		// Core methods
		"update":      viewportUpdate,
		"view":        viewportView,
		"set_content": viewportSetContent,

		// Scroll position methods
		"scroll_to_top":    viewportGotoTop,
		"scroll_to_bottom": viewportGotoBottom,
		"scroll_percent":   viewportScrollPercent,
		"line_up":          viewportLineUp,
		"line_down":        viewportLineDown,
		"page_up":          viewportViewUp,
		"page_down":        viewportViewDown,
		"half_page_up":     viewportHalfViewUp,
		"half_page_down":   viewportHalfViewDown,

		// Position info
		"at_top":       viewportAtTop,
		"at_bottom":    viewportAtBottom,
		"y_offset":     viewportYOffset,
		"set_y_offset": viewportSetYOffset,

		// Configuration
		"set_style":         viewportSetStyle,
		"set_width":         viewportSetWidth,
		"set_height":        viewportSetHeight,
		"enable_mouse":      viewportEnableMouse,
		"mouse_wheel_delta": viewportSetMouseWheelDelta,

		// Content info
		"total_lines":   viewportTotalLines,
		"visible_lines": viewportVisibleLines,

		// todo: normalize
		"width": func(L *lua.LState) int {
			v := checkViewport(L)
			if v == nil {
				return 0
			}
			L.Push(lua.LNumber(v.model.Width))
			return 1
		},

		"height": func(L *lua.LState) int {
			v := checkViewport(L)
			if v == nil {
				return 0
			}

			L.Push(lua.LNumber(v.model.Height))
			return 1
		},
	}))

	// Register constructor
	l.SetField(mod, "new_viewport", l.NewFunction(newViewport))
}

func newViewport(l *lua.LState) int {
	opts := l.CheckTable(1)

	width := int(lua.LVAsNumber(opts.RawGetString("width")))
	height := int(lua.LVAsNumber(opts.RawGetString("height")))

	v := &Viewport{
		model: viewport.New(width, height),
	}

	// Process options
	opts.ForEach(func(k, val lua.LValue) {
		switch k.String() {
		case "mouse_wheel_enabled":
			v.model.MouseWheelEnabled = lua.LVAsBool(val)
		case "mouse_wheel_delta":
			v.model.MouseWheelDelta = int(lua.LVAsNumber(val))
		case "high_performance":
			v.model.HighPerformanceRendering = lua.LVAsBool(val)
		case "content":
			v.model.SetContent(lua.LVAsString(val))
		case "style":
			if s, ok := val.(*lua.LUserData); ok {
				if style, ok := s.Value.(*render.Style); ok {
					v.model.Style = style.Style
				}
			}
		}
	})

	ud := l.NewUserData()
	ud.Value = v
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Viewport"))
	l.Push(ud)
	return 1
}

func checkViewport(l *lua.LState) *Viewport {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Viewport); ok {
		return v
	}
	l.ArgError(1, "viewport expected")
	return nil
}

// Core methods

func viewportUpdate(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}

	msgValue := l.CheckAny(2)
	msg, err := protocol.LuaToMsg(msgValue)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}

	var cmd tea.Cmd
	v.model, cmd = v.model.Update(msg)

	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func viewportView(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	l.Push(lua.LString(v.model.View()))
	return 1
}

func viewportSetContent(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	content := l.CheckString(2)
	v.model.SetContent(content)
	return 0
}

// Scroll methods

func viewportGotoTop(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	lines := v.model.GotoTop()
	if v.model.HighPerformanceRendering && len(lines) > 0 {
		l.Push(protocol.WrapCommand(l, func() tea.Msg {
			return viewport.ViewUp(v.model, lines)
		}))
		return 1
	}
	return 0
}

func viewportGotoBottom(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	lines := v.model.GotoBottom()
	if v.model.HighPerformanceRendering && len(lines) > 0 {
		l.Push(protocol.WrapCommand(l, func() tea.Msg {
			return viewport.ViewDown(v.model, lines)
		}))
		return 1
	}
	return 0
}

func viewportScrollPercent(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	l.Push(lua.LNumber(v.model.ScrollPercent()))
	return 1
}

func viewportLineUp(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	n := l.OptInt(2, 1)
	lines := v.model.LineUp(n)
	if v.model.HighPerformanceRendering && len(lines) > 0 {
		l.Push(protocol.WrapCommand(l, func() tea.Msg {
			return viewport.ViewUp(v.model, lines)
		}))
		return 1
	}
	return 0
}

func viewportLineDown(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	n := l.OptInt(2, 1)
	lines := v.model.LineDown(n)
	if v.model.HighPerformanceRendering && len(lines) > 0 {
		l.Push(protocol.WrapCommand(l, func() tea.Msg {
			return viewport.ViewDown(v.model, lines)
		}))
		return 1
	}
	return 0
}

func viewportViewUp(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	lines := v.model.ViewUp()
	if v.model.HighPerformanceRendering && len(lines) > 0 {
		l.Push(protocol.WrapCommand(l, func() tea.Msg {
			return viewport.ViewUp(v.model, lines)
		}))
		return 1
	}
	return 0
}

func viewportViewDown(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	lines := v.model.ViewDown()
	if v.model.HighPerformanceRendering && len(lines) > 0 {
		l.Push(protocol.WrapCommand(l, func() tea.Msg {
			return viewport.ViewDown(v.model, lines)
		}))
		return 1
	}
	return 0
}

func viewportHalfViewUp(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	lines := v.model.HalfViewUp()
	if v.model.HighPerformanceRendering && len(lines) > 0 {
		l.Push(protocol.WrapCommand(l, func() tea.Msg {
			return viewport.ViewUp(v.model, lines)
		}))
		return 1
	}
	return 0
}

func viewportHalfViewDown(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	lines := v.model.HalfViewDown()
	if v.model.HighPerformanceRendering && len(lines) > 0 {
		l.Push(protocol.WrapCommand(l, func() tea.Msg {
			return viewport.ViewDown(v.model, lines)
		}))
		return 1
	}
	return 0
}

// Position methods

func viewportAtTop(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	l.Push(lua.LBool(v.model.AtTop()))
	return 1
}

func viewportAtBottom(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	l.Push(lua.LBool(v.model.AtBottom()))
	return 1
}

func viewportYOffset(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	l.Push(lua.LNumber(v.model.YOffset))
	return 1
}

func viewportSetYOffset(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	offset := l.CheckInt(2)
	v.model.SetYOffset(offset)
	return 0
}

// Configuration methods

func viewportSetStyle(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	style := render.CheckStyle(l, 2)
	if style == nil {
		return 0
	}
	v.model.Style = style.Style
	return 0
}

func viewportSetWidth(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	width := l.CheckInt(2)
	v.model.Width = width
	return 0
}

func viewportSetHeight(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	height := l.CheckInt(2)
	v.model.Height = height
	return 0
}

func viewportEnableMouse(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	enabled := l.CheckBool(2)
	v.model.MouseWheelEnabled = enabled
	return 0
}

func viewportSetMouseWheelDelta(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	delta := l.CheckInt(2)
	v.model.MouseWheelDelta = delta
	return 0
}

// Content info methods

func viewportTotalLines(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	l.Push(lua.LNumber(v.model.TotalLineCount()))
	return 1
}

func viewportVisibleLines(l *lua.LState) int {
	v := checkViewport(l)
	if v == nil {
		return 0
	}
	l.Push(lua.LNumber(v.model.VisibleLineCount()))
	return 1
}
