package btea

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	lua "github.com/yuin/gopher-lua"
)

// TextInput wraps textinput.Model for Lua
type TextInput struct {
	model textinput.Model
}

// RegisterTextInput registers the textinput component
func RegisterTextInput(l *lua.LState, mod *lua.LTable) {
	// Create and register the textinput metatable
	mt := l.NewTypeMetatable("btea.TextInput")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"focus":          textInputFocus,
		"blur":           textInputBlur,
		"update":         textInputUpdate,
		"view":           textInputView,
		"set_value":      textInputSetValue,
		"value":          textInputGetValue,
		"placeholder":    textInputSetPlaceholder,
		"set_char_limit": textInputSetCharLimit,
		"set_width":      textInputSetWidth,
	}))

	// Register constructor
	l.SetField(mod, "new_textinput", l.NewFunction(newTextInput))
}

// Create a new textinput instance
func newTextInput(l *lua.LState) int {
	// Create new textinput model
	model := textinput.New()

	// Create Lua userdata
	ud := l.NewUserData()
	ud.Value = &TextInput{model: model}

	// Set metatable
	l.SetMetatable(ud, l.GetTypeMetatable("btea.TextInput"))

	// Push the userdata onto the stack
	l.Push(ud)
	return 1
}

// Helper to get TextInput from Lua userdata
func checkTextInput(l *lua.LState) *TextInput {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*TextInput); ok {
		return v
	}
	l.ArgError(1, "textinput expected")
	return nil
}

// TextInput methods

func textInputFocus(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	ti.model.Focus()
	return 0
}

func textInputBlur(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	ti.model.Blur()
	return 0
}

func textInputUpdate(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}

	// Get message table from argument
	msgValue := l.CheckAny(2)
	teaMsg, err := FromLua(msgValue)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}

	// Update model
	var cmd tea.Cmd
	ti.model, cmd = ti.model.Update(teaMsg)

	// Return command message if any
	if cmd != nil {
		// todo: push cmd upward!
	}

	return 0
}

func textInputView(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}

	l.Push(lua.LString(ti.model.View()))

	return 1
}

func textInputSetValue(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	value := l.CheckString(2)
	ti.model.SetValue(value)
	return 0
}

func textInputGetValue(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	l.Push(lua.LString(ti.model.Value()))
	return 1
}

func textInputSetPlaceholder(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	placeholder := l.CheckString(2)
	ti.model.Placeholder = placeholder
	return 0
}

func textInputSetCharLimit(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	limit := l.CheckInt(2)
	ti.model.CharLimit = limit
	return 0
}

func textInputSetWidth(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	width := l.CheckInt(2)
	ti.model.Width = width
	return 0
}
