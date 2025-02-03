package models

import (
	"fmt"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	lua "github.com/yuin/gopher-lua"
)

// TextArea wraps our custom Model for Lua.
type TextArea struct {
	model textarea.Model
}

// RegisterTextArea registers the text area component to Lua.
func RegisterTextArea(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("btea.TextArea")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"update":    textAreaUpdate,
		"view":      textAreaView,
		"set_value": textAreaSetValue,
		"value":     textAreaValue,
		"focus":     textAreaFocus,
		"blur":      textAreaBlur,
		// Additional methods as needed.
	}))

	l.SetField(mod, "new_text_area", l.NewFunction(newTextArea))
}

// newTextArea is the Lua constructor for a text area.
// Supported options (passed as a Lua table):
//   - prompt: string
//   - placeholder: string
//   - value: string
//   - width: number
//   - height: number
//   - char_limit: number
//   - show_line_numbers: boolean
//   - focused_style: table or btea.Style userdata
//   - blurred_style: table or btea.Style userdata
//   - key_map: table of key binding userdatas (optional)
func newTextArea(l *lua.LState) int {
	opts := l.CheckTable(1)
	// Create a new Model (assume New() is defined in your package)
	m := textarea.New()

	// Process basic options.
	if lv := opts.RawGetString("prompt"); lv != lua.LNil {
		m.Prompt = lua.LVAsString(lv)
	}
	if lv := opts.RawGetString("placeholder"); lv != lua.LNil {
		m.Placeholder = lua.LVAsString(lv)
	}
	if lv := opts.RawGetString("value"); lv != lua.LNil {
		m.SetValue(lua.LVAsString(lv))
	}
	if lv := opts.RawGetString("width"); lv != lua.LNil {
		m.SetWidth(int(lua.LVAsNumber(lv)))
	}
	if lv := opts.RawGetString("height"); lv != lua.LNil {
		m.SetHeight(int(lua.LVAsNumber(lv)))
	}
	if lv := opts.RawGetString("char_limit"); lv != lua.LNil {
		m.CharLimit = int(lua.LVAsNumber(lv))
	}
	if lv := opts.RawGetString("show_line_numbers"); lv != lua.LNil {
		m.ShowLineNumbers = lua.LVAsBool(lv)
	}

	// Process focused_style.
	if lv := opts.RawGetString("focused_style"); lv != lua.LNil {
		var style textarea.Style
		switch v := lv.(type) {
		case *lua.LTable:
			var err error
			style, err = luaTableToStyle(v)
			if err != nil {
				l.RaiseError("focused_style error: %v", err)
			}
		default:
			l.RaiseError("focused_style must be a table or userdata")
		}
		m.FocusedStyle = style
	}

	// Process blurred_style.
	if lv := opts.RawGetString("blurred_style"); lv != lua.LNil {
		var style textarea.Style
		switch v := lv.(type) {
		case *lua.LTable:
			var err error
			style, err = luaTableToStyle(v)
			if err != nil {
				l.RaiseError("blurred_style error: %v", err)
			}
		default:
			l.RaiseError("blurred_style must be a table or userdata")
		}
		m.BlurredStyle = style
	}

	// Process custom key map if provided.
	if lv := opts.RawGetString("key_map"); lv != lua.LNil {
		if tbl, ok := lv.(*lua.LTable); ok {
			m.KeyMap = processTextareaKeyMap(tbl)
		}
	}

	ta := &TextArea{model: m}
	ud := l.NewUserData()
	ud.Value = ta
	l.SetMetatable(ud, l.GetTypeMetatable("btea.TextArea"))
	l.Push(ud)
	return 1
}

// luaTableToStyle converts a Lua table to a Style value.
// It expects the table to have keys: "base", "cursor_line", "cursor_line_number",
// "end_of_buffer", "line_number", "placeholder", "prompt", and "text".
// Each value should be a btea.Style userdata.
func luaTableToStyle(tbl *lua.LTable) (textarea.Style, error) {
	var s textarea.Style
	// Helper function for mapping a key.
	mapField := func(key string, dest *lipgloss.Style) error {
		lv := tbl.RawGetString(key)
		if lv == lua.LNil {
			// Leave dest as zero value if not provided.
			return nil
		}
		ud, ok := lv.(*lua.LUserData)
		if !ok {
			return fmt.Errorf("expected userdata for %s", key)
		}
		style, ok := ud.Value.(*render.Style)
		if !ok {
			return fmt.Errorf("expected btea.Style for %s", key)
		}
		*dest = style.Style
		return nil
	}

	if err := mapField("base", &s.Base); err != nil {
		return s, err
	}
	if err := mapField("cursor_line", &s.CursorLine); err != nil {
		return s, err
	}
	if err := mapField("cursor_line_number", &s.CursorLineNumber); err != nil {
		return s, err
	}
	if err := mapField("end_of_buffer", &s.EndOfBuffer); err != nil {
		return s, err
	}
	if err := mapField("line_number", &s.LineNumber); err != nil {
		return s, err
	}
	if err := mapField("placeholder", &s.Placeholder); err != nil {
		return s, err
	}
	if err := mapField("prompt", &s.Prompt); err != nil {
		return s, err
	}
	if err := mapField("text", &s.Text); err != nil {
		return s, err
	}
	return s, nil
}

func checkTextArea(l *lua.LState) *TextArea {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*TextArea); ok {
		return v
	}
	l.ArgError(1, "TextArea expected")
	return nil
}

func textAreaUpdate(l *lua.LState) int {
	ta := checkTextArea(l)
	msgLV := l.CheckAny(2)
	msg, err := protocol.LuaToMsg(msgLV)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}
	var cmd tea.Cmd
	ta.model, cmd = ta.model.Update(msg)
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func textAreaView(l *lua.LState) int {
	ta := checkTextArea(l)
	l.Push(lua.LString(ta.model.View()))
	return 1
}

func textAreaSetValue(l *lua.LState) int {
	ta := checkTextArea(l)
	value := l.CheckString(2)
	ta.model.SetValue(value)
	return 0
}

func textAreaValue(l *lua.LState) int {
	ta := checkTextArea(l)
	l.Push(lua.LString(ta.model.Value()))
	return 1
}

func textAreaFocus(l *lua.LState) int {
	ta := checkTextArea(l)
	cmd := ta.model.Focus()
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func textAreaBlur(l *lua.LState) int {
	ta := checkTextArea(l)
	ta.model.Blur()
	return 0
}

// processTextareaKeyMap converts a Lua table into a KeyMap.
func processTextareaKeyMap(tbl *lua.LTable) textarea.KeyMap {
	keyMap := textarea.DefaultKeyMap
	bindingMap := map[string]*key.Binding{
		"character_forward":         &keyMap.CharacterForward,
		"character_backward":        &keyMap.CharacterBackward,
		"word_forward":              &keyMap.WordForward,
		"word_backward":             &keyMap.WordBackward,
		"delete_character_backward": &keyMap.DeleteCharacterBackward,
		"delete_character_forward":  &keyMap.DeleteCharacterForward,
		"insert_newline":            &keyMap.InsertNewline,
		// Add additional bindings as needed.
	}
	for field, bindingPtr := range bindingMap {
		if lv := tbl.RawGetString(field); lv != lua.LNil {
			if ud, ok := lv.(*lua.LUserData); ok {
				if b, ok := ud.Value.(*protocol.KeyBinding); ok {
					*bindingPtr = b.Binding
				} else {
					fmt.Printf("Expected btea.KeyBinding for field %s\n", field)
				}
			}
		}
	}
	return keyMap
}
