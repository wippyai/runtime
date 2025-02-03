package btea

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	lua "github.com/yuin/gopher-lua"
)

// Spinner wraps spinner.Model for Lua
type Spinner struct {
	model spinner.Model
}

// RegisterSpinner registers the spinner component
func RegisterSpinner(l *lua.LState, mod *lua.LTable) {
	// Create and register the spinner metatable
	mt := l.NewTypeMetatable("btea.Spinner")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"update": spinnerUpdate,
		"tick":   spinnerTick,
		"view":   spinnerView,
		"style":  spinnerStyle,
	}))

	// Register constructor
	l.SetField(mod, "new_spinner", l.NewFunction(newSpinner))

	// Register spinner types
	spinnersTbl := l.NewTable()
	l.SetField(spinnersTbl, "LINE", luaSpinnerFromGo(l, spinner.Line))
	l.SetField(spinnersTbl, "DOT", luaSpinnerFromGo(l, spinner.Dot))
	l.SetField(spinnersTbl, "MINIDOT", luaSpinnerFromGo(l, spinner.MiniDot))
	l.SetField(spinnersTbl, "JUMP", luaSpinnerFromGo(l, spinner.Jump))
	l.SetField(spinnersTbl, "PULSE", luaSpinnerFromGo(l, spinner.Pulse))
	l.SetField(spinnersTbl, "POINTS", luaSpinnerFromGo(l, spinner.Points))
	l.SetField(spinnersTbl, "GLOBE", luaSpinnerFromGo(l, spinner.Globe))
	l.SetField(spinnersTbl, "MOON", luaSpinnerFromGo(l, spinner.Moon))
	l.SetField(spinnersTbl, "MONKEY", luaSpinnerFromGo(l, spinner.Monkey))
	l.SetField(spinnersTbl, "METER", luaSpinnerFromGo(l, spinner.Meter))
	l.SetField(spinnersTbl, "HAMBURGER", luaSpinnerFromGo(l, spinner.Hamburger))
	l.SetField(spinnersTbl, "ELLIPSIS", luaSpinnerFromGo(l, spinner.Ellipsis))
	l.SetField(mod, "spinners", spinnersTbl)
}

func newSpinner(l *lua.LState) int {
	opts := l.CheckTable(1)

	// Get spinner type from options
	spinnerType := opts.RawGetString("type")
	if spinnerType == lua.LNil {
		spinnerType = luaSpinnerFromGo(l, spinner.Line)
	}

	// Create spinner model
	model := spinner.New(
		spinner.WithSpinner(goSpinnerFromLua(spinnerType)),
	)

	// Create userdata
	ud := l.NewUserData()
	ud.Value = &Spinner{model: model}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Spinner"))
	l.Push(ud)
	return 1
}

func checkSpinner(l *lua.LState) *Spinner {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Spinner); ok {
		return v
	}
	l.ArgError(1, "spinner expected")
	return nil
}

func checkSpinnerStyle(l *lua.LState, idx int) (*Style, bool) {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Style); ok {
		return v, true
	}
	return nil, false
}

// Spinner methods

func spinnerTick(l *lua.LState) int {
	s := checkSpinner(l)
	if s == nil {
		return 0
	}
	l.Push(newCmdWrapper(l, s.model.Tick))
	return 1
}

func spinnerUpdate(l *lua.LState) int {
	s := checkSpinner(l)
	if s == nil {
		return 0
	}

	// Get message argument and convert to tea.Msg
	msgValue := l.CheckAny(2)
	msg, err := LuaToMsg(msgValue)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}

	var cmd tea.Cmd
	s.model, cmd = s.model.Update(msg)

	// Return command if any
	if cmd != nil {
		l.Push(newCmdWrapper(l, cmd))
		return 1
	}

	return 0
}

func spinnerView(l *lua.LState) int {
	s := checkSpinner(l)
	if s == nil {
		return 0
	}
	l.Push(lua.LString(s.model.View()))
	return 1
}

func spinnerStyle(l *lua.LState) int {
	s := checkSpinner(l)
	if s == nil {
		return 0
	}

	style, ok := checkSpinnerStyle(l, 2)
	if !ok {
		l.ArgError(2, "Style expected")
		return 0
	}

	s.model.Style = style.Style
	return 0
}

// Helper functions for converting between Go and Lua spinner types

func luaSpinnerFromGo(l *lua.LState, s spinner.Spinner) lua.LValue {
	tbl := l.NewTable()
	frames := l.NewTable()
	for i, frame := range s.Frames {
		frames.RawSetInt(i+1, lua.LString(frame))
	}
	tbl.RawSetString("frames", frames)
	tbl.RawSetString("interval", lua.LNumber(float64(s.FPS)/float64(time.Millisecond)))
	return tbl
}

func goSpinnerFromLua(v lua.LValue) spinner.Spinner {
	if tbl, ok := v.(*lua.LTable); ok {
		var frames []string
		if framesTbl := tbl.RawGetString("frames"); framesTbl != lua.LNil {
			if t, ok := framesTbl.(*lua.LTable); ok {
				t.ForEach(func(_, v lua.LValue) {
					if str, ok := v.(lua.LString); ok {
						frames = append(frames, string(str))
					}
				})
			}
		}
		interval := lua.LVAsNumber(tbl.RawGetString("interval"))
		return spinner.Spinner{
			Frames: frames,
			FPS:    time.Duration(interval) * time.Millisecond,
		}
	}

	return spinner.Line
}
