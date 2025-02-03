package list

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	lua "github.com/yuin/gopher-lua"
)

// List wraps list.Model for Lua
type List struct {
	model    list.Model
	delegate *lua.LFunction // Delegate function reference
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
	list := checkList(l)
	msg, err := protocol.LuaToMsg(l.Get(2))
	if err != nil {
		l.RaiseError("Error converting Lua value to message: %v", err)
	}

	newModel, cmd := list.model.Update(msg)
	list.model = newModel

	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}

	return 0
}

func listView(l *lua.LState) int {
	list := checkList(l)
	l.Push(lua.LString(list.model.View()))
	return 1
}

// Item Management

func listItems(l *lua.LState) int {
	list := checkList(l)
	itemsTable := l.NewTable()
	for _, itemUD := range list.items {
		itemsTable.Append(itemUD)
	}
	l.Push(itemsTable)
	return 1
}

func listSetItems(l *lua.LState) int {
	list := checkList(l)
	itemsTable := l.CheckTable(2)
	var items []list.Item
	list.items = make([]*lua.LUserData, 0) // Clear existing items
	itemsTable.ForEach(func(_ lua.LValue, v lua.LValue) {
		if item, ok := v.(*lua.LTable); ok {
			itemUD := l.NewUserData()
			itemUD.Value = &LuaItem{luaItem: item, luaState: l}
			l.SetMetatable(itemUD, l.GetTypeMetatable("btea.Item"))
			items = append(items, itemUD.Value.(list.Item))

			list.items = append(list.items, itemUD)
		} else if item, ok := v.(*lua.LUserData); ok {
			items = append(items, item.Value.(list.Item))

			list.items = append(list.items, item)
		}
	})

	cmd := list.model.SetItems(items)
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func listSetItem(l *lua.LState) int {
	list := checkList(l)
	index := l.CheckInt(2)
	itemTable := l.CheckTable(3)

	itemUD := l.NewUserData()
	itemUD.Value = &LuaItem{luaItem: itemTable, luaState: l}
	l.SetMetatable(itemUD, l.GetTypeMetatable("btea.Item"))

	// Check if the index is within the range of the items slice
	if index >= 0 && index < len(list.items) {
		// Update the item in the list.items slice
		list.items[index] = itemUD
	} else {
		l.RaiseError("Index out of range for listSetItem")
		return 0 // Return early if the index is out of range
	}

	cmd := list.model.SetItem(index, itemUD.Value.(list.Item))
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func listInsertItem(l *lua.LState) int {
	list := checkList(l)
	index := l.CheckInt(2)
	itemTable := l.CheckTable(3)

	itemUD := l.NewUserData()
	itemUD.Value = &LuaItem{luaItem: itemTable, luaState: l}
	l.SetMetatable(itemUD, l.GetTypeMetatable("btea.Item"))

	// Adjust the index for 0-based indexing in Go
	goIndex := index

	// Insert the item into the list.items slice
	if goIndex >= 0 && goIndex <= len(list.items) {
		list.items = append(list.items[:goIndex], append([]*lua.LUserData{itemUD}, list.items[goIndex:]...)...)
	} else {
		l.RaiseError("Index out of range for listInsertItem")
		return 0 // Return early if the index is out of range
	}

	// Call the underlying Go method to insert the item
	cmd := list.model.InsertItem(goIndex, itemUD.Value.(list.Item))
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

func listRemoveItem(l *lua.LState) int {
	list := checkList(l)
	index := l.CheckInt(2)
	// Adjust the index for 0-based indexing in Go
	goIndex := index

	// Remove the item from the list.items slice
	if goIndex >= 0 && goIndex < len(list.items) {
		list.items = append(list.items[:goIndex], list.items[goIndex+1:]...)
	} else {
		l.RaiseError("Index out of range for listRemoveItem")
		return 0 // Return early if the index is out of range
	}

	// Call the underlying Go method to remove the item
	list.model.RemoveItem(goIndex)
	return 0
}

func listSelectedItem(l *lua.LState) int {
	list := checkList(l)
	selected := list.model.SelectedItem()
	if selected == nil {
		return 0
	}

	// Find the corresponding Lua item in list.items
	for _, itemUD := range list.items {
		if itemUD.Value == selected {
			l.Push(itemUD)
			return 1
		}
	}

	return 0
}

func listMatchesForItem(l *lua.LState) int {
	list := checkList(l)
	index := l.CheckInt(2)
	matches := list.model.MatchesForItem(index)
	matchesTable := l.NewTable()
	for _, match := range matches {
		matchesTable.Append(lua.LNumber(match))
	}
	l.Push(matchesTable)
	return 1
}

// Navigation

func listCursorUp(l *lua.LState) int {
	list := checkList(l)
	list.model.CursorUp()
	return 0
}

func listCursorDown(l *lua.LState) int {
	list := checkList(l)
	list.model.CursorDown()
	return 0
}

func listPrevPage(l *lua.LState) int {
	list := checkList(l)
	list.model.PrevPage()
	return 0
}

func listNextPage(l *lua.LState) int {
	list := checkList(l)
	list.model.NextPage()
	return 0
}

func listSelect(l *lua.LState) int {
	list := checkList(l)
	index := l.CheckInt(2)
	list.model.Select(index)
	return 0
}

func listResetSelected(l *lua.LState) int {
	list := checkList(l)
	list.model.ResetSelected()
	return 0
}

// Filtering

func listFilterState(l *lua.LState) int {
	list := checkList(l)
	state := list.model.FilterState()
	l.Push(lua.LString(state.String()))
	return 1
}

func listFilterValue(l *lua.LState) int {
	list := checkList(l)
	value := list.model.FilterValue()
	l.Push(lua.LString(value))
	return 1
}

func listSettingFilter(l *lua.LState) int {
	list := checkList(l)
	l.Push(lua.LBool(list.model.SettingFilter()))
	return 1
}

func listIsFiltered(l *lua.LState) int {
	list := checkList(l)
	l.Push(lua.LBool(list.model.IsFiltered()))
	return 1
}

func listResetFilter(l *lua.LState) int {
	list := checkList(l)
	list.model.ResetFilter()
	return 0
}

// Display Control

func listSetWidth(l *lua.LState) int {
	list := checkList(l)
	list.model.SetWidth(l.CheckInt(2))
	return 0
}

func listSetHeight(l *lua.LState) int {
	list := checkList(l)
	list.model.SetHeight(l.CheckInt(2))
	return 0
}

func listSetShowTitle(l *lua.LState) int {
	list := checkList(l)
	list.model.SetShowTitle(l.CheckBool(2))
	return 0
}

func listSetShowFilter(l *lua.LState) int {
	list := checkList(l)
	list.model.SetShowFilter(l.CheckBool(2))
	return 0
}

func listSetShowStatusBar(l *lua.LState) int {
	list := checkList(l)
	list.model.SetShowStatusBar(l.CheckBool(2))
	return 0
}

func listSetShowPagination(l *lua.LState) int {
	list := checkList(l)
	list.model.SetShowPagination(l.CheckBool(2))
	return 0
}

func listSetShowHelp(l *lua.LState) int {
	list := checkList(l)
	list.model.SetShowHelp(l.CheckBool(2))
	return 0
}

func listSetStatusBarItemName(l *lua.LState) int {
	list := checkList(l)
	singular := l.CheckString(2)
	plural := l.CheckString(3)
	list.model.SetStatusBarItemName(singular, plural)
	return 0
}

func listDisableQuitKeybindings(l *lua.LState) int {
	list := checkList(l)
	list.model.DisableQuitKeybindings()
	return 0
}

// Spinner

func listStartSpinner(l *lua.LState) int {
	list := checkList(l)
	l.Push(protocol.WrapCommand(l, list.model.StartSpinner()))
	return 1
}

func listStopSpinner(l *lua.LState) int {
	list := checkList(l)
	list.model.StopSpinner()
	return 0
}

func listToggleSpinner(l *lua.LState) int {
	list := checkList(l)
	l.Push(protocol.WrapCommand(l, list.model.ToggleSpinner()))
	return 1
}

// Status

func listNewStatusMessage(l *lua.LState) int {
	list := checkList(l)
	message := l.CheckString(2)
	l.Push(protocol.WrapCommand(l, list.model.NewStatusMessage(message)))
	return 1
}

func newList(l *lua.LState) int {
	cfg := l.CheckTable(1)
	width := int(cfg.RawGetString("width").(lua.LNumber))
	height := int(cfg.RawGetString("height").(lua.LNumber))

	// Get items
	var items []list.Item
	itemsTable := cfg.RawGetString("items").(*lua.LTable)
	itemsTable.ForEach(func(_ lua.LValue, v lua.LValue) {
		if item, ok := v.(*lua.LTable); ok {
			items = append(items, &LuaItem{luaItem: item, luaState: l})
		}
	})

	// Get delegate
	var delegate list.ItemDelegate
	lv := cfg.RawGetString("delegate")
	if lv.Type() != lua.LTNil {
		luaDelegate, ok := lv.(*lua.LFunction)
		if !ok {
			l.RaiseError("delegate must be a function")
		}
		delegate = &LuaDelegate{luaDelegate: luaDelegate, luaState: l}
	} else {
		delegate = list.NewDefaultDelegate()
	}

	// Create the list model
	m := list.New(items, delegate, width, height)

	// Set title
	if title := cfg.RawGetString("title").(lua.LString); title != "" {
		m.Title = string(title)
	}

	// Configuration Options
	if lv := cfg.RawGetString("show_title"); lv != lua.LNil {
		m.SetShowTitle(lua.LVAsBool(lv))
	}
	if lv := cfg.RawGetString("show_filter"); lv != lua.LNil {
		m.SetShowFilter(lua.LVAsBool(lv))
	}
	if lv := cfg.RawGetString("show_status_bar"); lv != lua.LNil {
		m.SetShowStatusBar(lua.LVAsBool(lv))
	}
	if lv := cfg.RawGetString("show_pagination"); lv != lua.LNil {
		m.SetShowPagination(lua.LVAsBool(lv))
	}
	if lv := cfg.RawGetString("show_help"); lv != lua.LNil {
		m.SetShowHelp(lua.LVAsBool(lv))
	}
	if lv := cfg.RawGetString("filtering_enabled"); lv != lua.LNil {
		m.SetFilteringEnabled(lua.LVAsBool(lv))
	}
	if lv := cfg.RawGetString("infinite_scrolling"); lv != lua.LNil {
		m.InfiniteScrolling = lua.LVAsBool(lv)
	}

	// Status Bar Customization
	if itemNames := cfg.RawGetString("item_name"); itemNames != lua.LNil {
		if itemNamesTbl, ok := itemNames.(*lua.LTable); ok {
			singular := lua.LVAsString(itemNamesTbl.RawGetString("singular"))
			plural := lua.LVAsString(itemNamesTbl.RawGetString("plural"))
			m.SetStatusBarItemName(singular, plural)
		}
	}

	// Styling
	if styles := cfg.RawGetString("styles"); styles != lua.LNil {
		if stylesTbl, ok := styles.(*lua.LTable); ok {
			m.Styles = luaTableToStyles(l, stylesTbl)
		}
	}

	// Key bindings
	if keys := cfg.RawGetString("keys"); keys != lua.LNil {
		if keysTbl, ok := keys.(*lua.LTable); ok {
			m.KeyMap = luaTableToKeyMap(l, keysTbl)
		}
	}

	// Wrap the model for Lua
	ud := l.NewUserData()
	ud.Value = &List{
		model:    m,
		items:    make([]*lua.LUserData, 0),
		luaState: l,
	}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.List"))
	l.Push(ud)
	return 1
}

func checkList(l *lua.LState) *List {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*List); ok {
		return v
	}
	l.ArgError(1, "btea.List expected")
	return nil
}
