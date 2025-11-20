package models

import (
	"errors"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/wippyai/runtime/runtime/lua/modules/btea/protocol"
	"github.com/wippyai/runtime/runtime/lua/modules/btea/render"
	lua "github.com/yuin/gopher-lua"
)

// TextInput wraps textinput.Model with validation
type TextInput struct {
	model    textinput.Model
	validate *lua.LFunction // Store Lua validation function
	luaState *lua.LState    // Keep reference to Lua state for validation
}

func (ti *TextInput) Init() tea.Cmd {
	return nil
}

func (ti *TextInput) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	newModel, cmd := ti.model.Update(msg)

	// Spawn new instance with updated model
	return &TextInput{
		model:    newModel,
		validate: ti.validate,
		luaState: ti.luaState,
	}, cmd
}

func (ti *TextInput) View() string {
	return ti.model.View()
}

const (
	EchoNormal   = "normal"
	EchoPassword = "password"
	EchoNone     = "none"
)

// validateWrapper converts Lua validation function to Go
func (ti *TextInput) validateWrapper(s string) error {
	if ti.validate == nil {
		return nil
	}

	err := ti.luaState.CallByParam(lua.P{
		Fn:      ti.validate,
		NRet:    1,
		Protect: true,
	}, lua.LString(s))

	if err != nil {
		return err
	}

	ret := ti.luaState.Get(-1)
	ti.luaState.Pop(1)

	if retStr, ok := ret.(lua.LString); ok {
		return errors.New(retStr.String())
	}

	return nil
}

// RegisterTextInput registers the textinput component
func RegisterTextInput(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("btea.TextInput")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		// Core methods
		"update": textInputUpdate,
		"view":   textInputView,
		"focus":  textInputFocus,
		"blur":   textInputBlur,

		// Value management
		"value":     textInputValue,
		"set_value": textInputSetValue,
		"reset":     textInputReset,

		// Cursor control
		"position":     textInputPosition,
		"set_cursor":   textInputSetCursor,
		"cursor_start": textInputCursorStart,
		"cursor_end":   textInputCursorEnd,

		// Validation
		"is_valid":     textInputIsValid,
		"error":        textInputError,
		"set_validate": textInputSetValidate,

		// Configuration
		"set_placeholder": textInputSetPlaceholder,
		"set_char_limit":  textInputSetCharLimit,
		"set_width":       textInputSetWidth,
		"set_prompt":      textInputSetPrompt,
		"set_style":       textInputSetStyle,

		// Suggestions
		"set_suggestions": textInputSetSuggestions,
		"get_suggestions": textInputGetSuggestions,
	}))

	// Register echo modes
	l.SetField(mod, "ECHO_NORMAL", lua.LString(EchoNormal))
	l.SetField(mod, "ECHO_PASSWORD", lua.LString(EchoPassword))
	l.SetField(mod, "ECHO_NONE", lua.LString(EchoNone))

	// Register constructor
	l.SetField(mod, "text_input", l.NewFunction(newTextInput))
}

func newTextInput(l *lua.LState) int {
	opts := l.CheckTable(1)
	model := textinput.New()
	ti := &TextInput{luaState: l}

	// Process all options in a single pass
	opts.ForEach(func(k, v lua.LValue) {
		switch k.String() {
		case "prompt":
			model.Prompt = lua.LVAsString(v)
		case "placeholder":
			model.Placeholder = lua.LVAsString(v)
		case "value":
			model.SetValue(lua.LVAsString(v))
		case "char_limit":
			model.CharLimit = int(lua.LVAsNumber(v))
		case "width":
			model.Width = int(lua.LVAsNumber(v))
		case "echo_mode":
			switch lua.LVAsString(v) {
			case EchoPassword:
				model.EchoMode = textinput.EchoPassword
			case EchoNone:
				model.EchoMode = textinput.EchoNone
			default:
				model.EchoMode = textinput.EchoNormal
			}
		case "echo_character":
			if s := lua.LVAsString(v); len(s) > 0 {
				model.EchoCharacter = []rune(s)[0]
			}
		case "validate":
			if fn, ok := v.(*lua.LFunction); ok {
				ti.validate = fn
				model.Validate = ti.validateWrapper
			}
		case "show_suggestions":
			model.ShowSuggestions = lua.LVAsBool(v)
		case "suggestions":
			if tbl, ok := v.(*lua.LTable); ok {
				var suggestions []string
				tbl.ForEach(func(_, v lua.LValue) {
					suggestions = append(suggestions, lua.LVAsString(v))
				})
				model.SetSuggestions(suggestions)
			}
		case "prompt_style":
			if s, ok := v.(*lua.LUserData); ok {
				if style, ok := s.Value.(*render.Style); ok {
					model.PromptStyle = style.Style
				}
			}
		case "text_style":
			if s, ok := v.(*lua.LUserData); ok {
				if style, ok := s.Value.(*render.Style); ok {
					model.TextStyle = style.Style
				}
			}
		case "placeholder_style":
			if s, ok := v.(*lua.LUserData); ok {
				if style, ok := s.Value.(*render.Style); ok {
					model.PlaceholderStyle = style.Style
				}
			}
		case "completion_style":
			if s, ok := v.(*lua.LUserData); ok {
				if style, ok := s.Value.(*render.Style); ok {
					model.CompletionStyle = style.Style
				}
			}
		case "cursor_style":
			if s, ok := v.(*lua.LUserData); ok {
				if style, ok := s.Value.(*render.Style); ok {
					model.Cursor.Style = style.Style
				}
			}
		case "blink_speed":
			d, err := protocol.ParseDuration(v)
			if err == nil {
				model.Cursor.BlinkSpeed = d
			}

		case "key_map":
			model.KeyMap = processInputKeyMap(v)
		}
	})

	ti.model = model

	ud := l.NewUserData()
	ud.Value = ti
	l.SetMetatable(ud, l.GetTypeMetatable("btea.TextInput"))
	l.Push(ud)
	return 1
}

func checkTextInput(l *lua.LState) *TextInput {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*TextInput); ok {
		return v
	}
	l.ArgError(1, "text input expected")
	return nil
}

// Core methods
func textInputUpdate(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}

	msgValue := l.CheckAny(2)
	teaMsg, err := protocol.LuaToMsg(msgValue)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}

	newModel, cmd := ti.model.Update(teaMsg)
	ti.model = newModel // we are consistently mutable

	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
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

func textInputFocus(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	cmd := ti.model.Focus()
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
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

// Value and validation methods
func textInputValue(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	l.Push(lua.LString(ti.model.Value()))
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

func textInputIsValid(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	l.Push(lua.LBool(ti.model.Err == nil))
	return 1
}

func textInputError(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	if ti.model.Err != nil {
		l.Push(lua.LString(ti.model.Err.Error()))
		return 1
	}
	l.Push(lua.LNil)
	return 1
}

func textInputSetValidate(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	fn := l.CheckFunction(2)
	ti.validate = fn
	ti.model.Validate = ti.validateWrapper
	return 0
}

// Cursor methods
func textInputPosition(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	l.Push(lua.LNumber(ti.model.Position()))
	return 1
}

func textInputSetCursor(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	pos := l.CheckInt(2)
	ti.model.SetCursor(pos)
	return 0
}

func textInputCursorStart(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	ti.model.CursorStart()
	return 0
}

func textInputCursorEnd(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	ti.model.CursorEnd()
	return 0
}

// Configuration methods
func textInputSetPlaceholder(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	placeholder := l.CheckString(2)
	ti.model.Placeholder = placeholder
	return 0
}

func textInputSetPrompt(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	prompt := l.CheckString(2)
	ti.model.Prompt = prompt
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

func textInputSetStyle(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}

	styleType := l.CheckString(2)
	style := render.CheckStyle(l, 3)
	if style == nil {
		return 0
	}

	switch styleType {
	case "prompt":
		ti.model.PromptStyle = style.Style
	case "text":
		ti.model.TextStyle = style.Style
	case "placeholder":
		ti.model.PlaceholderStyle = style.Style
	default:
		l.ArgError(2, "invalid style type: must be prompt, text, or placeholder")
	}
	return 0
}

func textInputReset(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	ti.model.Reset()
	return 0
}

// Suggestion methods
func textInputSetSuggestions(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}
	tbl := l.CheckTable(2)
	suggestions := make([]string, 0)

	tbl.ForEach(func(_, v lua.LValue) {
		if str, ok := v.(lua.LString); ok {
			suggestions = append(suggestions, string(str))
		}
	})

	ti.model.SetSuggestions(suggestions)
	return 0
}

func textInputGetSuggestions(l *lua.LState) int {
	ti := checkTextInput(l)
	if ti == nil {
		return 0
	}

	suggestions := ti.model.AvailableSuggestions()
	tbl := l.NewTable()

	for i, s := range suggestions {
		tbl.RawSetInt(i+1, lua.LString(s))
	}

	l.Push(tbl)
	return 1
}

func processInputKeyMap(keyMapValue lua.LValue) textinput.KeyMap {
	keyMap := textinput.DefaultKeyMap

	// If no key map provided, return default
	keyMapTable, ok := keyMapValue.(*lua.LTable)
	if !ok {
		return keyMap
	}

	// Map of field names to their binding pointers
	bindingPtrs := map[string]*key.Binding{
		"character_forward":         &keyMap.CharacterForward,
		"character_backward":        &keyMap.CharacterBackward,
		"word_forward":              &keyMap.WordForward,
		"word_backward":             &keyMap.WordBackward,
		"delete_word_backward":      &keyMap.DeleteWordBackward,
		"delete_word_forward":       &keyMap.DeleteWordForward,
		"delete_after_cursor":       &keyMap.DeleteAfterCursor,
		"delete_before_cursor":      &keyMap.DeleteBeforeCursor,
		"delete_character_backward": &keyMap.DeleteCharacterBackward,
		"delete_character_forward":  &keyMap.DeleteCharacterForward,
		"line_start":                &keyMap.LineStart,
		"line_end":                  &keyMap.LineEnd,
		"paste":                     &keyMap.Paste,
		"accept_suggestion":         &keyMap.AcceptSuggestion,
		"next_suggestion":           &keyMap.NextSuggestion,
		"prev_suggestion":           &keyMap.PrevSuggestion,
	}

	// Process each binding field
	for fieldName, bindingPtr := range bindingPtrs {
		if fieldValue := keyMapTable.RawGetString(fieldName); fieldValue != lua.LNil {
			if ud, ok := fieldValue.(*lua.LUserData); ok {
				if b, ok := ud.Value.(*protocol.KeyBinding); ok {
					*bindingPtr = b.Binding
				}
			}
		}
	}

	return keyMap
}
