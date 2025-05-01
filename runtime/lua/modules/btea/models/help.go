package models

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	lua "github.com/yuin/gopher-lua"
)

// Help wraps help.Model for Lua
type Help struct {
	model help.Model
}

func (h *Help) Init() tea.Cmd {
	return nil
}

func (h *Help) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	h.model, cmd = h.model.Update(msg)
	return h, cmd
}

func (h *Help) View() string {
	return ""
}

// luaKeyMap implements help.KeyMap interface for Lua tables
type luaKeyMap struct {
	l        *lua.LState
	keymap   lua.LValue
	bindings []key.Binding
	groups   [][]key.Binding
}

func (lk *luaKeyMap) ShortHelp() []key.Binding {
	// Try to get short_help field using engine.GetField
	if fieldValue, ok := value.GetField(lk.l, lk.keymap, "short_help"); ok {
		switch v := fieldValue.(type) {
		case *lua.LFunction:
			// If it's a function, call it
			lk.l.Push(v)
			if err := lk.l.PCall(0, 1, nil); err != nil {
				return nil
			}
			ret := lk.l.Get(-1)
			lk.l.Pop(1)
			return lk.processBindingsTable(ret)

		case *lua.LTable:
			// If it's directly a table, process it
			return lk.processBindingsTable(v)
		}
	}

	return lk.bindings
}

func (lk *luaKeyMap) FullHelp() [][]key.Binding {
	// Try to get full_help field using engine.GetField
	if fieldValue, ok := value.GetField(lk.l, lk.keymap, "full_help"); ok {
		switch v := fieldValue.(type) {
		case *lua.LFunction:
			// If it's a function, call it
			lk.l.Push(v)
			if err := lk.l.PCall(0, 1, nil); err != nil {
				return nil
			}
			ret := lk.l.Get(-1)
			lk.l.Pop(1)
			return lk.processBindingGroups(ret)

		case *lua.LTable:
			// If it's directly a table, process it
			return lk.processBindingGroups(v)
		}
	}

	return lk.groups
}

// processBindingsTable converts a Lua table of key bindings to a Go slice
func (lk *luaKeyMap) processBindingsTable(value lua.LValue) []key.Binding {
	if tbl, ok := value.(*lua.LTable); ok {
		bindings := make([]key.Binding, 0)
		tbl.ForEach(func(_, v lua.LValue) {
			if b, ok := protocol.ToGoKeyBinding(v); ok {
				bindings = append(bindings, b)
			}
		})
		lk.bindings = bindings
		return bindings
	}
	return nil
}

// processBindingGroups converts a Lua table of binding groups to a Go slice of slices
func (lk *luaKeyMap) processBindingGroups(value lua.LValue) [][]key.Binding {
	if tbl, ok := value.(*lua.LTable); ok {
		groups := make([][]key.Binding, 0)
		tbl.ForEach(func(_, v lua.LValue) {
			if groupTbl, ok := v.(*lua.LTable); ok {
				group := make([]key.Binding, 0)
				groupTbl.ForEach(func(_, b lua.LValue) {
					if binding, ok := protocol.ToGoKeyBinding(b); ok {
						group = append(group, binding)
					}
				})
				if len(group) > 0 {
					groups = append(groups, group)
				}
			}
		})
		lk.groups = groups
		return groups
	}
	return nil
}

// RegisterHelp registers the help component
func RegisterHelp(l *lua.LState, mod *lua.LTable) {
	// Spawn and register the help metatable
	mt := l.NewTypeMetatable("btea.Help")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"update":         helpUpdate,
		"view":           helpView,
		"set_width":      helpSetWidth,
		"set_show_all":   helpSetShowAll,
		"set_styles":     helpSetStyles,
		"set_separators": helpSetSeparators,
		"set_ellipsis":   helpSetEllipsis,
		"get_full_help":  helpGetFullHelp,
		"get_short_help": helpGetShortHelp,
	}))

	// Register constructor
	l.SetField(mod, "help", l.NewFunction(newHelp))
}

func newHelp(l *lua.LState) int {
	opts := l.CheckTable(1)

	h := &Help{
		model: help.New(),
	}

	// Process options
	opts.ForEach(func(k, v lua.LValue) {
		switch k.String() {
		case "width":
			h.model.Width = int(lua.LVAsNumber(v))
		case "show_all":
			h.model.ShowAll = lua.LVAsBool(v)
		case "short_separator":
			h.model.ShortSeparator = lua.LVAsString(v)
		case "full_separator":
			h.model.FullSeparator = lua.LVAsString(v)
		case "ellipsis":
			h.model.Ellipsis = lua.LVAsString(v)
		case "styles":
			if styleTbl, ok := v.(*lua.LTable); ok {
				h.setStylesFromTable(l, styleTbl)
			}
		}
	})

	ud := l.NewUserData()
	ud.Value = h
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Help"))
	l.Push(ud)
	return 1
}

func CheckHelp(l *lua.LState) *Help {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Help); ok {
		return v
	}
	l.ArgError(1, "help expected")
	return nil
}

// Help methods

func helpUpdate(l *lua.LState) int {
	h := CheckHelp(l)
	if h == nil {
		return 0
	}

	msgValue := l.CheckAny(2)
	msg, err := protocol.LuaToMsg(msgValue)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}

	var cmd tea.Cmd
	h.model, cmd = h.model.Update(msg)

	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func helpView(l *lua.LState) int {
	h := CheckHelp(l)
	if h == nil {
		return 0
	}

	keymap := l.CheckAny(2)
	var helpStr string

	switch v := keymap.(type) {
	case *lua.LTable:
		// Handle Lua table implementing KeyMap interface
		lkm := &luaKeyMap{
			l:      l,
			keymap: v,
		}

		// Call the appropriate view based on ShowAll
		if h.model.ShowAll {
			helpStr = h.model.FullHelpView(lkm.FullHelp())
		} else {
			helpStr = h.model.ShortHelpView(lkm.ShortHelp())
		}
	case *lua.LUserData:
		// Try to get underlying model that implements KeyMap
		if km, ok := v.Value.(help.KeyMap); ok {
			helpStr = h.model.View(km)
		} else if wrapper, ok := v.Value.(interface{ Model() interface{} }); ok {
			if km, ok := wrapper.Model().(help.KeyMap); ok {
				helpStr = h.model.View(km)
			}
		}
	}

	l.Push(lua.LString(helpStr))
	return 1
}

func (h *Help) setStylesFromTable(_ *lua.LState, styles *lua.LTable) {
	styles.ForEach(func(k, v lua.LValue) {
		if style, ok := v.(*lua.LUserData); ok {
			if s, ok := style.Value.(*render.Style); ok {
				switch k.String() {
				case "short_key":
					h.model.Styles.ShortKey = s.Style
				case "short_desc":
					h.model.Styles.ShortDesc = s.Style
				case "short_separator":
					h.model.Styles.ShortSeparator = s.Style
				case "full_key":
					h.model.Styles.FullKey = s.Style
				case "full_desc":
					h.model.Styles.FullDesc = s.Style
				case "full_separator":
					h.model.Styles.FullSeparator = s.Style
				case "ellipsis":
					h.model.Styles.Ellipsis = s.Style
				}
			}
		}
	})
}

func helpSetStyles(l *lua.LState) int {
	h := CheckHelp(l)
	if h == nil {
		return 0
	}

	styles := l.CheckTable(2)
	h.setStylesFromTable(l, styles)
	return 0
}

func helpSetWidth(l *lua.LState) int {
	h := CheckHelp(l)
	if h == nil {
		return 0
	}
	width := l.CheckInt(2)
	h.model.Width = width
	return 0
}

func helpSetShowAll(l *lua.LState) int {
	h := CheckHelp(l)
	if h == nil {
		return 0
	}
	showAll := l.CheckBool(2)
	h.model.ShowAll = showAll
	return 0
}

func helpSetSeparators(l *lua.LState) int {
	h := CheckHelp(l)
	if h == nil {
		return 0
	}
	shortSep := l.CheckString(2)
	fullSep := l.OptString(3, "    ")
	h.model.ShortSeparator = shortSep
	h.model.FullSeparator = fullSep
	return 0
}

func helpSetEllipsis(l *lua.LState) int {
	h := CheckHelp(l)
	if h == nil {
		return 0
	}
	ellipsis := l.CheckString(2)
	h.model.Ellipsis = ellipsis
	return 0
}

func helpGetShortHelp(l *lua.LState) int {
	h := CheckHelp(l)
	if h == nil {
		return 0
	}

	keymap := l.CheckAny(2)
	var bindings []key.Binding

	switch v := keymap.(type) {
	case *lua.LTable:
		lkm := &luaKeyMap{l: l, keymap: v}
		bindings = lkm.ShortHelp()
	case *lua.LUserData:
		if km, ok := v.Value.(help.KeyMap); ok {
			bindings = km.ShortHelp()
		} else if wrapper, ok := v.Value.(interface{ Model() interface{} }); ok {
			if km, ok := wrapper.Model().(help.KeyMap); ok {
				bindings = km.ShortHelp()
			}
		}
	}

	tbl := l.NewTable()
	for i, b := range bindings {
		tbl.RawSetInt(i+1, protocol.ToLuaKeyBinding(l, b))
	}
	l.Push(tbl)
	return 1
}

func helpGetFullHelp(l *lua.LState) int {
	h := CheckHelp(l)
	if h == nil {
		return 0
	}

	keymap := l.CheckAny(2)
	var groups [][]key.Binding

	switch v := keymap.(type) {
	case *lua.LTable:
		lkm := &luaKeyMap{l: l, keymap: v}
		groups = lkm.FullHelp()
	case *lua.LUserData:
		if km, ok := v.Value.(help.KeyMap); ok {
			groups = km.FullHelp()
		} else if wrapper, ok := v.Value.(interface{ Model() interface{} }); ok {
			if km, ok := wrapper.Model().(help.KeyMap); ok {
				groups = km.FullHelp()
			}
		}
	}

	tbl := l.NewTable()
	for i, group := range groups {
		groupTbl := l.NewTable()
		for j, b := range group {
			groupTbl.RawSetInt(j+1, protocol.ToLuaKeyBinding(l, b))
		}
		tbl.RawSetInt(i+1, groupTbl)
	}
	l.Push(tbl)
	return 1
}
