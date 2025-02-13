// file: table.go
package models

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/protocol"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea/render"
	"github.com/yuin/gopher-lua"
)

// Table wraps tablewidget.Model for Lua.
type Table struct {
	model table.Model
}

func (t *Table) Init() tea.Cmd {
	return nil
}

func (t *Table) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	t.model, cmd = t.model.Update(msg)
	return t, cmd
}

func (t *Table) View() string {
	return t.View()
}

// RegisterTable registers the table widget to Lua.
func RegisterTable(l *lua.LState, mod *lua.LTable) {
	// Spawn and register the table metatable.
	mt := l.NewTypeMetatable("btea.Table")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"update":       tableUpdate,
		"view":         tableView,
		"help_view":    tableHelpView,
		"set_rows":     tableSetRows,
		"get_rows":     tableGetRows,
		"set_columns":  tableSetColumns,
		"get_columns":  tableGetColumns,
		"selected_row": tableSelectedRow,
		"set_cursor":   tableSetCursor,
		"cursor":       tableCursor,
		"move_up":      tableMoveUp,
		"move_down":    tableMoveDown,
		"goto_top":     tableGotoTop,
		"goto_bottom":  tableGotoBottom,
		"focus":        tableFocus,
		"blur":         tableBlur,
		"from_values":  tableFromValues,
		"set_width":    tableSetWidth,
		"set_height":   tableSetHeight,
		"width":        tableWidth,
		"height":       tableHeight,
	}))
	// Register the constructor.
	l.SetField(mod, "table", l.NewFunction(newTable))
}

// newTable is the Lua constructor for a table.
// It accepts a Lua table of options.
// Supported options:
//   - cols: a list of column tables; each column should have keys "title" (string)
//     and "width" (number)
//   - rows: a list of row tables; each row is a list of strings
//   - width: number (viewport width)
//   - height: number (viewport height)
//   - focused: boolean
//   - styles: table with keys "header", "cell", "selected", each a btea.Style instance.
func newTable(l *lua.LState) int {
	opts := l.OptTable(1, l.NewTable())

	var options []table.Option

	// Process columns option.
	colsLV := opts.RawGetString("cols")
	if colsTbl, ok := colsLV.(*lua.LTable); ok {
		cols := luaTableToColumns(l, colsTbl)
		options = append(options, table.WithColumns(cols))
	}

	// Process rows option.
	rowsLV := opts.RawGetString("rows")
	if rowsTbl, ok := rowsLV.(*lua.LTable); ok {
		rows := luaTableToRows(l, rowsTbl)
		options = append(options, table.WithRows(rows))
	}

	// Process width option.
	if widthLV := opts.RawGetString("width"); widthLV != lua.LNil {
		width := int(lua.LVAsNumber(widthLV))
		options = append(options, table.WithWidth(width))
	}

	// Process height option.
	if heightLV := opts.RawGetString("height"); heightLV != lua.LNil {
		height := int(lua.LVAsNumber(heightLV))
		options = append(options, table.WithHeight(height))
	}

	// Process focused option.
	if focusLV := opts.RawGetString("focused"); focusLV != lua.LNil {
		focused := lua.LVAsBool(focusLV)
		options = append(options, table.WithFocused(focused))
	}

	if stylesLV := opts.RawGetString("styles"); stylesLV != lua.LNil {
		if stylesTbl, ok := stylesLV.(*lua.LTable); ok {
			s := luaTableToStyles(l, stylesTbl)
			options = append(options, table.WithStyles(s))
		}
	}

	// Spawn the table widget.
	model := table.New(options...)
	t := &Table{model: model}

	ud := l.NewUserData()
	ud.Value = t
	l.SetMetatable(ud, l.GetTypeMetatable("btea.Table"))
	l.Push(ud)
	return 1
}

// luaTableToStyles converts a Lua table with style fields to a table.Styles.
func luaTableToStyles(l *lua.LState, tbl *lua.LTable) table.Styles {
	// Launch with default styles.
	s := table.DefaultStyles()

	// Map the "header" style.
	if headerLV := tbl.RawGetString("header"); headerLV != lua.LNil {
		if ud, ok := headerLV.(*lua.LUserData); ok {
			if style, ok := ud.Value.(*render.Style); ok {
				s.Header = style.Style
			} else {
				l.RaiseError("expected btea.Style for header style")
			}
		}
	}

	// Map the "cell" style.
	if cellLV := tbl.RawGetString("cell"); cellLV != lua.LNil {
		if ud, ok := cellLV.(*lua.LUserData); ok {
			if style, ok := ud.Value.(*render.Style); ok {
				s.Cell = style.Style
			} else {
				l.RaiseError("expected btea.Style for cell style")
			}
		}
	}

	// Map the "selected" style.
	if selectedLV := tbl.RawGetString("selected"); selectedLV != lua.LNil {
		if ud, ok := selectedLV.(*lua.LUserData); ok {
			if style, ok := ud.Value.(*render.Style); ok {
				s.Selected = style.Style
			} else {
				l.RaiseError("expected btea.Style for selected style")
			}
		}
	}

	return s
}

// checkTable checks whether the first Lua argument is a *Table.
func checkTable(l *lua.LState) *Table {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Table); ok {
		return v
	}
	l.ArgError(1, "Table expected")
	return nil
}

// tableUpdate updates the table widget with a given message.
// Usage: tbl:update(msg)
func tableUpdate(l *lua.LState) int {
	tbl := checkTable(l)
	msgLV := l.CheckAny(2)
	msg, err := protocol.LuaToMsg(msgLV)
	if err != nil {
		l.RaiseError("failed to convert message: %v", err)
		return 0
	}
	var cmd tea.Cmd
	tbl.model, cmd = tbl.model.Update(msg)
	if cmd != nil {
		l.Push(protocol.WrapCommand(l, cmd))
		return 1
	}
	return 0
}

// tableView returns the rendered view of the table.
// Usage: local s = tbl:view()
func tableView(l *lua.LState) int {
	tbl := checkTable(l)
	l.Push(lua.LString(tbl.model.View()))
	return 1
}

// tableHelpView returns the help view for the table.
// Usage: local s = tbl:help_view()
func tableHelpView(l *lua.LState) int {
	tbl := checkTable(l)
	l.Push(lua.LString(tbl.model.HelpView()))
	return 1
}

// tableSetRows sets the table rows.
// Usage: tbl:set_rows(rows)
// where rows is a Lua table: a list of row tables (each row a list of strings).
func tableSetRows(l *lua.LState) int {
	tbl := checkTable(l)
	rowsTbl := l.CheckTable(2)
	rows := luaTableToRows(l, rowsTbl)
	tbl.model.SetRows(rows)
	return 0
}

// tableGetRows returns the current rows as a Lua table.
func tableGetRows(l *lua.LState) int {
	tbl := checkTable(l)
	rows := tbl.model.Rows()
	lt := l.NewTable()
	for i, row := range rows {
		lt.RawSetInt(i+1, rowToLuaTable(l, row))
	}
	l.Push(lt)
	return 1
}

// tableSetColumns sets the table columns.
// Usage: tbl:set_columns(cols)
// where cols is a Lua table: a list of column tables { title = "foo", width = 10 }
func tableSetColumns(l *lua.LState) int {
	tbl := checkTable(l)
	colsTbl := l.CheckTable(2)
	cols := luaTableToColumns(l, colsTbl)
	tbl.model.SetColumns(cols)
	return 0
}

// tableGetColumns returns the current columns as a Lua table.
func tableGetColumns(l *lua.LState) int {
	tbl := checkTable(l)
	cols := tbl.model.Columns()
	lt := l.NewTable()
	for i, col := range cols {
		colTbl := l.NewTable()
		l.SetField(colTbl, "title", lua.LString(col.Title))
		l.SetField(colTbl, "width", lua.LNumber(col.Width))
		lt.RawSetInt(i+1, colTbl)
	}
	l.Push(lt)
	return 1
}

// tableSelectedRow returns the selected row as a Lua table.
func tableSelectedRow(l *lua.LState) int {
	tbl := checkTable(l)
	row := tbl.model.SelectedRow()
	if row == nil {
		l.Push(lua.LNil)
		return 1
	}
	l.Push(rowToLuaTable(l, row))
	return 1
}

// tableSetCursor sets the selected row index (0-indexed).
// Usage: tbl:set_cursor(n)
func tableSetCursor(l *lua.LState) int {
	tbl := checkTable(l)
	n := l.CheckInt(2)
	tbl.model.SetCursor(n)
	return 0
}

// tableCursor returns the current cursor (selected row) index.
func tableCursor(l *lua.LState) int {
	tbl := checkTable(l)
	l.Push(lua.LNumber(tbl.model.Cursor()))
	return 1
}

// tableMoveUp moves the selection up by a given number of rows.
// Usage: tbl:move_up(n)
func tableMoveUp(l *lua.LState) int {
	tbl := checkTable(l)
	n := l.OptInt(2, 1)
	tbl.model.MoveUp(n)
	return 0
}

// tableMoveDown moves the selection down by a given number of rows.
// Usage: tbl:move_down(n)
func tableMoveDown(l *lua.LState) int {
	tbl := checkTable(l)
	n := l.OptInt(2, 1)
	tbl.model.MoveDown(n)
	return 0
}

// tableGotoTop moves the selection to the first row.
func tableGotoTop(l *lua.LState) int {
	tbl := checkTable(l)
	tbl.model.GotoTop()
	return 0
}

// tableGotoBottom moves the selection to the last row.
func tableGotoBottom(l *lua.LState) int {
	tbl := checkTable(l)
	tbl.model.GotoBottom()
	return 0
}

// tableFocus focuses the table.
func tableFocus(l *lua.LState) int {
	tbl := checkTable(l)
	tbl.model.Focus()
	return 0
}

// tableBlur blurs the table.
func tableBlur(l *lua.LState) int {
	tbl := checkTable(l)
	tbl.model.Blur()
	return 0
}

// tableFromValues creates rows from a string value using a separator.
// Usage: tbl:from_values(value, separator)
func tableFromValues(l *lua.LState) int {
	tbl := checkTable(l)
	value := l.CheckString(2)
	separator := l.OptString(3, ",")
	tbl.model.FromValues(value, separator)
	return 0
}

// tableSetWidth sets the viewport width.
// Usage: tbl:set_width(n)
func tableSetWidth(l *lua.LState) int {
	tbl := checkTable(l)
	w := l.CheckInt(2)
	tbl.model.SetWidth(w)
	return 0
}

// tableSetHeight sets the viewport height.
// Usage: tbl:set_height(n)
func tableSetHeight(l *lua.LState) int {
	tbl := checkTable(l)
	h := l.CheckInt(2)
	tbl.model.SetHeight(h)
	return 0
}

// tableWidth returns the current viewport width.
func tableWidth(l *lua.LState) int {
	tbl := checkTable(l)
	l.Push(lua.LNumber(tbl.model.Width()))
	return 1
}

// tableHeight returns the current viewport height.
func tableHeight(l *lua.LState) int {
	tbl := checkTable(l)
	l.Push(lua.LNumber(tbl.model.Height()))
	return 1
}

// --- Helper conversion functions ---

// luaTableToColumns converts a Lua table (list of column tables)
// to a slice of tablewidget.Column.
func luaTableToColumns(l *lua.LState, tbl *lua.LTable) []table.Column {
	var cols []table.Column
	tbl.ForEach(func(key, value lua.LValue) {
		if colTbl, ok := value.(*lua.LTable); ok {
			title := ""
			width := 0
			if lv := colTbl.RawGetString("title"); lv != lua.LNil {
				title = lua.LVAsString(lv)
			}
			if lv := colTbl.RawGetString("width"); lv != lua.LNil {
				width = int(lua.LVAsNumber(lv))
			}
			cols = append(cols, table.Column{Title: title, Width: width})
		}
	})
	return cols
}

// luaTableToRows converts a Lua table (list of row tables) to a slice of tablewidget.Row.
func luaTableToRows(l *lua.LState, tbl *lua.LTable) []table.Row {
	var rows []table.Row
	tbl.ForEach(func(key, value lua.LValue) {
		// Each row is expected to be a Lua table (array of strings)
		if rowTbl, ok := value.(*lua.LTable); ok {
			var row table.Row
			rowTbl.ForEach(func(_, cell lua.LValue) {
				row = append(row, lua.LVAsString(cell))
			})
			rows = append(rows, row)
		}
	})
	return rows
}

// rowToLuaTable converts a tablewidget.Row (which is []string)
// to a Lua table.
func rowToLuaTable(l *lua.LState, row table.Row) *lua.LTable {
	lt := l.NewTable()
	for i, cell := range row {
		lt.RawSetInt(i+1, lua.LString(cell))
	}
	return lt
}
