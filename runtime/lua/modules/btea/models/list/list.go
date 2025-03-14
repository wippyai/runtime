package list

import (
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
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
		"cursor":         listCursor,
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
		"set_key_map":              listSetKeyMap,

		"filtering_enabled":     listFilteringEnabled,
		"set_filtering_enabled": listSetFilteringEnabled,
		"show_title":            listShowTitle,
		"show_filter":           listShowFilter,
		"show_status_bar":       listShowStatusBar,
		"show_pagination":       listShowPagination,
		"show_help":             listShowHelp,
		"width":                 listWidth,
		"height":                listHeight,

		// Spinner
		"start_spinner":  listStartSpinner,
		"stop_spinner":   listStopSpinner,
		"toggle_spinner": listToggleSpinner,

		// Status
		"new_status_message": listNewStatusMessage,
	}))

	l.SetField(mod, "list", l.NewFunction(newList))
}

// Core Method

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

	for _, item := range li.model.Items() {
		if luaItem, ok := item.(*LuaItem); ok {
			itemsTable.Append(luaItem.value)
		}
	}
	l.Push(itemsTable)
	return 1
}

func listSetItems(l *lua.LState) int {
	li := CheckList(l)
	itemsTable := l.CheckTable(2)
	var items []list.Item

	itemsTable.ForEach(func(_ lua.LValue, v lua.LValue) {
		switch v.Type() {
		case lua.LTTable:
			itemUD := l.NewUserData()
			itemUD.Value = &LuaItem{value: v, luaState: l}
			l.SetMetatable(itemUD, l.GetTypeMetatable("btea.Item"))
			items = append(items, itemUD.Value.(list.Item))
		case lua.LTUserData:
			if itemUD, ok := v.(*lua.LUserData); ok {
				if item, ok := itemUD.Value.(*LuaItem); ok {
					items = append(items, item)
				}
			}
		default:
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
	itemUD.Value = &LuaItem{value: itemTable, luaState: l}
	l.SetMetatable(itemUD, l.GetTypeMetatable("btea.Item"))

	if index < 0 || index >= len(li.model.Items()) {
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
	itemUD.Value = &LuaItem{value: itemTable, luaState: l}
	l.SetMetatable(itemUD, l.GetTypeMetatable("btea.Item"))

	if index < 0 || index > len(li.model.Items()) {
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

	if index < 0 || index >= len(li.model.Items()) {
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

	if luaItem, ok := selected.(*LuaItem); ok {
		l.Push(luaItem.value)
		return 1
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

func listCursor(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LNumber(li.model.Cursor()))
	return 1
}

func listCursorUp(l *lua.LState) int {
	li := CheckList(l)
	if li.model.Cursor() > 0 {
		li.model.CursorUp()
	}
	return 0
}

func listCursorDown(l *lua.LState) int {
	li := CheckList(l)
	if li.model.Cursor() < len(li.model.Items())-1 {
		li.model.CursorDown()
	}
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

func listSetFilteringEnabled(l *lua.LState) int {
	li := CheckList(l)
	enabled := l.CheckBool(2)
	li.model.SetFilteringEnabled(enabled)
	return 0
}

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

func listFilteringEnabled(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LBool(li.model.FilteringEnabled()))
	return 1
}

func listShowTitle(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LBool(li.model.ShowTitle()))
	return 1
}

func listShowFilter(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LBool(li.model.ShowFilter()))
	return 1
}

func listShowStatusBar(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LBool(li.model.ShowStatusBar()))
	return 1
}

func listShowPagination(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LBool(li.model.ShowPagination()))
	return 1
}

func listShowHelp(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LBool(li.model.ShowHelp()))
	return 1
}

func listWidth(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LNumber(li.model.Width()))
	return 1
}

func listHeight(l *lua.LState) int {
	li := CheckList(l)
	l.Push(lua.LNumber(li.model.Height()))
	return 1
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

func listSetKeyMap(l *lua.LState) int {
	li := CheckList(l)
	keyMapTable := l.CheckTable(2)

	li.model.KeyMap = luaTableToKeyMap(l, keyMapTable)
	return 0
}

// Status

func listNewStatusMessage(l *lua.LState) int {
	li := CheckList(l)
	message := l.CheckString(2)
	l.Push(protocol.WrapCommand(l, li.model.NewStatusMessage(message)))
	return 1
}

func newList(l *lua.LState) int {
	cfg := l.CheckTable(1)
	if cfg == nil {
		l.RaiseError("configuration table is required")
		return 0
	}

	// Spawn required width/height with defaults
	width := getIntOrDefault(l, cfg, "width", 80)
	height := getIntOrDefault(l, cfg, "height", 24)

	// Spawn base model
	model := list.New([]list.Item{}, list.NewDefaultDelegate(), width, height)

	// Apply basic settings first
	model.Title = getStringOrDefault(l, cfg, "title", "List")
	model.InfiniteScrolling = getBoolOrDefault(l, cfg, "infinite_scrolling", false)

	// Apply visibility settings
	if v := cfg.RawGetString("show_title"); v.Type() != lua.LTNil {
		model.SetShowTitle(lua.LVAsBool(v))
	}
	if v := cfg.RawGetString("show_filter"); v.Type() != lua.LTNil {
		model.SetShowFilter(lua.LVAsBool(v))
	}
	if v := cfg.RawGetString("show_status_bar"); v.Type() != lua.LTNil {
		model.SetShowStatusBar(lua.LVAsBool(v))
	}
	if v := cfg.RawGetString("show_pagination"); v.Type() != lua.LTNil {
		model.SetShowPagination(lua.LVAsBool(v))
	}
	if v := cfg.RawGetString("show_help"); v.Type() != lua.LTNil {
		model.SetShowHelp(lua.LVAsBool(v))
	}

	// Apply filtering settings
	if v := cfg.RawGetString("filtering_enabled"); v.Type() != lua.LTNil {
		model.SetFilteringEnabled(lua.LVAsBool(v))
	}

	// Set status bar item names if provided
	if v := cfg.RawGetString("item_name"); v.Type() != lua.LTNil {
		singular := lua.LVAsString(v)
		plural := singular + "s" // Default plural
		if p := cfg.RawGetString("item_name_plural"); p.Type() != lua.LTNil {
			plural = lua.LVAsString(p)
		}
		model.SetStatusBarItemName(singular, plural)
	}

	// Set status message lifetime if provided
	if v := cfg.RawGetString("status_message_lifetime"); v.Type() == lua.LTNumber {
		duration := time.Duration(float64(v.(lua.LNumber)) * float64(time.Second))
		model.StatusMessageLifetime = duration
	}

	// Set spinner if provided
	if spinnerVal := cfg.RawGetString("spinner"); spinnerVal.Type() != lua.LTNil {
		if spinnerTable, ok := spinnerVal.(*lua.LTable); ok {
			if styleVal := spinnerTable.RawGetString("style"); styleVal.Type() == lua.LTUserData {
				if style, ok := getStyleFromUserData(l, styleVal); ok {
					model.Styles.Spinner = style
				}
			}
			if typeVal := spinnerTable.RawGetString("type"); typeVal.Type() == lua.LTString {
				model.SetSpinner(spinner.Spinner{
					Frames: []string{lua.LVAsString(typeVal)},
				})
			}
		}
	}

	// Set delegate if provided
	if delegateVal := cfg.RawGetString("delegate"); delegateVal.Type() != lua.LTNil {
		if delegateTable, ok := delegateVal.(*lua.LTable); ok {
			model.SetDelegate(&LuaDelegate{luaDelegate: delegateTable, luaState: l})
		}
	}

	// Set key map if provided
	if keysVal := cfg.RawGetString("keys"); keysVal.Type() != lua.LTNil {
		if keysTable, ok := keysVal.(*lua.LTable); ok {
			model.KeyMap = luaTableToKeyMap(l, keysTable)
		}
	}

	// Set styles if provided
	if stylesVal := cfg.RawGetString("styles"); stylesVal.Type() != lua.LTNil {
		if stylesTable, ok := stylesVal.(*lua.LTable); ok {
			model.Styles = luaTableToStyles(l, stylesTable)
		}
	}

	// Set filter function if provided
	if filterVal := cfg.RawGetString("filter"); filterVal.Type() == lua.LTFunction {
		model.Filter = func(term string, targets []string) []list.Rank {
			if err := l.CallByParam(lua.P{
				Fn:      filterVal.(*lua.LFunction),
				NRet:    1,
				Protect: true,
			}, lua.LString(term), stringsToLuaTable(l, targets)); err != nil {
				l.RaiseError("error calling filter function: %v", err)
				return nil
			}
			ret := l.Get(-1)
			l.Pop(1)

			ranks := make([]list.Rank, 0)
			if t, ok := ret.(*lua.LTable); ok {
				t.ForEach(func(_, v lua.LValue) {
					if rankTable, ok := v.(*lua.LTable); ok {
						rank := list.Rank{
							Index: int(lua.LVAsNumber(rankTable.RawGetString("index"))),
						}

						if matchesVal := rankTable.RawGetString("matches"); matchesVal.Type() == lua.LTTable {
							matchesTable := matchesVal.(*lua.LTable)
							rank.MatchedIndexes = make([]int, matchesTable.Len())
							matchesTable.ForEach(func(i, v lua.LValue) {
								rank.MatchedIndexes[int(i.(lua.LNumber))-1] = int(v.(lua.LNumber))
							})
						}

						ranks = append(ranks, rank)
					}
				})
			}
			return ranks
		}
	}

	// Set items if provided (do this last as it may trigger filtering)
	if itemsVal := cfg.RawGetString("items"); itemsVal.Type() != lua.LTNil {
		itemsTable, ok := itemsVal.(*lua.LTable)
		if !ok {
			l.RaiseError("items must be a table")
			return 0
		}

		items := make([]list.Item, 0, itemsTable.Len())
		itemsTable.ForEach(func(_, v lua.LValue) {
			switch v.Type() {
			case lua.LTTable:
				item := &LuaItem{value: v.(*lua.LTable), luaState: l}
				items = append(items, item)
			case lua.LTUserData:
				if itemUD, ok := v.(*lua.LUserData); ok {
					if item, ok := itemUD.Value.(list.Item); ok {
						items = append(items, item)
					}
				}
			default:
			}
		})

		model.SetItems(items)
	}

	// Spawn and return the wrapped model
	ud := l.NewUserData()
	ud.Value = &List{
		model:    model,
		luaState: l,
	}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.List"))
	l.Push(ud)
	return 1
}

// Helper functions for configuration
func getIntOrDefault(l *lua.LState, t *lua.LTable, key string, def int) int {
	if v := t.RawGetString(key); v.Type() == lua.LTNumber {
		return int(lua.LVAsNumber(v))
	}
	return def
}

func getStringOrDefault(l *lua.LState, t *lua.LTable, key string, def string) string {
	if v := t.RawGetString(key); v.Type() == lua.LTString {
		return lua.LVAsString(v)
	}
	return def
}

func getBoolOrDefault(l *lua.LState, t *lua.LTable, key string, def bool) bool {
	if v := t.RawGetString(key); v.Type() == lua.LTBool {
		return lua.LVAsBool(v)
	}
	return def
}

func stringsToLuaTable(l *lua.LState, strings []string) *lua.LTable {
	t := l.NewTable()
	for _, s := range strings {
		t.Append(lua.LString(s))
	}
	return t
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
