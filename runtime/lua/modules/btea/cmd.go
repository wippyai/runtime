package btea

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
)

// CmdWrapper wraps a tea.Cmd for use in Lua
type CmdWrapper struct {
	cmd tea.Cmd
}

// RegisterCmd registers command-related functions in the Lua environment
func RegisterCmd(l *lua.LState, mod *lua.LTable) {
	// Create and register the command metatable
	mt := l.NewTypeMetatable("btea.Cmd")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"execute": cmdExecute,
	}))

	// Register batch and sequence functions
	l.SetField(mod, "batch", l.NewFunction(cmdBatch))
	l.SetField(mod, "sequence", l.NewFunction(cmdSequence))

	// Register standard commands table
	cmdsTbl := l.NewTable()

	// Screen management commands
	l.SetField(cmdsTbl, "clear_screen", newCmdWrapper(l, func() tea.Msg {
		return tea.ClearScreen()
	}))
	l.SetField(cmdsTbl, "enter_alt_screen", newCmdWrapper(l, func() tea.Msg {
		return tea.EnterAltScreen()
	}))
	l.SetField(cmdsTbl, "exit_alt_screen", newCmdWrapper(l, func() tea.Msg {
		return tea.ExitAltScreen()
	}))

	// Mouse control commands
	l.SetField(cmdsTbl, "enable_mouse_cell_motion", newCmdWrapper(l, func() tea.Msg {
		return tea.EnableMouseCellMotion()
	}))
	l.SetField(cmdsTbl, "enable_mouse_all_motion", newCmdWrapper(l, func() tea.Msg {
		return tea.EnableMouseAllMotion()
	}))
	l.SetField(cmdsTbl, "disable_mouse", newCmdWrapper(l, func() tea.Msg {
		return tea.DisableMouse()
	}))

	// Cursor control commands
	l.SetField(cmdsTbl, "hide_cursor", newCmdWrapper(l, func() tea.Msg {
		return tea.HideCursor()
	}))
	l.SetField(cmdsTbl, "show_cursor", newCmdWrapper(l, func() tea.Msg {
		return tea.ShowCursor()
	}))

	// Paste mode commands
	l.SetField(cmdsTbl, "enable_bracketed_paste", newCmdWrapper(l, func() tea.Msg {
		return tea.EnableBracketedPaste()
	}))
	l.SetField(cmdsTbl, "disable_bracketed_paste", newCmdWrapper(l, func() tea.Msg {
		return tea.DisableBracketedPaste()
	}))

	// Focus reporting commands
	l.SetField(cmdsTbl, "enable_report_focus", newCmdWrapper(l, func() tea.Msg {
		return tea.EnableReportFocus()
	}))
	l.SetField(cmdsTbl, "disable_report_focus", newCmdWrapper(l, func() tea.Msg {
		return tea.DisableReportFocus()
	}))

	// Control commands
	l.SetField(cmdsTbl, "quit", newCmdWrapper(l, func() tea.Msg {
		return tea.Quit()
	}))
	l.SetField(cmdsTbl, "suspend", newCmdWrapper(l, func() tea.Msg {
		return tea.Suspend()
	}))

	// Window
	l.SetField(cmdsTbl, "set_window_title", l.NewFunction(setWindowTitleCmd))

	l.SetField(cmdsTbl, "window_size", newCmdWrapper(l, func() tea.Msg {
		return tea.WindowSize()
	}))

	// Set the commands table
	l.SetField(mod, "commands", cmdsTbl)
}

// newCmdWrapper creates a new command wrapper
func newCmdWrapper(l *lua.LState, cmd tea.Cmd) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = &CmdWrapper{cmd: cmd}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Cmd"))
	return ud
}

// cmdExecute executes the wrapped command
func cmdExecute(l *lua.LState) int {
	ud := l.CheckUserData(1)
	wrapper, ok := ud.Value.(*CmdWrapper)
	if !ok || wrapper.cmd == nil {
		return 0
	}

	// Create an async function that executes the command
	coroutine.Wrap(l, func() *engine.Result {
		msg := wrapper.cmd()
		if msg == nil {
			return engine.NewResult(nil, nil, nil)
		}

		// Convert the message to Lua and return it
		return engine.NewResult(nil, []lua.LValue{MsgToLua(msg)}, nil)
	})

	return -1 // Yield
}

// cmdBatch creates a batch of commands that execute in parallel
func cmdBatch(l *lua.LState) int {
	tbl := l.CheckTable(1)
	cmds := make([]tea.Cmd, 0)

	// Collect commands from table
	tbl.ForEach(func(_ lua.LValue, value lua.LValue) {
		if ud, ok := value.(*lua.LUserData); ok {
			if wrapper, ok := ud.Value.(*CmdWrapper); ok && wrapper.cmd != nil {
				cmds = append(cmds, wrapper.cmd)
			}
		}
	})

	if len(cmds) == 0 {
		return 0
	}

	// Create a batch command
	batchCmd := tea.Batch(cmds...)

	// Return a wrapped batch command
	l.Push(newCmdWrapper(l, batchCmd))
	return 1
}

// cmdSequence creates a sequence of commands that execute in order
func cmdSequence(l *lua.LState) int {
	tbl := l.CheckTable(1)
	cmds := make([]tea.Cmd, 0)

	// Collect commands from table
	tbl.ForEach(func(_ lua.LValue, value lua.LValue) {
		if ud, ok := value.(*lua.LUserData); ok {
			if wrapper, ok := ud.Value.(*CmdWrapper); ok && wrapper.cmd != nil {
				cmds = append(cmds, wrapper.cmd)
			}
		}
	})

	if len(cmds) == 0 {
		return 0
	}

	// Create a sequence command
	seqCmd := tea.Sequence(cmds...)

	// Return a wrapped sequence command
	l.Push(newCmdWrapper(l, seqCmd))
	return 1
}

func setWindowTitleCmd(l *lua.LState) int {
	title := l.CheckString(1) // Get title argument from Lua
	cmd := func() tea.Msg {
		return tea.SetWindowTitle(title)
	}
	l.Push(newCmdWrapper(l, cmd))
	return 1
}
