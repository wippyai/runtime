package btea

import (
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	lua "github.com/yuin/gopher-lua"
)

// Table wraps table.Model for Lua
type Table struct {
	model table.Model
}

func RegisterTable(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("btea.Table")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"update":       tableUpdate,
		"view":         tableView,
		"set_rows":     tableSetRows,
		"set_columns":  tableSetColumns,
		"set_styles":   tableSetStyles,
		"set_focused":  tableSetFocused,
		"focused":      tableFocused,
		"blur":         tableBlur,
		"focus":        tableFocus,
		"selected_row": tableSelectedRow,
		"move_up":      tableMoveUp,
		"move_down":    tableMoveDown,
		"goto_top":     tableGotoTop,
		"goto_bottom":  tableGotoBottom,
		"set_cursor":   tableSetCursor,
		"cursor":       tableGetCursor,
		"set_width":    tableSetWidth,
		"set_height":   tableSetHeight,
		"width":        tableWidth,
		"height":       tableHeight,
		"help_view":    tableHelpView,
	}))

	// Register constructor
	l.SetField(mod, "new_table", l.NewFunction(newTable))
}

func newTable(l *lua.LState) int {
	// Get columns from first argument (required)
	columnsTable := l.CheckTable(1)

	var columns []table.Column
	columnsTable.ForEach(func(_, value lua.LValue) {
		if colTable, ok := value.(*lua.LTable); ok {
			title := colTable.RawGetString("title").String()
			width := colTable.RawGetString("width").(lua.LNumber)
			columns = append(columns, table.Column{Title: title, Width: int(width)})
		}
	})

	// Parse options from second argument (optional)
	var opts []table.Option
	if l.GetTop() > 1 {
		optsTable := l.CheckTable(2)

		// Height
		if height := optsTable.RawGetString("height"); height != lua.LNil {
			opts = append(opts, table.WithHeight(int(height.(lua.LNumber))))
		}

		// Width
		if width := optsTable.RawGetString("width"); width != lua.LNil {
			opts = append(opts, table.WithWidth(int(width.(lua.LNumber))))
		}

		// Focused
		if focused := optsTable.RawGetString("focused"); focused != lua.LNil {
			opts = append(opts, table.WithFocused(bool(focused.(lua.LBool))))
		}
	}

	// Add columns
	opts = append(opts, table.WithColumns(columns))

	// Create model with options
	model := table.New(opts...)

	// Create userdata
	ud := l.NewUserData()
	ud.Value = &Table{model: model}
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Table"))
	l.Push(ud)
	return 1
}

// Helper methods
func checkTable(l *lua.LState) *Table {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Table); ok {
		return v
	}
	l.ArgError(1, "table expected")
	return nil
}

// Table methods implementation
// Only showing a few key methods, the pattern is similar for others

func tableSetRows(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}

	rowsTable := l.CheckTable(2)
	var rows []table.Row
	rowsTable.ForEach(func(_, value lua.LValue) {
		if rowTable, ok := value.(*lua.LTable); ok {
			var row table.Row
			rowTable.ForEach(func(_, cell lua.LValue) {
				row = append(row, cell.String())
			})
			rows = append(rows, row)
		}
	})

	t.model.SetRows(rows)
	t.model.UpdateViewport()
	return 0
}

func tableMoveUp(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	n := l.OptInt(2, 1) // Default to 1 if not specified
	t.model.MoveUp(n)
	return 0
}

func tableMoveDown(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	n := l.OptInt(2, 1) // Default to 1 if not specified
	t.model.MoveDown(n)
	return 0
}

func tableSetStyles(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}

	stylesTable := l.CheckTable(2)
	styles := table.DefaultStyles()

	if s := stylesTable.RawGetString("selected"); s != lua.LNil {
		if style, ok := s.(*lua.LUserData).Value.(*Style); ok {
			styles.Selected = style.style
		}
	}
	if s := stylesTable.RawGetString("header"); s != lua.LNil {
		if style, ok := s.(*lua.LUserData).Value.(*Style); ok {
			styles.Header = style.style
		}
	}
	if s := stylesTable.RawGetString("cell"); s != lua.LNil {
		if style, ok := s.(*lua.LUserData).Value.(*Style); ok {
			styles.Cell = style.style
		}
	}

	t.model.SetStyles(styles)
	t.model.UpdateViewport()
	return 0
}

func tableUpdate(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}

	msgValue := l.CheckAny(2)
	teaMsg, err := LuaToMsg(msgValue)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}

	var cmd tea.Cmd
	t.model, cmd = t.model.Update(teaMsg)
	if cmd != nil {
		// Handle command if needed
	}
	return 0
}

func tableView(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	l.Push(lua.LString(t.model.View()))
	return 1
}

func tableSetColumns(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}

	columnsTable := l.CheckTable(2)
	var columns []table.Column
	columnsTable.ForEach(func(_, value lua.LValue) {
		if colTable, ok := value.(*lua.LTable); ok {
			title := colTable.RawGetString("title").String()
			width := colTable.RawGetString("width").(lua.LNumber)
			columns = append(columns, table.Column{Title: title, Width: int(width)})
		}
	})

	t.model.SetColumns(columns)
	return 0
}

func tableSetFocused(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	focused := l.CheckBool(2)
	if focused {
		t.model.Focus()
	} else {
		t.model.Blur()
	}
	return 0
}

func tableFocused(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	l.Push(lua.LBool(t.model.Focused()))
	return 1
}

func tableBlur(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	t.model.Blur()
	return 0
}

func tableFocus(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	t.model.Focus()
	return 0
}

func tableSelectedRow(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}

	row := t.model.SelectedRow()
	tbl := l.NewTable()
	for _, cell := range row {
		tbl.Append(lua.LString(cell))
	}

	l.Push(tbl)
	return 1
}

func tableGotoTop(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	t.model.GotoTop()
	return 0
}

func tableGotoBottom(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	t.model.GotoBottom()
	return 0
}

func tableSetCursor(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	n := l.CheckInt(2)
	t.model.SetCursor(n)
	return 0
}

func tableGetCursor(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	l.Push(lua.LNumber(t.model.Cursor()))
	return 1
}

func tableSetWidth(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	width := l.CheckInt(2)
	t.model.SetWidth(width)
	return 0
}

func tableSetHeight(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	height := l.CheckInt(2)
	t.model.SetHeight(height)
	return 0
}

func tableWidth(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	l.Push(lua.LNumber(t.model.Width()))
	return 1
}

func tableHeight(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	l.Push(lua.LNumber(t.model.Height()))
	return 1
}

func tableHelpView(l *lua.LState) int {
	t := checkTable(l)
	if t == nil {
		return 0
	}
	l.Push(lua.LString(t.model.HelpView()))
	return 1
}
