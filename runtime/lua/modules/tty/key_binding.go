package tty

import (
	"strings"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const keyBindingTypeName = "tty.KeyBinding"

func init() {
	value.RegisterTypeMethods(nil, keyBindingTypeName,
		map[string]lua.LGoFunc{"__tostring": keyBindingToString},
		map[string]lua.LGoFunc{
			"matches":     keyBindingMatches,
			"set_enabled": keyBindingSetEnabled,
			"is_enabled":  keyBindingIsEnabled,
			"help":        keyBindingHelp,
		})
}

type keyBinding struct {
	keys     []string
	helpKey  string
	helpDesc string
	enabled  bool
}

func checkKeyBinding(l *lua.LState) *keyBinding {
	ud := l.CheckUserData(1)
	if kb, ok := ud.Value.(*keyBinding); ok {
		return kb
	}
	l.ArgError(1, "tty.KeyBinding expected")
	return nil
}

// ttyBind creates a key binding from a table:
// tty.bind({keys = {"q", "ctrl+c"}, help = {key = "q", desc = "quit"}})
func ttyBind(l *lua.LState) int {
	tbl := l.CheckTable(1)

	kb := &keyBinding{enabled: true}

	// Read keys
	keysVal := tbl.RawGetString("keys")
	if keysTbl, ok := keysVal.(*lua.LTable); ok {
		keysTbl.ForEach(func(_, v lua.LValue) {
			if s, ok := v.(lua.LString); ok {
				kb.keys = append(kb.keys, string(s))
			}
		})
	}

	// Read help
	helpVal := tbl.RawGetString("help")
	if helpTbl, ok := helpVal.(*lua.LTable); ok {
		if v := helpTbl.RawGetString("key"); v != lua.LNil {
			kb.helpKey = v.String()
		}
		if v := helpTbl.RawGetString("desc"); v != lua.LNil {
			kb.helpDesc = v.String()
		}
	}

	value.PushTypedUserData(l, kb, keyBindingTypeName)
	return 1
}

func keyBindingToString(l *lua.LState) int {
	kb := checkKeyBinding(l)
	if kb == nil {
		return 0
	}
	l.Push(lua.LString("tty.KeyBinding{" + strings.Join(kb.keys, ", ") + "}"))
	return 1
}

// keyBindingMatches checks if an event table matches this binding.
// event must have: type="key", key or key_type fields, and modifier fields.
func keyBindingMatches(l *lua.LState) int {
	kb := checkKeyBinding(l)
	if kb == nil {
		return 0
	}

	if !kb.enabled {
		l.Push(lua.LFalse)
		return 1
	}

	eventTbl := l.CheckTable(2)

	// Only match key events
	evType := eventTbl.RawGetString("type")
	if evType.String() != "key" {
		l.Push(lua.LFalse)
		return 1
	}

	evKey := eventTbl.RawGetString("key").String()
	evKeyType := eventTbl.RawGetString("key_type").String()
	evCtrl := lua.LVAsBool(eventTbl.RawGetString("ctrl"))
	evAlt := lua.LVAsBool(eventTbl.RawGetString("alt"))
	evShift := lua.LVAsBool(eventTbl.RawGetString("shift"))

	// Build the keystroke string from the event
	keystroke := buildKeystroke(evKey, evKeyType, evCtrl, evAlt, evShift)

	for _, bindKey := range kb.keys {
		if bindKey == keystroke {
			l.Push(lua.LTrue)
			return 1
		}
		// Also match against plain key name for simple bindings
		if bindKey == evKey || bindKey == evKeyType {
			l.Push(lua.LTrue)
			return 1
		}
	}

	l.Push(lua.LFalse)
	return 1
}

func keyBindingSetEnabled(l *lua.LState) int {
	kb := checkKeyBinding(l)
	if kb == nil {
		return 0
	}
	kb.enabled = l.CheckBool(2)
	l.Push(l.Get(1))
	return 1
}

func keyBindingIsEnabled(l *lua.LState) int {
	kb := checkKeyBinding(l)
	if kb == nil {
		return 0
	}
	l.Push(lua.LBool(kb.enabled))
	return 1
}

func keyBindingHelp(l *lua.LState) int {
	kb := checkKeyBinding(l)
	if kb == nil {
		return 0
	}
	tbl := l.CreateTable(0, 2)
	tbl.RawSetString("key", lua.LString(kb.helpKey))
	tbl.RawSetString("desc", lua.LString(kb.helpDesc))
	l.Push(tbl)
	return 1
}

// buildKeystroke constructs a keystroke string like "ctrl+c" from event fields.
func buildKeystroke(key, keyType string, ctrl, alt, shift bool) string {
	var sb strings.Builder
	if ctrl {
		sb.WriteString("ctrl+")
	}
	if alt {
		sb.WriteString("alt+")
	}
	if shift {
		sb.WriteString("shift+")
	}

	// Use key_type for special keys, key for printable chars
	if keyType != "runes" && keyType != "" && keyType != "unknown" {
		sb.WriteString(keyType)
	} else {
		sb.WriteString(key)
	}

	return sb.String()
}
