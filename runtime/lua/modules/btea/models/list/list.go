package list

import (
	"fmt"
	"github.com/charmbracelet/bubbles/list"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
)

// Item is an item that appears in the list.
type Item interface {
	FilterValue() string
}

// List wraps list.Model for Lua
type List struct {
	model    list.Model
	delegate *lua.LTable
	items    []*lua.LUserData
	luaState *lua.LState
}

// RegisterList registers the list to the Lua state
func RegisterList(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("btea.List")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		// Core methods
		"update": listUpdate,
		"view":   listView,

		// Item management
		"items":            listItems,
		"set_items":        listSetItems,
		"set_item":         listSetItem,
		"insert_item":      listInsertItem,
		"remove_item":      listRemoveItem,
		"selected_item":    listSelectedItem,
		"matches_for_item": listMatchesForItem,

		// Navigation
		"cursor_up":      listCursorUp,
		"cursor_down":    listCursorDown,
		"prev_page":      listPrevPage,
		"next_page":      listNextPage,
		"select":         listSelect,
		"reset_selected": listResetSelected,

		// Filtering
		"filter_state":   listFilterState,
		"filter_value":   listFilterValue,
		"setting_filter": listSettingFilter,
		"is_filtered":    listIsFiltered,
		"reset_filter":   listResetFilter,

		// Display control
		"set_width":                listSetWidth,
		"set_height":               listSetHeight,
		"set_show_title":           listSetShowTitle,
		"set_show_filter":          listSetShowFilter,
		"set_show_status_bar":      listSetShowStatusBar,
		"set_show_pagination":      listSetShowPagination,
		"set_show_help":            listSetShowHelp,
		"set_status_bar_item_name": listSetStatusBarItemName,
		"disable_quit_keybindings": listDisableQuitKeybindings,

		// Spinner
		"start_spinner":  listStartSpinner,
		"stop_spinner":   listStopSpinner,
		"toggle_spinner": listToggleSpinner,

		// Status
		"new_status_message": listNewStatusMessage,
	}))

	l.SetField(mod, "new_list", l.NewFunction(newList))
}

// Core Methods

func listUpdate(l *lua.LState) int {
	li := CheckList(l)
	msg, err := protocol.LuaToMsg(l.Get(2))
	if err != nil {
		l.RaiseError("Error converting Lua value to message: %v", err)
	}

	newModel, cmd := li.model.Update(msg)
	li.model = newModel

	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}

	return 0
}

func listView(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LString(li.model.View()))
	return 1
}

// Item Management

func listItems(l *lua.LState) int {
	li := CheckList(l)
	itemsTable := l.NewTable()
	for _, itemUD := range li.items {
		itemsTable.Append(itemUD)
	}
	l.Push(itemsTable)
	return 1
}

func listSetItems(l *lua.LState) int {
	li := CheckList(l)
	itemsTable := l.CheckTable(2)
	var items []list.Item
	li.items = make([]*lua.LUserData, 0) // Clear existing items
	itemsTable.ForEach(func(_ lua.LValue, v lua.LValue) {
		if item, ok := v.(*lua.LTable); ok {
			itemUD := l.NewUserData()
			itemUD.Value = &LuaItem{luaItem: item, luaState: l}
			l.SetMetatable(itemUD, l.GetTypeMetatable("btea.Item"))
			items = append(items, itemUD.Value.(list.Item))
			li.items = append(li.items, itemUD)
		} else if itemUD, ok := v.(*lua.LUserData); ok {
			if _, ok := itemUD.Value.(list.Item); ok {
				items = append(items, itemUD.Value.(list.Item))
				li.items = append(li.items, itemUD)
			} else {
				l.RaiseError("Invalid item type")
			}
		} else {
			l.RaiseError("Invalid item type")
		}
	})

	cmd := li.model.SetItems(items)
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func listSetItem(l *lua.LState) int {
	li := CheckList(l)
	index := l.CheckInt(2)
	itemTable := l.CheckTable(3)

	itemUD := l.NewUserData()
	itemUD.Value = &LuaItem{luaItem: itemTable, luaState: l}
	l.SetMetatable(itemUD, l.GetTypeMetatable("btea.Item"))

	if index >= 0 && index < len(li.items) {
		li.items[index] = itemUD
	} else {
		l.RaiseError("Index out of range for listSetItem")
		return 0
	}

	cmd := li.model.SetItem(index, itemUD.Value.(list.Item))
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func listInsertItem(l *lua.LState) int {
	li := CheckList(l)
	index := l.CheckInt(2)
	itemTable := l.CheckTable(3)

	itemUD := l.NewUserData()
	itemUD.Value = &LuaItem{luaItem: itemTable, luaState: l}
	l.SetMetatable(itemUD, l.GetTypeMetatable("btea.Item"))

	if index >= 0 && index <= len(li.items) {
		li.items = append(li.items[:index], append([]*lua.LUserData{itemUD}, li.items[index:]...)...)
	} else {
		l.RaiseError("Index out of range for listInsertItem")
		return 0
	}

	cmd := li.model.InsertItem(index, itemUD.Value.(list.Item))
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func listRemoveItem(l *lua.LState) int {
	li := CheckList(l)
	index := l.CheckInt(2)

	if index >= 0 && index < len(li.items) {
		li.items = append(li.items[:index], li.items[index+1:]...)
	} else {
		l.RaiseError("Index out of range for listRemoveItem")
		return 0
	}

	li.model.RemoveItem(index)
	return 0
}

func listSelectedItem(l *lua.LState) int {
	li := CheckList(l)
	selected := li.model.SelectedItem()
	if selected == nil {
		return 0
	}

	for _, itemUD := range li.items {
		if itemUD.Value == selected {
			l.Push(itemUD)
			return 1
		}
	}

	return 0
}

func listMatchesForItem(l *lua.LState) int {
	li := CheckList(l)
	index := l.CheckInt(2)
	matches := li.model.MatchesForItem(index)
	matchesTable := l.NewTable()
	for _, match := range matches {
		matchesTable.Append(lua.LNumber(match))
	}
	l.Push(matchesTable)
	return 1
}

// Navigation

func listCursorUp(l *lua.LState) int {
	li := CheckList(l)
	li.model.CursorUp()
	return 0
}

func listCursorDown(l *lua.LState) int {
	li := CheckList(l)
	li.model.CursorDown()
	return 0
}

func listPrevPage(l *lua.LState) int {
	li := CheckList(l)
	li.model.PrevPage()
	return 0
}

func listNextPage(l *lua.LState) int {
	li := CheckList(l)
	li.model.NextPage()
	return 0
}

func listSelect(l *lua.LState) int {
	li := CheckList(l)
	index := l.CheckInt(2)
	li.model.Select(index)
	return 0
}

func listResetSelected(l *lua.LState) int {
	li := CheckList(l)
	li.model.ResetSelected()
	return 0
}

// Filtering

func listFilterState(l *lua.LState) int {
	li := CheckList(l)
	state := li.model.FilterState()
	l.Push(lua.LString(state.String()))
	return 1
}

func listFilterValue(l *lua.LState) int {
	li := CheckList(l)
	value := li.model.FilterValue()
	l.Push(lua.LString(value))
	return 1
}

func listSettingFilter(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LBool(li.model.SettingFilter()))
	return 1
}

func listIsFiltered(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LBool(li.model.IsFiltered()))
	return 1
}

func listResetFilter(l *lua.LState) int {
	li := CheckList(l)
	li.model.ResetFilter()
	return 0
}

// Display Control

func listSetWidth(l *lua.LState) int {
	li := CheckList(l)
	li.model.SetWidth(l.CheckInt(2))
	return 0
}

func listSetHeight(l *lua.LState) int {
	li := CheckList(l)
	li.model.SetHeight(l.CheckInt(2))
	return 0
}

func listSetShowTitle(l *lua.LState) int {
	li := CheckList(l)
	li.model.SetShowTitle(l.CheckBool(2))
	return 0
}

func listSetShowFilter(l *lua.LState) int {
	li := CheckList(l)
	li.model.SetShowFilter(l.CheckBool(2))
	return 0
}

func listSetShowStatusBar(l *lua.LState) int {
	li := CheckList(l)
	li.model.SetShowStatusBar(l.CheckBool(2))
	return 0
}

func listSetShowPagination(l *lua.LState) int {
	li := CheckList(l)
	li.model.SetShowPagination(l.CheckBool(2))
	return 0
}

func listSetShowHelp(l *lua.LState) int {
	li := CheckList(l)
	li.model.SetShowHelp(l.CheckBool(2))
	return 0
}

func listSetStatusBarItemName(l *lua.LState) int {
	li := CheckList(l)
	singular := l.CheckString(2)
	plural := l.CheckString(3)
	li.model.SetStatusBarItemName(singular, plural)
	return 0
}

func listDisableQuitKeybindings(l *lua.LState) int {
	li := CheckList(l)
	li.model.DisableQuitKeybindings()
	return 0
}

// Spinner

func listStartSpinner(l *lua.LState) int {
	li := CheckList(l)
	l.Push(protocol.WrapCommand(l, li.model.StartSpinner()))
	return 1
}

func listStopSpinner(l *lua.LState) int {
	li := CheckList(l)
	li.model.StopSpinner()
	return 0
}

func listToggleSpinner(l *lua.LState) int {
	li := CheckList(l)
	l.Push(protocol.WrapCommand(l, li.model.ToggleSpinner()))
	return 1
}

// Status

func listNewStatusMessage(l *lua.LState) int {
	li := CheckList(l)
	message := l.CheckString(2)
	l.Push(protocol.WrapCommand(l, li.model.NewStatusMessage(message)))
	return 1
}

func newList(l *lua.LState) int {
	if l.GetTop() < 1 {
		l.RaiseError("newList requires a configuration table")
		return 0
	}

	cfg := l.CheckTable(1)
	if cfg == nil {
		l.RaiseError("configuration must be a table")
		return 0
	}

	if err := validateListConfig(l, cfg); err != nil {
		l.RaiseError("invalid list configuration: %v", err)
		return 0
	}

	// Get and validate width/height with defaults
	width := 80  // default width
	height := 24 // default height

	if w := cfg.RawGetString("width"); w.Type() != lua.LTNil {
		if wNum, ok := w.(lua.LNumber); ok {
			if int(wNum) <= 0 {
				l.RaiseError("width must be positive")
				return 0
			}
			width = int(wNum)
		} else {
			l.RaiseError("width must be a number")
			return 0
		}
	}

	if h := cfg.RawGetString("height"); h.Type() != lua.LTNil {
		if hNum, ok := h.(lua.LNumber); ok {
			if int(hNum) <= 0 {
				l.RaiseError("height must be positive")
				return 0
			}
			height = int(hNum)
		} else {
			l.RaiseError("height must be a number")
			return 0
		}
	}

	// Get and validate items
	var items []list.Item
	if itemsVal := cfg.RawGetString("items"); itemsVal.Type() != lua.LTNil {
		itemsTable, ok := itemsVal.(*lua.LTable)
		if !ok {
			l.RaiseError("items must be a table")
			return 0
		}

		items = make([]list.Item, 0, itemsTable.Len())
		var itemError error
		itemsTable.ForEach(func(_ lua.LValue, v lua.LValue) {
			if itemError != nil {
				return
			}

			switch v.Type() {
			case lua.LTTable:
				item := &LuaItem{luaItem: v.(*lua.LTable), luaState: l}
				items = append(items, item)
			case lua.LTUserData:
				if itemUD, ok := v.(*lua.LUserData); ok {
					if item, ok := itemUD.Value.(list.Item); ok {
						items = append(items, item)
					} else {
						itemError = fmt.Errorf("invalid item type: expected list.Item interface")
					}
				}
			default:
				itemError = fmt.Errorf("invalid item type: expected table or userdata")
			}
		})

		if itemError != nil {
			l.RaiseError("error processing items: %v", itemError)
			return 0
		}
	}

	// Get and validate delegate
	var delegate list.ItemDelegate
	if delegateVal := cfg.RawGetString("delegate"); delegateVal.Type() != lua.LTNil {
		delegateTable, ok := delegateVal.(*lua.LTable)
		if !ok {
			l.RaiseError("delegate must be a table")
			return 0
		}
		delegate = &LuaDelegate{luaDelegate: delegateTable, luaState: l}
	} else {
		delegate = list.NewDefaultDelegate()
	}

	// Create the list model with validated parameters
	m := list.New(items, delegate, width, height)

	// Set optional title if provided
	if titleVal := cfg.RawGetString("title"); titleVal.Type() != lua.LTNil {
		if title, ok := titleVal.(lua.LString); ok {
			m.Title = string(title)
		} else {
			l.RaiseError("title must be a string")
			return 0
		}
	}

	// Create and set optional styles if provided
	if stylesVal := cfg.RawGetString("styles"); stylesVal.Type() != lua.LTNil {
		if stylesTable, ok := stylesVal.(*lua.LTable); ok {
			m.Styles = luaTableToStyles(l, stylesTable)
		} else {
			l.RaiseError("styles must be a table")
			return 0
		}
	}

	// Set optional additional settings
	if showTitle := cfg.RawGetString("show_title"); showTitle.Type() == lua.LTBool {
		m.SetShowTitle(bool(showTitle.(lua.LBool)))
	}

	if showFilter := cfg.RawGetString("show_filter"); showFilter.Type() == lua.LTBool {
		m.SetShowFilter(bool(showFilter.(lua.LBool)))
	}

	if showHelp := cfg.RawGetString("show_help"); showHelp.Type() == lua.LTBool {
		m.SetShowHelp(bool(showHelp.(lua.LBool)))
	}

	// Create and return the wrapped model with tracking for Lua items
	ud := l.NewUserData()
	ud.Value = &List{
		model:    m,
		items:    make([]*lua.LUserData, len(items)),
		luaState: l,
	}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.List"))
	l.Push(ud)
	return 1
}

// Helper function to safely convert Lua value to int with validation
func luaToInt(l *lua.LState, v lua.LValue, name string) (int, error) {
	if v.Type() != lua.LTNumber {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	num := int(v.(lua.LNumber))
	if num <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return num, nil
}

// CheckList checks if the first argument is a *List and returns it.
func CheckList(l *lua.LState) *List {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*List); ok {
		return v
	}
	l.ArgError(1, "btea.List expected")
	return nil
}

func validateListConfig(l *lua.LState, cfg *lua.LTable) error {
	// Validate required fields
	if cfg == nil {
		return fmt.Errorf("configuration table is required")
	}

	// Validate width/height
	if w := cfg.RawGetString("width"); w.Type() != lua.LTNil {
		if wNum, ok := w.(lua.LNumber); !ok || int(wNum) <= 0 {
			return fmt.Errorf("width must be a positive number")
		}
	}

	if h := cfg.RawGetString("height"); h.Type() != lua.LTNil {
		if hNum, ok := h.(lua.LNumber); !ok || int(hNum) <= 0 {
			return fmt.Errorf("height must be a positive number")
		}
	}

	// Validate items if present
	if itemsVal := cfg.RawGetString("items"); itemsVal.Type() != lua.LTNil {
		itemsTable, ok := itemsVal.(*lua.LTable)
		if !ok {
			return fmt.Errorf("items must be a table")
		}

		var err error
		itemsTable.ForEach(func(_ lua.LValue, v lua.LValue) {
			if err != nil {
				return
			}

			switch v.Type() {
			case lua.LTTable:
				// Validate required item methods
				item := v.(*lua.LTable)
				if item.RawGetString("filter_value").Type() == lua.LTNil {
					err = fmt.Errorf("items must implement filter_value method")
				}
			case lua.LTUserData:
				if itemUD, ok := v.(*lua.LUserData); !ok {
					err = fmt.Errorf("invalid item type")
				} else if _, ok := itemUD.Value.(list.Item); !ok {
					err = fmt.Errorf("items must implement list.Item interface")
				}
			default:
				err = fmt.Errorf("invalid item type: expected table or userdata")
			}
		})

		if err != nil {
			return fmt.Errorf("invalid items: %w", err)
		}
	}

	// Validate delegate if present
	if delegateVal := cfg.RawGetString("delegate"); delegateVal.Type() != lua.LTNil {
		delegateTable, ok := delegateVal.(*lua.LTable)
		if !ok {
			return fmt.Errorf("delegate must be a table")
		}

		// Validate required delegate methods
		required := []string{"height", "spacing", "render"}
		for _, method := range required {
			if delegateTable.RawGetString(method).Type() == lua.LTNil {
				return fmt.Errorf("delegate must implement %s method", method)
			}
		}
	}

	// Validate styles if present
	if stylesVal := cfg.RawGetString("styles"); stylesVal.Type() != lua.LTNil {
		if _, ok := stylesVal.(*lua.LTable); !ok {
			return fmt.Errorf("styles must be a table")
		}
	}

	return nil
}
