package protocol

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestCmdWrapping(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	mod := l.NewTable()
	RegisterCmd(l, mod)

	t.Run("wrap and execute simple command", func(t *testing.T) {
		var executed bool
		cmd := func() tea.Msg {
			executed = true
			return nil
		}

		wrapped := WrapCommand(l, cmd)
		assert.NotNil(t, wrapped)

		wrapper := wrapped.Value.(*CmdWrapper)
		msg := wrapper.cmd()
		assert.Nil(t, msg)
		assert.True(t, executed, "command should have been executed")
	})

	t.Run("wrap command with message", func(t *testing.T) {
		cmd := func() tea.Msg {
			return tea.WindowSizeMsg{Width: 80, Height: 24}
		}

		wrapped := WrapCommand(l, cmd)
		assert.NotNil(t, wrapped)

		wrapper := wrapped.Value.(*CmdWrapper)
		msg := wrapper.cmd()

		sizeMsg, ok := msg.(tea.WindowSizeMsg)
		require.True(t, ok)
		assert.Equal(t, 80, sizeMsg.Width)
		assert.Equal(t, 24, sizeMsg.Height)
	})
}

func TestCmdBatching(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	mod := l.NewTable()
	RegisterCmd(l, mod)

	t.Run("batch multiple commands", func(t *testing.T) {
		var count int
		cmd := func() tea.Msg {
			count++
			return nil
		}

		// Create batch table
		cmds := l.NewTable()
		for i := 0; i < 3; i++ {
			cmds.Append(WrapCommand(l, cmd))
		}

		// Call batch function
		l.Push(l.GetField(mod, "batch"))
		l.Push(cmds)
		l.Call(1, 1)

		batchCmd := UnwrapCommand(l, l.Get(-1))
		require.NotNil(t, batchCmd)

		batchMsg, ok := batchCmd().(tea.BatchMsg)
		require.True(t, ok, "expected batch message")

		// Execute each command in the batch
		for _, cmd := range batchMsg {
			cmd()
		}
		assert.Equal(t, 3, count, "all commands in batch should execute")
	})
}

func TestCmdSequence(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	mod := l.NewTable()
	RegisterCmd(l, mod)

	t.Run("sequence multiple commands", func(t *testing.T) {
		var order []int
		done := make(chan struct{})
		makeCmd := func(i int) func() tea.Msg {
			return func() tea.Msg {
				order = append(order, i)
				if i == 2 {
					close(done)
				}
				return nil
			}
		}

		// Create sequence table
		cmds := l.NewTable()
		for i := 0; i < 3; i++ {
			cmds.Append(WrapCommand(l, makeCmd(i)))
		}

		// Call sequence function
		l.Push(l.GetField(mod, "sequence"))
		l.Push(cmds)
		l.Call(1, 1)

		seqCmd := UnwrapCommand(l, l.Get(-1))
		require.NotNil(t, seqCmd)

		// Execute the sequence command to get the sequenceMsg
		msg := seqCmd()
		require.NotNil(t, msg, "sequence should return a message")

		// Instead of trying to type assert or access internals,
		// we'll verify the functionality by running it through a minimal tea.Program
		p := tea.NewProgram(testModel{cmds: []tea.Cmd{seqCmd}})
		if _, err := p.Run(); err != nil {
			t.Fatal(err)
		}

		<-done
		assert.Equal(t, []int{0, 1, 2}, order, "commands should execute in sequence")
	})
}

type testModel struct {
	cmds []tea.Cmd
}

func (m testModel) Init() tea.Cmd {
	if len(m.cmds) > 0 {
		return m.cmds[0]
	}
	return nil
}

func (m testModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.QuitMsg:
		return m, nil
	default:
		if len(m.cmds) > 0 {
			return m, tea.Quit
		}
		return m, nil
	}
}

func (m testModel) View() string {
	return ""
}
func TestStandardCommands(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	mod := l.NewTable()
	RegisterCmd(l, mod)

	cmdTbl := l.GetField(mod, "commands").(*lua.LTable)

	standardCmds := []string{
		"clear_screen",
		"enter_alt_screen",
		"exit_alt_screen",
		"enable_mouse_cell_motion",
		"enable_mouse_all_motion",
		"disable_mouse",
		"hide_cursor",
		"show_cursor",
		"enable_bracketed_paste",
		"disable_bracketed_paste",
		"enable_report_focus",
		"disable_report_focus",
		"quit",
		"suspend",
		"window_size",
	}

	for _, cmdName := range standardCmds {
		t.Run(cmdName, func(t *testing.T) {
			cmd := cmdTbl.RawGetString(cmdName)
			require.NotNil(t, cmd, "command should exist")

			wrapper := cmd.(*lua.LUserData).Value.(*CmdWrapper)
			msg := wrapper.cmd()
			require.NotNil(t, msg, "command should return a message")
		})
	}

	t.Run("set_window_title", func(t *testing.T) {
		titleFunc := l.GetField(cmdTbl, "set_window_title").(*lua.LFunction)
		l.Push(lua.LString("Test Title"))
		result := titleFunc.GFunction(l)
		require.Equal(t, 1, result)

		wrapper := l.Get(-1).(*lua.LUserData).Value.(*CmdWrapper)
		msg := wrapper.cmd()
		require.NotNil(t, msg)
	})
}

func TestCommandEdgeCases(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	mod := l.NewTable()
	RegisterCmd(l, mod)

	t.Run("empty batch", func(t *testing.T) {
		batchFunc := l.GetField(mod, "batch").(*lua.LFunction)
		l.Push(l.NewTable())
		result := batchFunc.GFunction(l)
		require.Equal(t, 0, result)
	})

	t.Run("empty sequence", func(t *testing.T) {
		seqFunc := l.GetField(mod, "sequence").(*lua.LFunction)
		l.Push(l.NewTable())
		result := seqFunc.GFunction(l)
		require.Equal(t, 0, result)
	})

	t.Run("nil command", func(t *testing.T) {
		wrapped := WrapCommand(l, nil)
		require.NotNil(t, wrapped)
		require.NotNil(t, wrapped.Value)
		wrapper := wrapped.Value.(*CmdWrapper)
		require.Nil(t, wrapper.cmd)
	})
}
